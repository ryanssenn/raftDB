const SPARK_COLORS = {
  writeOpsSec: "#38bdf8",
  readOpsSec: "#a78bfa",
  writeP99Ms: "#fbbf24",
  readP99Ms: "#34d399",
  maxReplicationLag: "#f87171",
  commitRate: "#4ade80",
};

function formatOps(v) {
  if (v == null || Number.isNaN(v)) return "—";
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k/s`;
  if (v >= 10) return `${v.toFixed(0)}/s`;
  if (v >= 1) return `${v.toFixed(1)}/s`;
  return `${v.toFixed(2)}/s`;
}

function formatMs(v) {
  if (v == null || Number.isNaN(v) || v <= 0) return "—";
  return `${v.toFixed(1)} ms`;
}

function formatLag(v) {
  if (v == null || Number.isNaN(v)) return "—";
  return `${Math.round(v)} entries`;
}

export function drawSparkline(canvas, points, color) {
  if (!canvas) return;
  const ctx = canvas.getContext("2d");
  const w = canvas.width;
  const h = canvas.height;
  ctx.clearRect(0, 0, w, h);

  if (!points || points.length < 2) {
    ctx.strokeStyle = "#334155";
    ctx.beginPath();
    ctx.moveTo(0, h / 2);
    ctx.lineTo(w, h / 2);
    ctx.stroke();
    return;
  }

  const vals = points.map((p) => p.val ?? 0);
  const min = Math.min(...vals);
  const max = Math.max(...vals);
  const range = max - min || 1;

  ctx.strokeStyle = color;
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  vals.forEach((v, i) => {
    const x = (i / (vals.length - 1)) * (w - 4) + 2;
    const y = h - 4 - ((v - min) / range) * (h - 8);
    if (i === 0) ctx.moveTo(x, y);
    else ctx.lineTo(x, y);
  });
  ctx.stroke();

  const last = vals[vals.length - 1];
  const lx = w - 4;
  const ly = h - 4 - ((last - min) / range) * (h - 8);
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.arc(lx, ly, 2.5, 0, Math.PI * 2);
  ctx.fill();
}

export function updateMetricsUI(data) {
  document.getElementById("val-write-ops").textContent = formatOps(data.writeOpsSec);
  document.getElementById("val-read-ops").textContent = formatOps(data.readOpsSec);
  document.getElementById("val-write-p99").textContent = formatMs(data.writeP99Ms);
  document.getElementById("val-read-p99").textContent = formatMs(data.readP99Ms);
  document.getElementById("val-lag").textContent = formatLag(data.maxReplicationLag);
  document.getElementById("val-failover").textContent =
    data.failoverMs != null ? `${Math.round(data.failoverMs)} ms` : "—";

  const hist = data.history || {};
  drawSparkline(document.getElementById("spark-write-ops"), hist.writeOpsSec, SPARK_COLORS.writeOpsSec);
  drawSparkline(document.getElementById("spark-read-ops"), hist.readOpsSec, SPARK_COLORS.readOpsSec);
  drawSparkline(document.getElementById("spark-write-p99"), hist.writeP99Ms, SPARK_COLORS.writeP99Ms);
  drawSparkline(document.getElementById("spark-read-p99"), hist.readP99Ms, SPARK_COLORS.readP99Ms);
  drawSparkline(document.getElementById("spark-lag"), hist.maxReplicationLag, SPARK_COLORS.maxReplicationLag);
}
