# Testing & Correctness

Quorum validates its Raft implementation with unit tests (isolated logic) and integration tests (real multi-node clusters). The playground adds API tests for the scenario runner and metrics layer.

## Commands

```bash
# Unit tests (race detector)
go test -race -count=1 -timeout 5m ./core

# Integration tests (5-node cluster)
go test -count=1 -timeout 10m -v ./test

# Playground control API and metrics
go test -count=1 -timeout 5m ./playground/...
```

CI runs unit tests with `-race`, integration tests with `-count=3`, and playground tests on every push.

## Unit tests (`core/node_test.go`)

| Test | Proves |
|---|---|
| `TestUpdateCommitIndex` | Leader commits only current-term entries with majority match |
| `TestApplyCommittedOrdering` | Committed entries apply in log order |
| `TestCommitWaitsForApply` | Client waits until entry is applied to state machine |
| `TestRequestVoteGrantDeny` | Vote grant/deny rules (term, log completeness) |
| `TestAppendEntriesConsistency` | Log consistency check on append |
| `TestRecoverStateAppliesLog` | Restart replays persisted log into state machine |
| `TestLoggerRoundTrip` | Disk log encode/decode round-trip |

## Integration tests (`test/integration_test.go`)

All tests build the binary and launch a real 5-node cluster on ports 8001-8005 / 9001-9005.

| Test | Proves |
|---|---|
| `TestElection` | Single leader elected; new leader after kill |
| `TestLogReplication` | Write replicates to all nodes |
| `Test100LogReplication` | 99 random writes converge |
| `TestLogPersistence` | Data survives sequential kill/restart of all nodes |
| `TestMissedLogsRecovery` | Down node catches up after rejoin |
| `TestFollowerChurnUnderLoad` | Writes succeed under repeated follower restarts |
| `TestNetworkPartition` | Minority partition cannot make progress; heals on rejoin |
| `TestNoDualLeaders` | At most one leader per term observed |
| `TestWriteWhileNoLeader` | Writes fail without quorum |
| `TestConcurrentWrites` | 100 concurrent writes all committed |

## Playground tests (`playground/api_test.go`)

| Test | Proves |
|---|---|
| `TestPlaygroundAPI` | Create/start/stop cluster, run scenario, node and cluster metrics |
| `TestLoadScenarioPaths` | Scenario JSON validation |
| `TestResolveScenarioPath` | Scenario path resolution from repo root |
| `TestWritePrometheusTargets` | Dynamic Prometheus target file generation |

## What is not tested automatically

- Grafana dashboard rendering (manual verification)
- Docker compose monitoring stack (manual verification)
- Benchmark regression (run manually via `go run ./benchmarks`)
- Log compaction / snapshots (not implemented)
- Dynamic membership (not implemented)

## Evidence of Raft conformance

The implementation follows the [Raft paper](https://raft.github.io/raft.pdf):
- Leader election with randomized timeouts
- Log replication with consistency check (`prevLogIndex` / `prevLogTerm`)
- Safety: committed entries from prior terms preserved
- Persistence: term, vote, and log entries survive restart

See [guide.md](../guide.md) for a code walkthrough mapping paper sections to source files.
