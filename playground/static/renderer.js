const SVG_NS = "http://www.w3.org/2000/svg";

export function createLayers() {
  return {
    beams: document.getElementById("layer-beams"),
    nodes: document.getElementById("node-layer"),
    client: document.getElementById("client-widget"),
  };
}

function el(tag, attrs = {}, parent) {
  const node = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "text") node.textContent = v;
    else node.setAttribute(k, v);
  }
  if (parent) parent.appendChild(node);
  return node;
}

const roleStyles = {
  follower: { stroke: "#3f3f46", badge: "Follower" },
  candidate: { stroke: "#ca8a04", badge: "Candidate" },
  leader: { stroke: "#2563eb", badge: "Leader" },
  offline: { stroke: "#27272a", badge: "Offline" },
  idle: { stroke: "#27272a", badge: "Idle" },
};

function nodeRole(node) {
  if (!node.running) return "offline";
  return node.stateName || ["follower", "candidate", "leader"][node.state] || "follower";
}

function layoutSignature(nodes, partitionNodes) {
  return nodes.map((n) => [n.id, n.running, n.state, n.stateName].join(",")).join(";")
    + "|" + (partitionNodes || []).join(",");
}

function dataSignature(node) {
  return [node.term, node.commitIndex, node.logLength].join(",");
}

function placeNode(el, p, bounds, animate = true) {
  if (!animate) el.style.transition = "none";
  el.style.left = `${(p.x / bounds.width) * 100}%`;
  el.style.top = `${(p.y / bounds.height) * 100}%`;
  el.style.setProperty("--scale", String(p.scale || 1));
  if (!animate) {
    void el.offsetWidth;
    el.style.transition = "";
  }
}

export class Renderer {
  constructor(layers) {
    this.layers = layers;
    this.nodeEls = {};
    this.beamEls = {};
    this.dataSigs = {};
    this.lastLayoutSig = "";
    this.lastBoundsSig = "";
    this.lastPos = {};
    this.lastBounds = null;
    this.leaderId = null;
    this._prevLeaderId = null;
    this.onAction = null;
    this.onLeaderChange = null;
    this.leaderSlotEl = document.getElementById("leader-slot");

    this.layers.nodes?.addEventListener("click", (e) => {
      const btn = e.target.closest("[data-action]");
      if (!btn || btn.disabled) return;
      const card = btn.closest("[data-node]");
      if (!card) return;
      e.stopPropagation();
      this.onAction?.(card.dataset.node, btn.dataset.action);
    });
  }

  setActionHandler(fn) {
    this.onAction = fn;
  }

  invalidateLayout() {
    this.lastLayoutSig = "";
    this.lastBoundsSig = "";
  }

  syncNodes(nodes, pos, partitionNodes = [], bounds) {
    this.lastPos = pos;
    this.lastBounds = bounds;
    const isolated = new Set(partitionNodes || []);
    const ids = new Set(nodes.map((n) => n.id));
    const boundsSig = `${bounds?.width ?? 0}x${bounds?.height ?? 0}`;
    const layoutSig = layoutSignature(nodes, partitionNodes);
    const layoutChanged = layoutSig !== this.lastLayoutSig || boundsSig !== this.lastBoundsSig;
    if (layoutChanged) {
      this.lastLayoutSig = layoutSig;
      this.lastBoundsSig = boundsSig;
    }

    for (const id of Object.keys(this.nodeEls)) {
      if (!ids.has(id)) {
        this.nodeEls[id].remove();
        delete this.nodeEls[id];
        delete this.dataSigs[id];
      }
    }

    const leader = nodes.find((n) => n.running && (n.state === 2 || n.stateName === "leader"));
    const prevLeader = this.leaderId;
    this.leaderId = leader?.id ?? null;

    if (prevLeader !== this.leaderId) {
      this._handleLeaderChange(prevLeader, this.leaderId);
    }

    if (layoutChanged || prevLeader !== this.leaderId) {
      this._syncBeams(leader, nodes, pos);
      this._syncClientBeam(leader, pos);
      this._syncClient(pos, bounds);
      this._syncLeaderSlot(pos, bounds, Boolean(leader));
    }

    for (const node of nodes) {
      const p = pos[node.id];
      if (!p) continue;

      let card = this.nodeEls[node.id];
      if (!card) {
        card = this._createNodeCard(node.id);
        this.nodeEls[node.id] = card;
        this.layers.nodes.appendChild(card);
      }

      const idle = !node.running && !this.clusterStarted;
      const role = idle ? "idle" : nodeRole(node);
      const part = isolated.has(node.id);
      const dSig = dataSignature(node) + "|" + idle;

      const moved = card._lastX !== p.x || card._lastY !== p.y;
      if (layoutChanged || card._lastRole !== role || card._part !== part || moved) {
        const animate = card._placed !== false;
        if (!card._placed) {
          card._placed = true;
          placeNode(card, p, bounds, false);
        } else {
          placeNode(card, p, bounds, animate);
        }
        card._lastX = p.x;
        card._lastY = p.y;
        card.classList.toggle("offline", !node.running && !idle);
        card.classList.toggle("idle", idle);
        card.classList.toggle("leader", role === "leader");
        card.classList.toggle("partitioned", part);
        card.dataset.role = role;

        const roleEl = card.querySelector(".node-role");
        roleEl.textContent = part
          ? "Isolated"
          : (roleStyles[role]?.badge ?? "Idle");

        const stopBtn = card.querySelector('[data-action="stop"]');
        const startBtn = card.querySelector('[data-action="start"]');
        const actions = card.querySelector(".node-actions");
        actions.classList.toggle("running", node.running);
        actions.classList.toggle("stopped", !node.running);
        stopBtn.disabled = !node.running;
        startBtn.disabled = node.running;

        card._lastRole = role;
        card._part = part;
      }

      if (layoutChanged || this.dataSigs[node.id] !== dSig) {
        const prevCommit = card._lastCommit ?? -1;
        const commit = node.commitIndex ?? -1;
        const idleNow = !node.running && !this.clusterStarted;
        card.querySelector(".node-term").textContent = node.running
          ? `term ${node.term ?? "-"}`
          : idleNow ? "not started" : "crashed";
        card.querySelector(".node-commit").textContent = node.running
          ? `commit ${node.commitIndex ?? "-"}`
          : idleNow ? "" : "Start to recover";
        if (node.running && commit > prevCommit && prevCommit >= 0) {
          card.classList.add("commit-flash");
          clearTimeout(card._flashTimer);
          card._flashTimer = setTimeout(() => card.classList.remove("commit-flash"), 350);
        }
        card._lastCommit = commit;
        this.dataSigs[node.id] = dSig;
      }
    }
  }

  setClientActive(active) {
    this.layers.client?.classList.toggle("active", active);
    document.getElementById("topology-svg")?.classList.toggle("beams-active", active);
  }

  _handleLeaderChange(from, to) {
    if (from && this.nodeEls[from]) {
      const card = this.nodeEls[from];
      card.classList.add("leader-outgoing");
      clearTimeout(card._outTimer);
      card._outTimer = setTimeout(() => card.classList.remove("leader-outgoing"), 950);
    }
    if (to && this.nodeEls[to]) {
      const card = this.nodeEls[to];
      card.classList.add("leader-incoming");
      clearTimeout(card._inTimer);
      card._inTimer = setTimeout(() => card.classList.remove("leader-incoming"), 1200);
    }
    if (from != null && to && from !== to) {
      this.onLeaderChange?.(from, to);
    }
    this._prevLeaderId = to;
  }

  _syncLeaderSlot(pos, bounds, hasLeader) {
    const slot = this.leaderSlotEl;
    if (!slot || !pos._leaderSlot) return;
    slot.classList.remove("hidden");
    slot.classList.toggle("filled", hasLeader);
    slot.classList.toggle("electing", !hasLeader && this.clusterStarted);
    const animate = slot._placed !== false;
    if (!slot._placed) {
      slot._placed = true;
      placeNode(slot, pos._leaderSlot, bounds, false);
    } else {
      placeNode(slot, pos._leaderSlot, bounds, animate);
    }
  }

  _syncClientBeam(leader, pos) {
    const key = "client->leader";
    if (!leader || !pos.client || !pos[leader.id]) {
      if (this.beamEls[key]) {
        this.beamEls[key].remove();
        delete this.beamEls[key];
      }
      return;
    }
    const from = pos.client;
    const to = pos[leader.id];
    const d = clientBeamPath(from, to);
    let beam = this.beamEls[key];
    if (!beam) {
      beam = el("path", {
        fill: "none",
        stroke: "#5e8fb5",
        "stroke-width": "1",
        "stroke-opacity": "0.2",
        "stroke-dasharray": "4 6",
        class: "beam client-beam",
      }, this.layers.beams);
      this.beamEls[key] = beam;
    }
    if (beam.getAttribute("d") !== d) beam.setAttribute("d", d);
  }

  _syncClient(pos, bounds) {
    const el = this.layers.client;
    if (!el || !pos.client) return;
    placeNode(el, pos.client, bounds);
  }

  _syncBeams(leader, nodes, pos) {
    const beamKeys = new Set();
    if (leader && pos[leader.id]) {
      for (const node of nodes) {
        if (!node.running || node.id === leader.id || !pos[node.id]) continue;
        const key = `${leader.id}->${node.id}`;
        beamKeys.add(key);
        const d = beamPath(pos[leader.id], pos[node.id]);
        let beam = this.beamEls[key];
        if (!beam) {
          beam = el("path", {
            fill: "none",
            stroke: "#3a3a40",
            "stroke-width": "1",
            class: "beam repl-beam",
          }, this.layers.beams);
          this.beamEls[key] = beam;
        }
        if (beam.getAttribute("d") !== d) beam.setAttribute("d", d);
      }
    }
    for (const key of Object.keys(this.beamEls)) {
      if (key === "client->leader") continue;
      if (!beamKeys.has(key)) {
        this.beamEls[key].remove();
        delete this.beamEls[key];
      }
    }
  }

  _createNodeCard(id) {
    const card = document.createElement("div");
    card.className = "node-card";
    card.dataset.node = id;
    card.innerHTML = `
      <div class="node-card-inner">
        <div class="node-head">
          <span class="node-role">Follower</span>
          <span class="node-id">${id}</span>
        </div>
        <div class="node-meta">
          <span class="node-term">term -</span>
          <span class="node-commit">commit -</span>
        </div>
        <div class="node-writes-wrap">
          <div class="node-writes-head">
            <span class="node-writes-title">Recent writes</span>
            <span class="node-sync-badge"></span>
          </div>
          <ul class="node-writes"></ul>
        </div>
        <div class="node-actions running">
          <button type="button" class="node-action stop" data-action="stop">Crash node</button>
          <button type="button" class="node-action start" data-action="start">Start node</button>
        </div>
      </div>
    `;
    return card;
  }

  /**
   * Render each node's recent writes and animate a node catching up after it
   * restarts. `logsByNode` maps node id -> { running, commitIndex, logLength, entries }.
   */
  updateLogs(logsByNode) {
    const infos = Object.values(logsByNode || {});
    const leaderCommit = infos.reduce(
      (m, n) => (n.running ? Math.max(m, n.commitIndex ?? -1) : m),
      -1
    );

    for (const id of Object.keys(this.nodeEls)) {
      const card = this.nodeEls[id];
      const info = logsByNode?.[id];
      const listEl = card.querySelector(".node-writes");
      const badge = card.querySelector(".node-sync-badge");
      if (!listEl) continue;

      if (!info || (!info.running && (!card._writes || card._writes.length === 0))) {
        listEl.innerHTML = `<li class="node-write empty">-</li>`;
        card._writes = card._writes || [];
        if (badge) badge.textContent = "";
        continue;
      }

      if (!info.running) {
        // Freeze the last-known writes and mark them stale instead of clearing.
        card.classList.add("writes-stale");
        if (badge) {
          badge.textContent = "stale";
          badge.className = "node-sync-badge stale";
        }
        continue;
      }

      card.classList.remove("writes-stale");

      const entries = (info.entries || []).slice(-4);
      const prevKeys = new Set(card._writeKeys || []);
      const commit = info.commitIndex ?? -1;

      const html = entries
        .map((e) => {
          const committed = e.index <= commit;
          const fresh = !prevKeys.has(e.index);
          const val = e.value ? `=${e.value}` : "";
          const cls = [
            "node-write",
            committed ? "committed" : "pending",
            fresh && card._everPopulated ? "entry-in" : "",
          ].join(" ").trim();
          return `<li class="${cls}"><span class="w-idx">#${e.index}</span>` +
            `<span class="w-key">${escapeHtml(e.key)}${escapeHtml(val)}</span></li>`;
        })
        .join("");
      listEl.innerHTML = html || `<li class="node-write empty">no writes yet</li>`;

      card._writeKeys = entries.map((e) => e.index);
      card._writes = entries;
      card._everPopulated = true;

      const behind = leaderCommit - commit;
      const wasBehind = card._behind === true;
      const nowBehind = behind > 1 && leaderCommit >= 0;

      if (badge) {
        if (nowBehind) {
          badge.textContent = `behind ${behind}`;
          badge.className = "node-sync-badge behind";
        } else if (wasBehind) {
          badge.textContent = "synced";
          badge.className = "node-sync-badge synced";
          card.classList.add("catchup-flash");
          clearTimeout(card._catchupTimer);
          card._catchupTimer = setTimeout(() => {
            card.classList.remove("catchup-flash");
            badge.textContent = "";
            badge.className = "node-sync-badge";
          }, 1400);
        } else if (!card._catchupTimer) {
          badge.textContent = "";
          badge.className = "node-sync-badge";
        }
      }
      card._behind = nowBehind;
    }
  }
}

function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"]/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])
  );
}

function cardRadius(scale) {
  return 52 * (scale || 1);
}

function beamPath(from, to) {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const dist = Math.hypot(dx, dy) || 1;
  const nx = dx / dist;
  const ny = dy / dist;
  const fromR = cardRadius(from.scale);
  const toR = cardRadius(to.scale);
  const x1 = from.x + nx * fromR;
  const y1 = from.y + ny * fromR;
  const x2 = to.x - nx * toR;
  const y2 = to.y - ny * toR;
  const mx = (x1 + x2) / 2 - ny * 24;
  const my = (y1 + y2) / 2 + nx * 24;
  return `M ${x1} ${y1} Q ${mx} ${my} ${x2} ${y2}`;
}

function clientBeamPath(from, to) {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const dist = Math.hypot(dx, dy) || 1;
  const nx = dx / dist;
  const ny = dy / dist;
  const x1 = from.x + cardRadius(from.scale) * 0.85;
  const y1 = from.y;
  const x2 = to.x - nx * cardRadius(to.scale);
  const y2 = to.y - ny * cardRadius(to.scale);
  return `M ${x1} ${y1} L ${x2} ${y2}`;
}

export { SVG_NS };
