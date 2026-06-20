# RaftDB Benchmark Report

A performance characterization of **RaftDB**, the educational Raft-based,
replicated in-memory key-value store in this repository. The goal is to pick the
benchmarking metrics that actually matter for a Raft KV store, measure them
against a live cluster, and present the results with graphs.

> Reproduce everything with `go run ./benchmarks` followed by
> `python3 benchmarks/plot.py`. See [`README.md`](README.md).

---

## 1. Which metrics matter (and why)

Distributed-database benchmarking has well-established conventions (YCSB, the
etcd performance guide, and tail-latency best practice). The headline metrics
are **throughput** and **latency percentiles**, plus **availability** for
replicated systems. Below is each candidate metric and whether it is meaningful
*for this specific system*.

| Metric | Useful here? | Why |
|---|---|---|
| **Throughput (ops/sec)** | ✅ Core | The standard capacity metric. RaftDB has two very different paths — writes go through consensus, reads are served from the leader's in-memory map — so we measure each separately. |
| **Latency percentiles (p50/p95/p99)** | ✅ Core | Averages hide tails. Percentiles are computed from raw per-request samples (never averaged), per YCSB/tail-latency guidance. |
| **Write vs read asymmetry** | ✅ Core | Writes pay the Raft commit cost (replication RTT + disk persistence); reads do not. This is *the* defining performance trait of a Raft KV store. |
| **Concurrency / load scaling** | ✅ Core | Shows how throughput grows and how latency degrades (queuing) as concurrent clients increase. |
| **Availability — failover recovery time** | ✅ Core | For a consensus system the key resilience metric: how long writes are unavailable after the leader crashes. |
| **Request routing overhead (leader vs follower)** | ✅ RaftDB-specific | Followers forward writes to the leader over gRPC. We quantify that extra hop. |
| **Cluster-size impact (3 vs 5 nodes)** | ✅ Relevant | More followers = more replication fan-out but a larger quorum. Shows the cost/benefit of replication factor. |
| Disk fsync isolation | ⚠️ Partial | RaftDB persists each entry to `.rlog`/`.meta`, so fsync is *included* in write latency, but we don't isolate it. The ~11 ms floor at concurrency 1 is consistent with per-entry disk persistence. |
| Scan / range-query throughput | ❌ N/A | No scan API. |
| Consistency/staleness measurement | ❌ Out of scope | Correctness is already covered by the integration suite (`test/`). This report is about performance. |

**Methodology notes**

- **Closed-loop** load: each of *N* worker goroutines issues one request, waits
  for the response, repeats, for a fixed time window. Throughput = completed
  ops / wall time; latency = per-request round trip. (Closed-loop under-reports
  tails vs an open-loop model — see [Limitations](#6-limitations).)
- Percentiles are interpolated from the **sorted raw sample set** of every
  successful request in the window.
- Each point runs for **5 s** after a warm cluster + (for reads) a 2,000-key preload.
- HTTP keep-alive is on with a large connection pool, so we measure the store,
  not connection churn.

**Environment:** single host (the Cursor Cloud VM), all nodes as local
processes, Go 1.24.0. Absolute numbers are host-specific; the *shapes* and
*ratios* are the takeaways.

---

## 2. Throughput: reads vs writes

![Throughput vs concurrency](results/img/throughput_vs_concurrency.png)

| Concurrency | Write ops/sec | Read ops/sec | Read ÷ Write |
|---:|---:|---:|---:|
| 1 | 99 | 16,480 | 167× |
| 4 | 313 | 45,571 | 146× |
| 8 | 527 | 60,852 | 115× |
| 16 | 875 | 67,158 | 77× |
| 32 | 1,397 | 71,759 | 51× |
| 64 | 2,410 | 69,877 | 29× |

- **Writes** scale close to linearly with concurrency (99 → 2,410 ops/sec) — the
  single-request latency is dominated by the consensus commit, so adding
  in-flight clients amortizes that cost and keeps filling the pipeline.
- **Reads** are 1–2 orders of magnitude faster (a map lookup on the leader) and
  **saturate at ~70k ops/sec** around 32 clients; beyond that, adding clients
  stops helping (CPU/HTTP-bound), the classic throughput plateau.

---

## 3. Latency percentiles

### Writes (PUT — go through Raft)

![Write latency percentiles](results/img/write_latency_percentiles.png)

| Concurrency | p50 (ms) | p95 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 1 | 11.6 | 12.3 | 12.5 |
| 8 | 15.8 | 19.2 | 20.8 |
| 16 | 17.9 | 27.5 | 31.1 |
| 32 | 20.4 | 41.4 | 54.5 |
| 64 | 23.3 | 48.8 | 72.8 |

A ~**11–12 ms floor** even at concurrency 1 reflects the unavoidable
commit cost (replicate to a majority + persist to disk). As load rises, p50
grows slowly but the **tail (p99) fans out sharply** (12 → 73 ms) — textbook
queuing behavior as the leader's commit pipeline approaches saturation.

### Reads (GET — served from leader memory)

![Read latency percentiles](results/img/read_latency_percentiles.png)

| Concurrency | p50 (ms) | p95 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 1 | 0.055 | 0.084 | 0.13 |
| 16 | 0.168 | 0.62 | 1.52 |
| 64 | 0.67 | 2.73 | 4.39 |

Reads are **sub-millisecond** at low concurrency and stay in the low single-digit
milliseconds even at p99 under heavy load — there is no consensus or disk on the
read path.

---

## 4. Request routing: leader-direct vs follower-forwarded

![Routing comparison](results/img/routing_comparison.png)

| Target | Throughput (ops/sec) | p50 (ms) | p99 (ms) |
|---|---:|---:|---:|
| Leader (direct) | 807 | 19.3 | 35.5 |
| Follower (forwarded) | 823 | 19.3 | 33.2 |

Sending writes to a **follower** (which forwards to the leader over gRPC) is
**effectively free** here — within run-to-run noise of writing directly to the
leader. The intra-host forwarding hop is negligible next to the consensus +
disk commit cost that dominates every write. Clients don't need to discover the
leader to get good write performance.

---

## 5. Cluster size & failover

### Write performance vs cluster size

![Cluster size comparison](results/img/cluster_size_comparison.png)

| Cluster size | Throughput (ops/sec) | p50 (ms) | p99 (ms) |
|---|---:|---:|---:|
| 3 nodes | 875 | 18.6 | 32.4 |
| 5 nodes | 942 | 17.0 | 26.2 |

Going from 3 to 5 nodes (16 clients, writes to leader) **did not degrade** write
performance — the two were comparable, with the 5-node run marginally ahead in
this sample. The leader replicates to followers in parallel and only needs a
**majority** to ack (2-of-3 vs 3-of-5), so two extra local followers add little
to the critical path. This is a single operating point and the difference is
within measurement noise; the takeaway is "no meaningful penalty," not "bigger
is faster."

### Availability: recovery after a leader crash

![Failover recovery](results/img/failover_recovery.png)

| Trial | Leader change | Recovery (ms) |
|---|---|---:|
| 1 | node3 → node2 | 386 |
| 2 | node2 → node3 | 346 |
| 3 | node3 → node2 | 339 |
| **Mean** | | **357** |

We kill the leader mid-operation and time until a write (sent to a surviving
follower) is committed again. Recovery averaged **~357 ms**, squarely in the
expected band for RaftDB's **300–450 ms randomized election timeout** (per the
README). In other words, after a leader failure the cluster restores write
availability in roughly one election timeout — no manual intervention.

---

## 6. Limitations

- **Single host:** all nodes share one machine, so replication "network" RTT is
  loopback. On real networks, write latency and failover time would rise by the
  inter-node RTT (etcd notes ~50 ms cross-US, up to ~400 ms cross-continent).
- **Closed-loop generation:** this model lets the harness slow down when the
  server slows down, so it **under-reports tail latency** vs an open-loop /
  fixed-arrival-rate generator (coordinated omission). Treat p99 as a lower bound.
- **Small, uniform values** (`"v"`) and unique keys; no payload-size, key-skew,
  or read/write-mix sweeps.
- **Single-point** cluster-size and routing comparisons (one concurrency level).
- Numbers are **host-relative**; re-run on the target environment for absolute figures.

## 7. Headline takeaways

1. **Reads are ~30–170× faster than writes** — the defining trait of this Raft KV
   store. Reads peak ~70k ops/sec; writes reach ~2.4k ops/sec at 64 clients.
2. **Write latency has a ~11 ms consensus+disk floor** and a tail that fans out
   under load (p99 12 → 73 ms); reads stay sub-millisecond to low-ms.
3. **Follower forwarding is essentially free** — no need to target the leader.
4. **3 vs 5 nodes: no meaningful write penalty** at this scale.
5. **Failover restores writes in ~357 ms**, ≈ one election timeout.
