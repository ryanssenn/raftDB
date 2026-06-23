export function computeLayout(nodes, bounds = { width: 1000, height: 420 }) {
  const pos = {};
  const w = bounds.width;
  const h = bounds.height;
  const cx = w / 2;
  const cy = h * 0.5;

  const running = nodes.filter((n) => n.running !== false);
  const n = running.length;
  const scale = Math.max(0.58, Math.min(0.92, 4.2 / Math.max(n, 1)));

  pos.client = { x: 48, y: cy, role: "client", scale: 0.9 };

  const radius = Math.min(w * 0.38, h * 0.44, 60 + n * 42);

  running.forEach((node, i) => {
    const angle = -Math.PI / 2 + (i / Math.max(n, 1)) * Math.PI * 2;
    const isLeader = node.state === 2 || node.stateName === "leader";
    const r = isLeader ? radius * 1.04 : radius;
    pos[node.id] = {
      x: cx + Math.cos(angle) * r,
      y: cy + Math.sin(angle) * r * 0.88,
      role: isLeader ? "leader" : "follower",
      scale,
    };
  });

  const offline = nodes.filter((node) => node.running === false);
  offline.forEach((node, i) => {
    const gap = 130 * scale;
    const totalW = Math.max(0, offline.length - 1) * gap;
    pos[node.id] = {
      x: cx - totalW / 2 + i * gap,
      y: h - 48,
      role: "offline",
      scale: scale * 0.88,
    };
  });

  return pos;
}

export function quorumNeeded(nodeCount) {
  return Math.floor(nodeCount / 2) + 1;
}
