import { computeLayout, quorumNeeded } from "./layout.js";
import { AnimationEngine, eventLabel, eventCallout } from "./animation.js";

export class EventStream {
  constructor(renderer, anim, onState) {
    this.renderer = renderer;
    this.anim = anim;
    this.onState = onState;
    this.seen = new Set();
    this.nodes = [];
    this.clients = ["client-A"];
    this.pos = {};
    this.source = null;
  }

  connect() {
    if (this.source) this.source.close();
    this.source = new EventSource("/api/stream");
    this.source.onmessage = (ev) => {
      try {
        const data = JSON.parse(ev.data);
        this.handleSnapshot(data);
      } catch (_) { /* ignore */ }
    };
    this.source.onerror = () => {
      setTimeout(() => this.connect(), 2000);
    };
  }

  handleSnapshot(data) {
    const nodes = data.nodes || [];
    this.nodes = nodes;
    this.pos = computeLayout(nodes, this.clients);
    this.renderer.syncNodes(nodes, this.pos);
    this.renderer.syncClients(this.clients, this.pos);

    const events = data.events || [];
    for (const e of events) {
      const key = `${e.from}:${e.seq}:${e.type}:${e.to}:${e.ts}:${e.detail}`;
      if (this.seen.has(key)) continue;
      this.seen.add(key);
      this.animateEvent(e);
    }

    if (this.onState) {
      this.onState({
        nodes,
        clusterStarted: data.clusterStarted,
        partitionActive: data.partitionActive,
        partitionNodes: data.partitionNodes || [],
        log: data.log || [],
        scenario: data.scenario,
      });
    }
  }

  animateEvent(e) {
    const from = normalizeId(e.from);
    const to = normalizeId(e.to);
    const label = eventLabel(e);

    if (e.type === "commit") {
      this.anim.burstCommit(this.pos, this.nodes);
      showCallout(eventCallout(e));
      return;
    }

    if (e.type === "state_change") {
      showCallout(eventCallout(e));
      return;
    }

    if (e.type === "client_request") {
      this.anim.spawnFlow(from, to, this.pos, e.type, label, { curve: 30 });
      showCallout(eventCallout(e));
      return;
    }

    if (e.type === "forward_command") {
      this.anim.spawnFlow(from, to, this.pos, e.type, label, { curve: -20 });
      return;
    }

    if (e.type === "request_vote") {
      this.anim.spawnFlow(from, to, this.pos, e.type, label, { curve: 40, r: 5 });
      if (e.detail === "granted") showCallout(eventCallout(e));
      return;
    }

    if (e.type === "append_entries" || e.type === "append_response") {
      if (e.entries === 0 && e.type === "append_entries") {
        this.anim.spawnFlow(from, to, this.pos, e.type, "heartbeat", { curve: 10, r: 4 });
      } else {
        this.anim.spawnFlow(from, to, this.pos, e.type, label, { curve: 15 });
      }
      return;
    }
  }

  setClients(clients) {
    this.clients = clients;
    this.pos = computeLayout(this.nodes, this.clients);
    this.renderer.syncClients(this.clients, this.pos);
  }

  addClient(id) {
    if (!this.clients.includes(id)) {
      this.clients.push(id);
      this.setClients(this.clients);
    }
  }
}

function normalizeId(id) {
  if (!id || id === "client") return "client-A";
  if (id.startsWith("client")) return id;
  return id;
}

let calloutTimer;
function showCallout(text) {
  if (!text) return;
  const el = document.getElementById("callout");
  el.textContent = text;
  el.classList.remove("hidden");
  clearTimeout(calloutTimer);
  calloutTimer = setTimeout(() => el.classList.add("hidden"), 3200);
}

export function updateHUD(state) {
  const nodes = state.nodes || [];
  const running = nodes.filter((n) => n.running);
  const leader = running.find((n) => n.state === 2 || n.stateName === "leader");
  const maxCommit = running.reduce((m, n) => Math.max(m, n.commitIndex ?? -1), -1);

  document.getElementById("hud-term").textContent = leader?.term ?? running[0]?.term ?? "-";
  document.getElementById("hud-commit").textContent = maxCommit >= 0 ? maxCommit : "-";
  document.getElementById("hud-leader").textContent = leader?.id ?? "none";

  const needed = quorumNeeded(nodes.length || 5);
  let acks = 0;
  if (leader && leader.matchIndex) {
    acks = 1;
    for (const v of Object.values(leader.matchIndex)) {
      if (v >= maxCommit) acks++;
    }
  } else {
    acks = running.length;
  }
  document.getElementById("hud-quorum").textContent = `${Math.min(acks, needed)} / ${needed}`;
  document.getElementById("quorum-fill").style.width = `${Math.min(100, (acks / needed) * 100)}%`;
}

export { quorumNeeded };
