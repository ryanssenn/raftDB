import { computeLayout, quorumNeeded } from "./layout.js";
import { createLayers, Renderer } from "./renderer.js";
import { ClientFx, clientSource, leaderTarget } from "./clientFx.js";
import { updateMetricsStats, initGrafanaPanels } from "./metrics.js";

const layers = createLayers();
const renderer = new Renderer(layers);
const clientFx = new ClientFx(document.getElementById("fx-layer"));

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
let loadActive = false;
let lastFrame = 0;
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

function metricsStatsSignature(m) {
  return [
    m.writeOpsSec, m.readOpsSec, m.writeP99Ms, m.readP99Ms,
    m.maxReplicationLag, m.failoverMs, m.clientSendRate,
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

async function waitForReady() {
  const overlay = document.getElementById("loading-overlay");
  const status = document.getElementById("loading-status");
  const app = document.getElementById("app");
  const deadline = Date.now() + 90000;
  let grafanaEnabled = false;

  while (Date.now() < deadline) {
    try {
      const res = await fetch("/api/ready");
      const data = await res.json();
      updateReadyChecks(data.checks);
      grafanaEnabled = data.checks?.grafana === "ok";
      if (data.ready) {
        overlay.classList.add("hidden");
        app.classList.remove("hidden");
        initGrafanaPanels(grafanaEnabled);
        return;
      }
      const pending = Object.entries(data.checks || {})
        .filter(([, v]) => v === "pending")
        .map(([k]) => k);
      status.textContent = pending.length ? `Waiting for ${pending.join(", ")}…` : "Starting…";
    } catch (_) {
      status.textContent = "Connecting…";
    }
    await new Promise((r) => setTimeout(r, 500));
  }

  overlay.classList.add("hidden");
  app.classList.remove("hidden");
  initGrafanaPanels(grafanaEnabled);
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

  const text = formatRateLarge(rate);
  value.textContent = text;
  hero?.classList.toggle("hidden", !loadActive && rate <= 0);
}

function updateClientRate(metrics, scenario) {
  const load = scenario?.load;
  const sendRate = load?.active ? load.sendRate : metrics?.clientSendRate;
  const successRate = load?.active ? load.successRate : metrics?.clientSuccessRate;
  loadActive = Boolean((load?.active && sendRate > 0) || sendRate > 0 || scenario?.running);
  targetRate = sendRate || 0;

  renderer.setClientActive(loadActive);

  const sub = document.getElementById("client-rate-sub");
  if (sub) {
    const workers = load?.concurrency ? `${load.concurrency} workers · ` : "";
    sub.textContent = loadActive
      ? `${workers}${formatRateShort(successRate || 0)} ok/s`
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

  if (!data.clusterStarted) {
    liveDot.className = "live-dot";
    sidebarStatus.textContent = "Idle";
    nodeHint?.classList.add("hidden");
  } else if (leader) {
    liveDot.className = "live-dot ok";
    sidebarStatus.textContent = `Leader ${leader.id} · term ${leader.term} · commit ${maxCommit}`;
    nodeHint?.classList.remove("hidden");
  } else {
    liveDot.className = "live-dot";
    sidebarStatus.textContent = `${running.length}/${nodes.length} nodes · electing…`;
    nodeHint?.classList.remove("hidden");
  }

  stressBtn.disabled = stressActive;
  stressBtn.classList.toggle("running", stressActive);
  idleOverlay?.classList.toggle("hidden", stressActive || (data.clusterStarted && scenario?.done));

  const writeCount = scenario?.writeCount ?? 0;
  document.getElementById("topology-stats").textContent = data.clusterStarted
    ? `${running.length}/${nodes.length} up · quorum ${quorumNeeded(nodes.length || selectedNodes)} · ${writeCount} writes`
    : "";

  const phaseBanner = document.getElementById("phase-banner");
  if (phaseBanner) {
    const phase = scenario?.phase || "";
    if (stressActive && phase) {
      phaseBanner.textContent = phase;
      phaseBanner.classList.remove("hidden");
    } else if (data.clusterStarted && leader) {
      phaseBanner.textContent = "Running";
      phaseBanner.classList.remove("hidden");
    } else {
      phaseBanner.classList.add("hidden");
    }
  }

  document.getElementById("partition-badge")?.classList.toggle("hidden", !data.partitionActive);
  document.getElementById("btn-start").disabled = data.clusterStarted;
  document.getElementById("btn-stop").disabled = !data.clusterStarted;
  document.getElementById("btn-run").disabled = !data.clusterStarted;

  const layoutSig = layoutSignature(data);
  const dataSig = dataSignature(data);
  const combined = layoutSig + "|" + dataSig;
  if (combined === lastLayoutSig) return;
  lastLayoutSig = combined;

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
    clientFx.setActive(targetRate, loadActive);

    const statsSig = metricsStatsSignature(metrics);
    if (statsSig !== lastMetricsStatsSig) {
      lastMetricsStatsSig = statsSig;
      updateMetricsStats(metrics);
    }
  } catch (_) { /* ignore */ }
}

function frameLoop(ts) {
  if (!lastFrame) lastFrame = ts;
  const dt = Math.min(0.05, (ts - lastFrame) / 1000);
  lastFrame = ts;

  displayRate += (targetRate - displayRate) * Math.min(1, dt * 14);
  paintRateDisplay(displayRate);
  clientFx.tick(dt);

  requestAnimationFrame(frameLoop);
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

document.getElementById("btn-stress-test").addEventListener("click", async () => {
  const btn = document.getElementById("btn-stress-test");
  btn.disabled = true;
  try {
    await apiPost("/api/scenario/stress-test");
    lastLayoutSig = "";
    lastScenarioSig = "";
    pollCluster();
    pollMetrics();
  } catch (e) {
    alert(e.message);
    btn.disabled = false;
  }
});

document.getElementById("btn-quit").addEventListener("click", async () => {
  if (!confirm("Stop the stress test, shut down the cluster, and quit the playground?")) return;
  const btn = document.getElementById("btn-quit");
  btn.disabled = true;
  btn.textContent = "Stopping…";
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
  resizeTimer = setTimeout(resizeTopology, 150);
});
ro.observe(document.getElementById("topology-canvas"));

waitForReady().then(() => {
  resizeTopology();
  pollCluster();
  pollMetrics();
  setInterval(pollCluster, 1200);
  setInterval(pollMetrics, 350);
  requestAnimationFrame(frameLoop);
});
