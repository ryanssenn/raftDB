import { SVG_NS } from "./renderer.js";

const FLOW_COLORS = {
  put: "#60a5fa",
  forward: "#2dd4bf",
  append: "#c4b5fd",
  commit: "#4ade80",
  state_change: "#fbbf24",
};

const FLOW_DURATIONS = {
  put: 420,
  forward: 380,
  append: 520,
  commit: 580,
  state_change: 640,
};

const MAX_FLOWS = 48;

export class AnimationEngine {
  constructor(layer) {
    this.layer = layer;
    this.flows = [];
    this.raf = null;
  }

  spawnFlow(from, to, pos, type, opts = {}) {
    if (this.flows.length >= MAX_FLOWS) {
      const oldest = this.flows.shift();
      oldest?.g?.remove();
    }

    const fp = this._point(from, pos, "from");
    const tp = this._point(to, pos, "to");
    if (!fp || !tp) return;

    const curve = opts.curve ?? 22;
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
    trail.setAttribute("stroke-width", type === "append" ? "2" : "1.5");
    trail.setAttribute("opacity", type === "append" ? "0.35" : "0.2");
    g.appendChild(trail);

    const dot = document.createElementNS(SVG_NS, "circle");
    dot.setAttribute("r", opts.r ?? (type === "append" ? 3.5 : 4));
    dot.setAttribute("fill", color);
    dot.setAttribute("opacity", "0.9");
    g.appendChild(dot);

    this.layer.appendChild(g);

    const duration = opts.duration ?? FLOW_DURATIONS[type] ?? 240;
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

  /** Leader → followers replication burst */
  spawnReplication(leaderId, followerIds, pos) {
    if (!leaderId || !pos[leaderId]) return;
    for (const fid of followerIds) {
      if (fid === leaderId || !pos[fid]) continue;
      this.spawnFlow(leaderId, fid, pos, "append", { curve: 18 + Math.random() * 14 });
    }
  }

  tick(now = performance.now()) {
    this.flows = this.flows.filter((f) => {
      const t = Math.min(1, (now - f.start) / f.duration);
      const u = 1 - t;
      const x = u * u * f.fp.x + 2 * u * t * f.mid.x + t * t * f.tp.x;
      const y = u * u * f.fp.y + 2 * u * t * f.mid.y + t * t * f.tp.y;
      f.dot.setAttribute("cx", x);
      f.dot.setAttribute("cy", y);
      f.dot.setAttribute("opacity", String(0.9 * (1 - t * 0.6)));
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

  /** Immediately remove all in-flight flow packets. */
  clear() {
    for (const f of this.flows) f.g?.remove();
    this.flows = [];
    if (this.raf != null) {
      cancelAnimationFrame(this.raf);
      this.raf = null;
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
      return { x: p.x, y: p.y + edge * 0.3 };
    }
    return { x: p.x, y: p.y - edge };
  }
}
