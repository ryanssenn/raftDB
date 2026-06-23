package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type PutStep struct {
	Node  string `json:"node"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type GetStep struct {
	Node   string `json:"node"`
	Key    string `json:"key"`
	Expect string `json:"expect,omitempty"`
}

type LoadStep struct {
	Duration  string `json:"duration"`
	Interval  string `json:"interval"`
	KeyPrefix string `json:"keyPrefix"`
}

type PartitionStep struct {
	Isolated []string `json:"isolated"`
}

type Step struct {
	Wait           string         `json:"wait,omitempty"`
	Comment        string         `json:"comment,omitempty"`
	Kill           string         `json:"kill,omitempty"`
	Restart        string         `json:"restart,omitempty"`
	ClearPartition bool           `json:"clear_partition,omitempty"`
	Put            *PutStep       `json:"put,omitempty"`
	Get            *GetStep       `json:"get,omitempty"`
	Partition      *PartitionStep `json:"partition,omitempty"`
	Load           *LoadStep      `json:"load,omitempty"`
}

type Scenario struct {
	Name       string  `json:"name"`
	Nodes      int     `json:"nodes"`
	Steps      []Step  `json:"steps"`
	Showcase   bool    `json:"showcase,omitempty"`
	Realtime   bool    `json:"realtime,omitempty"`
	Loop       bool    `json:"loop,omitempty"`
	DurationMs int     `json:"durationMs,omitempty"`
	Scenes     []Scene `json:"scenes,omitempty"`
}

type Scene struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle"`
	StartMs    int    `json:"startMs"`
	DurationMs int    `json:"durationMs"`
}

func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Nodes < 3 || s.Nodes > 9 {
		return nil, fmt.Errorf("nodes must be between 3 and 9, got %d", s.Nodes)
	}
	if len(s.Steps) == 0 {
		return nil, fmt.Errorf("scenario has no steps")
	}
	for i, step := range s.Steps {
		if err := validateStep(step, s.Nodes); err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
	}
	return &s, nil
}

func validateStep(s Step, nodeCount int) error {
	kinds := 0
	if s.Wait != "" {
		kinds++
	}
	if s.Kill != "" {
		kinds++
	}
	if s.Restart != "" {
		kinds++
	}
	if s.Put != nil {
		kinds++
	}
	if s.Get != nil {
		kinds++
	}
	if s.Partition != nil {
		kinds++
	}
	if s.ClearPartition {
		kinds++
	}
	if s.Load != nil {
		kinds++
	}
	if kinds != 1 {
		return fmt.Errorf("each step must have exactly one action")
	}
	if s.Kill != "" {
		if s.Kill != "leader" {
			if err := checkNodeID(s.Kill, nodeCount); err != nil {
				return err
			}
		}
	}
	if s.Restart != "" {
		if s.Restart != "killed" {
			if err := checkNodeID(s.Restart, nodeCount); err != nil {
				return err
			}
		}
	}
	if s.Put != nil {
		if err := checkNodeID(s.Put.Node, nodeCount); err != nil {
			return err
		}
	}
	if s.Get != nil {
		if err := checkNodeID(s.Get.Node, nodeCount); err != nil {
			return err
		}
	}
	if s.Load != nil {
		if _, err := time.ParseDuration(s.Load.Duration); err != nil {
			return fmt.Errorf("load duration: %w", err)
		}
		if _, err := time.ParseDuration(s.Load.Interval); err != nil {
			return fmt.Errorf("load interval: %w", err)
		}
	}
	return nil
}

func checkNodeID(id string, nodeCount int) error {
	for i := 1; i <= nodeCount; i++ {
		if id == fmt.Sprintf("node%d", i) {
			return nil
		}
	}
	return fmt.Errorf("invalid node id %q for cluster size %d", id, nodeCount)
}

func (s *Step) Description() string {
	switch {
	case s.Wait != "":
		if s.Comment != "" {
			return "wait " + s.Wait + ": " + s.Comment
		}
		return "wait " + s.Wait
	case s.Kill != "":
		if s.Comment != "" {
			return "kill " + s.Kill + ": " + s.Comment
		}
		return "kill " + s.Kill
	case s.Restart != "":
		if s.Comment != "" {
			return "restart " + s.Restart + ": " + s.Comment
		}
		return "restart " + s.Restart
	case s.Put != nil:
		return fmt.Sprintf("put %s=%s → %s", s.Put.Key, s.Put.Value, s.Put.Node)
	case s.Get != nil:
		return fmt.Sprintf("get %s from %s", s.Get.Key, s.Get.Node)
	case s.Partition != nil:
		return fmt.Sprintf("partition isolate %v", s.Partition.Isolated)
	case s.ClearPartition:
		return "clear partition"
	case s.Load != nil:
		if s.Comment != "" {
			return fmt.Sprintf("load %s @ %s: %s", s.Load.Duration, s.Load.Interval, s.Comment)
		}
		return fmt.Sprintf("load %s @ %s", s.Load.Duration, s.Load.Interval)
	default:
		return "unknown step"
	}
}

func (srv *Server) runScenarioControlled() {
	srv.runScenario()
}

func (srv *Server) waitIfPaused() bool {
	for {
		srv.mu.RLock()
		paused := srv.scenarioPaused
		stop := srv.scenarioStop
		srv.mu.RUnlock()
		if stop != nil {
			select {
			case <-stop:
				return false
			default:
			}
		}
		if !paused {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (srv *Server) runScenario() {
	for cycle := 0; ; cycle++ {
		if cycle > 0 {
			if err := srv.restartForLoop(); err != nil {
				srv.mu.Lock()
				srv.err = err.Error()
				srv.done = true
				srv.appendLog("ERROR: " + err.Error())
				srv.mu.Unlock()
				return
			}
		}

		srv.mu.Lock()
		srv.cycle = cycle
		srv.done = false
		srv.err = ""
		srv.stepIndex = 0
		srv.lastKilled = ""
		srv.mu.Unlock()

		failed := false
		for i, step := range srv.scenario.Steps {
			if !srv.waitIfPaused() {
				return
			}
			srv.mu.Lock()
			srv.stepIndex = i
			srv.currentDesc = step.Description()
			if step.Comment != "" {
				srv.phase = step.Comment
			}
			srv.mu.Unlock()

			if err := srv.executeStep(step); err != nil {
				srv.mu.Lock()
				srv.err = err.Error()
				srv.appendLog("ERROR: " + err.Error())
				srv.done = true
				srv.mu.Unlock()
				failed = true
				break
			}
		}

		if failed || !srv.scenario.Loop {
			srv.mu.Lock()
			if !failed {
				srv.appendLog("scenario complete")
			}
			srv.done = true
			srv.mu.Unlock()
			return
		}

		srv.appendLog(fmt.Sprintf("loop: cycle %d complete, restarting", cycle+1))
	}
}

func (srv *Server) restartForLoop() error {
	srv.appendLog("restarting cluster for loop")
	srv.cluster.StopAll()
	time.Sleep(300 * time.Millisecond)
	if srv.scenario.Showcase {
		if err := srv.cluster.StartStaggered(srv.binaryPath, true, 500*time.Millisecond); err != nil {
			return err
		}
	} else {
		if err := srv.cluster.StartAll(srv.binaryPath, true); err != nil {
			return err
		}
	}
	srv.mu.Lock()
	srv.showcaseStart = time.Now()
	srv.mu.Unlock()
	return nil
}

func compressWait(d time.Duration) time.Duration {
	ms := d.Milliseconds()
	compressed := ms / 7
	if compressed < 450 {
		compressed = 450
	}
	if compressed > 900 {
		compressed = 900
	}
	return time.Duration(compressed) * time.Millisecond
}

func (srv *Server) continueOnClientError(err error, msg string) error {
	if err == nil {
		return nil
	}
	if srv.scenario.Showcase {
		srv.appendLog("  (missed) " + msg + ": " + err.Error())
		return nil
	}
	return err
}

func (srv *Server) executeStep(step Step) error {
	switch {
	case step.Wait != "":
		d, err := time.ParseDuration(step.Wait)
		if err != nil {
			return err
		}
		if srv.demoPace && !srv.scenario.Showcase && !srv.scenario.Realtime {
			d = compressWait(d)
		}
		srv.appendLog("waiting " + step.Wait)
		time.Sleep(d)
		return nil

	case step.Kill != "":
		target := step.Kill
		if target == "leader" {
			id, err := srv.currentLeaderID()
			if err != nil {
				return err
			}
			target = id
		}
		srv.appendLog("killing " + target)
		node := srv.cluster.NodeByID(target)
		if node == nil {
			return fmt.Errorf("node %s not found", target)
		}
		node.Stop()
		srv.mu.Lock()
		srv.lastKilled = target
		srv.mu.Unlock()
		if step.Kill == "leader" || target != "" {
			srv.recordFailoverStart()
		}
		return nil

	case step.Restart != "":
		target := step.Restart
		if target == "killed" {
			srv.mu.RLock()
			target = srv.lastKilled
			srv.mu.RUnlock()
			if target == "" {
				return fmt.Errorf("no killed node to restart")
			}
		}
		srv.appendLog("restarting " + target)
		node := srv.cluster.NodeByID(target)
		if node == nil {
			return fmt.Errorf("node %s not found", target)
		}
		return node.Restart(srv.binaryPath)

	case step.Put != nil:
		srv.appendLog(fmt.Sprintf("put %s=%s → %s", step.Put.Key, step.Put.Value, step.Put.Node))
		node := srv.cluster.NodeByID(step.Put.Node)
		if node == nil {
			return fmt.Errorf("node %s not found", step.Put.Node)
		}
		if !node.Running {
			return srv.continueOnClientError(
				fmt.Errorf("node %s is not running", step.Put.Node),
				"write",
			)
		}
		result, err := doPut(node.Port, step.Put.Key, step.Put.Value, "client")
		if err != nil {
			return srv.continueOnClientError(err, "write")
		}
		srv.recordWrite(step.Put.Node, step.Put.Key)
		srv.appendLog("  → " + result)
		return nil

	case step.Get != nil:
		srv.appendLog(fmt.Sprintf("get %s from %s", step.Get.Key, step.Get.Node))
		node := srv.cluster.NodeByID(step.Get.Node)
		if node == nil {
			return fmt.Errorf("node %s not found", step.Get.Node)
		}
		if !node.Running {
			return srv.continueOnClientError(
				fmt.Errorf("node %s is not running", step.Get.Node),
				"read",
			)
		}
		result, err := doGet(node.Port, step.Get.Key, "client")
		if err != nil {
			return srv.continueOnClientError(err, "read")
		}
		srv.appendLog("  → " + result)
		if step.Get.Expect != "" && result != step.Get.Expect {
			return srv.continueOnClientError(
				fmt.Errorf("expected %q, got %q", step.Get.Expect, result),
				"read",
			)
		}
		return nil

	case step.Partition != nil:
		srv.appendLog(fmt.Sprintf("partition: isolate %v", step.Partition.Isolated))
		if err := srv.cluster.SetPartition(step.Partition.Isolated); err != nil {
			return err
		}
		srv.mu.Lock()
		srv.partitionActive = len(step.Partition.Isolated) > 0
		srv.partitionNodes = append([]string(nil), step.Partition.Isolated...)
		srv.mu.Unlock()
		return nil

	case step.ClearPartition:
		srv.appendLog("clear partition")
		if err := srv.cluster.ClearPartition(); err != nil {
			return err
		}
		srv.mu.Lock()
		srv.partitionActive = false
		srv.partitionNodes = nil
		srv.mu.Unlock()
		return nil

	case step.Load != nil:
		duration, err := time.ParseDuration(step.Load.Duration)
		if err != nil {
			return err
		}
		interval, err := time.ParseDuration(step.Load.Interval)
		if err != nil {
			return err
		}
		prefix := step.Load.KeyPrefix
		if prefix == "" {
			prefix = "key"
		}
		nodeCount := len(srv.cluster.Nodes)
		srv.appendLog(fmt.Sprintf("load %s @ %s (prefix %s)", step.Load.Duration, step.Load.Interval, prefix))
		deadline := time.Now().Add(duration)
		tick := 0
		for time.Now().Before(deadline) {
			if !srv.waitIfPaused() {
				return nil
			}
			nodeID := fmt.Sprintf("node%d", (tick%nodeCount)+1)
			key := fmt.Sprintf("%s:%d", prefix, tick+1)
			node := srv.cluster.NodeByID(nodeID)
			if node != nil && node.Running {
				result, err := doPut(node.Port, key, fmt.Sprintf("v%d", tick+1), "client")
				if err != nil {
					_ = srv.continueOnClientError(err, "write")
				} else {
					srv.recordWrite(nodeID, key)
					if tick%10 == 0 {
						srv.appendLog(fmt.Sprintf("  → %s on %s", result, nodeID))
					}
				}
			}
			tick++
			if !srv.sleepLoadInterval(interval) {
				return nil
			}
		}
		return nil

	default:
		return fmt.Errorf("empty step")
	}
}

func (srv *Server) recordWrite(to, key string) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.writeCount++
	srv.lastWrite = WriteEvent{From: "client", To: to, Key: key}
}

func (srv *Server) sleepLoadInterval(d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if !srv.waitIfPaused() {
			return false
		}
		remaining := time.Until(end)
		if remaining <= 0 {
			return true
		}
		sleep := 50 * time.Millisecond
		if remaining < sleep {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
	return true
}

func (srv *Server) currentLeaderID() (string, error) {
	for _, node := range srv.cluster.Nodes {
		if !node.Running {
			continue
		}
		st, err := fetchStatus(node.Port)
		if err != nil {
			continue
		}
		if st.State == 2 {
			return node.ID, nil
		}
	}
	return "", fmt.Errorf("no leader found")
}
