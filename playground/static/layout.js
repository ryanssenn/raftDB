export function leaderSlotPosition(bounds = { width: 1000, height: 420 }) {
  const w = bounds.width;
  const h = bounds.height;
  const clientX = Math.max(56, w * 0.06);
  return {
    x: clientX + Math.min(210, w * 0.2),
    y: h * 0.5,
  };
}

export function computeLayout(nodes, bounds = { width: 1000, height: 420 }) {
  const pos = {};
  const w = bounds.width;
  const h = bounds.height;
  const cy = h * 0.5;

  const running = nodes.filter((n) => n.running !== false);
  const n = running.length;
  const scale = Math.max(0.68, Math.min(1.0, 4.8 / Math.max(n, 1)));

  const clientX = Math.max(56, w * 0.06);
  pos.client = { x: clientX, y: cy, role: "client", scale: 0.9 };

  const slot = leaderSlotPosition(bounds);
  pos._leaderSlot = { ...slot, scale: scale * 1.02 };

  const leaderNode = running.find(
    (node) => node.state === 2 || node.stateName === "leader"
  );
  const followers = running
    .filter((node) => node !== leaderNode)
    .sort((a, b) => a.id.localeCompare(b.id));

  const clusterCx = w * 0.64;
  const radius = Math.min(w * 0.26, h * 0.4, 56 + Math.max(followers.length, 1) * 36);
  const start = -Math.PI * 0.42;
  const end = Math.PI * 0.42;

  followers.forEach((node, i) => {
    const count = followers.length;
    const angle = count === 1 ? 0 : start + (i / (count - 1)) * (end - start);
    pos[node.id] = {
      x: clusterCx + Math.cos(angle) * radius,
      y: cy + Math.sin(angle) * radius,
      role: "follower",
      scale,
      slotIndex: i,
    };
  });

  if (leaderNode) {
    pos[leaderNode.id] = {
      ...slot,
      role: "leader",
      scale: scale * 1.02,
    };
  }

  const offline = nodes.filter((node) => node.running === false);
  offline.forEach((node, i) => {
    const gap = 130 * scale;
    const totalW = Math.max(0, offline.length - 1) * gap;
    pos[node.id] = {
      x: w * 0.58 - totalW / 2 + i * gap,
      y: h - 52,
      role: "offline",
      scale: scale * 0.88,
    };
  });

  return pos;
}

export function quorumNeeded(nodeCount) {
  return Math.floor(nodeCount / 2) + 1;
}
