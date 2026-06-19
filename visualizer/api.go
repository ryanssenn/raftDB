package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

// Client requests must not hang forever when the leader is dead mid-commit.
var visualizerHTTPClient = &http.Client{Timeout: 3 * time.Second}

type NodeStatus struct {
	ID          string           `json:"id"`
	Running     bool             `json:"running"`
	Reachable   bool             `json:"reachable"`
	State       int              `json:"state,omitempty"`
	Term        int64            `json:"term,omitempty"`
	LeaderId    string           `json:"leaderId,omitempty"`
	CommitIndex int64            `json:"commitIndex,omitempty"`
	LastApplied int64            `json:"lastApplied,omitempty"`
	LogLength   int              `json:"logLength,omitempty"`
	MatchIndex  map[string]int64 `json:"matchIndex,omitempty"`
	NextIndex   map[string]int64 `json:"nextIndex,omitempty"`
}

type Server struct {
	mu            sync.RWMutex
	cluster       *Cluster
	scenario      *Scenario
	binaryPath    string
	demoPace      bool
	showcaseStart time.Time
	cycle         int
	stepIndex     int
	currentDesc   string
	done          bool
	err           string
	scenarioLog   []string
	eventSince    map[string]int64
	lastKilled    string
}

func fetchStatus(port string) (*NodeStatus, error) {
	resp, err := visualizerHTTPClient.Get("http://localhost:" + port + "/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	s := &NodeStatus{Reachable: true}
	if v, ok := raw["id"].(string); ok {
		s.ID = v
	}
	if v, ok := raw["state"].(float64); ok {
		s.State = int(v)
	}
	if v, ok := raw["term"].(float64); ok {
		s.Term = int64(v)
	}
	if v, ok := raw["leaderId"].(string); ok {
		s.LeaderId = v
	}
	if v, ok := raw["commitIndex"].(float64); ok {
		s.CommitIndex = int64(v)
	}
	if v, ok := raw["lastApplied"].(float64); ok {
		s.LastApplied = int64(v)
	}
	if v, ok := raw["logLength"].(float64); ok {
		s.LogLength = int(v)
	}
	if v, ok := raw["matchIndex"].(map[string]any); ok {
		s.MatchIndex = toInt64Map(v)
	}
	if v, ok := raw["nextIndex"].(map[string]any); ok {
		s.NextIndex = toInt64Map(v)
	}
	return s, nil
}

func toInt64Map(m map[string]any) map[string]int64 {
	out := map[string]int64{}
	for k, v := range m {
		if f, ok := v.(float64); ok {
			out[k] = int64(f)
		}
	}
	return out
}

func fetchEvents(port string, since int64) ([]Event, int64, error) {
	resp, err := visualizerHTTPClient.Get(fmt.Sprintf("http://localhost:%s/events?since=%d", port, since))
	if err != nil {
		return nil, since, err
	}
	defer resp.Body.Close()

	var body struct {
		Events    []Event `json:"events"`
		LatestSeq int64   `json:"latestSeq"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, since, err
	}
	return body.Events, body.LatestSeq, nil
}

type Event struct {
	Seq     int64  `json:"seq"`
	Ts      int64  `json:"ts"`
	Type    string `json:"type"`
	From    string `json:"from"`
	To      string `json:"to"`
	Term    int64  `json:"term,omitempty"`
	Entries int    `json:"entries,omitempty"`
	Op      string `json:"op,omitempty"`
	Key     string `json:"key,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

func doPut(port, key, value string) (string, error) {
	params := url.Values{}
	params.Set("key", key)
	params.Set("value", value)
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

func doGet(port, key string) (string, error) {
	params := url.Values{}
	params.Set("key", key)
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

func (srv *Server) handleScenario(w http.ResponseWriter, r *http.Request) {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	resp := map[string]any{
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
	}
	if srv.scenario.Showcase && !srv.showcaseStart.IsZero() {
		resp["showcaseStartMs"] = srv.showcaseStart.UnixMilli()
	}
	json.NewEncoder(w).Encode(resp)
}

func (srv *Server) handleScenarioLog(w http.ResponseWriter, r *http.Request) {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	json.NewEncoder(w).Encode(map[string]any{
		"lines": srv.scenarioLog,
	})
}

func (srv *Server) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	srv.mu.RLock()
	cluster := srv.cluster
	srv.mu.RUnlock()

	var statuses []NodeStatus
	for _, node := range cluster.Nodes {
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
	json.NewEncoder(w).Encode(map[string]any{"nodes": statuses})
}

func (srv *Server) handleClusterEvents(w http.ResponseWriter, r *http.Request) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

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

	json.NewEncoder(w).Encode(map[string]any{"events": all})
}

func (srv *Server) registerRoutes(mux *http.ServeMux, static http.Handler) {
	mux.Handle("/", static)
	mux.HandleFunc("/api/scenario", srv.handleScenario)
	mux.HandleFunc("/api/scenario/log", srv.handleScenarioLog)
	mux.HandleFunc("/api/cluster/status", srv.handleClusterStatus)
	mux.HandleFunc("/api/cluster/events", srv.handleClusterEvents)
}
