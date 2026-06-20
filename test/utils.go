package test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Prevent integration tests from hanging until go test's default timeout.
// Puts block until commit; allow headroom for election churn on slow CI.
var testHTTPClient = &http.Client{Timeout: 20 * time.Second}

var (
	repoRoot  = filepath.Join(filepath.Dir(getTestFile()), "..")
	binary    = filepath.Join(repoRoot, "ryanDB")
	buildOnce sync.Once
)

func getTestFile() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return file
}

func buildBinary(t *testing.T) {
	t.Helper()
	buildOnce.Do(func() {
		cmd := exec.Command("go", "build", "-o", binary, ".")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to build ryanDB: %v\n%s", err, out)
		}
	})
}

func KillPorts(n int) {
	for i := 0; i < n; i++ {
		for _, port := range []int{8001 + i, 9001 + i} {
			out, err := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port)).Output()
			if err != nil {
				continue
			}
			pids := strings.Fields(string(out))
			for _, pid := range pids {
				_ = exec.Command("kill", "-9", pid).Run()
			}
		}
	}
	time.Sleep(200 * time.Millisecond)
}

type Node struct {
	id      string
	port    string
	peers   string
	cmd     *exec.Cmd
	running bool
}

type Status struct {
	Id          string         `json:"id"`
	LeaderId    *string        `json:"leaderId"`
	State       int            `json:"state"`
	Term        int64          `json:"term"`
	CommitIndex int64          `json:"commitIndex"`
	LastApplied int64          `json:"lastApplied"`
	LogLength   int            `json:"logLength"`
	MatchIndex  map[string]int64 `json:"matchIndex"`
	NextIndex   map[string]int64 `json:"nextIndex"`
}

func (n *Node) Status(t *testing.T) *Status {
	status, err := n.TryStatus()
	if err != nil {
		t.Fatalf("HTTP request failed for %s: %v", n.id, err)
	}
	return status
}

func (n *Node) TryStatus() (*Status, error) {
	statusURL := fmt.Sprintf("http://127.0.0.1:%s/status", n.port)
	resp, err := testHTTPClient.Get(statusURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var status Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (n *Node) Get(t *testing.T, key string) string {
	body, err := n.TryGet(key)
	if err != nil {
		t.Fatalf("HTTP request failed for %s: %v", n.id, err)
	}
	return body
}

func (n *Node) TryGet(key string) (string, error) {
	baseURL := fmt.Sprintf("http://127.0.0.1:%s/get", n.port)
	params := url.Values{}
	params.Add("key", key)
	fullURL := baseURL + "?" + params.Encode()

	resp, err := testHTTPClient.Get(fullURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (n *Node) Put(t *testing.T, key string, value string) string {
	body, err := n.TryPut(key, value)
	if err != nil {
		t.Fatalf("HTTP request failed for %s: %v", n.id, err)
	}
	return body
}

func (n *Node) TryPut(key string, value string) (string, error) {
	baseURL := fmt.Sprintf("http://127.0.0.1:%s/put", n.port)
	params := url.Values{}
	params.Add("key", key)
	params.Add("value", value)
	fullURL := baseURL + "?" + params.Encode()

	resp, err := testHTTPClient.Get(fullURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (n *Node) PutMustSucceed(t *testing.T, key, value string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		resp, err := n.TryPut(key, value)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		last = resp
		if resp == "success" {
			return
		}
		if isTransientPutError(resp) {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		t.Fatalf("%s put %s=%s failed: %q", n.id, key, value, last)
	}
	t.Fatalf("%s put %s=%s timed out, last response: %q", n.id, key, value, last)
}

func isTransientPutError(resp string) bool {
	switch resp {
	case "", "Error: election", "no leader elected yet", "leader not accessible":
		return true
	default:
		return strings.HasPrefix(resp, "Error:")
	}
}

func NewNodes(n int) []*Node {
	var nodes []*Node
	peers := ""
	for i := 0; i < n; i++ {
		id := "node" + strconv.Itoa(i+1)
		addr := "127.0.0.1:" + strconv.Itoa(9001+i)
		peers += id + "=" + addr
		if i != n-1 {
			peers += ","
		}
	}

	for i := 0; i < n; i++ {
		node := &Node{
			id:      "node" + strconv.Itoa(i+1),
			port:    strconv.Itoa(8001 + i),
			peers:   peers,
			running: false,
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func (n *Node) StartNode(t *testing.T, reset string) {
	t.Helper()
	buildBinary(t)

	cmd := exec.Command(binary, "--id="+n.id, "--port="+n.port, "--peers="+n.peers, "--reset="+reset)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start node %s: %v", n.id, err)
	}
	n.running = true
	n.cmd = cmd
	WaitForReady(t, n, 30*time.Second)
}

func (n *Node) StopNode() {
	if n.cmd != nil && n.cmd.Process != nil {
		_ = n.cmd.Process.Kill()
		n.running = false
		_ = n.cmd.Wait()
		n.cmd = nil
	}
}

func StartNodes(t *testing.T, nodes []*Node, reset string) {
	for _, node := range nodes {
		node.StartNode(t, reset)
	}
	WaitForLeader(t, nodes, 15*time.Second)
}

func StopNodes(nodes []*Node) {
	for _, node := range nodes {
		node.StopNode()
	}
}

func InitNodes(t *testing.T) []*Node {
	t.Helper()
	KillPorts(N)
	nodes := NewNodes(N)
	t.Cleanup(func() { StopNodes(nodes) })
	StartNodes(t, nodes, "true")
	return nodes
}

func CountLeader(t *testing.T, nodes []*Node) (*Node, int) {
	t.Helper()
	leaderCount := 0
	var leader *Node

	for _, node := range nodes {
		if !node.running {
			continue
		}
		status, err := node.TryStatus()
		if err != nil {
			continue
		}
		if status.State == 2 {
			leaderCount++
			leader = node
		}
	}
	return leader, leaderCount
}

func dumpNodeStatuses(t *testing.T, nodes []*Node) {
	t.Helper()
	for _, node := range nodes {
		if !node.running {
			t.Logf("%s: not running", node.id)
			continue
		}
		status, err := node.TryStatus()
		if err != nil {
			t.Logf("%s: status unavailable: %v", node.id, err)
			continue
		}
		data, _ := json.Marshal(status)
		t.Logf("%s: %s", node.id, string(data))
	}
}

func WaitForReady(t *testing.T, node *Node, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := node.TryStatus(); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	dumpNodeStatuses(t, []*Node{node})
	t.Fatalf("timed out waiting for %s to become ready", node.id)
}

func WaitForNodeDown(t *testing.T, node *Node, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := node.TryStatus(); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to stop", node.id)
}

func clusterCaughtUp(nodes []*Node) bool {
	var target int64 = -1
	running := 0
	for _, node := range nodes {
		if !node.running {
			continue
		}
		running++
		status, err := node.TryStatus()
		if err != nil {
			return false
		}
		if status.LastApplied > target {
			target = status.LastApplied
		}
	}
	if running == 0 {
		return true
	}
	for _, node := range nodes {
		if !node.running {
			continue
		}
		status, err := node.TryStatus()
		if err != nil || status.LastApplied < target {
			return false
		}
	}
	return true
}

func WaitForLeader(t *testing.T, nodes []*Node, timeout time.Duration) *Node {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		leader, leaderCount := CountLeader(t, nodes)
		if leaderCount == 1 {
			return leader
		}
		time.Sleep(100 * time.Millisecond)
	}
	dumpNodeStatuses(t, nodes)
	_, leaderCount := CountLeader(t, nodes)
	t.Fatalf("timed out waiting for one leader, got %d", leaderCount)
	return nil
}

func WaitForValue(t *testing.T, nodes []*Node, key, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !clusterCaughtUp(nodes) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		allMatch := true
		for _, node := range nodes {
			if !node.running {
				continue
			}
			value, err := node.TryGet(key)
			if err != nil || value != expected {
				allMatch = false
				break
			}
		}
		if allMatch {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	dumpNodeStatuses(t, nodes)
	for _, node := range nodes {
		if !node.running {
			continue
		}
		value, err := node.TryGet(key)
		if err != nil {
			t.Fatalf("%s failed to read %s: %v", node.id, key, err)
		}
		if value != expected {
			t.Fatalf("%s has wrong value for %s: got %s, want %s", node.id, key, value, expected)
		}
	}
}
