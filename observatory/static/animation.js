import { SVG_NS } from "./renderer.js";

const FLOW_COLORS = {
  put: "#22d3ee",
  forward: "#2dd4bf",
  append: "#c4b5fd",
  commit: "#4ade80",
  state_change: "#fbbf24",
};

const FLOW_DURATIONS = {
  put: 380,
  forward: 340,
  append: 440,
  commit: 480,
  state_change: 560,
};

export class AnimationEngine {
  constructor(layer) {
    this.layer = layer;
    this.flows = [];
    this.raf = null;
  }

  spawnFlow(from, to, pos, type, label, opts = {}) {
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
    trail.setAttribute("stroke-width", "2");
    trail.setAttribute("opacity", "0.22");
    trail.setAttribute("stroke-dasharray", "3 8");
    g.appendChild(trail);

    const dot = document.createElementNS(SVG_NS, "circle");
    dot.setAttribute("r", opts.r ?? 6);
    dot.setAttribute("fill", color);
    dot.setAttribute("filter", "url(#packetGlow)");
    g.appendChild(dot);

    const halo = document.createElementNS(SVG_NS, "circle");
    halo.setAttribute("r", (opts.r ?? 6) + 4);
    halo.setAttribute("fill", color);
    halo.setAttribute("opacity", "0.18");
    g.insertBefore(halo, dot);

    if (label) {
      const text = document.createElementNS(SVG_NS, "text");
      text.setAttribute("fill", "#f8fafc");
      text.setAttribute("font-size", "9");
      text.setAttribute("font-weight", "700");
      text.setAttribute("text-anchor", "middle");
      text.setAttribute("letter-spacing", "0.04em");
      text.textContent = label;
      g.appendChild(text);
    }

    this.layer.appendChild(g);

    const duration = opts.duration ?? FLOW_DURATIONS[type] ?? 400;
    this.flows.push({
      g,
      dot,
      halo,
      text: label ? g.querySelector("text") : null,
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
      f.halo.setAttribute("cx", x);
      f.halo.setAttribute("cy", y);
      f.halo.setAttribute("opacity", String(0.18 * (1 - t)));
      if (f.text) {
        f.text.setAttribute("x", x);
        f.text.setAttribute("y", y - 12);
        f.text.setAttribute("opacity", String(1 - t * 0.35));
      }
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
      return p ? { x: p.x, y: p.y - 22 } : null;
    }
    const p = pos[id];
    if (!p) return null;
    if (role === "from") {
      return { x: p.x, y: p.y + (p.scale || 1) * 46 };
    }
    return { x: p.x, y: p.y - (p.scale || 1) * 46 };
  }
}
