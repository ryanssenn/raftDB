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
| 1 | 1,665 | 20,693 | 12 |
| 4 | 4,926 | 60,440 | 12 |
| 8 | 8,790 | 80,650 | 9 |
| 16 | 14,776 | 87,784 | 6 |
| 32 | 21,400 | 90,530 | 4 |
| 64 | 28,096 | 94,501 | 3 |

Reads run 3–12× faster than writes: a read is an in-memory map lookup on the leader, while a write has to replicate to a majority and hit disk. The gap narrows with concurrency. Writes climb from 1,665 to 28,096 ops/sec because group commit and deferred fsync let many in-flight writes share one disk sync and one replication round trip. Reads flatten near 94k, bound by CPU and HTTP handling on a single host rather than by consensus.

## Latency

### Writes (PUT, full commit path)

![Write latency percentiles](results/img/write_latency_percentiles.png)

| Concurrency | p50 (ms) | p95 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 1 | 0.47 | 1.52 | 1.94 |
| 8 | 0.77 | 2.07 | 2.73 |
| 16 | 0.95 | 2.11 | 2.82 |
| 32 | 1.31 | 2.58 | 3.77 |
| 64 | 2.05 | 3.71 | 5.95 |

A write commits in well under a millisecond at low concurrency (p50 0.47 ms at one client) — it still has to replicate to a majority and fsync, but that path is fast on this host. The median stays sub-millisecond through 16 clients and the tail stretches under load (p99 5.95 ms at 64 clients) as writes queue behind shared disk syncs.

### Reads (GET, leader memory)

![Read latency percentiles](results/img/read_latency_percentiles.png)

| Concurrency | p50 (ms) | p95 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 1 | 0.045 | 0.059 | 0.083 |
| 16 | 0.14 | 0.40 | 1.04 |
| 64 | 0.51 | 1.79 | 3.26 |

Sub-millisecond at low load, low single-digit p99 under load. No consensus, no disk.

## Follower forwarding

![Routing comparison](results/img/routing_comparison.png)

| Target | Throughput (ops/sec) | p50 (ms) | p99 (ms) |
|---|---:|---:|---:|
| Leader (direct) | 13,595 | 1.0 | 3.1 |
| Follower (forwarded) | 10,286 | 1.4 | 3.6 |

A write sent to a follower is forwarded to the leader over gRPC, costing ~24% throughput and some tail latency here (one extra in-process hop before the commit path). Over a real network that hop would weigh more.

## Cluster size

![Cluster size comparison](results/img/cluster_size_comparison.png)

| Cluster size | Throughput (ops/sec) | p50 (ms) | p99 (ms) |
|---|---:|---:|---:|
| 3 nodes | 13,815 | 1.0 | 3.0 |
| 5 nodes | 11,546 | 1.3 | 3.2 |

At 16 clients (leader-direct), three nodes sustain ~13.8k writes/sec versus ~11.5k at five — about 16% lower. The leader replicates in parallel and only waits for a majority (2 of 3 versus 3 of 5), but on this host the two extra local followers add enough replication and fsync load to show up. On a real network the gap would have a different shape: reaching a larger majority costs more round trips, but the followers no longer contend for the same local disk and CPU.

## Failover

![Failover recovery](results/img/failover_recovery.png)

| Trial | Leader change | Recovery (ms) |
|---|---|---:|
| 1 | node3 → node1 | 689 |
| 2 | node1 → node2 | 1,372 |
| 3 | node2 → node1 | 2,142 |
| Mean | | 1,401 |

The leader is killed mid-load; recovery is the time from the kill until a write commits on a surviving node. It is dominated by the randomized **600–1000 ms** election timeout: a follower has to ride out its timeout before standing for election, and if the first round splits the vote another timeout elapses before a leader emerges. That makes the figure noisy — across repeated runs it landed anywhere from ~0.7 s (one clean election) to ~3.4 s (a split round plus a retry), averaging roughly 1.4 s here. No manual intervention is needed for writes to resume.
