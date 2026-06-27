# Performance

Quorum includes a benchmark harness and a documented optimization campaign. These numbers demonstrate that the educational implementation is also performant on a single host.

## Summary (3-node cluster)

| Metric | Result |
|---|---|
| Read throughput (peak) | ~94,000 ops/sec |
| Write throughput (64 clients) | ~28,000 ops/sec |
| Read latency, p99 (16 clients) | ~1.0 ms |
| Write latency, p99 (16 clients) | ~2.8 ms |
| Failover recovery after leader crash | ~1.4 s |

Full methodology, graphs, and limitations: [benchmarks/REPORT.md](../../benchmarks/REPORT.md)

## Reproduce

```bash
go run ./benchmarks --quick
python3 benchmarks/plot.py
```

## Optimization history

See [OPTIMIZATIONS.md](../../OPTIMIZATIONS.md) for the completed optimization campaign (8× write throughput improvement) and [performance.md](../performance.md) for the future roadmap.

## Correctness vs performance

Performance optimizations (batched fsync, replication sleep tuning) trade durability margins for throughput in this educational implementation. Correctness is validated by the [integration test suite](../development/testing.md), not by benchmarks.
