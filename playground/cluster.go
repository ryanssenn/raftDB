package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ryansenn/quorum/internal/harness"
)

type ClusterNode struct {
	ID      string
	Port    string
	Peers   string
	Cmd     *exec.Cmd
	Running bool
}

type Cluster struct {
	Nodes []*ClusterNode
}

func NewCluster(n int) *Cluster {
	peers := harness.BuildPeers(n)
	nodes := make([]*ClusterNode, n)
	for i := 0; i < n; i++ {
		nodes[i] = &ClusterNode{
			ID:    "node" + strconv.Itoa(i+1),
			Port:  strconv.Itoa(8001 + i),
			Peers: peers,
		}
	}
	return &Cluster{Nodes: nodes}
}

func (c *Cluster) StartStaggered(binary string, reset bool, interval time.Duration) error {
	for i, node := range c.Nodes {
		rs := "false"
		if reset {
			rs = "true"
		}
		if err := node.Start(binary, rs); err != nil {
			return err
		}
		if i < len(c.Nodes)-1 {
			time.Sleep(interval)
		}
	}
	return nil
}

func (c *Cluster) StartAll(binary string, reset bool) error {
	resetStr := "false"
	if reset {
		resetStr = "true"
	}
	for _, node := range c.Nodes {
		if err := node.Start(binary, resetStr); err != nil {
			return err
		}
	}
	return WaitForLeader(c, 15*time.Second)
}

func (c *Cluster) StopAll() {
	for _, node := range c.Nodes {
		node.Stop()
	}
}

func (n *ClusterNode) Start(binary, reset string) error {
	cmd := exec.Command(binary,
		"--id="+n.ID,
		"--port="+n.Port,
		"--peers="+n.Peers,
		"--reset="+reset,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", n.ID, err)
	}
	n.Cmd = cmd
	n.Running = true
	return nil
}

func (n *ClusterNode) Stop() {
	if n.Cmd != nil && n.Cmd.Process != nil {
		_ = n.Cmd.Process.Kill()
		n.Running = false
		_ = n.Cmd.Wait()
		n.Cmd = nil
	}
}

func (n *ClusterNode) Restart(binary string) error {
	n.Stop()
	return n.Start(binary, "false")
}

func (c *Cluster) NodeByID(id string) *ClusterNode {
	for _, node := range c.Nodes {
		if node.ID == id {
			return node
		}
	}
	return nil
}

func (c *Cluster) RunningCount() int {
	count := 0
	for _, node := range c.Nodes {
		if node.Running {
			count++
		}
	}
	return count
}

func WaitForLeader(c *Cluster, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, count := countLeader(c)
		if count == 1 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, count := countLeader(c)
	return fmt.Errorf("timed out waiting for leader, got %d leaders", count)
}

func countLeader(c *Cluster) (*ClusterNode, int) {
	count := 0
	var leader *ClusterNode
	for _, node := range c.Nodes {
		if !node.Running {
			continue
		}
		status, err := fetchStatus(node.Port)
		if err != nil {
			continue
		}
		if status.State == 2 {
			count++
			leader = node
		}
	}
	return leader, count
}

func (c *Cluster) SetPartition(isolated []string) error {
	isolatedSet := map[string]bool{}
	for _, id := range isolated {
		isolatedSet[id] = true
	}
	for _, node := range c.Nodes {
		if !node.Running {
			continue
		}
		if err := unblockAll(node.Port); err != nil {
			return err
		}
		for _, peer := range c.Nodes {
			if peer.ID == node.ID {
				continue
			}
			nodeIsolated := isolatedSet[node.ID]
			peerIsolated := isolatedSet[peer.ID]
			if nodeIsolated != peerIsolated {
				if err := blockPeer(node.Port, peer.ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Cluster) ClearPartition() error {
	for _, node := range c.Nodes {
		if !node.Running {
			continue
		}
		if err := unblockAll(node.Port); err != nil {
			return err
		}
	}
	return nil
}

func ensureBinary(repoRoot, binaryPath string) (string, error) {
	if binaryPath != "" {
		return binaryPath, nil
	}
	path := filepath.Join(repoRoot, "quorum")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	cmd := exec.Command("go", "build", "-o", "quorum", ".")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build quorum: %w\n%s", err, out)
	}
	return path, nil
}

func findRepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "core")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}

func openBrowser(url string) {
	for _, cmd := range []string{"xdg-open", "open"} {
		if path, err := exec.LookPath(cmd); err == nil {
			_ = exec.Command(path, url).Start()
			return
		}
	}
}

