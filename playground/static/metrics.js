function formatOps(v) {
  if (v == null || Number.isNaN(v)) return "-";
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k/s`;
  if (v >= 10) return `${v.toFixed(0)}/s`;
  if (v >= 1) return `${v.toFixed(1)}/s`;
  return `${v.toFixed(2)}/s`;
}

function formatMs(v) {
  if (v == null || Number.isNaN(v) || v <= 0) return "-";
  return `${v.toFixed(1)} ms`;
}

function formatLag(v) {
  if (v == null || Number.isNaN(v)) return "-";
  return `${Math.round(v)} entries`;
}

export function updateMetricsStats(data) {
  document.getElementById("stat-write-ops").textContent = formatOps(data.writeOpsSec);
  document.getElementById("stat-read-ops").textContent = formatOps(data.readOpsSec);
  document.getElementById("stat-write-p99").textContent = formatMs(data.writeP99Ms);
  document.getElementById("stat-read-p99").textContent = formatMs(data.readP99Ms);
  document.getElementById("stat-lag").textContent = formatLag(data.maxReplicationLag);
  document.getElementById("stat-failover").textContent =
    data.failoverMs != null ? `${Math.round(data.failoverMs)} ms` : "-";
}

function setText(id, text) {
  const el = document.getElementById(id);
  if (el) el.textContent = text;
}

export function updateSidebarMetrics(data) {
  setText("sm-spark-value", formatOps(data.writeOpsSec));
  setText("sm-read-ops", formatOps(data.readOpsSec));
  setText("sm-write-p99", formatMs(data.writeP99Ms));
  setText("sm-read-p99", formatMs(data.readP99Ms));
  setText("sm-lag", formatLag(data.maxReplicationLag));
  setText("sm-failover", data.failoverMs != null ? `${Math.round(data.failoverMs)} ms` : "-");
  drawSidebarSpark(data.history?.writeOpsSec || []);
}

export function updateSidebarCluster({ writeCount, running, total } = {}) {
  if (writeCount != null) setText("sm-writes", writeCount.toLocaleString());
  if (running != null && total != null) setText("sm-nodes", `${running}/${total}`);
}

function drawSidebarSpark(series) {
  const canvas = document.getElementById("sm-spark");
  if (!canvas) return;
  const dpr = window.devicePixelRatio || 1;
  const w = canvas.clientWidth;
  const h = canvas.clientHeight;
  if (w === 0 || h === 0) return;
  if (canvas.width !== Math.floor(w * dpr) || canvas.height !== Math.floor(h * dpr)) {
    canvas.width = Math.floor(w * dpr);
    canvas.height = Math.floor(h * dpr);
  }
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, w, h);

  const pts = (series || []).map((p) => p.val);
  if (pts.length < 2) {
    ctx.fillStyle = "#52525b";
    ctx.font = "10px JetBrains Mono, monospace";
    ctx.textAlign = "center";
    ctx.fillText("waiting for load…", w / 2, h / 2 + 3);
    return;
  }

  const max = Math.max(...pts, 1) * 1.15;
  const n = pts.length;
  const xAt = (i) => (i / (n - 1)) * w;
  const yAt = (v) => h - (v / max) * (h - 3) - 1;

  const grad = ctx.createLinearGradient(0, 0, 0, h);
  grad.addColorStop(0, "#22d3ee55");
  grad.addColorStop(1, "#22d3ee08");
  ctx.beginPath();
  ctx.moveTo(0, h);
  for (let i = 0; i < n; i++) ctx.lineTo(xAt(i), yAt(pts[i]));
  ctx.lineTo(w, h);
  ctx.closePath();
  ctx.fillStyle = grad;
  ctx.fill();

  ctx.beginPath();
  ctx.moveTo(0, yAt(pts[0]));
  for (let i = 1; i < n; i++) ctx.lineTo(xAt(i), yAt(pts[i]));
  ctx.strokeStyle = "#22d3ee";
  ctx.lineWidth = 1.5;
  ctx.lineJoin = "round";
  ctx.stroke();

  const lx = xAt(n - 1);
  const ly = yAt(pts[n - 1]);
  ctx.beginPath();
  ctx.arc(lx, ly, 2.5, 0, Math.PI * 2);
  ctx.fillStyle = "#22d3ee";
  ctx.fill();
}

function patchHistorySeries(history, key, value) {
  if (value == null || value <= 0) return;
  const hist = history[key] || [];
  if (hist.length === 0) {
    const now = Math.floor(Date.now() / 1000);
    history[key] = [
      { ts: now - 2, val: value * 0.5 },
      { ts: now - 1, val: value * 0.8 },
      { ts: now, val: value },
    ];
    return;
  }
  const last = hist[hist.length - 1];
  if (last.val !== value) {
    history[key] = [...hist.slice(0, -1), { ...last, val: value }];
  }
}

export function mergeDisplayMetrics(metrics, scenario) {
  const load = scenario?.load;
  const m = {
    ...metrics,
    history: {
      writeOpsSec: [...(metrics.history?.writeOpsSec || [])],
      readOpsSec: [...(metrics.history?.readOpsSec || [])],
      writeP99Ms: [...(metrics.history?.writeP99Ms || [])],
      readP99Ms: [...(metrics.history?.readP99Ms || [])],
      commitRate: [...(metrics.history?.commitRate || [])],
      maxReplicationLag: [...(metrics.history?.maxReplicationLag || [])],
    },
  };

  if (load?.active) {
    if (load.successRate > 0) m.writeOpsSec = load.successRate;
    if (load.readSuccessRate > 0) m.readOpsSec = load.readSuccessRate;
    if (load.writeP99Ms > 0) m.writeP99Ms = load.writeP99Ms;
    if (load.readP99Ms > 0) m.readP99Ms = load.readP99Ms;

    patchHistorySeries(m.history, "writeOpsSec", m.writeOpsSec);
    patchHistorySeries(m.history, "readOpsSec", m.readOpsSec);
    patchHistorySeries(m.history, "writeP99Ms", m.writeP99Ms);
    patchHistorySeries(m.history, "readP99Ms", m.readP99Ms);
  }

  return m;
}

export function showGrafanaHint(enabled) {
  const hint = document.getElementById("grafana-hint");
  if (hint && enabled) hint.classList.remove("hidden");
}
