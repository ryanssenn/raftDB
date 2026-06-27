const PALETTE = {
  blue: "#5e8fb5",
  slate: "#8089a0",
  amber: "#bf9b54",
  green: "#5a9c6e",
  red: "#bd6b6b",
  grid: "#1d1d21",
  baseline: "#2a2a2f",
  threshold: "#4a4a52",
  text: "#52525b",
};

const CHARTS = [
  {
    key: "writeThroughput",
    label: "Write throughput",
    series: [{ source: "writeOpsSec", color: PALETTE.blue, fill: true }],
    format: formatOps,
  },
  {
    key: "readThroughput",
    label: "Read throughput",
    series: [{ source: "readOpsSec", color: PALETTE.slate, fill: true }],
    format: formatOps,
  },
  {
    key: "latency",
    label: "Commit latency",
    series: [
      { source: "writeP50Ms", color: PALETTE.amber, label: "p50" },
      { source: "writeP99Ms", color: PALETTE.amber, label: "p99", dim: true },
    ],
    format: formatMs,
  },
  {
    key: "leadership",
    label: "Leadership & quorum",
    stepped: true,
    integer: true,
    series: [
      { source: "nodesUp", color: PALETTE.blue, label: "up" },
      { source: "leaderCount", color: PALETTE.green, label: "leaders", state: leaderState },
    ],
    format: (v) => String(Math.round(v)),
  },
];

function leaderState(v) {
  if (v <= 0) return PALETTE.red;
  if (v === 1) return PALETTE.green;
  return PALETTE.amber;
}

function formatOps(v) {
  if (v == null || Number.isNaN(v) || v <= 0) return "0/s";
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k/s`;
  if (v >= 10) return `${Math.round(v)}/s`;
  return `${v.toFixed(1)}/s`;
}

function formatMs(v) {
  if (v == null || Number.isNaN(v) || v <= 0) return "0 ms";
  if (v >= 100) return `${Math.round(v)} ms`;
  return `${v.toFixed(1)} ms`;
}

function niceMax(v) {
  if (v <= 0) return 1;
  const exp = Math.floor(Math.log10(v));
  const base = Math.pow(10, exp);
  const frac = v / base;
  const step = frac <= 1 ? 1 : frac <= 2 ? 2 : frac <= 5 ? 5 : 10;
  return step * base;
}

export class LiveCharts {
  constructor(container) {
    this.container = container;
    this.clusterSize = 0;
    this.charts = CHARTS.map((def) => {
      const el = document.createElement("article");
      el.className = "live-chart";
      const legend = def.series
        .filter((s) => s.label)
        .map(
          (s) =>
            `<span class="lc-key${s.dim ? " dim" : ""}" style="--c:${s.color}">${s.label}</span>`
        )
        .join("");
      el.innerHTML = `
        <header class="live-chart-head">
          <span class="live-chart-label">${def.label}</span>
          <span class="live-chart-meta">
            ${legend ? `<span class="live-chart-legend">${legend}</span>` : ""}
            <strong class="live-chart-value">-</strong>
          </span>
        </header>
        <canvas class="live-chart-canvas"></canvas>
      `;
      container.appendChild(el);
      const canvas = el.querySelector("canvas");
      return {
        def,
        el,
        canvas,
        ctx: canvas.getContext("2d"),
        valueEl: el.querySelector(".live-chart-value"),
      };
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
    if (metrics.clusterSize) this.clusterSize = metrics.clusterSize;
    for (const chart of this.charts) {
      const seriesData = chart.def.series.map((s) => metrics.history[s.source] || []);
      const latest = seriesData.map((arr) => (arr.length ? arr[arr.length - 1].val : 0));
      chart.valueEl.textContent = chart.def.format(latest[0] ?? 0);
      this._draw(chart, seriesData, latest);
    }
  }

  _draw(chart, seriesData, latest) {
    const { ctx, canvas, def } = chart;
    const w = canvas.width / (window.devicePixelRatio || 1);
    const h = canvas.height / (window.devicePixelRatio || 1);
    ctx.clearRect(0, 0, w, h);

    const pad = { l: 6, r: 8, t: 12, b: 6 };
    const plotW = w - pad.l - pad.r;
    const plotH = h - pad.t - pad.b;

    const quorum = def.key === "leadership" && this.clusterSize > 0
      ? Math.floor(this.clusterSize / 2) + 1
      : null;

    let max = 0;
    for (const arr of seriesData) for (const p of arr) max = Math.max(max, p.val);
    for (const v of latest) max = Math.max(max, v);
    if (def.integer) {
      max = Math.max(max, this.clusterSize || 1);
    } else {
      max = niceMax(max);
    }
    if (max <= 0) max = 1;

    const totalPoints = Math.max(...seriesData.map((a) => a.length), 0);
    const xAt = (i, n) => pad.l + (n <= 1 ? plotW : (i / (n - 1)) * plotW);
    const yAt = (v) => pad.t + plotH - (v / max) * plotH;

    ctx.strokeStyle = PALETTE.baseline;
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(pad.l, pad.t + plotH + 0.5);
    ctx.lineTo(pad.l + plotW, pad.t + plotH + 0.5);
    ctx.stroke();

    ctx.strokeStyle = PALETTE.grid;
    ctx.beginPath();
    ctx.moveTo(pad.l, pad.t + 0.5);
    ctx.lineTo(pad.l + plotW, pad.t + 0.5);
    ctx.stroke();

    ctx.fillStyle = PALETTE.text;
    ctx.font = "9px JetBrains Mono, monospace";
    ctx.textAlign = "right";
    ctx.textBaseline = "top";
    ctx.fillText(def.format(max), pad.l + plotW, 1);

    if (quorum != null && quorum <= max) {
      const qy = yAt(quorum);
      ctx.strokeStyle = PALETTE.threshold;
      ctx.lineWidth = 1;
      ctx.setLineDash([3, 3]);
      ctx.beginPath();
      ctx.moveTo(pad.l, qy);
      ctx.lineTo(pad.l + plotW, qy);
      ctx.stroke();
      ctx.setLineDash([]);
      ctx.fillStyle = PALETTE.threshold;
      ctx.textAlign = "left";
      ctx.fillText("quorum", pad.l + 2, qy - 10);
    }

    if (totalPoints < 2) {
      ctx.fillStyle = PALETTE.text;
      ctx.font = "10px JetBrains Mono, monospace";
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      ctx.fillText("waiting for load…", pad.l + plotW / 2, pad.t + plotH / 2);
      return;
    }

    def.series.forEach((s, si) => {
      const arr = seriesData[si];
      if (arr.length < 2) return;
      const n = arr.length;

      if (s.fill && !def.stepped) {
        const grad = ctx.createLinearGradient(0, pad.t, 0, pad.t + plotH);
        grad.addColorStop(0, s.color + "33");
        grad.addColorStop(1, s.color + "00");
        ctx.beginPath();
        ctx.moveTo(xAt(0, n), pad.t + plotH);
        for (let i = 0; i < n; i++) ctx.lineTo(xAt(i, n), yAt(arr[i].val));
        ctx.lineTo(xAt(n - 1, n), pad.t + plotH);
        ctx.closePath();
        ctx.fillStyle = grad;
        ctx.fill();
      }

      ctx.lineWidth = s.dim ? 1 : 1.75;
      ctx.lineJoin = "round";
      ctx.globalAlpha = s.dim ? 0.5 : 1;

      if (def.stepped) {
        for (let i = 0; i < n - 1; i++) {
          const color = s.state ? s.state(arr[i].val) : s.color;
          ctx.strokeStyle = color;
          ctx.beginPath();
          ctx.moveTo(xAt(i, n), yAt(arr[i].val));
          ctx.lineTo(xAt(i + 1, n), yAt(arr[i].val));
          ctx.lineTo(xAt(i + 1, n), yAt(arr[i + 1].val));
          ctx.stroke();
        }
      } else {
        ctx.strokeStyle = s.color;
        ctx.beginPath();
        ctx.moveTo(xAt(0, n), yAt(arr[0].val));
        for (let i = 1; i < n; i++) ctx.lineTo(xAt(i, n), yAt(arr[i].val));
        ctx.stroke();
      }

      const lx = xAt(n - 1, n);
      const ly = yAt(arr[n - 1].val);
      ctx.beginPath();
      ctx.arc(lx, ly, 2.25, 0, Math.PI * 2);
      ctx.fillStyle = s.state ? s.state(arr[n - 1].val) : s.color;
      ctx.fill();
      ctx.globalAlpha = 1;
    });
  }
}

export function initLiveCharts() {
  const grid = document.getElementById("live-charts");
  if (!grid) return null;
  grid.classList.remove("hidden");
  return new LiveCharts(grid);
}
