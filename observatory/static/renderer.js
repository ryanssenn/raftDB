const SVG_NS = "http://www.w3.org/2000/svg";

const BASE_CARD = { w: 152, h: 108, logW: 128, logH: 22 };

export function createLayers() {
  return {
    beams: document.getElementById("layer-beams"),
    nodes: document.getElementById("layer-nodes"),
    flow: document.getElementById("layer-flow"),
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

const roleStyles = {
  follower: { stroke: "url(#followerRing)", badge: "#94a3b8", glow: "none" },
  candidate: { stroke: "url(#candidateRing)", badge: "#fcd34d", glow: "url(#candidateGlow)" },
  leader: { stroke: "url(#leaderRing)", badge: "#67e8f9", glow: "url(#leaderGlow)" },
  offline: { stroke: "#3f3f46", badge: "#71717a", glow: "none" },
};

export class Renderer {
  constructor(layers) {
    this.layers = layers;
    this.nodeEls = {};
    this.clientEl = null;
    this.prevCommit = {};
    this.prevLeader = null;
  }

  syncNodes(nodes, pos, partitionNodes = [], demoActive = false) {
    const isolated = new Set(partitionNodes || []);
    const ids = new Set(nodes.map((n) => n.id));
    for (const id of Object.keys(this.nodeEls)) {
      if (!ids.has(id)) {
        this.nodeEls[id].g.remove();
        delete this.nodeEls[id];
      }
    }

    this._syncClient(pos.client, demoActive);

    this.layers.beams.innerHTML = "";
    const leader = nodes.find((n) => n.running && (n.state === 2 || n.stateName === "leader"));
    if (leader && pos[leader.id]) {
      for (const node of nodes) {
        if (!node.running || node.id === leader.id || !pos[node.id]) continue;
        const beam = el("path", {
          d: beamPath(pos[leader.id], pos[node.id]),
          fill: "none",
          stroke: "url(#beamGrad)",
          "stroke-width": "2",
          opacity: "0.55",
          class: "beam",
        }, this.layers.beams);
        beam.dataset.from = leader.id;
        beam.dataset.to = node.id;
      }
    }

    const leaderId = leader?.id ?? null;
    if (leaderId && this.prevLeader && leaderId !== this.prevLeader) {
      this._leaderChangePulse(nodes, pos);
    }
    this.prevLeader = leaderId;

    for (const node of nodes) {
      const p = pos[node.id];
      if (!p) continue;
      let entry = this.nodeEls[node.id];
      if (!entry) {
        entry = this._createNodeCard(node.id);
        this.nodeEls[node.id] = entry;
        this.layers.nodes.appendChild(entry.g);
      }
      this._updateNodeCard(entry, node, p, isolated.has(node.id));
    }
  }

  getAnchor(id, pos) {
    if (id === "client") {
      const p = pos.client;
      return p ? { x: p.x, y: p.y - 22 } : null;
    }
    const p = pos[id];
    if (!p) return null;
    return { x: p.x, y: p.y + (p.scale || 1) * 46 };
  }

  getNodeCenter(id, pos) {
    if (id === "client") {
      const p = pos.client;
      return p ? { x: p.x, y: p.y } : null;
    }
    const p = pos[id];
    return p ? { x: p.x, y: p.y } : null;
  }

  pulseCommitBeams(leaderId, followerIds) {
    for (const fid of followerIds) {
      const beam = this.layers.beams.querySelector(`path[data-from="${leaderId}"][data-to="${fid}"]`);
      if (beam) {
        beam.classList.remove("beam-commit");
        void beam.offsetWidth;
        beam.classList.add("beam-commit");
      }
    }
  }

  _syncClient(clientPos, visible) {
    if (!clientPos || !visible) {
      if (this.clientEl) {
        this.clientEl.g.remove();
        this.clientEl = null;
      }
      return;
    }
    if (!this.clientEl) {
      const g = el("g", { class: "client-actor" });
      const shadow = el("ellipse", {
        class: "client-shadow",
        rx: 58,
        ry: 26,
        fill: "url(#clientGrad)",
        opacity: 0.35,
      }, g);
      const body = el("rect", {
        class: "client-body",
        x: -58,
        y: -26,
        width: 116,
        height: 52,
        rx: 26,
        fill: "url(#clientGrad)",
        stroke: "url(#clientStroke)",
        "stroke-width": 1.5,
      }, g);
      const dot = el("circle", { r: 4, fill: "#22d3ee", cx: -38, cy: 0 }, g);
      dot.setAttribute("filter", "url(#packetGlow)");
      const label = el("text", {
        "text-anchor": "start",
        fill: "#e2e8f0",
        "font-size": "12",
        "font-weight": "600",
        text: "Client",
      }, g);
      label.setAttribute("x", -26);
      label.setAttribute("y", 4);
      const sub = el("text", {
        "text-anchor": "start",
        fill: "#64748b",
        "font-size": "9",
        text: "write path",
      }, g);
      sub.setAttribute("x", -26);
      sub.setAttribute("y", 16);
      this.layers.nodes.appendChild(g);
      this.clientEl = { g, shadow, body, label };
    }
    this.clientEl.g.setAttribute("transform", `translate(${clientPos.x}, ${clientPos.y})`);
  }

  _leaderChangePulse(nodes, pos) {
    for (const node of nodes) {
      const p = pos[node.id];
      if (!p) continue;
      const fx = el("circle", {
        cx: p.x,
        cy: p.y,
        r: (p.scale || 1) * 75,
        fill: "none",
        stroke: "url(#candidateRing)",
        "stroke-width": 2,
        opacity: 0.8,
        class: "state-change-pulse",
      }, this.layers.fx);
      requestAnimationFrame(() => {
        fx.setAttribute("r", (p.scale || 1) * 105);
        fx.setAttribute("opacity", "0");
      });
      setTimeout(() => fx.remove(), 750);
    }
  }

  _cardDims(scale) {
    const s = scale || 1;
    return {
      w: BASE_CARD.w * s,
      h: BASE_CARD.h * s,
      logW: BASE_CARD.logW * s,
      logH: BASE_CARD.logH * s,
    };
  }

  _createNodeCard(id) {
    const g = el("g", { class: "node-card" });
    g.style.transition = "transform 0.5s cubic-bezier(0.22, 1, 0.36, 1)";

    const outerRing = el("rect", {
      class: "outer-ring",
      rx: 16,
      fill: "none",
      "stroke-width": 2,
      opacity: 0.9,
    }, g);
    const bg = el("rect", { class: "card-bg", rx: 14 }, g);
    const shine = el("rect", { class: "card-shine", rx: 14, opacity: 0.35 }, g);

    const badgeBg = el("rect", { class: "role-pill", rx: 8, height: 16, width: 54, opacity: 0.9 }, g);
    const roleBadge = el("text", {
      class: "role-badge",
      fill: "#94a3b8",
      "font-size": "9",
      "font-weight": "600",
      "letter-spacing": "0.06em",
    }, g);
    roleBadge.textContent = "FOLLOWER";

    const nodeId = el("text", {
      class: "node-id",
      "text-anchor": "middle",
      fill: "#f8fafc",
      "font-size": "15",
      "font-weight": "700",
      "letter-spacing": "-0.02em",
    }, g);
    nodeId.textContent = id;

    const metaG = el("g", { class: "node-meta" }, g);
    const termText = el("text", { fill: "#64748b", "font-size": "10", "font-family": "JetBrains Mono, monospace" }, metaG);
    termText.textContent = "term —";
    const commitText = el("text", { fill: "#64748b", "font-size": "10", "font-family": "JetBrains Mono, monospace" }, metaG);
    commitText.textContent = "commit —";

    const logLabel = el("text", {
      fill: "#52525b",
      "font-size": "8",
      "letter-spacing": "0.08em",
      text: "LOG",
    }, g);

    const logG = el("g", { class: "log-strip" }, g);
    const slots = [];
    for (let i = 0; i < 5; i++) {
      slots.push(el("rect", { class: "log-slot", rx: 4, fill: "url(#entryGrad)", opacity: 0.25 }, logG));
    }

    const commitBar = el("rect", { class: "commit-bar", rx: 2, fill: "url(#committedGrad)", opacity: 0 }, g);
    const pulse = el("circle", { r: 0, fill: "none", stroke: "#38bdf8", "stroke-width": 2, opacity: 0 }, g);

    return {
      g, outerRing, bg, shine, badgeBg, roleBadge, nodeId, termText, commitText,
      logLabel, logG, slots, commitBar, pulse,
    };
  }

  _updateNodeCard(entry, node, pos, partitioned) {
    const {
      g, outerRing, bg, shine, badgeBg, roleBadge, nodeId, termText, commitText,
      logLabel, logG, slots, commitBar, pulse,
    } = entry;
    const card = this._cardDims(pos.scale);
    const role = node.running === false
      ? "offline"
      : (node.stateName || ["follower", "candidate", "leader"][node.state] || "follower");
    const style = roleStyles[role] || roleStyles.follower;

    g.setAttribute("transform", `translate(${pos.x}, ${pos.y})`);
    g.classList.toggle("offline", !node.running);
    g.classList.toggle("leader", role === "leader");
    g.classList.toggle("candidate", role === "candidate");
    g.classList.toggle("partitioned", partitioned);

    const pad = 6;
    outerRing.setAttribute("x", -card.w / 2 - pad);
    outerRing.setAttribute("y", -card.h / 2 - pad);
    outerRing.setAttribute("width", card.w + pad * 2);
    outerRing.setAttribute("height", card.h + pad * 2);
    outerRing.setAttribute("stroke", partitioned ? "#f87171" : style.stroke);

    bg.setAttribute("x", -card.w / 2);
    bg.setAttribute("y", -card.h / 2);
    bg.setAttribute("width", card.w);
    bg.setAttribute("height", card.h);
    bg.setAttribute("fill", "url(#cardFill)");
    if (style.glow !== "none") bg.setAttribute("filter", style.glow);
    else bg.removeAttribute("filter");

    shine.setAttribute("x", -card.w / 2);
    shine.setAttribute("y", -card.h / 2);
    shine.setAttribute("width", card.w);
    shine.setAttribute("height", card.h * 0.45);
    shine.setAttribute("fill", "url(#cardShine)");

    badgeBg.setAttribute("x", -card.w / 2 + 12);
    badgeBg.setAttribute("y", -card.h / 2 + 10);
    badgeBg.setAttribute("fill", role === "leader" ? "rgba(34,211,238,0.12)" : "rgba(255,255,255,0.04)");
    roleBadge.setAttribute("x", -card.w / 2 + 18);
    roleBadge.setAttribute("y", -card.h / 2 + 22);
    roleBadge.textContent = partitioned ? "ISOLATED" : role.toUpperCase();
    roleBadge.setAttribute("fill", partitioned ? "#fca5a5" : style.badge);

    nodeId.setAttribute("y", -4);
    termText.setAttribute("x", -card.w / 2 + 14);
    termText.setAttribute("y", 14);
    commitText.setAttribute("x", card.w / 2 - 14);
    commitText.setAttribute("y", 14);
    commitText.setAttribute("text-anchor", "end");
    termText.textContent = node.running ? `T${node.term ?? "—"}` : "offline";
    commitText.textContent = node.running ? `C${node.commitIndex ?? "—"}` : "—";

    logLabel.setAttribute("x", -card.w / 2 + 12);
    logLabel.setAttribute("y", 30);
    logG.setAttribute("transform", `translate(${-card.logW / 2}, 36)`);

    const logLen = node.logLength ?? 0;
    const commit = node.commitIndex ?? -1;
    for (let i = 0; i < 5; i++) {
      const idx = logLen - 5 + i;
      const slot = slots[i];
      slot.setAttribute("x", i * (card.logW / 5 + 3));
      slot.setAttribute("width", card.logW / 5 - 3);
      slot.setAttribute("height", card.logH);
      if (idx >= 0 && idx <= commit) {
        slot.setAttribute("fill", "url(#committedGrad)");
        slot.setAttribute("opacity", "1");
      } else if (idx >= 0) {
        slot.setAttribute("fill", "url(#entryGrad)");
        slot.setAttribute("opacity", "0.9");
      } else {
        slot.setAttribute("opacity", "0.15");
      }
    }

    if (commit > (this.prevCommit[node.id] ?? -1)) {
      commitBar.setAttribute("x", -card.w / 2 + 10);
      commitBar.setAttribute("y", card.h / 2 - 10);
      commitBar.setAttribute("width", card.w - 20);
      commitBar.setAttribute("height", 3);
      commitBar.setAttribute("opacity", "1");
      setTimeout(() => commitBar.setAttribute("opacity", "0"), 600);
      if (role === "leader") {
        pulse.setAttribute("cx", 0);
        pulse.setAttribute("cy", 0);
        pulse.setAttribute("r", card.w * 0.5);
        pulse.setAttribute("opacity", "0.55");
        pulse.setAttribute("stroke", "url(#leaderRing)");
        pulse.style.transition = "r 0.7s ease-out, opacity 0.7s ease-out";
        requestAnimationFrame(() => {
          pulse.setAttribute("r", card.w * 0.85);
          pulse.setAttribute("opacity", "0");
        });
      }
    }
    this.prevCommit[node.id] = commit;
  }
}

function beamPath(from, to) {
  const mx = (from.x + to.x) / 2;
  const my = (from.y + to.y) / 2 - 28;
  return `M ${from.x} ${from.y - 24} Q ${mx} ${my} ${to.x} ${to.y + 24}`;
}

export { SVG_NS, BASE_CARD };
