import { computeLayout, quorumNeeded } from "./layout.js";
import { createLayers, Renderer } from "./renderer.js";
import { AnimationEngine } from "./animation.js";
import { updateMetricsUI } from "./charts.js";

const layers = createLayers();
const renderer = new Renderer(layers);
const animationEngine = new AnimationEngine(layers.flow);
let topologyBounds = { width: 1000, height: 420 };
let selectedNodes = 5;
let lastPos = {};
let lastWriteKey = "";
let lastLeaderId = null;
let prevCommits = {};

async function apiPost(path, body) {
  const res = await fetch(path, {
    method: "POST",
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) throw new Error(await res.text() || res.statusText);
  return res.json().catch(() => ({}));
}

function escapeHtml(s) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function updateReadyChecks(checks) {
  const labels = {
    compose: "Docker",
    prometheus: "Prometheus",
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

  while (Date.now() < deadline) {
    try {
      const res = await fetch("/api/ready");
      const data = await res.json();
      updateReadyChecks(data.checks);
      if (data.ready) {
        overlay.classList.add("hidden");
        app.classList.remove("hidden");
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
}

function spawnWriteFlows(nodes, pos, lastWrite) {
  if (!lastWrite?.to) return;
  const target = lastWrite.to;
  const leader = nodes.find((n) => n.running && (n.state === 2 || n.stateName === "leader"));

  animationEngine.spawnFlow("client", target, pos, "put", "PUT", { curve: 35 });

  if (leader && target !== leader.id) {
    animationEngine.spawnFlow(target, leader.id, pos, "forward", "fwd", { curve: -20 });
  }

  if (leader) {
    const followers = nodes.filter(
      (n) => n.running && n.id !== leader.id && n.state !== 2 && n.stateName !== "leader"
    );
    followers.forEach((f, i) => {
      setTimeout(() => {
        animationEngine.spawnFlow(leader.id, f.id, pos, "append", "log", {
          curve: 15 + i * 5,
          r: 4,
        });
      }, i * 80);
    });
  }
}

function handleCommitAdvances(nodes, pos) {
  const leader = nodes.find((n) => n.running && (n.state === 2 || n.stateName === "leader"));
  if (!leader) return;

  let advanced = false;
  const followers = [];
  for (const node of nodes) {
    const prev = prevCommits[node.id] ?? -1;
    const cur = node.commitIndex ?? -1;
    if (node.running && cur > prev) {
      advanced = true;
      if (node.id !== leader.id) followers.push(node.id);
    }
    prevCommits[node.id] = cur;
  }

  if (advanced && followers.length) {
    renderer.pulseCommitBeams(leader.id, followers);
    followers.forEach((fid, i) => {
      setTimeout(() => {
        animationEngine.spawnFlow(leader.id, fid, pos, "commit", "", { curve: 10, r: 6 });
      }, i * 60);
    });
  }
}

function updateStatus(data, scenario) {
  const nodes = data.nodes || [];
  const running = nodes.filter((n) => n.running);
  const leader = running.find((n) => n.state === 2 || n.stateName === "leader");
  const maxCommit = running.reduce((m, n) => Math.max(m, n.commitIndex ?? -1), -1);
  const demoActive = Boolean(scenario?.running);

  const liveDot = document.getElementById("live-dot");
  const sidebarStatus = document.getElementById("sidebar-status");
  const demoBtn = document.getElementById("btn-demo");
  const idleOverlay = document.getElementById("topology-idle");

  if (!data.clusterStarted) {
    liveDot.className = "live-dot";
    sidebarStatus.textContent = "Waiting for Start Demo";
  } else if (leader) {
    liveDot.className = "live-dot ok";
    sidebarStatus.textContent = `Leader ${leader.id} · term ${leader.term} · commit ${maxCommit}`;
  } else {
    liveDot.className = "live-dot";
    sidebarStatus.textContent = `${running.length}/${nodes.length} nodes · electing…`;
  }

  demoBtn.disabled = demoActive;
  demoBtn.classList.toggle("running", demoActive);
  if (idleOverlay) {
    idleOverlay.classList.toggle("hidden", demoActive || (data.clusterStarted && scenario?.done));
  }

  const writeCount = scenario?.writeCount ?? 0;
  document.getElementById("topology-stats").textContent = data.clusterStarted
    ? `${running.length}/${nodes.length} nodes · quorum ${quorumNeeded(nodes.length || selectedNodes)} · writes ${writeCount}`
    : "Cluster idle";

  const phaseBanner = document.getElementById("phase-banner");
  if (phaseBanner) {
    const phase = scenario?.phase || "";
    if (demoActive && phase) {
      phaseBanner.textContent = phase;
      phaseBanner.classList.remove("hidden");
    } else if (data.clusterStarted && leader) {
      phaseBanner.textContent = "Cluster ready";
      phaseBanner.classList.remove("hidden");
    } else {
      phaseBanner.classList.add("hidden");
    }
  }

  const partitionBadge = document.getElementById("partition-badge");
  partitionBadge.classList.toggle("hidden", !data.partitionActive);

  document.getElementById("btn-start").disabled = data.clusterStarted;
  document.getElementById("btn-stop").disabled = !data.clusterStarted;
  document.getElementById("btn-run").disabled = !data.clusterStarted;

  const pos = computeLayout(nodes, topologyBounds);
  lastPos = pos;
  renderer.syncNodes(nodes, pos, data.partitionNodes, demoActive);

  if (leader && leader.id !== lastLeaderId && lastLeaderId != null) {
    animationEngine.spawnFlow(leader.id, leader.id, pos, "state_change", "", { curve: 0, r: 8 });
  }
  lastLeaderId = leader?.id ?? null;

  handleCommitAdvances(nodes, pos);
}

function updateScenario(sc) {
  const stepEl = document.getElementById("scenario-step");
  const progress = document.getElementById("scenario-progress");
  if (!sc.loaded) {
    stepEl.textContent = "Click Start Demo to launch the cluster";
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
  log.innerHTML = (sc.log || []).slice(-8).map((l) => `<div>${escapeHtml(l)}</div>`).join("");
  log.scrollTop = log.scrollHeight;

  const lw = sc.lastWrite;
  const writeSig = lw ? `${lw.from}:${lw.to}:${lw.key}` : "";
  if (writeSig && writeSig !== lastWriteKey) {
    lastWriteKey = writeSig;
    fetch("/api/cluster/status")
      .then((r) => r.json())
      .then((cluster) => spawnWriteFlows(cluster.nodes || [], lastPos, lw))
      .catch(() => {});
  }
}

async function poll() {
  try {
    const [cluster, scenario, metrics] = await Promise.all([
      fetch("/api/cluster/status").then((r) => r.json()),
      fetch("/api/scenario").then((r) => r.json()),
      fetch("/api/metrics/live").then((r) => r.json()),
    ]);
    updateStatus(cluster, scenario);
    updateScenario(scenario);
    updateMetricsUI(metrics);
  } catch (_) { /* ignore */ }
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
  } catch (e) { alert(e.message); }
});

document.getElementById("btn-start").addEventListener("click", async () => {
  try { await apiPost("/api/cluster/start"); } catch (e) { alert(e.message); }
});

document.getElementById("btn-stop").addEventListener("click", async () => {
  try { await apiPost("/api/cluster/stop"); } catch (e) { alert(e.message); }
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

document.getElementById("btn-demo").addEventListener("click", async () => {
  const btn = document.getElementById("btn-demo");
  btn.disabled = true;
  try {
    await apiPost("/api/scenario/demo");
  } catch (e) {
    alert(e.message);
    btn.disabled = false;
  }
});

const ro = new ResizeObserver(() => resizeTopology());
ro.observe(document.getElementById("topology-canvas"));

function animationLoop() {
  animationEngine.tick();
  requestAnimationFrame(animationLoop);
}

waitForReady().then(() => {
  resizeTopology();
  poll();
  setInterval(poll, 350);
  requestAnimationFrame(animationLoop);
});
