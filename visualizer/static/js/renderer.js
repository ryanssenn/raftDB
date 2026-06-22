const SVG_NS = "http://www.w3.org/2000/svg";

const CARD = { w: 150, h: 110, logW: 130, logH: 22 };

export function createLayers() {
  return {
    beams: document.getElementById("layer-beams"),
    nodes: document.getElementById("layer-nodes"),
    flow: document.getElementById("layer-flow"),
    actors: document.getElementById("layer-actors"),
    fx: document.getElementById("layer-fx"),
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

const roleColors = {
  follower: "#6b7280",
  candidate: "#ffd166",
  leader: "#58c4dd",
  offline: "#4b5563",
};

export class Renderer {
  constructor(layers) {
    this.layers = layers;
    this.nodeEls = {};
    this.clientEls = {};
    this.prevLogLen = {};
    this.prevCommit = {};
  }

  syncNodes(nodes, pos) {
    const ids = new Set(nodes.map((n) => n.id));
    for (const id of Object.keys(this.nodeEls)) {
      if (!ids.has(id)) {
        this.nodeEls[id].g.remove();
        delete this.nodeEls[id];
      }
    }

    for (const node of nodes) {
      const p = pos[node.id];
      if (!p) continue;
      let entry = this.nodeEls[node.id];
      if (!entry) {
        entry = this._createNodeCard(node.id);
        this.nodeEls[node.id] = entry;
        this.layers.nodes.appendChild(entry.g);
      }
      this._updateNodeCard(entry, node, p);
    }
  }

  syncClients(clients, pos) {
    for (const id of Object.keys(this.clientEls)) {
      if (!clients.includes(id)) {
        this.clientEls[id].g.remove();
        delete this.clientEls[id];
      }
    }
    for (const id of clients) {
      const p = pos[id];
      if (!p) continue;
      let entry = this.clientEls[id];
      if (!entry) {
        entry = this._createClient(id, p.color);
        this.clientEls[id] = entry;
        this.layers.actors.appendChild(entry.g);
      }
      entry.g.setAttribute("transform", `translate(${p.x - 40}, ${p.y - 20})`);
    }
  }

  _createNodeCard(id) {
    const g = el("g", { class: "node-card" });
    const bg = el("rect", {
      class: "card-bg",
      x: -CARD.w / 2,
      y: -CARD.h / 2,
      width: CARD.w,
      height: CARD.h,
      rx: 8,
    }, g);
    const roleBadge = el("text", { class: "role-badge", x: -CARD.w / 2 + 10, y: -CARD.h / 2 + 16, fill: "#6b7280" }, g);
    roleBadge.textContent = "follower";
    const nodeId = el("text", { class: "node-id", x: 0, y: -8, "text-anchor": "middle" }, g);
    nodeId.textContent = id;
    const termText = el("text", { x: 0, y: 8, "text-anchor": "middle", fill: "#7a7d94", "font-size": "10" }, g);
    termText.textContent = "term -";
    const logG = el("g", { class: "log-strip", transform: `translate(${-CARD.logW / 2}, 18)` }, g);
    const slots = [];
    for (let i = 0; i < 5; i++) {
      const slot = el("rect", {
        class: "log-slot",
        x: i * 27,
        y: 0,
        width: 24,
        height: CARD.logH,
        rx: 3,
        fill: "url(#entryGrad)",
        opacity: 0.3,
      }, logG);
      slots.push(slot);
    }
    const commitZone = el("rect", {
      x: -CARD.w / 2 + 4,
      y: CARD.h / 2 - 8,
      width: CARD.w - 8,
      height: 4,
      rx: 2,
      fill: "#95d5b2",
      opacity: 0,
    }, g);
    return { g, bg, roleBadge, nodeId, termText, slots, commitZone, voteTally: null };
  }

  _createClient(id, color) {
    const g = el("g", { class: "client-actor" });
    el("rect", {
      class: "client-bg",
      width: 80,
      height: 36,
      rx: 8,
      stroke: color || "#58c4dd",
    }, g);
    el("text", {
      class: "client-label",
      x: 40,
      y: 22,
      "text-anchor": "middle",
      fill: color || "#58c4dd",
    }, g).textContent = id;
    return { g };
  }

  _updateNodeCard(entry, node, pos) {
    const { g, roleBadge, termText, slots, commitZone } = entry;
    const role = node.running === false ? "offline" : (node.stateName || ["follower", "candidate", "leader"][node.state] || "follower");
    g.setAttribute("transform", `translate(${pos.x}, ${pos.y})`);
    g.classList.toggle("offline", !node.running);
    g.classList.toggle("leader", role === "leader");
    g.classList.toggle("candidate", role === "candidate");
    roleBadge.textContent = role;
    roleBadge.setAttribute("fill", roleColors[role] || roleColors.follower);
    termText.textContent = node.running ? `term ${node.term ?? "-"}` : "offline";

    const logLen = node.logLength ?? 0;
    const commit = node.commitIndex ?? -1;
    for (let i = 0; i < 5; i++) {
      const idx = logLen - 5 + i;
      const slot = slots[i];
      if (idx >= 0 && idx <= commit) {
        slot.setAttribute("fill", "url(#committedGrad)");
        slot.setAttribute("opacity", "1");
      } else if (idx >= 0) {
        slot.setAttribute("fill", "url(#entryGrad)");
        slot.setAttribute("opacity", "0.85");
      } else {
        slot.setAttribute("opacity", "0.2");
      }
    }

    if (commit > (this.prevCommit[node.id] ?? -1)) {
      commitZone.setAttribute("opacity", "0.8");
      setTimeout(() => commitZone.setAttribute("opacity", "0"), 600);
    }
    this.prevCommit[node.id] = commit;
    this.prevLogLen[node.id] = logLen;
  }

  getAnchor(id, pos) {
    const p = pos[id];
    if (!p) return null;
    if (p.role === "client") return { x: p.x, y: p.y - 20 };
    return { x: p.x, y: p.y + CARD.h / 2 - 10 };
  }

  getNodeCenter(id, pos) {
    const p = pos[id];
    return p ? { x: p.x, y: p.y } : null;
  }
}

export { SVG_NS, CARD };
