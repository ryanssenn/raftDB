# RaftDB

A Go implementation of the [Raft paper](https://raft.github.io/raft.pdf) with a small in-memory key-value store on top. The goal is to learn how replicated consensus works; this is not a production database.

The implementation covers leader election, log replication, disk persistence, and recovery. Clients use HTTP; nodes exchange Raft RPCs over gRPC.

## Benchmarks

3-node cluster on a single host ([full report](benchmarks/REPORT.md)):

| Metric | Result |
|---|---|
| Read throughput (peak) | ~72,000 ops/sec |
| Write throughput (64 clients) | ~2,400 ops/sec |
| Read latency, p99 (16 clients) | ~1.5 ms |
| Write latency, p99 (16 clients) | ~31 ms |
| Failover recovery after leader crash | ~357 ms |

These numbers come from a Cursor Cloud VM with 4 vCPUs, 16 GB RAM, and Go 1.24.0. Re-run with `go run ./benchmarks` on your own machine.

## Running a cluster

Each node needs an HTTP port (`--port`) and a gRPC port (in `--peers` as `id=host:port`). Start at least three nodes for a working cluster.

```bash
go build -o ryanDB .

./ryanDB \
  --id=node1 \
  --port=8001 \
  --peers=node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003 \
  --reset=true
```

Start `node2` and `node3` on ports `8002`/`8003` with the same `--peers` string. Use `--reset=false` to keep logs between restarts, or `./launch_node.sh 1 true` to start one node via the helper script.

## HTTP API

| Endpoint | Description |
|---|---|
| `GET /put?key=<key>&value=<value>` | Write a key |
| `GET /get?key=<key>` | Read a key |
| `GET /status` | Node id, term, role, and leader |

```bash
curl "http://localhost:8001/put?key=foo&value=bar"
curl "http://localhost:8002/get?key=foo"
curl "http://localhost:8001/status"
```

## Tests

Integration tests build the binary, launch a 5-node cluster, and drive it over HTTP:

```bash
go test -v ./test
```

## Layout

| Path | Purpose |
|---|---|
| `docs/guide.md` | Beginner's guide to Raft and this codebase |
| `docs/performance.md` | Performance improvement opportunities |
| `core/` | Raft logic |
| `main.go` | HTTP server entrypoint |
| `test/` | Integration tests |
| `visualizer/` | Optional browser demo |

## Not yet implemented

- Log compaction / snapshots
- Dynamic cluster membership
