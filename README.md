# Raft Playground

An **interactive distributed-systems laboratory** for exploring Raft consensus — powered by a correct, tested, and performant Go implementation.

Run real multi-node clusters, send client requests, kill nodes, simulate network partitions, and watch leader elections, log replication, and commits animate in real time. The underlying Raft engine is unchanged: same correctness guarantees, same test suite, same benchmark numbers.

```bash
go run ./visualizer --no-browser --sandbox
```

Open **http://localhost:8080** — configure a cluster, click Start, and experiment.

## What you'll observe

- **Client request flow** — writes to any node, forwarding to the leader
- **Leader election** — randomized timeouts, voting, term increments
- **Log replication** — append entries, acks, quorum tracking
- **Node failures & recovery** — kill, restart, catch-up replication
- **Network partitions** — isolate nodes, see quorum enforce safety
- **Guided tours** — preset scenarios that walk through each concept

Full user guide: [docs/playground.md](docs/playground.md)

## Playground features

| Feature | Description |
|---|---|
| Configurable cluster | 3–9 nodes, start/stop at runtime |
| Multiple clients | Up to 3 independent client actors |
| Failure lab | Kill, restart, network partition per node |
| Live visualization | SSE-driven animations with educational callouts |
| Guided tours | Lifecycle, election, failure, partition, persistence |

## Correctness & testing

This is an educational implementation, not a production database — but it is rigorously tested:

```bash
go test -race -count=1 -timeout 5m ./core      # 7 unit tests
go test -count=1 -timeout 10m -v ./test         # 10 integration tests
go test -count=1 -timeout 5m ./visualizer/... # playground API tests
```

Integration tests launch a real 5-node cluster and verify elections, replication, persistence, partitions, and concurrent writes. Details: [docs/development/testing.md](docs/development/testing.md)

Raft internals walkthrough: [docs/guide.md](docs/guide.md)

## Performance

3-node cluster on a single host ([full report](benchmarks/REPORT.md)):

| Metric | Result |
|---|---|
| Read throughput (peak) | ~72,000 ops/sec |
| Write throughput (64 clients) | ~19,500 ops/sec |
| Read latency, p99 (16 clients) | ~1.3 ms |
| Write latency, p99 (16 clients) | ~4 ms |
| Failover recovery after leader crash | ~327 ms |

Re-run with `go run ./benchmarks` on your machine. Summary: [docs/performance/README.md](docs/performance/README.md)

## Running manually (CLI)

Each node needs an HTTP port (`--port`) and a gRPC port (in `--peers`). Start at least three nodes:

```bash
go build -o ryanDB .

./ryanDB \
  --id=node1 \
  --port=8001 \
  --peers=node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003 \
  --reset=true
```

HTTP API: `GET /put?key=&value=`, `GET /get?key=`, `GET /status`

## Layout

| Path | Purpose |
|---|---|
| [docs/playground.md](docs/playground.md) | Playground user guide |
| [docs/guide.md](docs/guide.md) | Raft internals walkthrough |
| [docs/development/testing.md](docs/development/testing.md) | Test inventory & CI |
| [docs/performance/](docs/performance/) | Benchmarks & optimization history |
| `core/` | Raft logic |
| `visualizer/` | Interactive playground |
| `test/` | Integration tests |
| `benchmarks/` | Load harness & report |

## Not yet implemented

- Log compaction / snapshots
- Dynamic cluster membership
