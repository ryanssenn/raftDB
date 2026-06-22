# Raft Playground

Interactive distributed-systems laboratory powered by a production-quality Raft implementation in Go.

## Quick start

```bash
go run ./visualizer --no-browser --sandbox
```

Open [http://localhost:8080](http://localhost:8080). Configure a 3–9 node cluster, click **Start**, and experiment.

### Guided tour (auto-run)

```bash
go run ./visualizer --no-browser visualizer/scenarios/showcase.json
```

## Modes

| Mode | Command | Behavior |
|---|---|---|
| **Sandbox** (default) | `go run ./visualizer --sandbox` | UI controls cluster lifecycle |
| **Guided tour** | `go run ./visualizer <scenario.json>` | Auto-starts cluster and runs scripted steps |

## Sandbox controls

### Cluster
- **Nodes** slider (3–9) + **Configure** prepares the cluster
- **Start** / **Stop** boot or kill all node processes

### Clients
- Up to three clients (`client-A`, `client-B`, `client-C`)
- **Write** / **Read** sends requests to any running node (follower forwarding is animated)

### Failure lab
- Select nodes → **Kill** / **Restart**
- **Network partition**: select isolated nodes → **Isolate** (blocks gRPC between partitions)
- **Clear** restores connectivity

### Guided tours
Preset scenarios in `visualizer/scenarios/`:
- `showcase.json`: full lifecycle loop
- `election.json`: leader re-election
- `failure.json`: leader kill and recovery
- `partition.json`: network split and healing
- `persistence.json`: restart and verify data

Load a tour from the sidebar, then **Run** / **Pause**.

## API reference

| Method | Path | Description |
|---|---|---|
| GET | `/api/stream` | SSE stream of status + events |
| GET | `/api/cluster/status` | Node snapshots |
| POST | `/api/cluster/create` | `{"nodes": 5}` |
| POST | `/api/cluster/start` | Boot cluster |
| POST | `/api/cluster/stop` | Stop cluster |
| POST | `/api/cluster/nodes/{id}/kill` | Kill node |
| POST | `/api/cluster/nodes/{id}/restart` | Restart node |
| POST | `/api/cluster/partition` | `{"isolated": ["node1"]}` |
| POST | `/api/cluster/partition/clear` | Restore network |
| POST | `/api/request` | `{"client","op","key","value","node"}` |
| POST | `/api/scenario/load` | `{"path": "visualizer/scenarios/election.json"}` |
| POST | `/api/scenario/run` | Start loaded scenario |
| POST | `/api/scenario/pause` | Toggle pause |

## Scenario JSON format

Each step must have exactly one action:

```json
{ "wait": "2s" }
{ "put": { "node": "node1", "key": "k", "value": "v" } }
{ "get": { "node": "node2", "key": "k", "expect": "v" } }
{ "kill": "leader" }
{ "restart": "killed" }
{ "partition": { "isolated": ["node1", "node2"] } }
{ "clear_partition": true }
```

## Ports

For N nodes: HTTP `8001…8000+N`, gRPC `9001…9000+N`. The playground calls `KillPorts` on startup to free them.

## Architecture

```
Browser UI  ←SSE/REST→  Playground server  ←HTTP→  ryanDB nodes (gRPC Raft)
                              ↓
                     scenario driver (guided tours)
```

See [docs/playground.md](../docs/playground.md) for a full user guide and [docs/guide.md](../docs/guide.md) for Raft internals.
