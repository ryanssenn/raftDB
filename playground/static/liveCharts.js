const CHARTS = [
  { key: "writeOpsSec", label: "Write throughput", color: "#22d3ee", format: (v) => formatOps(v) },
  { key: "readOpsSec", label: "Read throughput", color: "#a78bfa", format: (v) => formatOps(v) },
  { key: "writeP99Ms", label: "Write p99", color: "#fbbf24", format: (v) => formatMs(v) },
  { key: "maxReplicationLag", label: "Replication lag", color: "#f87171", format: (v) => formatLag(v) },
];

function formatOps(v) {
  if (v == null || Number.isNaN(v) || v <= 0) return "0/s";
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k/s`;
  if (v >= 10) return `${Math.round(v)}/s`;
  return `${v.toFixed(1)}/s`;
}

function formatMs(v) {
  if (v == null || Number.isNaN(v) || v <= 0) return "—";
  return `${v.toFixed(1)} ms`;
}

function formatLag(v) {
  if (v == null || Number.isNaN(v)) return "0";
  return String(Math.round(v));
}

export class LiveCharts {
  constructor(container) {
    this.container = container;
    this.charts = CHARTS.map((def) => {
      const el = document.createElement("article");
      el.className = "live-chart";
      el.innerHTML = `
        <header class="live-chart-head">
          <span class="live-chart-label">${def.label}</span>
          <strong class="live-chart-value">—</strong>
        </header>
        <canvas class="live-chart-canvas"></canvas>
      `;
      container.appendChild(el);
      const canvas = el.querySelector("canvas");
      const ctx = canvas.getContext("2d");
      return { def, el, canvas, ctx, valueEl: el.querySelector(".live-chart-value") };
    });
    this._ro = new ResizeObserver(() => this._resizeAll());
    this._ro.observe(container);
    this._resizeAll();
  }

  _resizeAll() {
    for (const chart of this.charts) {
      const rect = chart.canvas.getBoundingClientRect();
      const dpr = window.devicePixelRatio || 1;
      chart.canvas.width = Math.max(1, Math.floor(rect.width * dpr));
      chart.canvas.height = Math.max(1, Math.floor(rect.height * dpr));
      chart.ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    }
  }

  update(metrics) {
    if (!metrics?.history) return;
    for (const chart of this.charts) {
      const series = metrics.history[chart.def.key] || [];
      const latest = series.length ? series[series.length - 1].val : 0;
      chart.valueEl.textContent = chart.def.format(latest);
      this._draw(chart, series, latest);
    }
  }

  _draw(chart, series, latest) {
    const { ctx, canvas } = chart;
    const w = canvas.width / (window.devicePixelRatio || 1);
    const h = canvas.height / (window.devicePixelRatio || 1);
    ctx.clearRect(0, 0, w, h);

    const pad = { l: 4, r: 4, t: 4, b: 4 };
    const plotW = w - pad.l - pad.r;
    const plotH = h - pad.t - pad.b;

    const values = series.map((p) => p.val);
    let max = Math.max(...values, latest, 0);
    if (max <= 0) max = 1;
    max *= 1.15;

    ctx.strokeStyle = "#27272a";
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(pad.l, pad.t + plotH);
    ctx.lineTo(pad.l + plotW, pad.t + plotH);
    ctx.stroke();

    if (series.length < 2 && latest <= 0) {
      ctx.fillStyle = "#52525b";
      ctx.font = "10px JetBrains Mono, monospace";
      ctx.textAlign = "center";
      ctx.fillText("waiting for load…", pad.l + plotW / 2, pad.t + plotH / 2);
      return;
    }

    const pts = series.length >= 2 ? series : [{ val: 0 }, { val: latest }];
    const n = pts.length;

    const xAt = (i) => pad.l + (i / Math.max(n - 1, 1)) * plotW;
    const yAt = (v) => pad.t + plotH - (v / max) * plotH;

    const grad = ctx.createLinearGradient(0, pad.t, 0, pad.t + plotH);
    grad.addColorStop(0, chart.def.color + "55");
    grad.addColorStop(1, chart.def.color + "08");

    ctx.beginPath();
    ctx.moveTo(xAt(0), pad.t + plotH);
    for (let i = 0; i < n; i++) {
      ctx.lineTo(xAt(i), yAt(pts[i].val));
    }
    ctx.lineTo(xAt(n - 1), pad.t + plotH);
    ctx.closePath();
    ctx.fillStyle = grad;
    ctx.fill();

    ctx.beginPath();
    ctx.moveTo(xAt(0), yAt(pts[0].val));
    for (let i = 1; i < n; i++) {
      ctx.lineTo(xAt(i), yAt(pts[i].val));
    }
    ctx.strokeStyle = chart.def.color;
    ctx.lineWidth = 2;
    ctx.lineJoin = "round";
    ctx.stroke();

    const lx = xAt(n - 1);
    const ly = yAt(pts[n - 1].val);
    ctx.beginPath();
    ctx.arc(lx, ly, 3, 0, Math.PI * 2);
    ctx.fillStyle = chart.def.color;
    ctx.fill();
  }
}

export function initLiveCharts() {
  const grid = document.getElementById("live-charts");
  if (!grid) return null;
  grid.classList.remove("hidden");
  return new LiveCharts(grid);
}
