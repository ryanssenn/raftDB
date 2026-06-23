# Observability Guide

Reference for metrics, PromQL queries, Grafana panels, and alerts when monitoring a live Raft cluster with ryanDB.

## Architecture

```mermaid
flowchart LR
  Scenarios[Scenario runner] --> Nodes[ryanDB nodes]
  Nodes -->|"/metrics"| Prom[Prometheus]
  Playground[Playground :8080] -->|cluster metrics| Prom
  Prom --> Graf[Grafana :3000]
  Scenarios -->|annotations| Graf
```

Each node exposes Prometheus metrics at `/metrics`. The playground aggregates cluster-level metrics and drives scenario load. Grafana is optional.

---

## A. Machine / node state

| Metric | Type | Labels | Meaning | Visualization |
|---|---|---|---|---|
| `raftdb_state` | Gauge | `node` | 0=follower, 1=candidate, 2=leader | Stat panel or state timeline per node |
| `raftdb_is_leader` | Gauge | `node` | 1 if leader | Leader identity stat with threshold coloring |
| `raftdb_term` | Gauge | `node` | Current Raft term | Time series; spikes indicate elections |
| `raftdb_commit_index` | Gauge | `node` | Highest committed log index | Multi-series line; converging lines = healthy replication |
| `raftdb_last_applied` | Gauge | `node` | Highest applied log index | Line chart per node |
| `raftdb_apply_lag` | Gauge | `node` | `commit_index - last_applied` | Line; sustained lag = slow apply loop |
| `raftdb_log_length` | Gauge | `node` | Number of log entries | Line; divergence hints replication issues |
| `up{job="ryanDB"}` | Gauge | `instance` | Scrape success | Node up/down table |

**Useful PromQL**

```promql
# Current leader
max by (node) (raftdb_is_leader == 1)

# Cluster commit spread
max(raftdb_commit_index) - min(raftdb_commit_index)

# Apply lag per node
raftdb_commit_index - raftdb_last_applied
# or use raftdb_apply_lag directly
```

---

## B. Replication health (cluster-level)

| Metric | Type | Labels | Source | Visualization |
|---|---|---|---|---|
| `raftdb_replication_lag` | Gauge | `node` | Playground | Line; spikes during load or partition |
| `raftdb_leader_count` | Gauge | - | Playground | Stat; alert when `!= 1` |
| `raftdb_cluster_nodes` | Gauge | - | Playground | Configured cluster size |
| `raftdb_nodes_running` | Gauge | - | Playground | Processes currently up |

**Useful PromQL**

```promql
# Split brain or no leader
raftdb_leader_count

# Worst follower lag
max(raftdb_replication_lag)

# Nodes reachable by Prometheus
count(up{job="ryanDB"} == 1)
```

---

## C. Event rates (counters)

| Metric | Type | Labels | Meaning | Visualization |
|---|---|---|---|---|
| `raftdb_elections_total` | Counter | `node` | Elections started | `rate(...[1m])` bar gauge |
| `raftdb_commits_total` | Counter | `node` | Commits as leader | `rate(...[1m])` line during load |
| `raftdb_append_entries_total` | Counter | `node`, `result` | AppendEntries outcomes | Stacked area by result |
| `raftdb_requestvote_total` | Counter | `node`, `result` | Vote RPC outcomes | Line by result |

**Useful PromQL**

```promql
rate(raftdb_elections_total[1m])
rate(raftdb_commits_total[1m])
rate(raftdb_append_entries_total{result="success"}[1m])
rate(raftdb_append_entries_total{result="failure"}[1m])
rate(raftdb_append_entries_total{result="error"}[1m])
rate(raftdb_requestvote_total{result="granted"}[1m])
```

---

## D. Client / workload

| Metric | Type | Labels | Meaning | Visualization |
|---|---|---|---|---|
| `raftdb_client_requests_total` | Counter | `op`, `result`, `node` | HTTP put/get volume | Rate by op and result |
| `raftdb_client_request_duration_seconds` | Histogram | `op`, `node` | Request latency | Heatmap or p50/p99 |
| `raftdb_scenario_step` | Gauge | `scenario` | Current step index | Stat panel |
| `raftdb_scenario_running` | Gauge | — | 1 if scenario active | Stat panel |

**Useful PromQL**

```promql
sum by (op) (rate(raftdb_client_requests_total[1m]))
histogram_quantile(0.99, sum by (le, op) (rate(raftdb_client_request_duration_seconds_bucket[5m])))
```

---

## E. Recommended Grafana dashboard layout

### Row 1: Cluster health

- **Leader count** — `raftdb_leader_count` (threshold: green=1, red otherwise)
- **Current term** — `max(raftdb_term)`
- **Nodes running** — `raftdb_nodes_running / raftdb_cluster_nodes`
- **Commit spread** — `max(raftdb_commit_index) - min(raftdb_commit_index)`

### Row 2: Consensus and replication

- **Commit index by node** — `raftdb_commit_index`
- **Replication lag by node** — `raftdb_replication_lag`
- **Apply lag by node** — `raftdb_apply_lag`

### Row 3: Activity and stability

- **Election rate** — `rate(raftdb_elections_total[1m])`
- **Commit rate** — `rate(raftdb_commits_total[1m])`
- **AppendEntries success vs failure** — rates by `result`

### Row 4: Scenario context

- **Scenario step** — `raftdb_scenario_step`
- **Scenario running** — `raftdb_scenario_running`
- **Annotations**: kill, partition, load bursts marked by the playground

### Row 5: Node table

| Column | Query |
|---|---|
| Node | label `node` |
| Role | `raftdb_state` |
| Term | `raftdb_term` |
| Commit | `raftdb_commit_index` |
| Lag | `raftdb_replication_lag` |
| Up | `up{job="ryanDB"}` |

---

## F. Alert rules (reference)

| Alert | Expression | Duration | Meaning |
|---|---|---|---|
| NoSingleLeader | `raftdb_leader_count != 1` | 10s | Split brain or election in progress |
| HighReplicationLag | `max(raftdb_replication_lag) > 5` | 30s | Follower stuck behind leader |
| NodeDown | `up{job="ryanDB"} == 0` | 15s | Node process killed or unreachable |
| ElectionStorm | `sum(rate(raftdb_elections_total[5m])) > 0.5` | 2m | Unstable cluster |

---

## G. Scenario demos

| Scenario | What to watch |
|---|---|
| `steady-writes.json` | Commit rate rises; lag stays near zero |
| `leader-failure.json` | Term spike, election rate bump, brief lag |
| `partition.json` | Lag on minority; append failures rise |
| `recovery.json` | Lag decays after partition heal |

---

## H. Startup

**Prerequisite:** Docker Desktop must be running.

```bash
go run ./playground
```

Single command: starts Prometheus, boots a 5-node cluster, runs `full-demo.json`, opens the browser with metrics charts.

Live metrics API: `GET /api/metrics/live` (write/read throughput, p99 latency, replication lag, failover ms).

- Demo UI: http://localhost:8080
- Prometheus (proxied): http://localhost:8080/prometheus/

Disable per-node metrics when running ryanDB manually: `--metrics=false`.
