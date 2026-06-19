const SVG_NS = "http://www.w3.org/2000/svg";
const POLL_MS = 80;

const L = {
  centerX: 500,
  leaderY: 310,
  followerY: 70,
  clientY: 530,
  followerSpan: 860,
};

const CARD = {
  fw: 168,
  fh: 118,
  lw: 204,
  lh: 136,
  pad: 10,
  queueH: 14,
  logH: 32,
};

const ENTRY_W = 22;
const ENTRY_GAP = 2;
const MAX_LOG_SLOTS = 5;

const T = { req: 100, forward: 85, entry: 150, ack: 110, vote: 130, commit: 380 };

const T_SHOWCASE = {
  req: 520,
  forward: 480,
  entry: 420,
  ack: 380,
  vote: 650,
  commit: 450,
  entryStagger: 40,
  commitStagger: 30,
};

const layerBeams = document.getElementById("layer-beams");
const layerCommitFx = document.getElementById("layer-commit-fx");
const layerNodes = document.getElementById("layer-nodes");
const layerFlow = document.getElementById("layer-flow");
const layerActors = document.getElementById("layer-actors");

const scenarioName = document.getElementById("scenario-name");
const demoBadge = document.getElementById("demo-badge");
const termDisplay = document.getElementById("term-display");
const stepDisplay = document.getElementById("step-display");
const progressFill = document.getElementById("progress-fill");
const statusChip = document.getElementById("status-chip");
const callout = document.getElementById("callout");
const commitIndexDisplay = document.getElementById("commit-index-display");
const quorumFill = document.getElementById("quorum-fill");
const quorumText = document.getElementById("quorum-text");
const stage = document.getElementById("stage");
const sceneCard = document.getElementById("scene-card");
const sceneTitle = document.getElementById("scene-title");
const sceneSubtitle = document.getElementById("scene-subtitle");
const showcaseTimeline = document.getElementById("showcase-timeline");
const showcaseProgress = document.getElementById("showcase-progress");

let seenEvents = new Set();
let actors = {};
let pos = {};
let lastNodes = [];
let lastLeaderId = null;
let lastTerm = null;
let calloutTimer = null;
let prevLogLength = {};
let prevCommitIndex = {};
let votesByCandidate = {};
let focusIds = new Set();
let focusUntil = 0;
let stageReady = false;
let requestQueue = [];
let quorumTrack = { index: -1, acks: 0, needed: 0 };

let showcaseMode = false;
let showcaseScenes = [];
let showcaseDurationMs = 30000;
let showcaseStartMs = 0;
let showcaseSceneIndex = -1;
let revealedNodes = new Set();
let showcaseLeaderPrimed = false;
let clientRevealed = false;
let showcaseCycle = -1;
let showcaseLoop = false;
let activeFlowCount = 0;
let highlightDone = false;

const MAX_SHOWCASE_FLOWS = 24;

function resetVisualState() {
  seenEvents.clear();
  Object.values(actors).forEach((a) => {
    a.nodeG?.remove();
    a.g?.remove();
  });
  actors = {};
  pos = {};
  lastLeaderId = null;
  lastTerm = null;
  prevLogLength = {};
  prevCommitIndex = {};
  votesByCandidate = {};
  requestQueue = [];
  quorumTrack = { index: -1, acks: 0, needed: 0 };
  revealedNodes.clear();
  showcaseLeaderPrimed = false;
  clientRevealed = false;
  showcaseSceneIndex = -1;
  stageReady = false;
  activeFlowCount = 0;
  highlightDone = false;
  layerBeams.innerHTML = '';
  layerNodes.innerHTML = '';
  layerFlow.innerHTML = '';
  layerCommitFx.innerHTML = '';
  sceneCard.classList.remove("visible");
}

function timing() {
  return showcaseMode ? T_SHOWCASE : T;
}

function showcaseElapsed() {
  if (!showcaseMode || !showcaseStartMs) return 0;
  return Date.now() - showcaseStartMs;
}

function initShowcase(scenario) {
  const cycle = scenario.cycle ?? 0;
  const isShowcase = !!scenario.showcase;

  if (isShowcase && cycle !== showcaseCycle) {
    resetVisualState();
    showcaseCycle = cycle;
    showcaseStartMs = scenario.showcaseStartMs || Date.now();
  }

  showcaseMode = isShowcase;
  showcaseLoop = !!scenario.loop;
  showcaseScenes = scenario.scenes || [];
  showcaseDurationMs = scenario.durationMs || 30000;
  if (isShowcase && scenario.showcaseStartMs && cycle === showcaseCycle) {
    showcaseStartMs = scenario.showcaseStartMs;
  }

  stage.classList.toggle("showcase-mode", showcaseMode);
  sceneCard.classList.toggle("hidden", !showcaseMode);
  showcaseTimeline.classList.toggle("hidden", !showcaseMode);
  demoBadge.classList.toggle("hidden", showcaseMode || !scenario.demoPace);

  if (showcaseMode) {
    progressFill.style.display = "none";
  } else {
    progressFill.style.display = "block";
  }
}

function currentShowcaseScene(elapsed) {
  if (!showcaseScenes.length) return -1;
  for (let i = showcaseScenes.length - 1; i >= 0; i--) {
    if (elapsed >= showcaseScenes[i].startMs) return i;
  }
  return 0;
}

function updateShowcaseUI() {
  if (!showcaseMode) return;

  const elapsed = showcaseElapsed();
  showcaseProgress.style.width = `${Math.min(100, (elapsed / showcaseDurationMs) * 100)}%`;

  const idx = currentShowcaseScene(elapsed);
  if (idx !== showcaseSceneIndex && idx >= 0) {
    showcaseSceneIndex = idx;
    const scene = showcaseScenes[idx];
    sceneTitle.textContent = scene.title;
    sceneSubtitle.textContent = scene.subtitle;
    sceneCard.classList.add("visible");
  }

  if (idx === showcaseScenes.length - 1 && elapsed >= showcaseScenes[idx].startMs && showcaseLoop) {
    sceneCard.classList.add("visible");
  }

  const stable = showcaseScenes.find((s) => s.id === "stable");
  if (stable && elapsed >= stable.startMs + 600 && !highlightDone) {
    highlightDone = true;
    highlightCommittedEntries();
  }

  if (elapsed >= 2800 && !clientRevealed) revealClient();

  const leader = getLeader(lastNodes);
  const uncertainty = !leader && elapsed >= 11500 && elapsed < 16000;
  lastNodes.forEach((n) => {
    const a = actors[n.id];
    if (a?.nodeG) a.nodeG.classList.toggle("uncertainty", uncertainty && n.running);
  });
}

function revealNode(id) {
  if (revealedNodes.has(id)) return;
  revealedNodes.add(id);
  const a = actors[id];
  if (!a?.nodeG) return;
  a.nodeG.classList.remove("await-reveal");
  a.nodeG.classList.add("reveal-in");
}

function revealClient() {
  if (clientRevealed) return;
  clientRevealed = true;
  const a = actors.client;
  if (!a?.g) return;
  a.g.classList.remove("await-reveal");
  a.g.classList.add("reveal-in");
}

function markAwaitReveal(id) {
  const a = actors[id];
  if (!a || revealedNodes.has(id)) return;
  a.nodeG?.classList.add("await-reveal");
}

function markClientAwait() {
  const a = actors.client;
  if (!a?.g || clientRevealed) return;
  a.g.classList.add("await-reveal");
}

function shouldAnimateEvent(e) {
  if (!showcaseMode) return true;
  if (showcaseStartMs && e.ts && e.ts < showcaseStartMs) return false;
  const from = normalizeId(e.from);
  const to = normalizeId(e.to);
  if (from !== "client" && !revealedNodes.has(from)) return false;
  if (to !== "client" && !revealedNodes.has(to)) return false;
  return true;
}

function syncShowcaseReveal(nodes) {
  if (!showcaseMode) return;
  const elapsed = showcaseElapsed();
  const sorted = [...nodes].sort((a, b) => a.id.localeCompare(b.id, undefined, { numeric: true }));
  sorted.forEach((n, i) => {
    const revealAt = 250 + i * 500;
    if (n.running && elapsed >= revealAt) revealNode(n.id);
  });
}

function clearFlows() {
  layerFlow.innerHTML = "";
  activeFlowCount = 0;
}

function entryVisibleSlot(nodeId, index) {
  const node = lastNodes.find((n) => n.id === nodeId);
  const logLen = node?.logLength ?? 0;
  if (logLen <= 0 || index < 0 || index >= logLen) return -1;
  const start = Math.max(0, logLen - MAX_LOG_SLOTS);
  if (index < start) return -1;
  const vis = index - start;
  return vis < MAX_LOG_SLOTS ? vis : -1;
}

function trimOldEntries(nodeId) {
  const a = actors[nodeId];
  const node = lastNodes.find((n) => n.id === nodeId);
  if (!a?.entries || !node) return;
  const start = Math.max(0, node.logLength - MAX_LOG_SLOTS);
  [...a.entries.keys()].forEach((idx) => {
    if (idx < start) {
      a.entries.get(idx)?.remove();
      a.entries.delete(idx);
    }
  });
}

function highlightCommittedEntries() {
  document.querySelectorAll('.log-entry.committed').forEach((el) => {
    el.classList.add("lock-in");
  });
}

function normalizeId(id) {
  return id === "visualizer" ? "client" : id;
}

function getLeader(nodes) {
  return nodes.find((n) => n.running && n.reachable && n.state === 2);
}

function majorityNeeded() {
  const alive = lastNodes.filter((n) => n.running && n.reachable).length;
  return Math.max(1, Math.floor(alive / 2) + 1);
}

function nodeAlive(id) {
  if (id === "client") return true;
  const n = lastNodes.find((x) => x.id === id);
  return !!(n && n.running && n.reachable);
}

function roleOf(node) {
  if (!node.running || !node.reachable) return "offline";
  if (node.state === 2) return "leader";
  if (node.state === 1) return "candidate";
  return "follower";
}

function followerSlotX(index) {
  const slotW = L.followerSpan / 4;
  const start = L.centerX - L.followerSpan / 2;
  return start + slotW * index + slotW / 2;
}

function layoutPositions(nodes) {
  pos.client = { x: L.centerX, role: "client" };

  const leader = getLeader(nodes);
  const candidate = nodes.find((n) => n.running && n.reachable && n.state === 1);
  const centerNode = leader || candidate;
  const centerId = centerNode?.id;

  if (centerId) {
    pos[centerId] = {
      x: L.centerX,
      cardX: L.centerX - CARD.lw / 2,
      cardY: L.leaderY,
      cw: CARD.lw,
      ch: CARD.lh,
      role: roleOf(centerNode),
      isCenter: true,
    };
  }

  const others = nodes
    .filter((n) => n.id !== centerId)
    .sort((a, b) => a.id.localeCompare(b.id, undefined, { numeric: true }));

  others.forEach((node, i) => {
    if (i >= 4) return;
    const cx = followerSlotX(i);
    pos[node.id] = {
      x: cx,
      cardX: cx - CARD.fw / 2,
      cardY: L.followerY,
      cw: CARD.fw,
      ch: CARD.fh,
      role: roleOf(node),
      isCenter: false,
    };
  });

  // During initial boot before roles settle, place any unassigned nodes in follower slots
  nodes.forEach((node) => {
    if (pos[node.id]) return;
    const used = Object.values(pos).filter((p) => !p.isCenter && p.cardY === L.followerY).length;
    if (used >= 4) return;
    const cx = followerSlotX(used);
    pos[node.id] = {
      x: cx,
      cardX: cx - CARD.fw / 2,
      cardY: L.followerY,
      cw: CARD.fw,
      ch: CARD.fh,
      role: roleOf(node),
      isCenter: false,
    };
  });
}

function nodeLayout(actorId) {
  const p = pos[actorId];
  if (!p || p.role === "client") return null;

  const isCenter = p.isCenter;
  const isLeader = p.role === "leader";
  const logW = p.cw - CARD.pad * 2;
  const logY = isCenter ? 62 : 44;
  const queueY = 28;
  const absLogY = p.cardY + logY;

  return {
    cx: p.x,
    cardX: p.cardX,
    cardY: p.cardY,
    cw: p.cw,
    ch: p.ch,
    logX: CARD.pad,
    logY,
    logW,
    logH: CARD.logH,
    logMidY: absLogY + CARD.logH / 2,
    queueX: CARD.pad,
    queueY,
    queueW: logW,
    queueH: CARD.queueH,
    queueMidY: p.cardY + queueY + CARD.queueH / 2,
    isLeader,
    isCenter,
    maxSlots: MAX_LOG_SLOTS,
  };
}

function anchorPoint(actorId, kind) {
  const r = nodeLayout(actorId);
  if (!r) {
    if (actorId === "client") {
      const x = pos.client?.x ?? L.centerX;
      const y = kind === "link-out" ? L.clientY - 14 : L.clientY;
      return { x, y };
    }
    return { x: L.centerX, y: L.clientY };
  }

  switch (kind) {
    case "queue":
      return { x: r.cx, y: r.queueMidY };
    case "link-out":
      return { x: r.cx, y: r.cardY };
    case "link-in":
      return { x: r.cx, y: r.cardY + r.ch };
    case "log-out":
      return { x: r.cx, y: r.logMidY };
    case "log-in":
      return { x: r.cx, y: r.logMidY };
    case "machine":
      return { x: r.cx, y: r.cardY + r.ch / 2 };
    default:
      return { x: r.cx, y: r.logMidY };
  }
}

function logCenter(actorId, index) {
  const r = nodeLayout(actorId);
  if (!r) return anchorPoint("client", "log");
  const vis = entryVisibleSlot(actorId, index);
  if (vis < 0) return anchorPoint(actorId, "log-out");
  const slotW = (r.logW - 8) / r.maxSlots;
  return {
    x: r.cardX + r.logX + 4 + vis * slotW + Math.min(ENTRY_W, slotW - 4) / 2,
    y: r.logMidY,
  };
}

function initStage() {
  if (stageReady) return;
  stageReady = true;
  ensureActor("client", "client");
  if (showcaseMode) markClientAwait();
}

function ensureActor(id, kind) {
  if (actors[id]) return actors[id];

  if (kind === "client") {
    const g = document.createElementNS(SVG_NS, "g");
    g.classList.add("actor", "client-actor");
    g.dataset.id = id;
    const body = document.createElementNS(SVG_NS, "rect");
    body.classList.add("client-body");
    body.setAttribute("x", -36);
    body.setAttribute("y", -12);
    body.setAttribute("width", 72);
    body.setAttribute("height", 24);
    body.setAttribute("rx", 12);
    g.appendChild(body);
    const label = document.createElementNS(SVG_NS, "text");
    label.classList.add("client-label");
    label.textContent = "client";
    g.appendChild(label);
    layerActors.appendChild(g);
    actors[id] = { g, body, kind: "client", label };
    return actors[id];
  }

  const nodeG = document.createElementNS(SVG_NS, "g");
  nodeG.classList.add("node-machine");
  nodeG.dataset.id = id;
  layerNodes.appendChild(nodeG);

  const machineBody = document.createElementNS(SVG_NS, "rect");
  machineBody.classList.add("machine-body");
  machineBody.setAttribute("width", CARD.fw);
  machineBody.setAttribute("height", CARD.fh);
  machineBody.setAttribute("rx", 6);
  nodeG.appendChild(machineBody);

  const nodeIdLabel = document.createElementNS(SVG_NS, "text");
  nodeIdLabel.classList.add("node-id-label");
  nodeIdLabel.textContent = id;
  nodeG.appendChild(nodeIdLabel);

  const roleBadge = document.createElementNS(SVG_NS, "text");
  roleBadge.classList.add("role-badge");
  nodeG.appendChild(roleBadge);

  const voteTally = document.createElementNS(SVG_NS, "text");
  voteTally.classList.add("vote-tally");
  nodeG.appendChild(voteTally);

  const queueG = document.createElementNS(SVG_NS, "g");
  queueG.classList.add("queue-group", "hidden");
  const qBg = document.createElementNS(SVG_NS, "rect");
  qBg.classList.add("rail-bg", "queue-rail");
  queueG.appendChild(qBg);
  nodeG.appendChild(queueG);

  const logSectionLabel = document.createElementNS(SVG_NS, "text");
  logSectionLabel.classList.add("log-section-label");
  logSectionLabel.textContent = "log";
  nodeG.appendChild(logSectionLabel);

  const commitZone = document.createElementNS(SVG_NS, "rect");
  commitZone.classList.add("commit-zone");
  nodeG.appendChild(commitZone);

  const mainRailBg = document.createElementNS(SVG_NS, "rect");
  mainRailBg.classList.add("rail-bg", "log-rail");
  nodeG.appendChild(mainRailBg);

  const ghostsG = document.createElementNS(SVG_NS, "g");
  ghostsG.classList.add("ghosts-group");
  nodeG.appendChild(ghostsG);

  const entriesG = document.createElementNS(SVG_NS, "g");
  entriesG.classList.add("entries-group");
  nodeG.appendChild(entriesG);

  const commitLine = document.createElementNS(SVG_NS, "line");
  commitLine.classList.add("commit-line");
  nodeG.appendChild(commitLine);

  actors[id] = {
    nodeG, machineBody, nodeIdLabel, roleBadge, voteTally,
    queueG, logSectionLabel, entriesG, ghostsG,
    commitLine, commitZone, mainRailBg,
    kind: "node", entries: new Map(),
  };
  if (showcaseMode) markAwaitReveal(id);
  return actors[id];
}

function roleLabel(role) {
  if (role === "leader") return "leader";
  if (role === "candidate") return "candidate";
  if (role === "offline") return "offline";
  return "follower";
}

function renderGhostSlots(id) {
  const a = actors[id];
  const r = nodeLayout(id);
  if (!a?.ghostsG || !r) return;
  a.ghostsG.innerHTML = "";

  const slotW = (r.logW - 8) / r.maxSlots;
  for (let i = 0; i < r.maxSlots; i++) {
    const ghost = document.createElementNS(SVG_NS, "rect");
    ghost.classList.add("slot-ghost");
    ghost.setAttribute("x", r.logX + 4 + i * slotW);
    ghost.setAttribute("y", r.logY + (r.logH - ENTRY_W + 2) / 2);
    ghost.setAttribute("width", slotW - 2);
    ghost.setAttribute("height", ENTRY_W - 2);
    ghost.setAttribute("rx", 2);
    a.ghostsG.appendChild(ghost);
  }
}

function updateFanConnectors() {
  layerBeams.innerHTML = "";

  const leader = getLeader(lastNodes);
  if (!leader) return;
  const lr = nodeLayout(leader.id);
  if (!lr) return;

  const fromX = lr.cx;
  const fromY = lr.cardY;

  lastNodes.forEach((n) => {
    if (n.id === leader.id || !n.running) return;
    const fr = nodeLayout(n.id);
    if (!fr) return;
    const toX = fr.cx;
    const toY = fr.cardY + fr.ch;
    const line = document.createElementNS(SVG_NS, "line");
    line.classList.add("fan-connector");
    line.setAttribute("x1", fromX);
    line.setAttribute("y1", fromY);
    line.setAttribute("x2", toX);
    line.setAttribute("y2", toY);
    layerBeams.appendChild(line);
  });
}

function flashBeam(toId) {
  const leader = getLeader(lastNodes);
  const lr = leader && nodeLayout(leader.id);
  const fr = nodeLayout(toId);
  if (!lr || !fr) return;
  const line = document.createElementNS(SVG_NS, "line");
  line.classList.add("replicate-beam");
  line.setAttribute("x1", lr.cx);
  line.setAttribute("y1", lr.cardY);
  line.setAttribute("x2", fr.cx);
  line.setAttribute("y2", fr.cardY + fr.ch);
  layerBeams.appendChild(line);
  requestAnimationFrame(() => line.classList.add("active"));
  setTimeout(() => line.remove(), 400);
}

function updateActorRails() {
  Object.keys(actors).forEach((id) => {
    const a = actors[id];
    const p = pos[id];
    if (!p) return;

    if (a.kind === "client") {
      a.g.setAttribute("transform", `translate(${p.x},${L.clientY})`);
      return;
    }

    const r = nodeLayout(id);
    if (!r) return;

    const role = p.role;
    const isLeader = role === "leader";
    a.nodeG.setAttribute("transform", `translate(${r.cardX},${r.cardY})`);

    a.machineBody.setAttribute("width", r.cw);
    a.machineBody.setAttribute("height", r.ch);

    a.machineBody.classList.remove("leader", "follower", "candidate", "offline", "center-node");
    a.machineBody.classList.add(
      isLeader ? "leader" : role === "candidate" ? "candidate" : role === "offline" ? "offline" : "follower"
    );
    if (r.isCenter) a.machineBody.classList.add("center-node");

    a.nodeIdLabel.setAttribute("x", CARD.pad);
    a.nodeIdLabel.setAttribute("y", 18);

    a.roleBadge.setAttribute("x", r.cw - CARD.pad);
    a.roleBadge.setAttribute("y", 18);
    a.roleBadge.textContent = roleLabel(role);
    a.roleBadge.classList.remove("leader", "follower", "candidate", "offline");
    a.roleBadge.classList.add(role);

    a.voteTally.setAttribute("x", r.cw / 2);
    a.voteTally.setAttribute("y", 28);

    const isCenter = r.isCenter;
    a.queueG.classList.toggle("hidden", !isCenter || !isLeader);
    a.logSectionLabel.setAttribute("x", CARD.pad);
    a.logSectionLabel.setAttribute("y", isCenter ? 50 : 34);

    if (isCenter && isLeader) {
      const qBg = a.queueG.querySelector("rect");
      qBg?.setAttribute("x", CARD.pad);
      qBg?.setAttribute("y", 26);
      qBg?.setAttribute("width", r.logW);
      qBg?.setAttribute("height", CARD.queueH);
      qBg?.setAttribute("rx", 3);
    }

    a.mainRailBg.setAttribute("x", CARD.pad);
    a.mainRailBg.setAttribute("y", r.logY);
    a.mainRailBg.setAttribute("width", r.logW);
    a.mainRailBg.setAttribute("height", CARD.logH);
    a.mainRailBg.setAttribute("rx", 4);
    a.mainRailBg.classList.toggle("leader-rail", isLeader);

    renderGhostSlots(id);

    const node = lastNodes.find((n) => n.id === id);
    const commitIdx = node?.commitIndex ?? -1;
    const slotW = (r.logW - 8) / r.maxSlots;
    const visCommit = entryVisibleSlot(id, commitIdx);
    const lineX = visCommit >= 0
      ? r.logX + 4 + (visCommit + 1) * slotW - 1
      : r.logX + 4 - 1;

    a.commitLine.setAttribute("x1", lineX);
    a.commitLine.setAttribute("x2", lineX);
    a.commitLine.setAttribute("y1", r.logY);
    a.commitLine.setAttribute("y2", r.logY + CARD.logH);

    a.commitZone.setAttribute("x", CARD.pad);
    a.commitZone.setAttribute("y", r.logY);
    a.commitZone.setAttribute("width", Math.max(0, lineX - CARD.pad));
    a.commitZone.setAttribute("height", CARD.logH);

    trimOldEntries(id);
    a.entries.forEach((el, idx) => {
      const vis = entryVisibleSlot(id, idx);
      if (vis < 0) return;
      const ex = r.logX + 4 + vis * slotW;
      const ey = r.logY + (r.logH - ENTRY_W + 2) / 2;
      el.setAttribute("transform", `translate(${ex},${ey})`);
    });

    const focused = Date.now() < focusUntil;
    a.nodeG.classList.toggle("dimmed", focused && focusIds.size > 0 && !focusIds.has(id));
  });

  updateFanConnectors();
  updateEngineHud();
}

function updateEngineHud() {
  const leader = getLeader(lastNodes);
  if (leader) {
    commitIndexDisplay.textContent = leader.commitIndex >= 0 ? String(leader.commitIndex) : "—";
  }
  const needed = quorumTrack.needed || majorityNeeded();
  const acks = quorumTrack.acks;
  quorumText.textContent = `${Math.min(acks, needed)} / ${needed}`;
  quorumFill.style.width = needed > 0 ? `${Math.min(100, (acks / needed) * 100)}%` : "0%";
}

function createEntryEl(index, committed, slotW) {
  const g = document.createElementNS(SVG_NS, "g");
  g.classList.add("log-entry-group");
  g.dataset.index = index;
  const w = Math.min(ENTRY_W, slotW - 4);
  const h = w - 2;
  const rect = document.createElementNS(SVG_NS, "rect");
  rect.classList.add("log-entry", committed ? "committed" : "uncommitted");
  rect.setAttribute("width", w);
  rect.setAttribute("height", h);
  rect.setAttribute("rx", 2);
  g.appendChild(rect);
  const txt = document.createElementNS(SVG_NS, "text");
  txt.classList.add("log-entry-index");
  txt.setAttribute("x", w / 2);
  txt.setAttribute("y", h / 2);
  txt.textContent = index;
  g.appendChild(txt);
  return g;
}

function mountEntry(actorId, index, committed = false, animate = true) {
  const a = actors[actorId];
  const r = nodeLayout(actorId);
  if (!a?.entriesG || !r) return;

  const vis = entryVisibleSlot(actorId, index);
  if (vis < 0) return;

  trimOldEntries(actorId);

  if (a.entries.has(index)) {
    if (committed) {
      const rect = a.entries.get(index).querySelector("rect.log-entry");
      rect?.classList.replace("uncommitted", "committed");
      rect?.classList.add("lock-in");
    }
    return;
  }
  const slotW = (r.logW - 8) / r.maxSlots;
  const x = r.logX + 4 + vis * slotW;
  const y = r.logY + (r.logH - ENTRY_W + 2) / 2;
  const g = createEntryEl(index, committed, slotW);
  g.setAttribute("transform", `translate(${x},${y}) scale(${animate ? 0.5 : 1})`);
  g.setAttribute("opacity", animate ? "0" : "1");
  a.entriesG.appendChild(g);
  a.entries.set(index, g);
  if (animate) {
    requestAnimationFrame(() => {
      g.setAttribute("transform", `translate(${x},${y}) scale(1)`);
      g.setAttribute("opacity", "1");
    });
  }
}

function pushQueue(label) {
  requestQueue.push(label);
  renderQueue();
}

function popQueue() {
  requestQueue.shift();
  renderQueue();
}

function renderQueue() {
  const leader = getLeader(lastNodes);
  if (!leader) return;
  const a = actors[leader.id];
  const r = nodeLayout(leader.id);
  if (!a?.queueG || !r) return;
  a.queueG.querySelectorAll("g.queue-item").forEach((el) => el.remove());
  requestQueue.slice(0, 4).forEach((label, i) => {
    const g = document.createElementNS(SVG_NS, "g");
    g.classList.add("queue-item");
    const chip = document.createElementNS(SVG_NS, "rect");
    chip.classList.add("queue-chip");
    chip.setAttribute("x", CARD.pad + 4 + i * 36);
    chip.setAttribute("y", 30);
    chip.setAttribute("width", 32);
    chip.setAttribute("height", 12);
    chip.setAttribute("rx", 2);
    g.appendChild(chip);
    const t = document.createElementNS(SVG_NS, "text");
    t.classList.add("queue-chip-label");
    t.setAttribute("x", CARD.pad + 20 + i * 36);
    t.setAttribute("y", 38);
    t.textContent = label;
    g.appendChild(t);
    a.queueG.appendChild(g);
  });
}

function setFocus(ids, ms = 900) {
  focusIds = new Set(ids.filter(Boolean));
  focusUntil = Date.now() + ms;
}

function showCallout(text, kind = "", ms = 900) {
  if (showcaseMode && kind !== "commit" && kind !== "leader") return;
  clearTimeout(calloutTimer);
  callout.textContent = text;
  callout.className = `callout visible ${kind}`.trim();
  calloutTimer = setTimeout(() => callout.classList.remove("visible"), ms);
}

function easeOut(u) {
  return 1 - Math.pow(1 - u, 3);
}

function animateAlong(from, to, el, trail, duration, onDone) {
  const start = performance.now();
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  function frame(now) {
    const u = Math.min((now - start) / duration, 1);
    const t = easeOut(u);
    const x = from.x + dx * t;
    const y = from.y + dy * t;
    el.setAttribute("transform", `translate(${x},${y})`);
    if (trail) {
      trail.setAttribute("x1", from.x);
      trail.setAttribute("y1", from.y);
      trail.setAttribute("x2", x);
      trail.setAttribute("y2", y);
    }
    if (u < 1) requestAnimationFrame(frame);
    else { el.remove(); trail?.remove(); onDone?.(); }
  }
  requestAnimationFrame(frame);
}

function flowEndpoints(from, to, type) {
  from = normalizeId(from);
  to = normalizeId(to);

  if (from === "client") {
    const tp = to === "client"
      ? anchorPoint("client", "log-in")
      : anchorPoint(to, "queue");
    return { from: anchorPoint("client", "link-out"), to: tp };
  }

  const leader = getLeader(lastNodes);
  const fromIsLeader = leader && from === leader.id;
  const toIsLeader = leader && to === leader.id;

  if (type === "entry" && fromIsLeader) {
    return { from: anchorPoint(from, "link-out"), to: anchorPoint(to, "link-in") };
  }
  if (type === "ack" && toIsLeader) {
    return { from: anchorPoint(from, "link-in"), to: anchorPoint(to, "link-out") };
  }
  if (type === "vote" || type === "vote-granted") {
    return { from: anchorPoint(from, "machine"), to: anchorPoint(to, "machine") };
  }

  return { from: anchorPoint(from, "log-out"), to: anchorPoint(to, "log-in") };
}

function spawnFlow(from, to, type, label, size, duration, onArrive) {
  from = normalizeId(from);
  to = normalizeId(to);
  if (!nodeAlive(to) || (!nodeAlive(from) && from !== "client")) return;

  if (showcaseMode && activeFlowCount >= MAX_SHOWCASE_FLOWS) {
    onArrive?.();
    return;
  }

  const tm = timing();
  const typeKey = type === "vote-granted" ? "vote" : type;
  const dur = duration ?? tm[typeKey] ?? 100;

  const { from: fp, to: tp } = flowEndpoints(from, to, type);

  const g = document.createElementNS(SVG_NS, "g");
  layerFlow.appendChild(g);

  const shape = document.createElementNS(SVG_NS, type === "entry" ? "rect" : "circle");
  shape.classList.add(`flow-${type}`);
  if (type === "entry") {
    shape.setAttribute("x", -ENTRY_W / 2);
    shape.setAttribute("y", -(ENTRY_W - 2) / 2);
    shape.setAttribute("width", ENTRY_W);
    shape.setAttribute("height", ENTRY_W - 2);
    shape.setAttribute("rx", 3);
  } else {
    shape.setAttribute("r", size || 4);
  }
  g.appendChild(shape);

  if (label) {
    const lw = label.length * 5 + 12;
    const labelOffset = (size || 5) + 14;
    const bg = document.createElementNS(SVG_NS, "rect");
    bg.classList.add("flow-label-bg");
    bg.setAttribute("x", -lw / 2);
    bg.setAttribute("y", labelOffset - 10);
    bg.setAttribute("width", lw);
    bg.setAttribute("height", 11);
    bg.setAttribute("rx", 3);
    g.appendChild(bg);
    const lbl = document.createElementNS(SVG_NS, "text");
    lbl.classList.add("flow-label");
    lbl.setAttribute("y", labelOffset - 2);
    lbl.textContent = label;
    g.appendChild(lbl);
  }

  const colors = {
    request: "#60a5fa", forward: "#2dd4bf", entry: "#94a3b8",
    ack: "#818cf8", vote: "#fb923c", "vote-granted": "#fcd34d",
  };
  const trail = document.createElementNS(SVG_NS, "line");
  trail.classList.add("flow-trail");
  trail.setAttribute("stroke", colors[type] || "#94a3b8");
  trail.setAttribute("stroke-width", type === "entry" ? 1.5 : 1);
  layerFlow.insertBefore(trail, g);

  activeFlowCount++;
  setFocus([from, to], dur + 150);
  animateAlong(fp, tp, g, trail, dur, () => {
    activeFlowCount = Math.max(0, activeFlowCount - 1);
    onArrive?.();
  });
}

function leaderProcess() {
  const leader = getLeader(lastNodes);
  if (!leader) return;
  const a = actors[leader.id];
  if (!a) return;
  a.mainRailBg?.classList.add("processing");
  popQueue();
  setTimeout(() => a.mainRailBg?.classList.remove("processing"), 280);
}

function resetQuorum(index) {
  quorumTrack = { index, acks: 1, needed: majorityNeeded() };
  updateEngineHud();
}

function onAck(event) {
  const leader = getLeader(lastNodes);
  if (!leader || event.to !== leader.id) return;
  const idx = Math.max(0, (leader.logLength || 1) - 1);
  if (quorumTrack.index !== idx) resetQuorum(idx);
  else quorumTrack.acks++;
  updateEngineHud();
}

function replicateEntries(event) {
  const leader = getLeader(lastNodes);
  if (!leader || event.from !== leader.id) return;
  const tm = timing();
  const count = event.entries || 1;
  const endIdx = Math.max(0, (leader.logLength || count) - 1);
  const startIdx = Math.max(0, endIdx - count + 1);
  resetQuorum(endIdx);
  flashBeam(event.to);

  if (showcaseMode) {
    const label = count > 1 ? `replicate ×${count}` : "replicate";
    spawnFlow(leader.id, event.to, "entry", label, ENTRY_W / 2, null, () => {
      for (let idx = startIdx; idx <= endIdx; idx++) {
        mountEntry(event.to, idx, false);
      }
    });
    return;
  }

  const stagger = tm.entryStagger;
  for (let i = 0; i < count; i++) {
    const idx = startIdx + i;
    setTimeout(() => {
      spawnFlow(leader.id, event.to, "entry", `#${idx}`, ENTRY_W / 2, null, () => {
        mountEntry(event.to, idx, false);
      });
    }, i * stagger);
  }
}

function syncLogsFromStatus(nodes) {
  nodes.forEach((n) => {
    if (!n.running || !n.reachable) return;
    if (showcaseMode && !revealedNodes.has(n.id)) return;
    const prev = prevLogLength[n.id] ?? 0;
    if (n.logLength > prev) {
      for (let i = prev; i < n.logLength; i++) {
        mountEntry(n.id, i, i <= n.commitIndex, n.state === 2 && i === n.logLength - 1);
      }
    }
    prevLogLength[n.id] = n.logLength;
    const prevC = prevCommitIndex[n.id] ?? -1;
    if (n.commitIndex > prevC) {
      for (let i = Math.max(0, prevC + 1); i <= n.commitIndex; i++) {
        mountEntry(n.id, i, true, false);
      }
    }
    prevCommitIndex[n.id] = n.commitIndex;
  });
}

function playCommit(index) {
  const tm = timing();
  const ids = lastNodes.filter((n) => n.running && n.reachable).map((n) => n.id);
  setFocus(ids, tm.commit + 200);
  if (showcaseMode) {
    showCallout("Committed", "commit", 1200);
  } else {
    showCallout(`Commit #${index}`, "commit", 900);
  }
  quorumTrack = { index: -1, acks: 0, needed: majorityNeeded() };
  updateEngineHud();

  const leader = getLeader(lastNodes);
  const lr = leader && nodeLayout(leader.id);
  const cx = lr ? lr.cx : L.centerX;
  const cy = lr ? lr.logMidY : L.leaderY + 60;

  const burst = document.createElementNS(SVG_NS, "circle");
  burst.classList.add("commit-burst");
  burst.setAttribute("cx", cx);
  burst.setAttribute("cy", cy);
  burst.setAttribute("r", 12);
  layerCommitFx.appendChild(burst);
  requestAnimationFrame(() => burst.classList.add("active"));
  setTimeout(() => burst.remove(), 600);

  const stagger = showcaseMode ? tm.commitStagger : 35;
  ids.forEach((id, i) => {
    setTimeout(() => {
      const a = actors[id];
      if (!a) return;
      a.commitZone.classList.remove("active");
      void a.commitZone.getBoundingClientRect();
      a.commitZone.classList.add("active");
      mountEntry(id, index, true, false);
    }, i * stagger);
  });
}

function updateVoteTally(id) {
  const a = actors[id];
  if (!a?.voteTally) return;
  const v = votesByCandidate[id];
  if (!v) { a.voteTally.classList.remove("visible"); return; }
  a.voteTally.textContent = `${v} votes`;
  a.voteTally.classList.add("visible");
}

function handleEvent(e) {
  if (!shouldAnimateEvent(e)) return;

  const from = normalizeId(e.from);
  const to = normalizeId(e.to);

  if (e.type === "state_change") {
    if (e.detail === "candidate") {
      votesByCandidate[from] = 1;
      updateVoteTally(from);
      setFocus([from], showcaseMode ? 2200 : 1000);
      if (!showcaseMode) showCallout("Election", "election", 800);
    }
    if (e.detail === "leader") onLeaderChange(from);
    if (e.detail === "follower") {
      votesByCandidate = {};
      lastNodes.forEach((n) => updateVoteTally(n.id));
    }
    return;
  }

  if (e.type === "commit") {
    playCommit(parseInt(e.detail, 10) || 0);
    return;
  }

  if (!nodeAlive(to) || (!nodeAlive(from) && from !== "client")) return;

  if (e.type === "client_request") {
    const leader = getLeader(lastNodes);
    const label = e.op === "put" ? "write" : "read";
    pushQueue(label);
    const targetIsLeader = leader && to === leader.id;
    spawnFlow("client", to, "request", showcaseMode ? "write" : label, 5, null, () => {
      if (targetIsLeader) leaderProcess();
    });
    return;
  }

  if (e.type === "forward_command") {
    if (showcaseMode) return;
    pushQueue("fwd");
    spawnFlow(from, to, "forward", null, 5, null, () => leaderProcess());
    return;
  }

  if (e.type === "append_entries" && e.entries > 0) {
    replicateEntries({ ...e, from, to });
    return;
  }

  if (e.type === "append_response" && e.entries > 0) {
    if (showcaseMode) {
      onAck({ ...e, from, to });
      return;
    }
    spawnFlow(from, to, "ack", "ack", 4, null, () => onAck({ ...e, from, to }));
    return;
  }

  if (e.type === "request_vote" && e.detail !== "granted") {
    spawnFlow(from, to, "vote", showcaseMode ? "vote" : "vote", 5, null);
    return;
  }

  if (e.type === "request_vote" && e.detail === "granted") {
    spawnFlow(from, to, "vote-granted", showcaseMode ? "granted" : "granted", 4, null, () => {
      votesByCandidate[to] = (votesByCandidate[to] || 1) + 1;
      updateVoteTally(to);
    });
  }
}

function onLeaderChange(newId) {
  if (!newId || newId === lastLeaderId) return;
  votesByCandidate = {};
  requestQueue = [];
  lastNodes.forEach((n) => updateVoteTally(n.id));

  if (showcaseMode) {
    if (!showcaseLeaderPrimed) {
      showcaseLeaderPrimed = true;
    } else {
      clearFlows();
      setFocus([newId], timing().vote + 400);
      showCallout("New leader", "leader", 1400);
    }
  } else {
    setFocus([newId], 1200);
    showCallout("Leader elected", "leader", 900);
  }
  lastLeaderId = newId;
}

function onTermChange(term) {
  if (term == null || term === lastTerm) return;
  termDisplay.textContent = term;
  termDisplay.classList.add("bump");
  setTimeout(() => termDisplay.classList.remove("bump"), 350);
  lastTerm = term;
}

function syncActorKind(n) {
  ensureActor(n.id, "node");
  if (pos[n.id]) pos[n.id].role = roleOf(n);
}

function syncActors(nodes) {
  initStage();
  layoutPositions(nodes);
  lastNodes = nodes;

  ensureActor("client", "client");
  nodes.forEach((n) => syncActorKind(n));

  const leader = getLeader(nodes);
  if (leader) {
    onLeaderChange(leader.id);
    onTermChange(leader.term);
  } else {
    lastLeaderId = null;
    const terms = nodes.filter((n) => n.running && n.reachable).map((n) => n.term);
    if (terms.length) onTermChange(Math.max(...terms));
  }

  nodes.forEach((n) => {
    const was = actors[n.id]?._alive;
    const alive = n.running && n.reachable;
    if (actors[n.id]) actors[n.id]._alive = alive;

    if (!showcaseMode) revealNode(n.id);

    if (was && !alive) {
      setFocus([n.id], showcaseMode ? 1800 : 1000);
      if (!showcaseMode) showCallout("Node failed", "fail", 700);
    }
    if (!was && alive && showcaseMode && revealedNodes.has(n.id)) {
      showCallout("Node rejoined", "leader", 1000);
    }
  });

  syncLogsFromStatus(nodes);
  syncShowcaseReveal(nodes);
  updateActorRails();
}

async function poll() {
  try {
    const [scenarioRes, statusRes, eventsRes] = await Promise.all([
      fetch("/api/scenario"),
      fetch("/api/cluster/status"),
      fetch("/api/cluster/events"),
    ]);

    const scenario = await scenarioRes.json();
    const status = await statusRes.json();
    const events = await eventsRes.json();

    initShowcase(scenario);

    const nodeCount = (status.nodes || []).length;
    const leaderNode = getLeader(status.nodes || []);
    const followerCount = nodeCount - (leaderNode ? 1 : 0);
    if (nodeCount > 0 && !showcaseMode) {
      const base = scenario.name || "Raft";
      scenarioName.textContent = `${base} · ${nodeCount} nodes · ${leaderNode ? "1 leader" : "electing"} · ${followerCount} followers`;
    } else {
      scenarioName.textContent = scenario.name || '';
    }

    stepDisplay.textContent = `${scenario.stepIndex + 1} / ${scenario.totalSteps || 1}`;
    if (!showcaseMode) {
      progressFill.style.width = scenario.done ? "100%" : `${Math.round((scenario.stepIndex / (scenario.totalSteps || 1)) * 100)}%`;
    }
    demoBadge.classList.toggle("hidden", showcaseMode || !scenario.demoPace);

    if (scenario.done) {
      statusChip.classList.remove("hidden");
      statusChip.textContent = scenario.error ? "failed" : "complete";
      statusChip.classList.toggle("error", !!scenario.error);
    }

    syncActors(status.nodes || []);

    (events.events || []).forEach((e) => {
      const key = `${e.from}:${e.seq}:${e.type}:${e.to}:${e.ts}:${e.detail || ""}`;
      if (seenEvents.has(key)) return;
      seenEvents.add(key);
      if (!shouldAnimateEvent(e)) return;
      try {
        handleEvent(e);
      } catch (err) {
        console.error("handleEvent", err, e);
      }
    });

    updateActorRails();
    updateShowcaseUI();
  } catch (err) {
    console.error(err);
  }
}

setInterval(poll, POLL_MS);
poll();
