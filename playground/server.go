package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ryansenn/ryanDB/internal/harness"
)

type Server struct {
	mu              sync.RWMutex
	logMu           sync.Mutex
	cluster         *Cluster
	scenario        *Scenario
	binaryPath      string
	repoRoot        string
	composeEnabled  bool
	clusterStarted  bool
	demoPace        bool
	showcaseStart   time.Time
	cycle           int
	stepIndex       int
	currentDesc     string
	phase           string
	done            bool
	err             string
	scenarioLog     []string
	lastKilled      string
	scenarioRunning bool
	scenarioPaused  bool
	scenarioStop    chan struct{}
	scenarioDone    chan struct{}
	partitionActive bool
	partitionNodes  []string
	writeCount      int64
	lastWrite       WriteEvent
	loadStats       *loadTracker
	metricsMu           sync.Mutex
	metricsHistory      metricsHistory
	failoverStartedAt   *time.Time
	lastFailoverMs      *float64
	shutdownCh          chan struct{}
}

type WriteEvent struct {
	From string `json:"from"`
	To   string `json:"to"`
	Key  string `json:"key"`
}

func NewServer(binaryPath, repoRoot string, composeEnabled bool) *Server {
	return &Server{
		cluster:        NewCluster(5),
		binaryPath:     binaryPath,
		repoRoot:       repoRoot,
		composeEnabled: composeEnabled,
		shutdownCh:     make(chan struct{}, 1),
	}
}

func (srv *Server) ShutdownRequested() <-chan struct{} {
	return srv.shutdownCh
}

func (srv *Server) requestShutdown() {
	select {
	case srv.shutdownCh <- struct{}{}:
	default:
	}
}

// Stop halts any running scenario, load workers, and cluster nodes.
func (srv *Server) Stop() {
	srv.mu.Lock()
	running := srv.scenarioRunning
	stopCh := srv.scenarioStop
	doneCh := srv.scenarioDone
	srv.mu.Unlock()

	if running && stopCh != nil {
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
		if doneCh != nil {
			<-doneCh
		}
	}

	srv.mu.Lock()
	srv.cluster.StopAll()
	srv.clusterStarted = false
	srv.scenarioRunning = false
	srv.mu.Unlock()
}

func (srv *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
	go srv.requestShutdown()
}

func (srv *Server) appendLog(line string) {
	srv.logMu.Lock()
	defer srv.logMu.Unlock()
	ts := time.Now().Format("15:04:05")
	srv.scenarioLog = append(srv.scenarioLog, fmt.Sprintf("[%s] %s", ts, line))
	if len(srv.scenarioLog) > 500 {
		srv.scenarioLog = srv.scenarioLog[len(srv.scenarioLog)-500:]
	}
}

func (srv *Server) logSnapshot() []string {
	srv.logMu.Lock()
	defer srv.logMu.Unlock()
	return append([]string(nil), srv.scenarioLog...)
}

func (srv *Server) clusterStatusLocked() []NodeStatus {
	var statuses []NodeStatus
	for _, node := range srv.cluster.Nodes {
		ns := NodeStatus{ID: node.ID, Running: node.Running}
		if node.Running {
			st, err := fetchStatus(node.Port)
			if err != nil {
				ns.Reachable = false
			} else {
				st.Running = true
				st.Reachable = true
				ns = *st
				ns.Running = true
			}
		}
		statuses = append(statuses, ns)
	}
	return statuses
}

func (srv *Server) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	json.NewEncoder(w).Encode(map[string]any{
		"nodes":           srv.clusterStatusLocked(),
		"clusterStarted":  srv.clusterStarted,
		"nodeCount":       len(srv.cluster.Nodes),
		"partitionActive": srv.partitionActive,
		"partitionNodes":  srv.partitionNodes,
	})
}

func (srv *Server) handleClusterCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Nodes int `json:"nodes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Nodes < 3 || req.Nodes > 9 {
		http.Error(w, "nodes must be 3-9", http.StatusBadRequest)
		return
	}

	srv.mu.Lock()
	if srv.clusterStarted {
		srv.cluster.StopAll()
		srv.clusterStarted = false
	}
	harness.KillPorts(req.Nodes)
	srv.cluster = NewCluster(req.Nodes)
	srv.partitionActive = false
	srv.partitionNodes = nil
	srv.appendLog(fmt.Sprintf("cluster configured with %d nodes", req.Nodes))
	srv.mu.Unlock()

	_ = writePrometheusTargets(srv.repoRoot, req.Nodes)
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "nodes": req.Nodes})
}

func (srv *Server) handleClusterStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	srv.mu.Lock()
	if srv.clusterStarted {
		srv.mu.Unlock()
		http.Error(w, "cluster already started", http.StatusConflict)
		return
	}
	cluster := srv.cluster
	binary := srv.binaryPath
	nodeCount := len(cluster.Nodes)
	srv.mu.Unlock()

	if err := cluster.StartAll(binary, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	srv.mu.Lock()
	srv.clusterStarted = true
	srv.appendLog("cluster started")
	srv.mu.Unlock()
	_ = writePrometheusTargets(srv.repoRoot, nodeCount)
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (srv *Server) handleClusterStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	srv.mu.Lock()
	srv.cluster.StopAll()
	srv.clusterStarted = false
	srv.partitionActive = false
	srv.partitionNodes = nil
	srv.appendLog("cluster stopped")
	srv.mu.Unlock()
	_ = clearPrometheusTargets(srv.repoRoot)
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (srv *Server) handleScenario(w http.ResponseWriter, r *http.Request) {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	if srv.scenario == nil {
		json.NewEncoder(w).Encode(map[string]any{"loaded": false})
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"loaded":      true,
		"name":        srv.scenario.Name,
		"nodes":       srv.scenario.Nodes,
		"stepIndex":   srv.stepIndex,
		"totalSteps":  len(srv.scenario.Steps),
		"currentStep": srv.currentDesc,
		"phase":       srv.phase,
		"writeCount":  atomicLoadWriteCount(srv),
		"lastWrite":   srv.lastWrite,
		"load":        srv.loadStatsSnapshot(),
		"done":        srv.done,
		"error":       srv.err,
		"running":     srv.scenarioRunning,
		"paused":      srv.scenarioPaused,
		"log":         srv.logSnapshot(),
	})
}

func (srv *Server) handleScenarioLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	scenario, err := LoadScenario(resolveScenarioPath(req.Path))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	srv.mu.Lock()
	srv.scenario = scenario
	srv.stepIndex = 0
	srv.done = false
	srv.err = ""
	srv.writeCount = 0
	srv.lastWrite = WriteEvent{}
	srv.scenarioRunning = false
	srv.scenarioPaused = false
	if scenario.Realtime {
		srv.demoPace = false
	}
	srv.appendLog("loaded scenario: " + scenario.Name)
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "name": scenario.Name})
}

func (srv *Server) handleScenarioRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	srv.mu.Lock()
	if srv.scenario == nil {
		srv.mu.Unlock()
		http.Error(w, "no scenario loaded", http.StatusBadRequest)
		return
	}
	if srv.scenarioRunning {
		srv.mu.Unlock()
		http.Error(w, "scenario already running", http.StatusConflict)
		return
	}
	if !srv.clusterStarted {
		srv.mu.Unlock()
		http.Error(w, "cluster not started", http.StatusBadRequest)
		return
	}
	srv.scenarioRunning = true
	srv.scenarioPaused = false
	srv.scenarioStop = make(chan struct{})
	srv.scenarioDone = make(chan struct{})
	srv.done = false
	srv.err = ""
	srv.stepIndex = 0
	srv.appendLog("scenario started")
	srv.mu.Unlock()

	go func() {
		srv.runScenarioControlled()
		srv.mu.Lock()
		srv.scenarioRunning = false
		close(srv.scenarioDone)
		srv.mu.Unlock()
	}()

	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (srv *Server) handleScenarioPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	srv.mu.Lock()
	srv.scenarioPaused = !srv.scenarioPaused
	paused := srv.scenarioPaused
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"paused": paused})
}

func (srv *Server) handleScenarioReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	srv.mu.Lock()
	if srv.scenarioRunning {
		close(srv.scenarioStop)
	}
	srv.mu.Unlock()

	srv.mu.Lock()
	if srv.scenarioDone != nil {
		srv.mu.Unlock()
		<-srv.scenarioDone
		srv.mu.Lock()
	}
	srv.stepIndex = 0
	srv.done = false
	srv.err = ""
	srv.scenarioPaused = false
	srv.appendLog("scenario reset")
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

const fullDemoScenario = "playground/scenarios/full-demo.json"

func (srv *Server) ensureCluster(nodeCount int) error {
	srv.mu.RLock()
	started := srv.clusterStarted
	binary := srv.binaryPath
	srv.mu.RUnlock()
	if started {
		return nil
	}

	harness.KillPorts(nodeCount)
	srv.mu.Lock()
	srv.cluster = NewCluster(nodeCount)
	srv.mu.Unlock()
	_ = writePrometheusTargets(srv.repoRoot, nodeCount)

	if err := srv.cluster.StartAll(binary, true); err != nil {
		return err
	}

	srv.mu.Lock()
	srv.clusterStarted = true
	srv.appendLog(fmt.Sprintf("cluster started (%d nodes)", nodeCount))
	srv.mu.Unlock()
	return nil
}

func (srv *Server) handleStressTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scenario, err := LoadScenario(resolveScenarioPath(fullDemoScenario))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := srv.ensureCluster(scenario.Nodes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	srv.mu.Lock()
	if srv.scenarioRunning {
		srv.mu.Unlock()
		http.Error(w, "scenario already running", http.StatusConflict)
		return
	}
	srv.scenario = scenario
	srv.stepIndex = 0
	srv.done = false
	srv.err = ""
	srv.writeCount = 0
	srv.lastWrite = WriteEvent{}
	if scenario.Realtime {
		srv.demoPace = false
	}
	srv.scenarioRunning = true
	srv.scenarioPaused = false
	srv.scenarioStop = make(chan struct{})
	srv.scenarioDone = make(chan struct{})
	srv.appendLog("running stress test: " + scenario.Name)
	srv.mu.Unlock()

	go func() {
		srv.runScenarioControlled()
		srv.mu.Lock()
		srv.scenarioRunning = false
		close(srv.scenarioDone)
		srv.mu.Unlock()
	}()

	json.NewEncoder(w).Encode(map[string]any{"ok": true, "name": scenario.Name})
}

func resolveScenarioPath(path string) string {
	if path == "" {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	root := findRepoRoot()
	candidate := filepath.Join(root, path)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return path
}

func atomicLoadWriteCount(srv *Server) int64 {
	return atomic.LoadInt64(&srv.writeCount)
}

func (srv *Server) handleNodeStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	srv.mu.Lock()
	node := srv.cluster.NodeByID(req.ID)
	srv.mu.Unlock()
	if node == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if !node.Running {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "running": false})
		return
	}
	wasLeader := false
	if st, err := fetchStatus(node.Port); err == nil && st.State == 2 {
		wasLeader = true
	}
	node.Stop()
	srv.mu.Lock()
	srv.lastKilled = req.ID
	srv.appendLog("stopped " + req.ID)
	srv.mu.Unlock()
	if wasLeader {
		srv.recordFailoverStart()
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "running": false})
}

func (srv *Server) handleNodeStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	srv.mu.Lock()
	node := srv.cluster.NodeByID(req.ID)
	binary := srv.binaryPath
	started := srv.clusterStarted
	srv.mu.Unlock()
	if node == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if node.Running {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "running": true})
		return
	}
	if !started {
		http.Error(w, "cluster not started", http.StatusBadRequest)
		return
	}
	if err := node.Restart(binary); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	srv.mu.Lock()
	srv.appendLog("started " + req.ID)
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "running": true})
}

func (srv *Server) registerRoutes(mux *http.ServeMux, static http.Handler) {
	registerProxyRoutes(mux)
	mux.HandleFunc("/api/ready", srv.handleReady)
	mux.HandleFunc("/api/metrics/live", srv.handleMetricsLive)
	mux.HandleFunc("/metrics", srv.handleMetrics)
	mux.HandleFunc("/api/scenario/stress-test", srv.handleStressTest)
	mux.HandleFunc("/api/scenario/demo", srv.handleStressTest)
	mux.Handle("/", static)
	mux.HandleFunc("/api/cluster/status", srv.handleClusterStatus)
	mux.HandleFunc("/api/cluster/create", srv.handleClusterCreate)
	mux.HandleFunc("/api/cluster/start", srv.handleClusterStart)
	mux.HandleFunc("/api/cluster/stop", srv.handleClusterStop)
	mux.HandleFunc("/api/cluster/node/stop", srv.handleNodeStop)
	mux.HandleFunc("/api/cluster/node/start", srv.handleNodeStart)
	mux.HandleFunc("/api/scenario", srv.handleScenario)
	mux.HandleFunc("/api/scenario/load", srv.handleScenarioLoad)
	mux.HandleFunc("/api/scenario/run", srv.handleScenarioRun)
	mux.HandleFunc("/api/scenario/pause", srv.handleScenarioPause)
	mux.HandleFunc("/api/scenario/reset", srv.handleScenarioReset)
	mux.HandleFunc("/api/quit", srv.handleQuit)
}
