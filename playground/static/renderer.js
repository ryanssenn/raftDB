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

function placeNode(el, p, bounds) {
  el.style.left = `${(p.x / bounds.width) * 100}%`;
  el.style.top = `${(p.y / bounds.height) * 100}%`;
  el.style.setProperty("--scale", String(p.scale || 1));
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
    this.onAction = null;

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
    this.leaderId = leader?.id ?? null;

    if (layoutChanged) {
      this._syncBeams(leader, nodes, pos);
      this._syncClientBeam(leader, pos);
      this._syncClient(pos, bounds);
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

      const role = nodeRole(node);
      const part = isolated.has(node.id);
      const dSig = dataSignature(node);

      if (layoutChanged || card._lastRole !== role || card._part !== part) {
        placeNode(card, p, bounds);
        card.classList.toggle("offline", !node.running);
        card.classList.toggle("leader", role === "leader");
        card.classList.toggle("partitioned", part);
        card.dataset.role = role;

        const roleEl = card.querySelector(".node-role");
        roleEl.textContent = part ? "Isolated" : roleStyles[role].badge;

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
        card.querySelector(".node-term").textContent = node.running
          ? `term ${node.term ?? "—"}`
          : "stopped";
        card.querySelector(".node-commit").textContent = node.running
          ? `commit ${node.commitIndex ?? "—"}`
          : "Start to recover";
        this.dataSigs[node.id] = dSig;
      }
    }
  }

  setClientActive(active) {
    this.layers.client?.classList.toggle("active", active);
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
        stroke: "#3b82f6",
        "stroke-width": "1",
        "stroke-opacity": "0.25",
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
            stroke: "#52525b",
            "stroke-width": "1.5",
            class: "beam",
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
          <span class="node-term">term —</span>
          <span class="node-commit">commit —</span>
        </div>
        <div class="node-actions running">
          <button type="button" class="node-action stop" data-action="stop">Stop node</button>
          <button type="button" class="node-action start" data-action="start">Start node</button>
        </div>
      </div>
    `;
    return card;
  }
}

function cardRadius(scale) {
  return 44 * (scale || 1);
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
