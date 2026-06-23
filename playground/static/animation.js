import { SVG_NS } from "./renderer.js";

const FLOW_COLORS = {
  put: "#22d3ee",
  forward: "#2dd4bf",
  append: "#c4b5fd",
  commit: "#4ade80",
  state_change: "#fbbf24",
};

const FLOW_DURATIONS = {
  put: 320,
  forward: 280,
  append: 360,
  commit: 400,
  state_change: 480,
};

const MAX_FLOWS = 12;

export class AnimationEngine {
  constructor(layer) {
    this.layer = layer;
    this.flows = [];
    this.raf = null;
  }

  spawnFlow(from, to, pos, type, label, opts = {}) {
    if (this.flows.length >= MAX_FLOWS) return;

    const fp = this._point(from, pos, "from");
    const tp = this._point(to, pos, "to");
    if (!fp || !tp) return;

    const curve = opts.curve ?? 28;
    const mid = {
      x: (fp.x + tp.x) / 2,
      y: (fp.y + tp.y) / 2 - curve,
    };
    const color = FLOW_COLORS[type] || FLOW_COLORS.put;

    const g = document.createElementNS(SVG_NS, "g");
    g.classList.add("flow-packet");

    const trail = document.createElementNS(SVG_NS, "path");
    trail.setAttribute(
      "d",
      `M ${fp.x} ${fp.y} Q ${mid.x} ${mid.y} ${tp.x} ${tp.y}`
    );
    trail.setAttribute("fill", "none");
    trail.setAttribute("stroke", color);
    trail.setAttribute("stroke-width", "1.5");
    trail.setAttribute("opacity", "0.12");
    g.appendChild(trail);

    const dot = document.createElementNS(SVG_NS, "circle");
    dot.setAttribute("r", opts.r ?? 4);
    dot.setAttribute("fill", color);
    dot.setAttribute("opacity", "0.75");
    g.appendChild(dot);

    this.layer.appendChild(g);

    const duration = opts.duration ?? FLOW_DURATIONS[type] ?? 400;
    this.flows.push({
      g,
      dot,
      fp,
      mid,
      tp,
      start: performance.now(),
      duration,
    });

    this._ensureTick();
  }

  tick(now = performance.now()) {
    this.flows = this.flows.filter((f) => {
      const t = Math.min(1, (now - f.start) / f.duration);
      const u = 1 - t;
      const x = u * u * f.fp.x + 2 * u * t * f.mid.x + t * t * f.tp.x;
      const y = u * u * f.fp.y + 2 * u * t * f.mid.y + t * t * f.tp.y;
      f.dot.setAttribute("cx", x);
      f.dot.setAttribute("cy", y);
      f.dot.setAttribute("opacity", String(0.75 * (1 - t * 0.5)));
      if (t >= 1) {
        f.g.remove();
        return false;
      }
      return true;
    });

    if (this.flows.length > 0) {
      this.raf = requestAnimationFrame((t) => this.tick(t));
    } else {
      this.raf = null;
    }
  }

  _ensureTick() {
    if (this.raf == null) {
      this.raf = requestAnimationFrame((t) => this.tick(t));
    }
  }

  _point(id, pos, role) {
    if (id === "client") {
      const p = pos.client;
      return p ? { x: p.x + 40, y: p.y } : null;
    }
    const p = pos[id];
    if (!p) return null;
    const edge = (p.scale || 1) * 40;
    if (role === "from") {
      return { x: p.x, y: p.y + edge };
    }
    return { x: p.x, y: p.y - edge };
  }
}
