import { SVG_NS } from "./renderer.js";

const COLORS = {
  client_request: "#58c4dd",
  forward_command: "#4ecdc4",
  request_vote: "#ffd166",
  append_entries: "#b39cd0",
  append_response: "#95d5b2",
  commit: "#ffd166",
  state_change: "#ff6b6b",
};

const DURATIONS = {
  client_request: 420,
  forward_command: 380,
  request_vote: 500,
  append_entries: 400,
  append_response: 350,
  commit: 600,
  state_change: 400,
};

function easeOut(t) {
  return 1 - Math.pow(1 - t, 3);
}

function bezierPoint(t, p0, p1, p2) {
  const u = 1 - t;
  return {
    x: u * u * p0.x + 2 * u * t * p1.x + t * t * p2.x,
    y: u * u * p0.y + 2 * u * t * p1.y + t * t * p2.y,
  };
}

export class AnimationEngine {
  constructor(layers, renderer) {
    this.layers = layers;
    this.renderer = renderer;
    this.flows = [];
    this.priority = { state_change: 0, commit: 1, client_request: 2, forward_command: 3, request_vote: 4, append_entries: 5, append_response: 6 };
  }

  spawnFlow(from, to, pos, type, label, opts = {}) {
    const a = this.renderer.getAnchor(from, pos) || this.renderer.getNodeCenter(from, pos);
    const b = this.renderer.getAnchor(to, pos) || this.renderer.getNodeCenter(to, pos);
    if (!a || !b) return;

    const mid = {
      x: (a.x + b.x) / 2 + (opts.curve || 0),
      y: (a.y + b.y) / 2 - 60,
    };

    const g = document.createElementNS(SVG_NS, "g");
    g.setAttribute("class", "flow-packet");
    const trail = document.createElementNS(SVG_NS, "path");
    trail.setAttribute("fill", "none");
    trail.setAttribute("stroke", COLORS[type] || "#58c4dd");
    trail.setAttribute("stroke-width", "2");
    trail.setAttribute("opacity", "0.35");
    trail.setAttribute("stroke-dasharray", "4 4");
    g.appendChild(trail);

    const dot = document.createElementNS(SVG_NS, "circle");
    dot.setAttribute("r", opts.r || 6);
    dot.setAttribute("fill", COLORS[type] || "#58c4dd");
    g.appendChild(dot);

    if (label) {
      const txt = document.createElementNS(SVG_NS, "text");
      txt.setAttribute("class", "flow-label");
      txt.setAttribute("fill", COLORS[type] || "#58c4dd");
      txt.setAttribute("text-anchor", "middle");
      txt.textContent = label;
      g.appendChild(txt);
    }

    this.layers.flow.appendChild(g);

    const duration = DURATIONS[type] || 400;
    const start = performance.now();
    const flow = { g, start, duration, a, mid, b, type, priority: this.priority[type] ?? 9 };

    this.flows.push(flow);
    if (this.flows.length > 40) {
      const sorted = [...this.flows].sort((x, y) => y.priority - x.priority);
      const drop = sorted[sorted.length - 1];
      drop.g.remove();
      this.flows = this.flows.filter((f) => f !== drop);
    }
  }

  tick(now) {
    this.flows = this.flows.filter((flow) => {
      const t = Math.min(1, (now - flow.start) / flow.duration);
      const e = easeOut(t);
      const pt = bezierPoint(e, flow.a, flow.mid, flow.b);
      const dot = flow.g.querySelector("circle");
      const txt = flow.g.querySelector("text");
      if (dot) {
        dot.setAttribute("cx", pt.x);
        dot.setAttribute("cy", pt.y);
      }
      if (txt) {
        txt.setAttribute("x", pt.x);
        txt.setAttribute("y", pt.y - 12);
        txt.setAttribute("opacity", String(1 - t * 0.5));
      }
      const trailD = `M ${flow.a.x} ${flow.a.y} Q ${flow.mid.x} ${flow.mid.y} ${pt.x} ${pt.y}`;
      const trail = flow.g.querySelector("path");
      if (trail) trail.setAttribute("d", trailD);

      if (t >= 1) {
        flow.g.remove();
        return false;
      }
      return true;
    });
  }

  burstCommit(pos, nodes) {
    for (const node of nodes) {
      if (!node.running) continue;
      const c = this.renderer.getNodeCenter(node.id, pos);
      if (!c) continue;
      const ring = document.createElementNS(SVG_NS, "circle");
      ring.setAttribute("cx", c.x);
      ring.setAttribute("cy", c.y);
      ring.setAttribute("r", 10);
      ring.setAttribute("fill", "none");
      ring.setAttribute("stroke", COLORS.commit);
      ring.setAttribute("stroke-width", "2");
      ring.setAttribute("opacity", "0.8");
      this.layers.fx.appendChild(ring);
      const start = performance.now();
      const animate = (now) => {
        const t = (now - start) / 600;
        if (t >= 1) {
          ring.remove();
          return;
        }
        ring.setAttribute("r", 10 + t * 40);
        ring.setAttribute("opacity", String(0.8 * (1 - t)));
        requestAnimationFrame(animate);
      };
      requestAnimationFrame(animate);
    }
  }
}

export function eventLabel(e) {
  switch (e.type) {
    case "client_request": return e.op === "put" ? "write" : "read";
    case "forward_command": return "forward";
    case "request_vote": return e.detail === "granted" ? "granted" : "vote";
    case "append_entries": return e.entries > 0 ? "replicate" : "heartbeat";
    case "append_response": return "ack";
    case "commit": return "commit";
    case "state_change": return e.detail;
    default: return e.type;
  }
}

export function eventCallout(e) {
  switch (e.type) {
    case "state_change":
      if (e.detail === "leader") return `${e.from} elected leader for term ${e.term}`;
      if (e.detail === "candidate") return `${e.from} starts election for term ${e.term}`;
      return `${e.from} becomes ${e.detail}`;
    case "commit":
      return `Entry ${e.detail} committed (majority reached)`;
    case "append_entries":
      return e.entries > 0
        ? `Leader replicates ${e.entries} entr${e.entries === 1 ? "y" : "ies"} to ${e.to}`
        : `Heartbeat to ${e.to}`;
    case "request_vote":
      return e.detail === "granted" ? `${e.from} grants vote to ${e.to}` : `${e.from} requests vote from ${e.to}`;
    case "client_request":
      return `Client ${e.from} sends ${e.op} to ${e.to}`;
    case "forward_command":
      return `${e.from} forwards command to leader ${e.to}`;
    default:
      return null;
  }
}

export { COLORS };
