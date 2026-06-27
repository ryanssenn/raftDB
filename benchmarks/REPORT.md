# Quorum benchmarks

Throughput, latency, and failover numbers for the replicated key-value store, measured on a single host. Reproduce with `go run ./benchmarks` then `python3 benchmarks/plot.py` (options in [README.md](README.md)).

Every run uses closed-loop load: *N* worker goroutines each send a request, wait for the reply, and repeat for a 5-second window after warmup. Throughput is completed operations over wall time; latency is the per-request round trip. Read benchmarks preload 2,000 keys. Log compaction is turned off (a high `--snapshot-threshold`) so the write path reflects pure consensus cost; compaction is measured on its own in [OPTIMIZATIONS.md](../OPTIMIZATIONS.md).

Environment: Cursor Cloud VM (4 vCPUs, 16 GB RAM), Go 1.24.0, all nodes as local processes over loopback. Absolute numbers are host-specific — the asymmetries and order-of-magnitude gaps are what carry over. Two things shape how to read the rest of this:

- **Loopback, not a network.** Replication never leaves the machine, so a real deployment adds inter-node RTT to every write and to failover.
- **Closed-loop load hides the tail.** When the server slows, the clients slow with it, so the p99 figures here are closer to a lower bound than what an open-loop generator would show.

## Reads vs writes

![Throughput vs concurrency](results/img/throughput_vs_concurrency.png)

| Concurrency | Write ops/sec | Read ops/sec | Ratio |
|---:|---:|---:|---:|
| 1 | 542 | 15,743 | 29 |
| 4 | 2,293 | 45,073 | 20 |
| 8 | 4,416 | 60,454 | 14 |
| 16 | 7,501 | 67,084 | 9 |
| 32 | 12,517 | 70,133 | 6 |
| 64 | 19,463 | 72,356 | 4 |

Reads run 4–29× faster than writes: a read is an in-memory map lookup on the leader, while a write has to replicate to a majority and hit disk. The gap narrows with concurrency. Writes climb from 542 to 19,463 ops/sec because group commit and deferred fsync let many in-flight writes share one disk sync and one replication round trip. Reads flatten near 72k, bound by CPU and HTTP handling on a single host rather than by consensus.

## Latency

### Writes (PUT, full commit path)

![Write latency percentiles](results/img/write_latency_percentiles.png)

| Concurrency | p50 (ms) | p95 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 1 | 1.8 | 3.1 | 3.6 |
| 8 | 1.8 | 2.9 | 3.5 |
| 16 | 2.1 | 3.3 | 4.0 |
| 32 | 2.5 | 3.7 | 5.5 |
| 64 | 3.1 | 4.8 | 8.3 |

A write costs ~2 ms even with a single client in flight — the price of replicating and persisting one entry. The median holds in the low single digits across the sweep; the tail stretches under load (p99 8.3 ms at 64 clients) as writes queue behind shared disk syncs.

### Reads (GET, leader memory)

![Read latency percentiles](results/img/read_latency_percentiles.png)

| Concurrency | p50 (ms) | p95 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 1 | 0.061 | 0.083 | 0.12 |
| 16 | 0.17 | 0.59 | 1.33 |
| 64 | 0.64 | 2.58 | 4.43 |

Sub-millisecond at low load, low single-digit p99 under load. No consensus, no disk.

## Follower forwarding

![Routing comparison](results/img/routing_comparison.png)

| Target | Throughput (ops/sec) | p50 (ms) | p99 (ms) |
|---|---:|---:|---:|
| Leader (direct) | 7,673 | 2.0 | 4.1 |
| Follower (forwarded) | 6,796 | 2.3 | 5.6 |

A write sent to a follower is forwarded to the leader over gRPC, costing ~11% throughput and a little tail latency here. On loopback that extra hop is small next to the commit path; over a real network it would weigh more.

## Cluster size

![Cluster size comparison](results/img/cluster_size_comparison.png)

| Cluster size | Throughput (ops/sec) | p50 (ms) | p99 (ms) |
|---|---:|---:|---:|
| 3 nodes | 7,769 | 2.0 | 3.7 |
| 5 nodes | 7,592 | 2.1 | 3.9 |

Three and five nodes write at the same rate (16 clients, leader-direct). The leader replicates in parallel and only waits for a majority — 2 of 3 versus 3 of 5 — so two extra local followers add no measurable cost. The difference here is within noise.

## Failover

![Failover recovery](results/img/failover_recovery.png)

| Trial | Leader change | Recovery (ms) |
|---|---|---:|
| 1 | node3 → node1 | 333 |
| 2 | node1 → node2 | 333 |
| 3 | node2 → node3 | 315 |
| Mean | | 327 |

The leader is killed mid-load; recovery is the time from the kill until a write commits on a surviving node. The ~327 ms mean tracks the randomized 300–450 ms election timeout — most of it is followers waiting out the timeout before standing for election. No manual intervention is needed for writes to resume.
