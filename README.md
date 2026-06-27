# Quorum

A Go implementation of the [Raft consensus algorithm](https://raft.github.io/raft.pdf) with built-in observability. Quorum implements the core Raft protocol with a replicated in-memory key-value store, allowing you to start a real cluster, run a predefined workload, and inspect consensus and performance metrics through Prometheus.


## Raft implementation

Implements the core Raft protocol from the original paper, including:

- Leader election
- Log replication
- Persistent state
- Log compaction through state-machine snapshots
- `InstallSnapshot` RPC for catching up far-behind followers

The replicated state machine is a simple in-memory key-value store.

Implementation guide: [docs/guide.md](docs/guide.md)

## Benchmarks

Results from a 3-node cluster on a single host ([full report](benchmarks/REPORT.md)); measured on a Cursor Cloud VM (4 vCPUs, 16 GB RAM, Go 1.24.0) with all nodes communicating over loopback.

| Metric                          | Result          |
| ------------------------------- | --------------- |
| Peak read throughput            | 94,501 ops/s    |
| Write throughput (64 clients)   | 28,096 ops/s    |
| Read latency, p99 (16 clients)  | 1.04 ms         |
| Write latency, p99 (16 clients) | 2.8 ms          |
| Leader failover recovery        | ~1.4 s          |

These numbers measure implementation overhead on a single machine rather than network performance across multiple hosts.

The write path (consensus, log replication, and disk persistence) was profiled, optimized, and benchmarked before and after every change. Optimizations that did not produce repeatable improvements were reverted.

Full methodology, including benchmark configuration, optimization results, and reverted experiments, is documented in [OPTIMIZATIONS.md](OPTIMIZATIONS.md).

To reproduce the benchmarks:

```bash
go run ./benchmarks --quick --concurrency=1,16,64
```

## Observability

<img width="800" height="404" alt="quorum_demo" src="https://github.com/user-attachments/assets/4eb1e2e3-e883-48a3-996a-cb1ad600c111" />

Prerequisite: [Docker Desktop](https://www.docker.com/products/docker-desktop/) must be running (used to start Prometheus).

```bash
go run ./playground
```

Metrics are documented in [docs/observability.md](docs/observability.md).

## Tests

```bash
go test -race ./core
go test -v ./test
go test ./playground/...
```

## Running a cluster manually

Each node exposes an HTTP API on `--port`. Raft RPC addresses are configured through `--peers` using the format `id=host:port`. At least three nodes are required.

```bash
go build -o quorum .

./quorum \
  --id=node1 \
  --port=8001 \
  --peers=node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003 \
  --reset=true
```

Each node exposes Prometheus metrics at `/metrics`. Disable metrics with `--metrics=false`.

## Limitations

- Dynamic cluster membership
- Segmented log files (compaction currently rewrites a single log file)
