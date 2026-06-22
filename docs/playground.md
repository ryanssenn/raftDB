# Playground User Guide

The playground runs real `ryanDB` processes and draws what they do. Not a simulation.

## Getting started

```bash
go run ./visualizer --no-browser --sandbox
```

1. Set the node count (3–9) and click **Configure**
2. Click **Start** and wait for a leader to appear in the HUD
3. Send writes from any client to any node
4. Watch replication, commits, and heartbeats on the canvas

Screenshots: [playground-start.png](images/playground-start.png), [playground-write.png](images/playground-write.png), [playground-failover.png](images/playground-failover.png)

## What you'll see

| Visual | Meaning |
|---|---|
| Teal packet: client → node | Client request |
| Teal dashed arc | Follower forwards to leader |
| Gold pulse | Vote request / election activity |
| Purple beam | Log replication (`append_entries`) |
| Green dot return | Replication ack |
| Gold ripple | Entry committed (majority reached) |
| Callout at bottom | Educational caption explaining the event |

### HUD metrics
- **Term**: current Raft term (increments on elections)
- **Commit**: highest committed log index
- **Quorum**: replicas that have caught up vs majority needed
- **Leader**: current leader node id

## Experiments to try

### Leader failure
1. Note the current leader in the HUD
2. Select that node in Failure Lab → **Kill**
3. Watch followers time out, hold an election, and elect a new leader
4. Send writes. They succeed under the new leader
5. **Restart** the killed node and watch it catch up

### Network partition
1. Start a 5-node cluster and write a key
2. Select `node1` and `node2` in the partition section → **Isolate**
3. Write from a node in the majority partition. Commits succeed
4. **Clear** the partition. Logs converge across all nodes

### Persistence
1. Write several keys
2. Kill and restart nodes one at a time
3. Read keys from restarted nodes. Data survives via disk persistence

### Concurrent clients
Add **client-B** and **client-C**, send writes from different clients to different nodes simultaneously. All committed writes appear on every replica.

## Guided tours

Load a preset from the sidebar dropdown:

| Tour | Teaches |
|---|---|
| Lifecycle showcase | Boot, writes, leader failure, recovery, loop |
| Leader election | Forced re-election after leader kill |
| Leader failure | Kill + restart + catch-up |
| Network partition | Split-brain prevention via quorum |
| Log persistence | Survive node restarts |

Click **Run** after loading. Use **Pause** to freeze between steps.

## Under the hood

Each node is a real `ryanDB` process:
- HTTP API on port 8001+
- gRPC Raft RPCs on port 9001+
- Logs persisted under `logs/` (`.rlog`, `.meta`)

The playground observes nodes via `/status` and `/events` endpoints. Partition simulation uses `/simulate/block` to drop gRPC between peers without stopping processes.

For correctness guarantees, testing methodology, and benchmarks, see:
- [Testing guide](development/testing.md)
- [Performance report](../benchmarks/REPORT.md)
- [Internals guide](guide.md)
