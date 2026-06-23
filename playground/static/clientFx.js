const MAX_DOTS = 7;
const pool = [];

export class ClientFx {
  constructor(layer) {
    this.layer = layer;
    this.rate = 0;
    this.active = false;
    this.from = null;
    this.to = null;
    this.accum = 0;
  }

  setRoute(from, to) {
    this.from = from;
    this.to = to;
  }

  setActive(rate, active) {
    this.rate = rate;
    this.active = active;
    if (!active) this.accum = 0;
  }

  tick(dt) {
    if (!this.active || !this.from || !this.to || this.rate <= 0) return;

    const perSec = Math.min(Math.max(this.rate / 30, 2), 9);
    this.accum += perSec * dt;
    while (this.accum >= 1 && this.layer.childElementCount < MAX_DOTS) {
      this.accum -= 1;
      this._spawn();
    }
  }

  _spawn() {
    let dot = pool.pop();
    if (!dot) {
      dot = document.createElement("div");
      dot.className = "req-dot";
      dot.addEventListener("animationend", () => {
        dot.classList.remove("flying");
        if (dot.parentNode) dot.parentNode.removeChild(dot);
        pool.push(dot);
      });
    }

    const dx = this.to.x - this.from.x;
    const dy = this.to.y - this.from.y;
    dot.style.left = `${this.from.x}px`;
    dot.style.top = `${this.from.y}px`;
    dot.style.setProperty("--dx", `${dx}px`);
    dot.style.setProperty("--dy", `${dy}px`);
    dot.classList.remove("flying");
    this.layer.appendChild(dot);
    void dot.offsetWidth;
    dot.classList.add("flying");
  }
}

export function canvasPoint(p, bounds, rect) {
  if (!p || !bounds?.width || !rect?.width) return null;
  return {
    x: (p.x / bounds.width) * rect.width,
    y: (p.y / bounds.height) * rect.height,
  };
}

export function leaderTarget(pos, leaderId, bounds, rect) {
  const lp = pos[leaderId];
  if (!lp) return null;
  const edge = (lp.scale || 1) * 36;
  return canvasPoint({ x: lp.x, y: lp.y - edge }, bounds, rect);
}

export function clientSource(pos, bounds, rect) {
  const cp = pos.client;
  if (!cp) return null;
  return canvasPoint({ x: cp.x + 36, y: cp.y }, bounds, rect);
}
