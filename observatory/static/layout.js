export function computeLayout(nodes, bounds = { width: 1000, height: 420 }) {
  const pos = {};
  const w = bounds.width;
  const h = bounds.height;
  const cx = w / 2;
  const cy = h * 0.48;
  const radius = Math.min(w, h) * 0.34;
  const scale = Math.max(0.75, Math.min(1.2, w / 1000));

  pos.client = { x: cx, y: h - 52, role: "client", scale: 1 };

  const running = nodes.filter((n) => n.running !== false);
  const leader = running.find((n) => n.state === 2 || n.stateName === "leader");
  const others = running.filter((n) => n !== leader);
  const n = others.length;

  if (leader) {
    pos[leader.id] = { x: cx, y: cy + radius * 0.42, role: "leader", scale };
  }

  if (n > 0) {
    const angleStart = -Math.PI * 0.92;
    const angleEnd = -Math.PI * 0.08;
    others.forEach((node, i) => {
      const t = n === 1 ? 0.5 : i / (n - 1);
      const angle = angleStart + t * (angleEnd - angleStart);
      pos[node.id] = {
        x: cx + Math.cos(angle) * radius,
        y: cy - radius * 0.35 + Math.sin(angle) * (radius * 0.45),
        role: "follower",
        scale,
      };
    });
  }

  const offline = nodes.filter((node) => node.running === false);
  offline.forEach((node, i) => {
    pos[node.id] = {
      x: 80 + i * (110 * scale),
      y: h - 60,
      role: "offline",
      scale,
    };
  });

  return pos;
}

export function quorumNeeded(nodeCount) {
  return Math.floor(nodeCount / 2) + 1;
}
