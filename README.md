# Raft Playground

Go implementation of Raft with a browser UI on top. You start a real cluster, send writes, kill nodes, and watch elections and replication happen.

This is for learning, not production. The Raft code is tested (unit + integration) and benchmarked. Details below.

## Try it

```bash
go run ./visualizer --no-browser --sandbox
```

Open http://localhost:8080. Pick a node count, click **Configure**, then **Start**.

Or run a scripted tour that drives the cluster for you:

```bash
go run ./visualizer --no-browser visualizer/scenarios/showcase.json
```

More detail: [docs/playground.md](docs/playground.md)

## Benchmarks

3-node cluster on a single host ([full report](benchmarks/REPORT.md)):

| Metric | Result |
|---|---|
| Read throughput (peak) | ~72,000 ops/sec |
| Write throughput (64 clients) | ~19,500 ops/sec |
| Read latency, p99 (16 clients) | ~1.3 ms |
| Write latency, p99 (16 clients) | ~4 ms |
| Failover recovery after leader crash | ~327 ms |

These numbers come from a Cursor Cloud VM with 4 vCPUs, 16 GB RAM, and Go 1.24.0. Re-run with `go run ./benchmarks` on your own machine.

## The Raft implementation

A Go implementation of the [Raft paper](https://raft.github.io/raft.pdf) with a small in-memory key-value store on top.

The implementation covers leader election, log replication, disk persistence, and recovery. Clients use HTTP; nodes exchange Raft RPCs over gRPC.

Read the code: [docs/guide.md](docs/guide.md)

## Tests

```bash
go test -race ./core          # unit tests, no cluster
go test -v ./test             # 5-node integration tests
go test ./visualizer/...      # playground API
```

Integration tests build the binary, launch a 5-node cluster, and drive it over HTTP. What each test covers: [docs/development/testing.md](docs/development/testing.md)

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

### HTTP API

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

## Not yet implemented

- Log compaction / snapshots
- Dynamic cluster membership
