# Observatory

Live observability console for ryanDB. Runs a real Raft cluster, drives a guided demo, and shows throughput, latency, and consensus metrics with native charts—no Grafana required.

## Prerequisite

[Docker Desktop](https://www.docker.com/products/docker-desktop/) must be running (Prometheus only).

## Quick start

```bash
go run ./observatory
```

Opens http://localhost:8080 with:

- **Start Demo** — one click starts the 5-node cluster and runs the ~45s production narrative (writes, follower failure, leader failover)
- **Cluster topology** — glass-style node cards, gradient replication beams, animated write flows
- **Metrics strip** — write/read throughput, p99 latency, replication lag, failover time

Nothing runs until you click **Start Demo**. Use `--bootstrap` to auto-start the cluster (without running the demo).

## CLI flags

| Flag | Default | Description |
|---|---|---|
| `--port` | 8080 | Observatory HTTP port |
| `--no-browser` | false | Skip opening browser |
| `--no-compose` | false | Do not start Prometheus (for tests) |
| `--no-bootstrap` | true | Do not auto-start cluster on launch |
| `--bootstrap` | false | Auto-start cluster on launch (demo still waits for Start Demo) |
| `--scenario` | full-demo | Scenario path when auto-bootstrapping |
| `--binary` | auto-build | Path to ryanDB binary |
| `--demo` | true | Compress scenario wait times (ignored when scenario has `"realtime": true`) |

## Scenario format

Steps can include a sustained **`load`** action:

```json
{ "load": { "duration": "12s", "interval": "350ms", "keyPrefix": "tx" }, "comment": "steady load" }
```

Set `"realtime": true` on the scenario to run at wall-clock pace (required for the 45s production demo).

## API

| Method | Path | Description |
|---|---|---|
| GET | `/api/ready` | Readiness checks |
| GET | `/api/metrics/live` | Live throughput, latency, lag, failover + sparkline history |
| POST | `/api/scenario/demo` | Run the full demo scenario |
| GET | `/api/cluster/status` | Node status snapshot |
| POST | `/api/cluster/create` | `{"nodes": N}` |
| POST | `/api/cluster/start` | Start cluster |
| POST | `/api/cluster/stop` | Stop cluster |
| GET | `/api/scenario` | Scenario state (`phase`, `writeCount`, `lastWrite`) |

## Docs

- [Observability metrics guide](../docs/observability.md)
- [Monitoring stack](../monitoring/README.md)
- [Raft internals](../docs/guide.md)
