# RaftDB Benchmark Report

Performance measurements for RaftDB, the replicated in-memory key-value store in this repository. This report describes which metrics were chosen, how they were measured, and the results from a single-host test run.

Reproduce the run with `go run ./benchmarks` followed by `python3 benchmarks/plot.py`. See [`README.md`](README.md) for options.

---

## 1. Metrics and methodology

For a Raft-backed store, throughput and latency percentiles are the primary capacity metrics. Availability after leader failure matters for replicated systems. The table below lists each metric considered and whether it applies to RaftDB.

| Metric | Included | Rationale |
|---|---|---|
| Throughput (ops/sec) | Yes | Writes and reads follow different paths (consensus vs in-memory lookup on the leader), so each is measured separately. |
| Latency percentiles (p50/p95/p99) | Yes | Percentiles are computed from the full set of per-request samples in each run window, not from averaged buckets. |
| Write vs read asymmetry | Yes | Writes incur replication and persistence; reads on the leader do not. |
| Concurrency scaling | Yes | Shows how throughput and latency change as concurrent clients increase. |
| Failover recovery time | Yes | Time from leader failure until writes succeed again. |
| Leader vs follower routing | Yes | Followers forward writes to the leader over gRPC; this measures any added cost. |
| Cluster size (3 vs 5 nodes) | Yes | Compares replication fan-out and quorum size at a fixed load. |
| Disk fsync isolation | Partial | Fsync is included in write latency but not measured in isolation. The ~11 ms floor at concurrency 1 is consistent with per-entry disk persistence. |
| Scan / range queries | No | No scan API. |
| Consistency / staleness | No | Correctness is covered by the integration test suite (`test/`). |

**Load generation.** The harness uses closed-loop load: each of *N* worker goroutines sends one request, waits for the response, and repeats for a fixed duration. Throughput is completed operations divided by wall time; latency is the round-trip time per request. Closed-loop load tends to under-report tail latency compared with open-loop generators when the server slows down (see [Limitations](#6-limitations)).

**Other settings.** Each data point uses a 5 s measurement window after cluster warmup. Read benchmarks preload 2,000 keys. HTTP keep-alive and a large connection pool are enabled so results reflect store behavior rather than connection setup.

**Environment.** Single host (Cursor Cloud VM, 4 vCPUs, 16 GB RAM), all nodes as local processes, Go 1.24.0. Absolute numbers depend on the host; relative comparisons and order-of-magnitude gaps are the main portable results.

---

## 2. Throughput: reads vs writes

![Throughput vs concurrency](results/img/throughput_vs_concurrency.png)

| Concurrency | Write ops/sec | Read ops/sec | Read / write ratio |
|---:|---:|---:|---:|
| 1 | 99 | 16,480 | 167 |
| 4 | 313 | 45,571 | 146 |
| 8 | 527 | 60,852 | 115 |
| 16 | 875 | 67,158 | 77 |
| 32 | 1,397 | 71,759 | 51 |
| 64 | 2,410 | 69,877 | 29 |

Write throughput increases with concurrency (99 to 2,410 ops/sec over the range tested). Per-request write latency is dominated by the commit path, so additional in-flight requests improve pipeline utilization.

Read throughput is higher by one to two orders of magnitude and levels off near 70k ops/sec between 32 and 64 clients, consistent with CPU or HTTP handling limits on a single host.

---

## 3. Latency percentiles

### Writes (PUT, Raft commit path)

![Write latency percentiles](results/img/write_latency_percentiles.png)

| Concurrency | p50 (ms) | p95 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 1 | 11.6 | 12.3 | 12.5 |
| 8 | 15.8 | 19.2 | 20.8 |
| 16 | 17.9 | 27.5 | 31.1 |
| 32 | 20.4 | 41.4 | 54.5 |
| 64 | 23.3 | 48.8 | 72.8 |

At concurrency 1, write latency has a floor of roughly 11–12 ms, reflecting replication to a majority and persistence to disk. Median latency grows modestly with load; p99 increases more sharply (12 ms to 73 ms at 64 clients), which is expected as the commit path saturates.

### Reads (GET, leader in-memory path)

![Read latency percentiles](results/img/read_latency_percentiles.png)

| Concurrency | p50 (ms) | p95 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 1 | 0.055 | 0.084 | 0.13 |
| 16 | 0.168 | 0.62 | 1.52 |
| 64 | 0.67 | 2.73 | 4.39 |

Reads remain sub-millisecond at low concurrency and stay in the low single-digit milliseconds at p99 under the loads tested. The read path does not run consensus or disk I/O.

---

## 4. Request routing: leader vs follower

![Routing comparison](results/img/routing_comparison.png)

| Target | Throughput (ops/sec) | p50 (ms) | p99 (ms) |
|---|---:|---:|---:|
| Leader (direct) | 807 | 19.3 | 35.5 |
| Follower (forwarded) | 823 | 19.3 | 33.2 |

Writes sent to a follower, which forwards to the leader over gRPC, show throughput and latency within run-to-run variation of writes sent directly to the leader. On loopback, the forwarding hop is small relative to commit and disk cost. This does not imply that leader discovery is unnecessary in production; it only characterizes this single-host setup.

---

## 5. Cluster size and failover

### Write performance vs cluster size

![Cluster size comparison](results/img/cluster_size_comparison.png)

| Cluster size | Throughput (ops/sec) | p50 (ms) | p99 (ms) |
|---|---:|---:|---:|
| 3 nodes | 875 | 18.6 | 32.4 |
| 5 nodes | 942 | 17.0 | 26.2 |

At 16 concurrent clients writing to the leader, 3-node and 5-node clusters performed similarly; the 5-node run was slightly faster in this sample. The leader replicates to followers in parallel and only requires a majority of acknowledgments (2 of 3 vs 3 of 5), so two additional local followers did not add measurable cost on this host. This is a single operating point; treat the difference as within measurement noise.

### Recovery after leader failure

![Failover recovery](results/img/failover_recovery.png)

| Trial | Leader change | Recovery (ms) |
|---|---|---:|
| 1 | node3 → node2 | 386 |
| 2 | node2 → node3 | 346 |
| 3 | node3 → node2 | 339 |
| Mean | | 357 |

The leader was killed during load. Recovery time is measured from the kill until a write to a surviving follower commits successfully. Mean recovery was 357 ms, which aligns with RaftDB’s randomized election timeout of 300–450 ms (see project README). No manual steps were required for the cluster to accept writes again.

---

## 6. Limitations

- **Single host.** All nodes share one machine; replication uses loopback networking. Real deployments would add inter-node RTT to write latency and failover time.
- **Closed-loop load.** When the server slows, clients slow with it, which can under-report tail latency compared with fixed-rate open-loop generators. Treat p99 as a lower bound under this harness.
- **Workload.** Small uniform values and unique keys only; no sweeps over payload size, key distribution, or mixed read/write ratios.
- **Sample size.** Cluster-size and routing comparisons use one concurrency level each.
- **Portability.** Re-run on the target environment for absolute figures.

---

## 7. Summary

| Observation | Result (this run) |
|---|---|
| Read vs write throughput | Reads peak near 70k ops/sec; writes reach ~2.4k ops/sec at 64 clients. |
| Write latency | ~11 ms floor at low load; p99 rises to ~73 ms at 64 clients. |
| Read latency | Sub-ms at low load; p99 under 5 ms at 64 clients. |
| Follower forwarding | No measurable penalty vs direct leader writes on loopback. |
| 3 vs 5 nodes | Comparable write performance at 16 clients. |
| Failover | Mean write recovery ~357 ms after leader kill. |
