const SVG = {
  centerX: 500,
  centerY: 280,
  clientY: 580,
  radius: 200,
};

export function computeLayout(nodes, clients) {
  const pos = {};
  const running = nodes.filter((n) => n.running !== false);
  const leader = running.find((n) => n.state === 2 || n.stateName === "leader");
  const others = running.filter((n) => n !== leader);
  const n = others.length;

  if (leader) {
    pos[leader.id] = { x: SVG.centerX, y: SVG.centerY + 40, role: "leader" };
  }

  if (n === 0 && leader) {
    // single node
  } else if (n <= 4) {
    const span = Math.min(700, 180 * n);
    const startX = SVG.centerX - span / 2;
    others.forEach((node, i) => {
      pos[node.id] = {
        x: startX + (n === 1 ? span / 2 : (span / (n - 1)) * i),
        y: SVG.centerY - 160,
        role: "follower",
      };
    });
  } else {
    const angleStart = -Math.PI * 0.85;
    const angleEnd = -Math.PI * 0.15;
    others.forEach((node, i) => {
      const t = n === 1 ? 0.5 : i / (n - 1);
      const angle = angleStart + t * (angleEnd - angleStart);
      pos[node.id] = {
        x: SVG.centerX + Math.cos(angle) * SVG.radius,
        y: SVG.centerY - 60 + Math.sin(angle) * (SVG.radius * 0.55),
        role: "follower",
      };
    });
  }

  // offline nodes along bottom edge
  const offline = nodes.filter((n) => n.running === false);
  offline.forEach((node, i) => {
    pos[node.id] = {
      x: 120 + i * 100,
      y: SVG.centerY + 180,
      role: "offline",
    };
  });

  const clientColors = ["#58c4dd", "#b39cd0", "#ffd166"];
  clients.forEach((id, i) => {
    const total = clients.length;
    const span = Math.min(400, 120 * total);
    const startX = SVG.centerX - span / 2;
    pos[id] = {
      x: total === 1 ? SVG.centerX : startX + (total === 1 ? 0 : (span / (total - 1)) * i),
      y: SVG.clientY,
      role: "client",
      color: clientColors[i % clientColors.length],
    };
  });

  return pos;
}

export function quorumNeeded(nodeCount) {
  return Math.floor(nodeCount / 2) + 1;
}

export { SVG };
