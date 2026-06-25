import { computeLayout, quorumNeeded } from "./layout.js";
import { createLayers, Renderer } from "./renderer.js";
import { ClientFx, clientSource, leaderTarget } from "./clientFx.js";
import { AnimationEngine } from "./animation.js";
import { updateMetricsStats, updateSidebarMetrics, updateSidebarCluster, mergeDisplayMetrics, showGrafanaHint } from "./metrics.js";
import { initLiveCharts } from "./liveCharts.js";

const layers = createLayers();
const renderer = new Renderer(layers);
const clientFx = new ClientFx(document.getElementById("fx-layer"));
const replFx = new AnimationEngine(document.getElementById("layer-beams"));

let liveCharts = null;
let topologyBounds = { width: 1000, height: 420 };
let selectedNodes = 5;
let lastLayoutSig = "";
let lastScenarioSig = "";
let lastMetricsStatsSig = "";
let pollClusterGen = 0;
let pollMetricsGen = 0;

let lastClusterData = null;
let lastScenarioData = null;
let displayRate = 0;
let targetRate = 0;
let loadActive = false;
let lastFrame = 0;
let replAccum = 0;
let clientFlowAccum = 0;
let pollClusterTimer = null;
let pollMetricsTimer = null;
let pollLogsTimer = null;
let frameRaf = null;

/** idle → booting → warming → active */
let stressPhase = "idle";
let stressPhaseStart = 0;
let configuredRate = 0;
let visualIntensity = 0;

const WARMUP_MS = 4200;
const BOOT_TIMEOUT_MS = 9000;

function easeInOutCubic(t) {
  return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
}

function resetStressVisuals() {
  stressPhase = "idle";
  stressPhaseStart = 0;
  configuredRate = 0;
  visualIntensity = 0;
  displayRate = 0;
  loadActive = false;
  targetRate = 0;
  clientFx.setActive(0, false);
  clientFx.setIntensity(0);
  clientFx.clear();
  replFx.clear();
  renderer.setClientActive(false);
  paintRateDisplay(0);
}

function maybeAdvanceStressPhase(cluster) {
  if (stressPhase !== "booting") return;
  const leader = (cluster?.nodes || []).find(
    (n) => n.running && (n.state === 2 || n.stateName === "leader")
  );
  if (cluster?.clusterStarted && leader) {
    stressPhase = "warming";
    stressPhaseStart = performance.now();
  } else if (performance.now() - stressPhaseStart > BOOT_TIMEOUT_MS) {
    stressPhase = "warming";
    stressPhaseStart = performance.now();
  }
}

function tickVisualIntensity() {
  if (stressPhase === "idle") {
    visualIntensity = 0;
    return;
  }
  const now = performance.now();
  if (stressPhase === "booting") {
    const wait = (now - stressPhaseStart) / 1000;
    visualIntensity = Math.min(0.1, wait * 0.035);
    return;
  }
  if (stressPhase === "warming") {
    const t = Math.min(1, (now - stressPhaseStart) / WARMUP_MS);
    visualIntensity = easeInOutCubic(t);
    if (t >= 1) stressPhase = "active";
    return;
  }
  visualIntensity = 1;
}

function stressPhaseLabel(cluster, scenario) {
  if (stressPhase === "booting") {
    if (!cluster?.clusterStarted) return "Starting cluster…";
    const leader = (cluster?.nodes || []).find(
      (n) => n.running && (n.state === 2 || n.stateName === "leader")
    );
    return leader ? "Cluster ready — warming up…" : "Waiting for leader election…";
  }
  if (stressPhase === "warming") {
    const pct = Math.round(visualIntensity * 100);
    return `Ramping load… ${pct}%`;
  }
  if (scenario?.running && scenario?.name === "Continuous stress") return "Running";
  return "";
}

renderer.onLeaderChange = (from, to) => {
  const toast = document.getElementById("leader-toast");
  if (!toast) return;
  toast.textContent = `Leader ${from} → ${to}`;
  toast.classList.remove("hidden");
  toast.classList.add("show");
  clearTimeout(toast._timer);
  toast._timer = setTimeout(() => {
    toast.classList.remove("show");
    setTimeout(() => toast.classList.add("hidden"), 450);
  }, 3200);
};

renderer.setActionHandler(async (id, action) => {
  const path = action === "stop" ? "/api/cluster/node/stop" : "/api/cluster/node/start";
  try {
    await apiPost(path, { id });
    lastLayoutSig = "";
    pollCluster();
  } catch (e) {
    alert(e.message);
  }
});

clientFx.onSpawn = () => {
  const pos = renderer.lastPos;
  const leaderId = renderer.leaderId;
  if (!pos || !leaderId) return;

  replFx.spawnFlow("client", leaderId, pos, "put");

  const followers = (lastClusterData?.nodes || [])
    .filter((n) => n.running && n.id !== leaderId)
    .map((n) => n.id);
  if (followers.length > 0 && Math.random() < 0.65) {
    replFx.spawnReplication(leaderId, followers, pos);
  }
};

async function apiPost(path, body) {
  const res = await fetch(path, {
    method: "POST",
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) throw new Error(await res.text() || res.statusText);
  return res.json().catch(() => ({}));
}

function layoutSignature(data) {
  const nodes = (data.nodes || []).map((n) =>
    [n.id, n.running, n.state, n.stateName].join(",")
  ).join(";");
  return [nodes, (data.partitionNodes || []).join(",")].join("|");
}

function dataSignature(data) {
  return (data.nodes || []).map((n) =>
    [n.id, n.term, n.commitIndex].join(",")
  ).join(";");
}

function scenarioSignature(sc) {
  if (!sc?.loaded) return "idle";
  return [sc.running, sc.stepIndex, sc.phase, sc.writeCount, sc.load?.sendRate, sc.done].join("|");
}

function metricsStatsSignature(m, scenario) {
  const load = scenario?.load;
  return [
    m.writeOpsSec, m.readOpsSec, m.writeP99Ms, m.readP99Ms,
    m.maxReplicationLag, m.failoverMs, m.clientSendRate,
    load?.writeP99Ms, load?.readSuccessRate,
  ].join("|");
}

function updateReadyChecks(checks) {
  const labels = {
    compose: "Docker",
    prometheus: "Prometheus",
    grafana: "Grafana",
    cluster: "Cluster",
    leader: "Leader",
    targets: "Scrape targets",
  };
  document.getElementById("ready-checks").innerHTML = Object.entries(labels)
    .map(([key, label]) => {
      const state = checks?.[key] || "pending";
      return `<li class="${state}">${label}: ${state}</li>`;
    })
    .join("");
}

function revealApp(grafanaOk) {
  document.getElementById("loading-overlay").classList.add("hidden");
  document.getElementById("app").classList.remove("hidden");
  if (!liveCharts) liveCharts = initLiveCharts();
  showGrafanaHint(grafanaOk);
}

async function waitForReady() {
  const status = document.getElementById("loading-status");
  const deadline = Date.now() + 15000;
  let grafanaOk = false;
  let revealed = false;

  while (Date.now() < deadline) {
    try {
      const res = await fetch("/api/ready");
      const data = await res.json();
      updateReadyChecks(data.checks);
      grafanaOk = data.checks?.grafana === "ok";

      if (!revealed) {
        revealApp(grafanaOk);
        revealed = true;
      }

      if (data.ready) {
        status.textContent = "Ready";
        return;
      }
      const pending = Object.entries(data.checks || {})
        .filter(([, v]) => v === "pending")
        .map(([k]) => k);
      status.textContent = pending.length ? `Starting ${pending.join(", ")}…` : "Starting…";
    } catch (_) {
      status.textContent = "Connecting…";
    }
    await new Promise((r) => setTimeout(r, 400));
  }

  if (!revealed) revealApp(grafanaOk);
}

function resizeTopology() {
  const canvas = document.getElementById("topology-canvas");
  const svg = document.getElementById("topology-svg");
  if (!canvas || !svg) return;
  const rect = canvas.getBoundingClientRect();
  topologyBounds = {
    width: Math.max(320, rect.width),
    height: Math.max(180, rect.height),
  };
  svg.setAttribute("viewBox", `0 0 ${topologyBounds.width} ${topologyBounds.height}`);
  renderer.invalidateLayout();
  lastLayoutSig = "";
  if (lastClusterData) {
    updateStatus(lastClusterData, lastScenarioData || {});
  }
  updateFxRoute(renderer.lastPos, renderer.leaderId);
}

function formatRateLarge(v) {
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k`;
  if (v >= 100) return `${Math.round(v)}`;
  return v > 0 ? v.toFixed(1) : "0";
}

function formatRateShort(v) {
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k`;
  if (v >= 100) return `${Math.round(v)}`;
  return v.toFixed(1);
}

function paintRateDisplay(rate) {
  const value = document.getElementById("client-rate-value");
  const sub = document.getElementById("client-rate-sub");
  const hero = document.getElementById("hero-rate");
  if (!value) return;

  value.textContent = formatRateLarge(rate);
  hero?.classList.toggle("hidden", !loadActive && rate <= 0);
}

function updateClientRate(metrics, scenario) {
  const load = scenario?.load;
  const sendRate = load?.active ? load.sendRate : metrics?.clientSendRate;
  const successRate = load?.active ? load.successRate : metrics?.clientSuccessRate;
  const measured = sendRate || successRate || 0;

  if (stressPhase !== "idle") {
    tickVisualIntensity();
    if (stressPhase === "active") {
      targetRate = measured || configuredRate;
      loadActive = Boolean(scenario?.running || measured > 0);
    } else {
      targetRate = configuredRate * visualIntensity;
      loadActive = visualIntensity > 0.08 && stressPhase !== "booting";
    }
    renderer.setClientActive(loadActive || stressPhase === "warming" || stressPhase === "active");
    clientFx.setActive(measured || configuredRate, stressPhase !== "idle");
    clientFx.setIntensity(visualIntensity);

    const sub = document.getElementById("client-rate-sub");
    if (sub) {
      if (stressPhase === "booting") {
        sub.textContent = "starting…";
      } else if (stressPhase === "warming") {
        sub.textContent = "warming up…";
      } else {
        sub.textContent = `${formatRateShort(successRate || sendRate || targetRate)} ok/s`;
      }
    }
    return;
  }

  loadActive = Boolean((load?.active && sendRate > 0) || sendRate > 0 || scenario?.running);
  targetRate = measured || (scenario?.running ? 800 : 0);

  renderer.setClientActive(loadActive);
  clientFx.setActive(targetRate, loadActive);
  clientFx.setIntensity(loadActive ? 1 : 0);

  const sub = document.getElementById("client-rate-sub");
  if (sub) {
    const workers = load?.concurrency ? `${load.concurrency} workers · ` : "";
    sub.textContent = loadActive
      ? `${workers}${formatRateShort(successRate || sendRate || 0)} ok/s`
      : "";
  }
}

function updateFxRoute(pos, leaderId) {
  const canvas = document.getElementById("topology-canvas");
  if (!canvas || !pos?.client) return;
  const rect = canvas.getBoundingClientRect();
  const from = clientSource(pos, topologyBounds, rect);
  const to = leaderId ? leaderTarget(pos, leaderId, topologyBounds, rect) : null;
  clientFx.setRoute(from, to);
}

function updateStatus(data, scenario) {
  lastClusterData = data;
  lastScenarioData = scenario;
  const nodes = data.nodes || [];
  const running = nodes.filter((n) => n.running);
  const leader = running.find((n) => n.state === 2 || n.stateName === "leader");
  const maxCommit = running.reduce((m, n) => Math.max(m, n.commitIndex ?? -1), -1);
  const stressActive = Boolean(scenario?.running);

  const liveDot = document.getElementById("live-dot");
  const sidebarStatus = document.getElementById("sidebar-status");
  const stressBtn = document.getElementById("btn-stress-test");
  const idleOverlay = document.getElementById("topology-idle");
  const nodeHint = document.getElementById("node-hint");
  const writeRpsInput = document.getElementById("write-rps");
  const readRpsInput = document.getElementById("read-rps");

  if (!data.clusterStarted) {
    liveDot.className = "live-dot";
    sidebarStatus.textContent = "Idle";
    nodeHint?.classList.add("hidden");
  } else if (leader) {
    liveDot.className = "live-dot ok";
    sidebarStatus.textContent = `Leader ${leader.id} · term ${leader.term} · commit ${maxCommit}`;
    nodeHint?.classList.remove("hidden");
  } else {
    liveDot.className = "live-dot warn";
    sidebarStatus.textContent = `${running.length}/${nodes.length} nodes · electing…`;
    nodeHint?.classList.remove("hidden");
  }

  if (stressActive) {
    stressBtn.textContent = "Stop stress test";
    stressBtn.classList.add("running", "stop-mode");
    stressBtn.disabled = false;
  } else {
    stressBtn.textContent = "Start stress test";
    stressBtn.classList.remove("running", "stop-mode");
    stressBtn.disabled = false;
    if (stressPhase !== "idle") resetStressVisuals();
  }
  writeRpsInput?.toggleAttribute("disabled", stressActive);
  readRpsInput?.toggleAttribute("disabled", stressActive);

  idleOverlay?.classList.toggle("hidden", stressActive || data.clusterStarted);

  const writeCount = scenario?.writeCount ?? 0;
  document.getElementById("topology-stats").textContent = data.clusterStarted
    ? `${running.length}/${nodes.length} up · quorum ${quorumNeeded(nodes.length || selectedNodes)} · ${writeCount} writes`
    : "";
  updateSidebarCluster({
    writeCount,
    running: running.length,
    total: nodes.length || selectedNodes,
  });

  const phaseBanner = document.getElementById("phase-banner");
  if (phaseBanner) {
    const stressLabel = stressPhase !== "idle" ? stressPhaseLabel(data, scenario) : "";
    const phase = scenario?.phase || "";
    if (stressLabel) {
      phaseBanner.textContent = stressLabel;
      phaseBanner.classList.remove("hidden");
    } else if (stressActive && phase) {
      phaseBanner.textContent = phase;
      phaseBanner.classList.remove("hidden");
    } else if (data.clusterStarted && leader) {
      phaseBanner.textContent = "Running";
      phaseBanner.classList.remove("hidden");
    } else {
      phaseBanner.classList.add("hidden");
    }
  }

  maybeAdvanceStressPhase(data);

  document.getElementById("partition-badge")?.classList.toggle("hidden", !data.partitionActive);
  document.getElementById("btn-start").disabled = data.clusterStarted;
  document.getElementById("btn-stop").disabled = !data.clusterStarted;
  document.getElementById("btn-run").disabled = !data.clusterStarted;

  const layoutSig = layoutSignature(data);
  const dataSig = dataSignature(data);
  const combined = layoutSig + "|" + dataSig;
  if (combined === lastLayoutSig) return;
  lastLayoutSig = combined;

  renderer.clusterStarted = Boolean(data.clusterStarted);
  const pos = computeLayout(nodes, topologyBounds);
  renderer.syncNodes(nodes, pos, data.partitionNodes || [], topologyBounds);
  updateFxRoute(pos, renderer.leaderId);
}

function updateScenario(sc) {
  const sig = scenarioSignature(sc);
  if (sig === lastScenarioSig) return;
  lastScenarioSig = sig;

  const stepEl = document.getElementById("scenario-step");
  const progress = document.getElementById("scenario-progress");
  if (!sc.loaded) {
    stepEl.textContent = "Idle";
    progress.style.width = "0%";
    return;
  }
  if (sc.running && sc.name === "Continuous stress") {
    stepEl.textContent = "Running continuous load — click Stop stress test to end";
    progress.style.width = "100%";
    document.getElementById("btn-pause").disabled = true;
    const log = document.getElementById("event-log");
    const line = (sc.log || []).slice(-1)[0];
    log.textContent = line ? line : "";
    return;
  }
  const pct = sc.totalSteps ? Math.round(((sc.stepIndex + (sc.running ? 0.5 : 0)) / sc.totalSteps) * 100) : 0;
  progress.style.width = `${Math.min(pct, 100)}%`;
  stepEl.textContent = sc.running
    ? `${sc.currentStep || sc.name} (${sc.stepIndex + 1}/${sc.totalSteps})`
    : sc.done ? `Done: ${sc.name}` : `Loaded: ${sc.name}`;

  document.getElementById("btn-pause").disabled = !sc.running;
  const log = document.getElementById("event-log");
  const line = (sc.log || []).slice(-1)[0];
  log.textContent = line ? line : "";
}

async function pollCluster() {
  const gen = ++pollClusterGen;
  try {
    const [cluster, scenario] = await Promise.all([
      fetch("/api/cluster/status").then((r) => r.json()),
      fetch("/api/scenario").then((r) => r.json()),
    ]);
    if (gen !== pollClusterGen) return;
    updateStatus(cluster, scenario);
    updateScenario(scenario);
    updateClientRate({}, scenario);
  } catch (_) { /* ignore */ }
}

async function pollMetrics() {
  const gen = ++pollMetricsGen;
  try {
    const [metrics, scenario] = await Promise.all([
      fetch("/api/metrics/live").then((r) => r.json()),
      fetch("/api/scenario").then((r) => r.json()),
    ]);
    if (gen !== pollMetricsGen) return;

    updateClientRate(metrics, scenario);
    if (stressPhase === "idle") {
      clientFx.setActive(targetRate, loadActive);
      clientFx.setIntensity(loadActive ? 1 : 0);
    }

    const display = mergeDisplayMetrics(metrics, scenario);
    const statsSig = metricsStatsSignature(display, scenario);
    if (statsSig !== lastMetricsStatsSig) {
      lastMetricsStatsSig = statsSig;
      updateMetricsStats(display);
    }
    updateSidebarMetrics(display);
    liveCharts?.update(display);
  } catch (_) { /* ignore */ }
}

let pollLogsGen = 0;
async function pollLogs() {
  const gen = ++pollLogsGen;
  try {
    const res = await fetch("/api/cluster/logs");
    const data = await res.json();
    if (gen !== pollLogsGen) return;
    const byNode = {};
    for (const n of data.nodes || []) byNode[n.id] = n;
    renderer.updateLogs(byNode);
  } catch (_) { /* ignore */ }
}

function tickClientFlows(dt) {
  if (visualIntensity < 0.08 || !renderer.leaderId || !renderer.lastPos) return;
  const pos = renderer.lastPos;
  if (!pos.client || !pos[renderer.leaderId]) return;

  clientFlowAccum += dt;
  const effective = (targetRate || configuredRate) * visualIntensity;
  const interval = 1 / Math.max(0.9, Math.min(5, effective / 160));
  if (clientFlowAccum < interval) return;
  clientFlowAccum = 0;
  replFx.spawnFlow("client", renderer.leaderId, pos, "put");
}

function tickReplication(dt) {
  if (visualIntensity < 0.3 || !renderer.leaderId) return;
  replAccum += dt * visualIntensity;
  const interval = Math.max(0.18, 0.55 - targetRate / 12000);
  if (replAccum < interval) return;
  replAccum = 0;

  const pos = renderer.lastPos;
  const followers = (lastClusterData?.nodes || [])
    .filter((n) => n.running && n.id !== renderer.leaderId)
    .map((n) => n.id);
  if (followers.length === 0) return;

  const burst = 1 + Math.floor(visualIntensity * 2);
  replFx.spawnReplication(
    renderer.leaderId,
    followers.slice(0, Math.min(followers.length, burst)),
    pos
  );
}

function frameLoop(ts) {
  if (!lastFrame) lastFrame = ts;
  const dt = Math.min(0.05, (ts - lastFrame) / 1000);
  lastFrame = ts;

  const ease = visualIntensity < 1 ? 5 : 14;
  displayRate += (targetRate - displayRate) * Math.min(1, dt * ease);
  tickVisualIntensity();
  clientFx.setIntensity(visualIntensity);
  paintRateDisplay(displayRate);
  clientFx.tick(dt);
  tickClientFlows(dt);
  tickReplication(dt);
  replFx.tick(ts);
  frameRaf = requestAnimationFrame(frameLoop);
}

function stopPolling() {
  if (pollClusterTimer != null) {
    clearInterval(pollClusterTimer);
    pollClusterTimer = null;
  }
  if (pollMetricsTimer != null) {
    clearInterval(pollMetricsTimer);
    pollMetricsTimer = null;
  }
  if (pollLogsTimer != null) {
    clearInterval(pollLogsTimer);
    pollLogsTimer = null;
  }
  if (frameRaf != null) {
    cancelAnimationFrame(frameRaf);
    frameRaf = null;
  }
}

document.querySelectorAll("#node-segments button").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll("#node-segments button").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    selectedNodes = parseInt(btn.dataset.n, 10);
  });
});

document.getElementById("btn-create").addEventListener("click", async () => {
  try {
    await apiPost("/api/cluster/create", { nodes: selectedNodes });
    lastLayoutSig = "";
    pollCluster();
  } catch (e) { alert(e.message); }
});

document.getElementById("btn-start").addEventListener("click", async () => {
  try {
    await apiPost("/api/cluster/start");
    lastLayoutSig = "";
    pollCluster();
  } catch (e) { alert(e.message); }
});

document.getElementById("btn-stop").addEventListener("click", async () => {
  try {
    await apiPost("/api/cluster/stop");
    lastLayoutSig = "";
    pollCluster();
  } catch (e) { alert(e.message); }
});

document.getElementById("btn-load").addEventListener("click", async () => {
  const path = document.getElementById("scenario-select").value;
  if (!path) return;
  try { await apiPost("/api/scenario/load", { path }); } catch (e) { alert(e.message); }
});

document.getElementById("btn-run").addEventListener("click", async () => {
  try { await apiPost("/api/scenario/run"); } catch (e) { alert(e.message); }
});

document.getElementById("btn-pause").addEventListener("click", async () => {
  try { await apiPost("/api/scenario/pause"); } catch (e) { alert(e.message); }
});

function clampRateInput(input) {
  if (!input) return;
  const min = parseInt(input.min, 10) || 0;
  const max = parseInt(input.max, 10) || Infinity;
  let v = parseInt(input.value, 10);
  if (Number.isNaN(v)) v = min;
  input.value = String(Math.min(max, Math.max(min, v)));
}

["write-rps", "read-rps"].forEach((id) => {
  const input = document.getElementById(id);
  input?.addEventListener("change", () => clampRateInput(input));
});

document.getElementById("btn-stress-test").addEventListener("click", async () => {
  const btn = document.getElementById("btn-stress-test");
  const isRunning = btn.classList.contains("stop-mode");

  if (isRunning) {
    btn.disabled = true;
    try {
      await apiPost("/api/stress/stop");
      resetStressVisuals();
      lastScenarioSig = "";
      pollCluster();
      pollMetrics();
    } catch (e) {
      alert(e.message);
    } finally {
      btn.disabled = false;
    }
    return;
  }

  const writeRps = parseInt(document.getElementById("write-rps").value, 10);
  const readRps = parseInt(document.getElementById("read-rps").value, 10);
  clampRateInput(document.getElementById("write-rps"));
  clampRateInput(document.getElementById("read-rps"));

  configuredRate = writeRps + readRps;
  stressPhase = "booting";
  stressPhaseStart = performance.now();
  loadActive = false;
  targetRate = 0;
  displayRate = 0;
  visualIntensity = 0;
  clientFx.setIntensity(0);
  document.getElementById("topology-idle")?.classList.add("hidden");
  document.getElementById("hero-rate")?.classList.remove("hidden");
  paintRateDisplay(0);

  btn.disabled = true;
  try {
    await apiPost("/api/scenario/stress-test", { writeRps, readRps });
    lastLayoutSig = "";
    lastScenarioSig = "";
    pollCluster();
    pollMetrics();
  } catch (e) {
    alert(e.message);
    resetStressVisuals();
  } finally {
    btn.disabled = false;
  }
});

document.getElementById("btn-quit").addEventListener("click", async () => {
  if (!confirm("Stop the stress test, shut down the cluster, and quit the playground?")) return;
  const btn = document.getElementById("btn-quit");
  btn.disabled = true;
  btn.textContent = "Stopping…";
  stopPolling();
  try {
    await apiPost("/api/quit");
  } catch (_) { /* server may exit before response completes */ }
  document.body.innerHTML = `
    <div class="quit-screen">
      <p>Playground stopped. Port freed — you can close this tab.</p>
    </div>
  `;
});

let resizeTimer;
const ro = new ResizeObserver(() => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(resizeTopology, 100);
});
ro.observe(document.getElementById("topology-canvas"));

waitForReady().then(() => {
  resizeTopology();
  pollCluster();
  pollMetrics();
  pollLogs();
  pollClusterTimer = setInterval(pollCluster, 800);
  pollMetricsTimer = setInterval(pollMetrics, 200);
  pollLogsTimer = setInterval(pollLogs, 1000);
  frameRaf = requestAnimationFrame(frameLoop);
});
