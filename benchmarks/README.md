# Quorum Benchmarks

A self-contained load-testing harness for the Quorum key-value store. It builds
the `quorum` binary, launches a real multi-node cluster (HTTP `8001+` / gRPC
`9001+`, exactly like the integration tests), drives it with closed-loop client
workers over the HTTP API, and records **per-request latency samples** so
percentiles are computed from raw data rather than pre-averaged windows.

See [`REPORT.md`](REPORT.md) for a full write-up of the metrics, methodology,
results, and graphs.

## Run

```bash
# Full suite (~2 min). Writes CSV/JSON to benchmarks/results/.
go run ./benchmarks

# Faster smoke run.
go run ./benchmarks --quick

# Custom knobs.
go run ./benchmarks --dur=5s --concurrency=1,4,8,16,32,64 --preload=2000
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--dur` | `5s` | Measurement window per concurrency point |
| `--concurrency` | `1,4,8,16,32,64` | Closed-loop client counts to sweep |
| `--preload` | `2000` | Keys preloaded before the read sweep |
| `--quick` | `false` | Shorter durations / smaller preload |
| `--out` | `benchmarks/results` | Output directory |

Ports `8001-8005` and `9001-9005` must be free (the harness force-frees them on start).

## Graphs

```bash
pip install matplotlib
python3 benchmarks/plot.py
```

PNGs are written to `benchmarks/results/img/`.

## Outputs

- `results/results.csv`: one row per load point (throughput + latency percentiles)
- `results/failover.csv`: leader-crash recovery times
- `results/results.json`: everything, machine-readable
- `results/img/*.png`: graphs
- `results/node-logs/`: per-node stdout/stderr (gitignored)
