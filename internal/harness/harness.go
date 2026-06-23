// Package harness holds shared helpers for starting clusters in tests,
// benchmarks, and the playground.
package harness

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// KillPorts frees the HTTP and gRPC ports used by an n-node cluster.
func KillPorts(n int) {
	for i := 0; i < n; i++ {
		for _, port := range []int{8001 + i, 9001 + i} {
			out, err := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port)).Output()
			if err != nil {
				continue
			}
			for _, pid := range strings.Fields(string(out)) {
				_ = exec.Command("kill", "-9", pid).Run()
			}
		}
	}
	time.Sleep(200 * time.Millisecond)
}

// BuildPeers returns the comma-separated id=addr peer string for n nodes.
func BuildPeers(n int) string {
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		id := "node" + strconv.Itoa(i+1)
		parts[i] = fmt.Sprintf("%s=127.0.0.1:%d", id, 9001+i)
	}
	return strings.Join(parts, ",")
}

// IsTransientError reports whether a client response may succeed on retry.
func IsTransientError(resp string) bool {
	switch resp {
	case "", "Error: election", "no leader elected yet", "leader not accessible":
		return true
	default:
		return strings.HasPrefix(resp, "Error:")
	}
}
