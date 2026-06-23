# Playground

Web UI for ryanDB. Boots a Raft cluster, runs stress test scenarios, and shows throughput, latency, and consensus metrics.

## Prerequisite

[Docker Desktop](https://www.docker.com/products/docker-desktop/) must be running (Prometheus + Grafana).

## Quick start

```bash
go run ./playground
```

Opens http://localhost:8080. Click **Run stress test** to start a 5-node cluster with 32 concurrent writers, node failures, and leader failover.

Use `--bootstrap` to auto-start the cluster (the stress test still waits for the button).

## CLI flags

| Flag | Default | Description |
|---|---|---|
| `--port` | 8080 | HTTP port |
| `--no-browser` | false | Skip opening browser |
| `--no-compose` | false | Do not start Prometheus/Grafana (for tests) |
| `--no-bootstrap` | true | Do not auto-start cluster on launch |
| `--bootstrap` | false | Auto-start cluster on launch |
| `--scenario` | full-demo | Scenario path when auto-bootstrapping |
| `--binary` | auto-build | Path to ryanDB binary |
| `--demo` | true | Compress scenario wait times (ignored when scenario has `"realtime": true`) |
| `--keep-monitoring` | false | Leave Prometheus/Grafana running after exit |

## Quitting

Use **Stop & quit** in the UI sidebar, or **Ctrl+C** in the terminal. This stops the cluster, frees port 8080, and tears down the Docker monitoring stack (unless `--keep-monitoring`).

If port 8080 is already in use, a previous playground is probably still running:

```bash
lsof -ti :8080 | xargs kill
```

## Scenario format

Load steps use concurrent workers like `benchmarks/`:

```json
{ "load": { "duration": "15s", "concurrency": 32, "keyPrefix": "tx" }, "comment": "stress load" }
```

Per-node control (while cluster is running):

| Method | Path | Body |
|---|---|---|
| POST | `/api/cluster/node/stop` | `{"id": "node1"}` |
| POST | `/api/cluster/node/start` | `{"id": "node1"}` |

## API

| Method | Path | Description |
|---|---|---|
| GET | `/api/ready` | Readiness checks |
| GET | `/api/metrics/live` | Live throughput, latency, lag, failover + history |
| POST | `/api/scenario/stress-test` | Run the full stress test scenario |
| GET | `/api/cluster/status` | Node status snapshot |
| POST | `/api/cluster/create` | `{"nodes": N}` |
| POST | `/api/cluster/start` | Start cluster |
| POST | `/api/cluster/stop` | Stop cluster |
| GET | `/api/scenario` | Scenario state |

## Docs

- [Observability metrics guide](../docs/observability.md)
- [Monitoring stack](../monitoring/README.md)
- [Raft internals](../docs/guide.md)
