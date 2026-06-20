# RaftDB

A Go implementation of the [Raft paper](https://raft.github.io/raft.pdf), built for learning how replicated consensus actually works.

This is not a production database. The core of the project is Raft: leader election, log replication, persistence, and recovery. On top of that there is a tiny in-memory key-value store (the state machine), HTTP endpoints so you can poke at a running cluster from your browser or curl, and an integration test suite that spins up real multi-node processes and breaks things on purpose.

## Benchmarks

3-node cluster, single host (see [`benchmarks/REPORT.md`](benchmarks/REPORT.md) for full results and graphs):

| Metric | Result |
|---|---|
| Read throughput (peak) | ~72,000 ops/sec |
| Write throughput (64 clients) | ~2,400 ops/sec |
| Read latency, p99 (16 clients) | ~1.5 ms |
| Write latency, p99 (16 clients) | ~31 ms |
| Failover recovery after leader crash | ~357 ms |

<img width="1280" height="786" alt="raft_demo" src="https://github.com/user-attachments/assets/561f122b-9ff0-4ab1-923f-832048c5d95b" />

## What gets implemented

The code follows the paper's main pieces:

- **Leader election** with randomized timeouts (300-450ms)
- **Log replication** through `AppendEntries`, with `nextIndex` / `matchIndex` for catch-up
- **Persistence** via per-node `.rlog` (log entries) and `.meta` (current term, voted-for) files under `logs/`
- **State machine apply** after commit (a simple `get` / `put` map)

Writes go through Raft. Reads on the leader wait until committed entries have been applied. Followers forward client requests to the leader over gRPC.

## Project layout

```
main.go          HTTP server, wires up a node
core/            Raft logic (node, leader, rpc, log, storage)
proto/           gRPC definitions and generated code
test/            Integration tests (spins up real nodes)
launch_node.sh   Helper script to start one node
```

If you are reading this to understand Raft, start in `core/node.go` and `core/leader.go`, then look at `core/rpc.go` for the RPC handlers.

<img width="60%" height="60%" alt="Raft state diagram" src="https://github.com/user-attachments/assets/6c7bf543-4297-4383-9367-21f5dbeb4911" />

## Running a cluster

Each node uses **two ports**:

- **HTTP** (client API): set with `--port`
- **gRPC** (node-to-node Raft RPCs): set in `--peers` as `id=host:port`

These must be different. The test suite uses HTTP on `8001-8005` and gRPC on `9001-9005`.

**Build:**

```bash
go build -o ryanDB .
```

**Start node 1** (fresh logs):

```bash
./ryanDB \
  --id=node1 \
  --port=8001 \
  --peers=node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003 \
  --reset=true
```

Start `node2` on port `8002` and `node3` on port `8003` with the same peers string. Use `--reset=false` on later runs to keep existing log files.

Or use the helper script (starts one node at a time):

```bash
./launch_node.sh 1 true
```

Give the cluster a second or two after startup before sending requests.

## HTTP API

These exist so you can interact with a running cluster without writing gRPC clients. They are intentionally simple, not a real REST design.

| Endpoint | What it does |
|---|---|
| `GET /get?key=<key>` | Read a key (forwarded to leader if needed) |
| `GET /put?key=<key>&value=<value>` | Write a key (goes through Raft) |
| `GET /status` | JSON with node id, term, state (0=follower, 1=candidate, 2=leader), leader id |

Example:

```bash
curl "http://localhost:8001/put?key=foo&value=bar"
curl "http://localhost:8002/get?key=foo"
curl "http://localhost:8001/status"
```

## Tests

The integration tests build the `ryanDB` binary, launch a 5-node cluster, and drive it over HTTP. Run them from the repo root:

```bash
go test -v ./test
```

The suite has also been run with Go's race detector:

```bash
go test -race ./...
```

Recent result on a local machine: all packages passed, including the integration suite.

| Test | What it checks |
|---|---|
| `TestElection` | Kill the leader, a new one shows up |
| `TestLogReplication` | A write on one node is readable on all nodes |
| `Test100LogReplication` | 99 writes routed to random nodes, all nodes agree at the end |
| `TestLogPersistence` | Kill and restart every node, data survives |
| `TestMissedLogsRecovery` | A node that was offline catches up when it comes back |
| `TestFollowerChurnUnderLoad` | Random followers die and restart under continuous writes |
| `TestNetworkPartition` | Minority nodes go offline, majority keeps committing, everyone converges after restart |

## Not implemented yet

- Log compaction / snapshotting
- Proper linearizable reads (the current read path is a simplified version)
- Dynamic cluster membership (peers are fixed at startup)
