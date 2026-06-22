package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ryansenn/ryanDB/core"
	"github.com/ryansenn/ryanDB/internal/harness"
)

var visualizerHTTPClient = &http.Client{Timeout: 3 * time.Second}

type NodeStatus struct {
	ID          string           `json:"id"`
	Running     bool             `json:"running,omitempty"`
	Reachable   bool             `json:"reachable,omitempty"`
	State       int              `json:"state,omitempty"`
	StateName   string           `json:"stateName,omitempty"`
	Term        int64            `json:"term,omitempty"`
	LeaderId    string           `json:"leaderId,omitempty"`
	VoteFor     string           `json:"voteFor,omitempty"`
	CommitIndex int64            `json:"commitIndex,omitempty"`
	LastApplied int64            `json:"lastApplied,omitempty"`
	LogLength   int              `json:"logLength,omitempty"`
	MatchIndex  map[string]int64 `json:"matchIndex,omitempty"`
	NextIndex   map[string]int64 `json:"nextIndex,omitempty"`
	BlockedPeers []string        `json:"blockedPeers,omitempty"`
}

type Server struct {
	mu              sync.RWMutex
	logMu           sync.Mutex
	cluster         *Cluster
	scenario        *Scenario
	binaryPath      string
	sandbox         bool
	clusterStarted  bool
	demoPace        bool
	showcaseStart   time.Time
	cycle           int
	stepIndex       int
	currentDesc     string
	done            bool
	err             string
	scenarioLog     []string
	eventSince      map[string]int64
	lastKilled      string
	scenarioRunning bool
	scenarioPaused  bool
	scenarioStop    chan struct{}
	scenarioDone    chan struct{}
	partitionActive bool
	partitionNodes  []string
	streamClients   map[chan []byte]struct{}
}

func NewServer(binaryPath string, sandbox bool) *Server {
	return &Server{
		cluster:       NewCluster(5),
		binaryPath:    binaryPath,
		sandbox:       sandbox,
		eventSince:    map[string]int64{},
		streamClients: map[chan []byte]struct{}{},
	}
}

func fetchStatus(port string) (*NodeStatus, error) {
	resp, err := visualizerHTTPClient.Get("http://localhost:" + port + "/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var s NodeStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	s.Reachable = true
	return &s, nil
}

func fetchEvents(port string, since int64) ([]core.Event, int64, error) {
	resp, err := visualizerHTTPClient.Get(fmt.Sprintf("http://localhost:%s/events?since=%d", port, since))
	if err != nil {
		return nil, since, err
	}
	defer resp.Body.Close()

	var body struct {
		Events    []core.Event `json:"events"`
		LatestSeq int64        `json:"latestSeq"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, since, err
	}
	return body.Events, body.LatestSeq, nil
}

type Event = core.Event

func doPut(port, key, value, client string) (string, error) {
	params := url.Values{}
	params.Set("key", key)
	params.Set("value", value)
	if client != "" {
		params.Set("client", client)
	}
	resp, err := visualizerHTTPClient.Get("http://localhost:" + port + "/put?" + params.Encode())
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

func doGet(port, key, client string) (string, error) {
	params := url.Values{}
	params.Set("key", key)
	if client != "" {
		params.Set("client", client)
	}
	resp, err := visualizerHTTPClient.Get("http://localhost:" + port + "/get?" + params.Encode())
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

func (srv *Server) collectEventsLocked() []Event {
	var all []Event
	for _, node := range srv.cluster.Nodes {
		if !node.Running {
			continue
		}
		since := srv.eventSince[node.ID]
		events, latest, err := fetchEvents(node.Port, since)
		if err != nil {
			continue
		}
		srv.eventSince[node.ID] = latest
		all = append(all, events...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Ts != all[j].Ts {
			return all[i].Ts < all[j].Ts
		}
		return all[i].Seq < all[j].Seq
	})
	return all
}

func (srv *Server) broadcastSnapshot() {
	srv.mu.Lock()
	payload := map[string]any{
		"nodes":      srv.clusterStatusLocked(),
		"events":     srv.collectEventsLocked(),
		"clusterStarted": srv.clusterStarted,
		"partitionActive": srv.partitionActive,
		"partitionNodes": srv.partitionNodes,
	}
	if srv.scenario != nil {
		payload["scenario"] = map[string]any{
			"name":        srv.scenario.Name,
			"stepIndex":   srv.stepIndex,
			"totalSteps":  len(srv.scenario.Steps),
			"currentStep": srv.currentDesc,
			"done":        srv.done,
			"running":     srv.scenarioRunning,
			"paused":      srv.scenarioPaused,
		}
	}
	logLines := srv.logSnapshot()
	srv.mu.Unlock()
	payload["log"] = logLines

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := append([]byte("data: "), data...)
	msg = append(msg, []byte("\n\n")...)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	for ch := range srv.streamClients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (srv *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 8)
	srv.mu.Lock()
	srv.streamClients[ch] = struct{}{}
	srv.mu.Unlock()

	defer func() {
		srv.mu.Lock()
		delete(srv.streamClients, ch)
		srv.mu.Unlock()
		close(ch)
	}()

	srv.broadcastSnapshot()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			srv.broadcastSnapshot()
		}
	}
}

func (srv *Server) handleScenario(w http.ResponseWriter, r *http.Request) {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	if srv.scenario == nil {
		json.NewEncoder(w).Encode(map[string]any{"loaded": false})
		return
	}

	resp := map[string]any{
		"loaded":      true,
		"name":        srv.scenario.Name,
		"nodes":       srv.scenario.Nodes,
		"stepIndex":   srv.stepIndex,
		"totalSteps":  len(srv.scenario.Steps),
		"currentStep": srv.currentDesc,
		"done":        srv.done,
		"error":       srv.err,
		"demoPace":    srv.demoPace,
		"showcase":    srv.scenario.Showcase,
		"loop":        srv.scenario.Loop,
		"cycle":       srv.cycle,
		"durationMs":  srv.scenario.DurationMs,
		"scenes":      srv.scenario.Scenes,
		"running":     srv.scenarioRunning,
		"paused":      srv.scenarioPaused,
		"log":         srv.logSnapshot(),
	}
	if srv.scenario.Showcase && !srv.showcaseStart.IsZero() {
		resp["showcaseStartMs"] = srv.showcaseStart.UnixMilli()
	}
	json.NewEncoder(w).Encode(resp)
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

func (srv *Server) handleClusterEvents(w http.ResponseWriter, r *http.Request) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"events": srv.collectEventsLocked()})
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
	defer srv.mu.Unlock()
	if srv.clusterStarted {
		srv.cluster.StopAll()
		srv.clusterStarted = false
	}
	harness.KillPorts(req.Nodes)
	srv.cluster = NewCluster(req.Nodes)
	srv.eventSince = map[string]int64{}
	srv.partitionActive = false
	srv.partitionNodes = nil
	srv.appendLog(fmt.Sprintf("cluster configured with %d nodes", req.Nodes))
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
	srv.mu.Unlock()

	if err := cluster.StartAll(binary, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	srv.mu.Lock()
	srv.clusterStarted = true
	srv.appendLog("cluster started")
	srv.mu.Unlock()
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
	srv.eventSince = map[string]int64{}
	srv.appendLog("cluster stopped")
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (srv *Server) handleNodeKill(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	srv.mu.Lock()
	node := srv.cluster.NodeByID(id)
	if node == nil {
		srv.mu.Unlock()
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	node.Stop()
	srv.lastKilled = id
	srv.appendLog("killed " + id)
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (srv *Server) handleNodeRestart(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	srv.mu.Lock()
	node := srv.cluster.NodeByID(id)
	binary := srv.binaryPath
	if node == nil {
		srv.mu.Unlock()
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	srv.mu.Unlock()

	if err := node.Restart(binary); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	srv.mu.Lock()
	srv.appendLog("restarted " + id)
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (srv *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Client string `json:"client"`
		Op     string `json:"op"`
		Key    string `json:"key"`
		Value  string `json:"value"`
		Node   string `json:"node"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Node == "" {
		http.Error(w, "node required", http.StatusBadRequest)
		return
	}

	srv.mu.RLock()
	node := srv.cluster.NodeByID(req.Node)
	srv.mu.RUnlock()
	if node == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if !node.Running {
		http.Error(w, "node not running", http.StatusBadRequest)
		return
	}

	var result string
	var err error
	switch req.Op {
	case "put":
		result, err = doPut(node.Port, req.Key, req.Value, req.Client)
	case "get":
		result, err = doGet(node.Port, req.Key, req.Client)
	default:
		http.Error(w, "op must be put or get", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	srv.appendLog(fmt.Sprintf("%s %s %s → %s = %s", req.Client, req.Op, req.Key, req.Node, result))
	json.NewEncoder(w).Encode(map[string]any{"result": result})
}

func (srv *Server) handlePartition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Isolated []string `json:"isolated"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	srv.mu.RLock()
	cluster := srv.cluster
	srv.mu.RUnlock()

	if err := cluster.SetPartition(req.Isolated); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	srv.mu.Lock()
	srv.partitionActive = len(req.Isolated) > 0
	srv.partitionNodes = append([]string(nil), req.Isolated...)
	srv.appendLog(fmt.Sprintf("partition: isolated %v", req.Isolated))
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (srv *Server) handlePartitionClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	srv.mu.RLock()
	cluster := srv.cluster
	srv.mu.RUnlock()

	if err := cluster.ClearPartition(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	srv.mu.Lock()
	srv.partitionActive = false
	srv.partitionNodes = nil
	srv.appendLog("partition cleared")
	srv.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
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
	srv.scenarioRunning = false
	srv.scenarioPaused = false
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

func (srv *Server) registerRoutes(mux *http.ServeMux, static http.Handler) {
	mux.Handle("/", static)
	mux.HandleFunc("/api/stream", srv.handleStream)
	mux.HandleFunc("/api/scenario", srv.handleScenario)
	mux.HandleFunc("/api/scenario/load", srv.handleScenarioLoad)
	mux.HandleFunc("/api/scenario/run", srv.handleScenarioRun)
	mux.HandleFunc("/api/scenario/pause", srv.handleScenarioPause)
	mux.HandleFunc("/api/scenario/reset", srv.handleScenarioReset)
	mux.HandleFunc("/api/cluster/create", srv.handleClusterCreate)
	mux.HandleFunc("/api/cluster/start", srv.handleClusterStart)
	mux.HandleFunc("/api/cluster/stop", srv.handleClusterStop)
	mux.HandleFunc("/api/cluster/status", srv.handleClusterStatus)
	mux.HandleFunc("/api/cluster/events", srv.handleClusterEvents)
	mux.HandleFunc("/api/cluster/partition", srv.handlePartition)
	mux.HandleFunc("/api/cluster/partition/clear", srv.handlePartitionClear)
	mux.HandleFunc("/api/request", srv.handleRequest)
	mux.HandleFunc("/api/cluster/nodes/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/cluster/nodes/"):]
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if len(id) > 5 && id[len(id)-5:] == "/kill" {
			srv.handleNodeKill(w, r, id[:len(id)-5])
			return
		}
		if len(id) > 8 && id[len(id)-8:] == "/restart" {
			srv.handleNodeRestart(w, r, id[:len(id)-8])
			return
		}
		http.NotFound(w, r)
	})
}
