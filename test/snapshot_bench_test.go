package test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSnapshotRecoveryBench measures the effect of log compaction on a
// long-running cluster: a workload that rewrites a small key space many times
// keeps the state machine tiny while the raw log grows without bound. With
// compaction the on-disk log and the restart-recovery time stay bounded.
//
// It is skipped by default (it writes tens of thousands of entries). Run with:
//
//	QUORUM_SNAPSHOT_BENCH=1 go test -run TestSnapshotRecoveryBench -v -timeout 20m ./test
func TestSnapshotRecoveryBench(t *testing.T) {
	if os.Getenv("QUORUM_SNAPSHOT_BENCH") == "" {
		t.Skip("set QUORUM_SNAPSHOT_BENCH=1 to run the snapshot recovery benchmark")
	}

	const (
		totalWrites = 40000 // log entries appended
		keySpace    = 500   // distinct keys (rewritten repeatedly)
		workers     = 40
	)

	run := func(label string, threshold int64) {
		SnapshotThresholdArg = threshold
		defer func() { SnapshotThresholdArg = 0 }()

		nodes := InitNodes(t)
		leader := WaitForLeader(t, nodes, 15*time.Second)

		// Bulk write, reusing a small key space so the state machine stays small.
		var counter int64
		var wg sync.WaitGroup
		writeStart := time.Now()
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := atomic.AddInt64(&counter, 1)
					if i > totalWrites {
						return
					}
					key := fmt.Sprintf("key%d", i%keySpace)
					leader.PutMustSucceed(t, key, fmt.Sprintf("v%d", i))
				}
			}()
		}
		wg.Wait()
		writeDur := time.Since(writeStart)

		// Let any final compaction tick run.
		time.Sleep(1 * time.Second)

		// Pick a follower to restart and inspect.
		var follower *Node
		for _, n := range nodes {
			if n != leader {
				follower = n
				break
			}
		}
		st, _ := follower.TryStatus()
		rlog := fileSize(t, follower.id+".rlog")
		snap := fileSize(t, follower.id+".snap")

		follower.StopNode()
		WaitForNodeDown(t, follower, 10*time.Second)
		recoverStart := time.Now()
		follower.StartNode(t, "false") // StartNode blocks until /status responds
		recoverDur := time.Since(recoverStart)

		t.Logf("[%s] threshold=%d writes=%d in %.1fs", label, threshold, totalWrites, writeDur.Seconds())
		t.Logf("[%s] follower logLength=%d snapshotIndex=%d rlog=%s snap=%s",
			label, st.LogLength, st.SnapshotIndex, humanBytes(rlog), humanBytes(snap))
		t.Logf("[%s] restart recovery time: %.0f ms", label, float64(recoverDur.Microseconds())/1000.0)

		StopNodes(nodes)
		time.Sleep(500 * time.Millisecond)
	}

	run("compaction-on", 2000)
	run("compaction-off", 1<<60)
}

func fileSize(t *testing.T, name string) int64 {
	t.Helper()
	info, err := os.Stat(filepath.Join(repoRoot, "logs", name))
	if err != nil {
		return 0
	}
	return info.Size()
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
