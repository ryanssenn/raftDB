const GRAFANA = "http://localhost:3000";

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

export function updateMetricsStats(data) {
  document.getElementById("stat-write-ops").textContent = formatOps(data.writeOpsSec);
  document.getElementById("stat-read-ops").textContent = formatOps(data.readOpsSec);
  document.getElementById("stat-write-p99").textContent = formatMs(data.writeP99Ms);
  document.getElementById("stat-read-p99").textContent = formatMs(data.readP99Ms);
  document.getElementById("stat-lag").textContent = formatLag(data.maxReplicationLag);
  document.getElementById("stat-failover").textContent =
    data.failoverMs != null ? `${Math.round(data.failoverMs)} ms` : "—";
}

const PANELS = [
  { id: 1, title: "Write throughput" },
  { id: 2, title: "Read throughput" },
  { id: 3, title: "Latency p99" },
  { id: 4, title: "Replication lag" },
];

function panelSrc(panelId) {
  const q = new URLSearchParams({
    orgId: "1",
    panelId: String(panelId),
    theme: "dark",
    refresh: "5s",
    kiosk: "",
  });
  return `${GRAFANA}/d-solo/playground-live/playground-live?${q}`;
}

export function grafanaDashboardURL() {
  return `${GRAFANA}/d/playground-live/playground-live?kiosk`;
}

export function initGrafanaPanels(enabled) {
  const grid = document.getElementById("grafana-panels");
  const fallback = document.getElementById("grafana-fallback");
  if (!grid) return;

  if (enabled) {
    grid.innerHTML = PANELS.map((p) => `
      <article class="grafana-panel">
        <iframe
          title="${p.title}"
          src="${panelSrc(p.id)}"
          loading="lazy"
          tabindex="-1"
        ></iframe>
      </article>
    `).join("");
    grid.classList.remove("hidden");
    fallback?.classList.add("hidden");
  } else {
    grid.classList.add("hidden");
    fallback?.classList.remove("hidden");
  }
}
