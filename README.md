# Quorum

A Go implementation of the Raft consensus algorithm with built-in observability. Start a real cluster, run a predefined workload, and inspect consensus and performance metrics through Prometheus.

## Try it

Prerequisite: [Docker Desktop](https://www.docker.com/products/docker-desktop/) must be running (used to start Prometheus).

```bash
go run ./playground
```

This command:

1. Starts Prometheus in Docker.
2. Opens [http://localhost:8080](http://localhost:8080).
3. Lets you start a 5-node cluster and run a 45-second workload.

Metrics are documented in [docs/observability.md](docs/observability.md).

## Optimizations

The write path (consensus, log replication, and disk persistence) was profiled, optimized, and re-benchmarked after every change. An optimization was kept only if it produced a measurable, repeatable improvement; changes that didn't move the numbers were reverted.

On a 3-node cluster at 64 concurrent clients, the optimization work took write throughput from 2,444 to 20,249 ops/s (8.3×) and write p99 latency from 60.19 ms to 6.09 ms (9.9×). Read throughput was unaffected (~69,000 ops/s): reads are served directly from the leader's state machine and never enter the Raft log.

Full methodology, including the smaller tweaks and the changes that were tried and reverted, is in [OPTIMIZATIONS.md](OPTIMIZATIONS.md).

To reproduce the benchmarks:

```bash
go run ./benchmarks --quick --concurrency=1,16,64
```

## Benchmarks

Results from a 3-node cluster on a single host ([full report](benchmarks/REPORT.md)); measured on a Cursor Cloud VM (4 vCPUs, 16 GB RAM, Go 1.24.0) with all nodes on loopback.

| Metric                          | Result          |
| ------------------------------- | --------------- |
| Peak read throughput            | 72,356 ops/s    |
| Write throughput (64 clients)   | 19,463 ops/s    |
| Read latency, p99 (16 clients)  | 1.33 ms         |
| Write latency, p99 (16 clients) | 4.0 ms          |
| Leader failover recovery        | 327 ms          |

## Raft implementation

An implementation of the Raft consensus algorithm from the original paper, with a simple in-memory key-value store built on top of the replicated log.

Implementation guide: [docs/guide.md](docs/guide.md)

## Tests

```bash
go test -race ./core
go test -v ./test
go test ./playground/...
```

## Running a cluster manually

Each node requires an HTTP port (`--port`) and a gRPC endpoint specified in `--peers` using the format `id=host:port`. At least three nodes are required.

```bash
go build -o quorum .

./quorum \
  --id=node1 \
  --port=8001 \
  --peers=node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003 \
  --reset=true
```

Each node exposes Prometheus metrics at `/metrics`. Disable metrics with `--metrics=false`.

## Not yet implemented

- Log compaction / snapshots
- Dynamic cluster membership
