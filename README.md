# ryanDB

Go implementation of Raft with an observability demo on top. Start a real cluster, run a guided demo, and watch throughput and consensus metrics live.

This is for learning, not production. The Raft code is tested (unit + integration) and benchmarked.

## Try it

**Prerequisite:** [Docker Desktop](https://www.docker.com/products/docker-desktop/) must be running (for Prometheus).

```bash
go run ./observatory
```

This single command:

1. Starts Prometheus via Docker
2. Opens http://localhost:8080 with live cluster topology and native metrics charts
3. Click **Start Demo** to boot a 5-node cluster and run the 45-second production scenario

Metrics reference: [docs/observability.md](docs/observability.md)

## Optimizations

ryanDB was profiled and tuned on the write path (consensus + replication + disk). Each change was benchmarked in isolation; only measurable wins were kept. Full before/after numbers and methodology: [OPTIMIZATIONS.md](OPTIMIZATIONS.md). Roadmap of ideas not yet implemented: [docs/performance.md](docs/performance.md).

On a 3-node cluster, cumulative improvements raised write throughput at 64 clients from **~2.4k to ~20k ops/s** (~8×) and cut write p99 latency from **~60 ms to ~6 ms** (~10×). Read throughput stayed near **~70k ops/s** — reads were already fast because they skip consensus.

What changed:

| Optimization | What it does |
|---|---|
| **Skip replication sleep** | Replicators no longer sleep 10 ms after every successful AppendEntries; sleep only on idle heartbeats |
| **Group commit** | Batch log appends and `fsync` once before replication instead of per entry |
| **Single fsync per batch** | Leader skips redundant syncs when the log is already flushed |
| **Cached serialized entries** | Log entries store pre-marshaled bytes so replication does not re-encode JSON every RPC |
| **Wake replicators on append** | New writes signal idle follower goroutines immediately instead of waiting out a sleep |
| **Remove replication backoff** | Retry log mismatches without a 1 ms delay |

Reproduce the numbers:

```bash
go run ./benchmarks --quick --concurrency=1,16,64
```

## Benchmarks

3-node cluster on a single host ([full report](benchmarks/REPORT.md); post-optimization snapshot):

| Metric | Result |
|---|---|
| Read throughput (peak) | ~72,000 ops/sec |
| Write throughput (64 clients) | ~19,500 ops/sec |
| Read latency, p99 (16 clients) | ~1.3 ms |
| Write latency, p99 (16 clients) | ~4 ms |
| Failover recovery after leader crash | ~327 ms |

## The Raft implementation

A Go implementation of the [Raft paper](https://raft.github.io/raft.pdf) with a small in-memory key-value store on top.

Read the code: [docs/guide.md](docs/guide.md)

## Tests

```bash
go test -race ./core
go test -v ./test
go test ./observatory/...
```

## Running a cluster manually

Each node needs an HTTP port (`--port`) and a gRPC port (in `--peers` as `id=host:port`). Start at least three nodes for a working cluster.

```bash
go build -o ryanDB .

./ryanDB \
  --id=node1 \
  --port=8001 \
  --peers=node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003 \
  --reset=true
```

Per-node Prometheus metrics are at `/metrics` (disable with `--metrics=false`).

## Not yet implemented

- Log compaction / snapshots
- Dynamic cluster membership
