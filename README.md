# ryanDB

Go implementation of the [Raft paper](https://raft.github.io/raft.pdf) with an in-memory key-value store on top. This is a learning project: tested and benchmarked, not intended for production.

## Benchmarks

3-node cluster on a single host ([full report](benchmarks/REPORT.md)):

| Metric | Result |
|--------|--------|
| Read throughput (peak) | ~72,000 ops/sec |
| Write throughput (64 clients) | ~19,500 ops/sec |
| Read latency, p99 (16 clients) | ~1.3 ms |
| Write latency, p99 (16 clients) | ~4 ms |
| Failover recovery after leader crash | ~327 ms |

Reproduce:

```bash
go run ./benchmarks --quick --concurrency=1,16,64
```

## Optimizations

The write path (consensus, replication, disk) was profiled and tuned. Each change was benchmarked in isolation. Full numbers and methodology: [OPTIMIZATIONS.md](OPTIMIZATIONS.md). Ideas not yet tried: [docs/performance.md](docs/performance.md).

On a 3-node cluster, cumulative changes raised write throughput at 64 clients from ~2.4k to ~20k ops/s (about 8x) and cut write p99 latency from ~60 ms to ~6 ms (about 10x). Read throughput stayed near ~70k ops/s because reads skip consensus.

| Change | Effect |
|--------|--------|
| Skip replication sleep | Replicators sleep only on idle heartbeats, not after every successful AppendEntries |
| Group commit | Batch log appends and `fsync` once before replication |
| Single fsync per batch | Leader skips redundant syncs when the log is already flushed |
| Cached serialized entries | Log entries store pre-marshaled bytes to avoid re-encoding on every RPC |
| Wake replicators on append | New writes signal idle follower goroutines immediately |
| Remove replication backoff | Retry log mismatches without a 1 ms delay |

## Observability

Each node exports Prometheus metrics at `/metrics`: Raft state (term, role, commit index, apply lag, log length), RPC counters (elections, commits, AppendEntries, RequestVote), and client request volume and latency histograms.

The playground adds cluster-level gauges (replication lag, leader count, nodes running) and a scenario runner that drives load and node failures while updating scrape targets in `monitoring/targets.json`. Prometheus starts via Docker and reloads those targets automatically. Aggregated live stats (throughput, p99 latency, commit rate, replication lag, election rate) are available at `GET /api/metrics/live`.

**Prerequisite:** [Docker Desktop](https://www.docker.com/products/docker-desktop/) running.

```bash
go run ./playground
```

Opens http://localhost:8080. Click **Run stress test** to boot a 5-node cluster and run the full scenario.

Full metrics reference: [docs/observability.md](docs/observability.md). Playground API and flags: [playground/README.md](playground/README.md).

## Raft implementation

Each node exposes HTTP for clients (`/get`, `/put`, `/status`) and gRPC for Raft RPCs (votes, log replication, command forwarding). Consensus logic lives in `core/`; persistence goes to `logs/*.rlog` and `logs/*.meta`.

Code walkthrough: [docs/guide.md](docs/guide.md)

## Tests

```bash
go test -race ./core
go test -v ./test
go test ./playground/...
```

Integration tests in `test/` build the binary and run a real five-node cluster.

## Running a cluster manually

Each node needs an HTTP port (`--port`) and a gRPC address in `--peers` (`id=host:port`). You need at least three nodes for a quorum.

```bash
go build -o ryanDB .

./ryanDB \
  --id=node1 \
  --port=8001 \
  --peers=node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003 \
  --reset=true
```

Per-node Prometheus metrics: `/metrics` (disable with `--metrics=false`).

## Not yet implemented

- Log compaction / snapshots
- Dynamic cluster membership
