import { updateHUD } from "./events.js";

const API = {
  async post(path, body) {
    const res = await fetch(path, {
      method: "POST",
      headers: body ? { "Content-Type": "application/json" } : {},
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || res.statusText);
    }
    return res.json().catch(() => ({}));
  },
};

export class Controls {
  constructor(stream) {
    this.stream = stream;
    this.selectedNodes = new Set();
    this.partitionNodes = new Set();
    this.activeClient = "client-A";
    this.clients = ["client-A"];
    this.clusterStarted = false;
    this.bindElements();
    this.renderClientTabs();
  }

  bindElements() {
    this.els = {
      nodeCount: document.getElementById("node-count"),
      nodeCountVal: document.getElementById("node-count-val"),
      btnCreate: document.getElementById("btn-create"),
      btnStart: document.getElementById("btn-start"),
      btnStop: document.getElementById("btn-stop"),
      health: document.getElementById("cluster-health"),
      targetNode: document.getElementById("target-node"),
      reqKey: document.getElementById("req-key"),
      reqValue: document.getElementById("req-value"),
      btnPut: document.getElementById("btn-put"),
      btnGet: document.getElementById("btn-get"),
      btnAddClient: document.getElementById("btn-add-client"),
      nodeActions: document.getElementById("node-actions"),
      partitionNodes: document.getElementById("partition-nodes"),
      btnKill: document.getElementById("btn-kill"),
      btnRestart: document.getElementById("btn-restart"),
      btnPartition: document.getElementById("btn-partition"),
      btnClearPartition: document.getElementById("btn-clear-partition"),
      eventLog: document.getElementById("event-log"),
      tourSelect: document.getElementById("tour-select"),
      btnTourLoad: document.getElementById("btn-tour-load"),
      btnTourRun: document.getElementById("btn-tour-run"),
      btnTourPause: document.getElementById("btn-tour-pause"),
    };

    this.els.nodeCount.addEventListener("input", () => {
      this.els.nodeCountVal.textContent = this.els.nodeCount.value;
    });

    this.els.btnCreate.addEventListener("click", () => this.createCluster());
    this.els.btnStart.addEventListener("click", () => this.startCluster());
    this.els.btnStop.addEventListener("click", () => this.stopCluster());
    this.els.btnPut.addEventListener("click", () => this.sendRequest("put"));
    this.els.btnGet.addEventListener("click", () => this.sendRequest("get"));
    this.els.btnAddClient.addEventListener("click", () => this.addClient());
    this.els.btnKill.addEventListener("click", () => this.killSelected());
    this.els.btnRestart.addEventListener("click", () => this.restartSelected());
    this.els.btnPartition.addEventListener("click", () => this.applyPartition());
    this.els.btnClearPartition.addEventListener("click", () => this.clearPartition());
    this.els.btnTourLoad.addEventListener("click", () => this.loadTour());
    this.els.btnTourRun.addEventListener("click", () => this.runTour());
    this.els.btnTourPause.addEventListener("click", () => this.pauseTour());
  }

  onState(state) {
    this.clusterStarted = !!state.clusterStarted;
    updateHUD(state);
    this.updateHealth(state);
    this.updateNodeChips(state.nodes || []);
    this.updateLog(state.log || []);
    this.updateButtons(state);
  }

  updateHealth(state) {
    const nodes = state.nodes || [];
    const running = nodes.filter((n) => n.running).length;
    const total = nodes.length;
    const el = this.els.health;
    if (!state.clusterStarted) {
      el.textContent = "Not started";
      el.className = "health-chip";
    } else if (running === 0) {
      el.textContent = "All nodes stopped";
      el.className = "health-chip error";
    } else {
      const leader = nodes.some((n) => n.running && (n.state === 2 || n.stateName === "leader"));
      el.textContent = leader
        ? `${running}/${total} nodes · leader elected`
        : `${running}/${total} nodes · electing…`;
      el.className = "health-chip " + (leader ? "healthy" : "warning");
    }
  }

  updateNodeChips(nodes) {
    const render = (container, selected, onClick, extraClass) => {
      container.innerHTML = "";
      for (const node of nodes) {
        const chip = document.createElement("button");
        chip.type = "button";
        chip.className = "node-chip";
        if (!node.running) chip.classList.add("offline");
        if (selected.has(node.id)) chip.classList.add("selected");
        if (extraClass && extraClass(node)) chip.classList.add(extraClass(node));
        chip.textContent = node.id;
        chip.addEventListener("click", () => onClick(node.id, chip));
        container.appendChild(chip);
      }
    };

    render(this.els.nodeActions, this.selectedNodes, (id, chip) => {
      if (this.selectedNodes.has(id)) this.selectedNodes.delete(id);
      else this.selectedNodes.add(id);
      chip.classList.toggle("selected");
    });

    render(this.els.partitionNodes, this.partitionNodes, (id, chip) => {
      if (this.partitionNodes.has(id)) this.partitionNodes.delete(id);
      else this.partitionNodes.add(id);
      chip.classList.toggle("selected");
    }, (node) => (node.isolated ? "isolated" : null));

    const select = this.els.targetNode;
    const prev = select.value;
    select.innerHTML = "";
    for (const node of nodes) {
      if (!node.running) continue;
      const opt = document.createElement("option");
      opt.value = node.id;
      opt.textContent = node.id;
      select.appendChild(opt);
    }
    if (prev && [...select.options].some((o) => o.value === prev)) select.value = prev;
  }

  updateLog(lines) {
    const el = this.els.eventLog;
    el.innerHTML = lines.slice(-80).map((l) => `<div class="line">${escapeHtml(l)}</div>`).join("");
    el.scrollTop = el.scrollHeight;
  }

  updateButtons(state) {
    const started = !!state.clusterStarted;
    this.els.btnStart.disabled = started;
    this.els.btnStop.disabled = !started;
    this.els.btnPut.disabled = !started;
    this.els.btnGet.disabled = !started;
    this.els.btnKill.disabled = this.selectedNodes.size === 0;
    this.els.btnRestart.disabled = this.selectedNodes.size === 0;
    this.els.btnPartition.disabled = this.partitionNodes.size === 0;
    this.els.btnClearPartition.disabled = !state.partitionActive;
    this.els.btnTourRun.disabled = !started;
  }

  async createCluster() {
    const nodes = parseInt(this.els.nodeCount.value, 10);
    try {
      await API.post("/api/cluster/create", { nodes });
      this.selectedNodes.clear();
      this.partitionNodes.clear();
    } catch (e) {
      alert(e.message);
    }
  }

  async startCluster() {
    try {
      await API.post("/api/cluster/start");
    } catch (e) {
      alert(e.message);
    }
  }

  async stopCluster() {
    try {
      await API.post("/api/cluster/stop");
      this.selectedNodes.clear();
    } catch (e) {
      alert(e.message);
    }
  }

  async sendRequest(op) {
    const node = this.els.targetNode.value;
    const key = this.els.reqKey.value.trim();
    if (!node || !key) return;
    try {
      const body = {
        client: this.activeClient,
        op,
        key,
        value: this.els.reqValue.value,
        node,
      };
      const res = await API.post("/api/request", body);
      if (op === "get" && res.result) {
        this.els.reqValue.value = res.result;
      }
    } catch (e) {
      alert(e.message);
    }
  }

  addClient() {
    const n = this.clients.length;
    const id = `client-${String.fromCharCode(65 + n)}`;
    if (n >= 3) return;
    this.clients.push(id);
    this.activeClient = id;
    this.stream.setClients(this.clients);
    this.renderClientTabs();
  }

  renderClientTabs() {
    const container = document.getElementById("client-tabs");
    container.innerHTML = "";
    for (const id of this.clients) {
      const tab = document.createElement("button");
      tab.type = "button";
      tab.className = "client-tab" + (id === this.activeClient ? " active" : "");
      tab.textContent = id;
      tab.addEventListener("click", () => {
        this.activeClient = id;
        this.renderClientTabs();
      });
      container.appendChild(tab);
    }
    this.stream.setClients(this.clients);
  }

  async killSelected() {
    for (const id of this.selectedNodes) {
      try {
        await API.post(`/api/cluster/nodes/${id}/kill`);
      } catch (e) {
        alert(e.message);
      }
    }
    this.selectedNodes.clear();
  }

  async restartSelected() {
    for (const id of this.selectedNodes) {
      try {
        await API.post(`/api/cluster/nodes/${id}/restart`);
      } catch (e) {
        alert(e.message);
      }
    }
    this.selectedNodes.clear();
  }

  async applyPartition() {
    try {
      await API.post("/api/cluster/partition", {
        isolated: [...this.partitionNodes],
      });
    } catch (e) {
      alert(e.message);
    }
  }

  async clearPartition() {
    try {
      await API.post("/api/cluster/partition/clear");
      this.partitionNodes.clear();
    } catch (e) {
      alert(e.message);
    }
  }

  async loadTour() {
    const path = this.els.tourSelect.value;
    if (!path) return;
    try {
      await API.post("/api/scenario/load", { path });
    } catch (e) {
      alert(e.message);
    }
  }

  async runTour() {
    try {
      await API.post("/api/scenario/run");
    } catch (e) {
      alert(e.message);
    }
  }

  async pauseTour() {
    try {
      await API.post("/api/scenario/pause");
    } catch (e) {
      alert(e.message);
    }
  }
}

function escapeHtml(s) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
