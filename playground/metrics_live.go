package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const metricsHistoryLen = 60

type metricsPoint struct {
	Ts  int64   `json:"ts"`
	Val float64 `json:"val"`
}

type metricsHistory struct {
	WriteOpsSec       []metricsPoint `json:"writeOpsSec"`
	ReadOpsSec        []metricsPoint `json:"readOpsSec"`
	WriteP50Ms        []metricsPoint `json:"writeP50Ms"`
	WriteP99Ms        []metricsPoint `json:"writeP99Ms"`
	ReadP99Ms         []metricsPoint `json:"readP99Ms"`
	CommitRate        []metricsPoint `json:"commitRate"`
	MaxReplicationLag []metricsPoint `json:"maxReplicationLag"`
	LeaderCount       []metricsPoint `json:"leaderCount"`
	NodesUp           []metricsPoint `json:"nodesUp"`
}

type liveMetricsResponse struct {
	WriteOpsSec       float64        `json:"writeOpsSec"`
	ReadOpsSec        float64        `json:"readOpsSec"`
	WriteP50Ms        float64        `json:"writeP50Ms"`
	WriteP99Ms        float64        `json:"writeP99Ms"`
	ReadP99Ms         float64        `json:"readP99Ms"`
	CommitRate        float64        `json:"commitRate"`
	MaxReplicationLag float64        `json:"maxReplicationLag"`
	ElectionRate      float64        `json:"electionRate"`
	FailoverMs        *float64       `json:"failoverMs"`
	ClientSendRate    float64        `json:"clientSendRate"`
	ClientSuccessRate float64        `json:"clientSuccessRate"`
	LeaderCount       int            `json:"leaderCount"`
	NodesUp           int            `json:"nodesUp"`
	ClusterSize       int            `json:"clusterSize"`
	History           metricsHistory `json:"history"`
}

func (srv *Server) recordFailoverStart() {
	srv.metricsMu.Lock()
	now := time.Now()
	srv.failoverStartedAt = &now
	srv.lastFailoverMs = nil
	srv.metricsMu.Unlock()
}

func (srv *Server) updateFailoverRecovery() {
	srv.mu.RLock()
	statuses := srv.clusterStatusLocked()
	srv.mu.RUnlock()

	leaders := 0
	for _, ns := range statuses {
		if ns.Running && ns.Reachable && (ns.State == 2 || ns.StateName == "leader") {
			leaders++
		}
	}

	srv.metricsMu.Lock()
	defer srv.metricsMu.Unlock()
	if srv.failoverStartedAt == nil || leaders != 1 {
		return
	}
	ms := float64(time.Since(*srv.failoverStartedAt).Milliseconds())
	srv.lastFailoverMs = &ms
	srv.failoverStartedAt = nil
}

func (srv *Server) appendHistory(h *metricsHistory, ts int64, writeOps, readOps, writeP50, writeP99, readP99, commitRate, maxLag, leaderCount, nodesUp float64) {
	appendPoint := func(series *[]metricsPoint, val float64) {
		*series = append(*series, metricsPoint{Ts: ts, Val: val})
		if len(*series) > metricsHistoryLen {
			*series = (*series)[len(*series)-metricsHistoryLen:]
		}
	}
	appendPoint(&h.WriteOpsSec, writeOps)
	appendPoint(&h.ReadOpsSec, readOps)
	appendPoint(&h.WriteP50Ms, writeP50)
	appendPoint(&h.WriteP99Ms, writeP99)
	appendPoint(&h.ReadP99Ms, readP99)
	appendPoint(&h.CommitRate, commitRate)
	appendPoint(&h.MaxReplicationLag, maxLag)
	appendPoint(&h.LeaderCount, leaderCount)
	appendPoint(&h.NodesUp, nodesUp)
}

func (srv *Server) handleMetricsLive(w http.ResponseWriter, r *http.Request) {
	srv.updateFailoverRecovery()

	now := time.Now().Unix()
	resp := liveMetricsResponse{}

	if srv.composeEnabled && prometheusReady() {
		resp.WriteOpsSec = promQueryScalar(`sum(rate(quorum_client_requests_total{op="put",result="success"}[5s]))`)
		resp.ReadOpsSec = promQueryScalar(`sum(rate(quorum_client_requests_total{op="get",result="success"}[5s]))`)
		resp.WriteP50Ms = promQueryScalar(`histogram_quantile(0.50, sum(rate(quorum_client_request_duration_seconds_bucket{op="put"}[5s])) by (le)) * 1000`)
		resp.WriteP99Ms = promQueryScalar(`histogram_quantile(0.99, sum(rate(quorum_client_request_duration_seconds_bucket{op="put"}[5s])) by (le)) * 1000`)
		resp.ReadP99Ms = promQueryScalar(`histogram_quantile(0.99, sum(rate(quorum_client_request_duration_seconds_bucket{op="get"}[5s])) by (le)) * 1000`)
		resp.CommitRate = promQueryScalar(`sum(rate(quorum_commits_total[5s]))`)
		resp.MaxReplicationLag = promQueryScalar(`max(quorum_replication_lag)`)
		resp.ElectionRate = promQueryScalar(`sum(rate(quorum_elections_total[5s]))`)
	}

	// Leadership and availability come straight from cluster status so the
	// chart works even when Docker/Prometheus are disabled.
	srv.mu.RLock()
	statuses := srv.clusterStatusLocked()
	clusterSize := len(srv.cluster.Nodes)
	srv.mu.RUnlock()
	leaders, nodesUp := 0, 0
	for _, ns := range statuses {
		if ns.Running && ns.Reachable {
			nodesUp++
			if ns.State == 2 || ns.StateName == "leader" {
				leaders++
			}
		}
	}
	resp.LeaderCount = leaders
	resp.NodesUp = nodesUp
	resp.ClusterSize = clusterSize

	srv.metricsMu.Lock()
	if srv.lastFailoverMs != nil {
		ms := *srv.lastFailoverMs
		resp.FailoverMs = &ms
	}

	load := srv.loadStatsSnapshot()
	if load.Active {
		resp.ClientSendRate = load.SendRate
		resp.ClientSuccessRate = load.SuccessRate
		if load.SuccessRate > resp.WriteOpsSec {
			resp.WriteOpsSec = load.SuccessRate
		}
		if load.ReadSuccessRate > resp.ReadOpsSec {
			resp.ReadOpsSec = load.ReadSuccessRate
		}
		if load.WriteP50Ms > 0 {
			resp.WriteP50Ms = load.WriteP50Ms
		}
		if load.WriteP99Ms > 0 {
			resp.WriteP99Ms = load.WriteP99Ms
		}
		if load.ReadP99Ms > 0 {
			resp.ReadP99Ms = load.ReadP99Ms
		}
	}

	srv.appendHistory(&srv.metricsHistory, now,
		resp.WriteOpsSec, resp.ReadOpsSec, resp.WriteP50Ms, resp.WriteP99Ms, resp.ReadP99Ms,
		resp.CommitRate, resp.MaxReplicationLag,
		float64(resp.LeaderCount), float64(resp.NodesUp))

	resp.History = srv.metricsHistory
	srv.metricsMu.Unlock()

	json.NewEncoder(w).Encode(resp)
}

func promQueryScalar(query string) float64 {
	q := url.Values{}
	q.Set("query", query)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://localhost:9090/api/v1/query?" + q.Encode())
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0
	}
	if result.Status != "success" || len(result.Data.Result) == 0 {
		return 0
	}
	if len(result.Data.Result[0].Value) < 2 {
		return 0
	}
	return parsePromValue(result.Data.Result[0].Value[1])
}

func parsePromValue(v any) float64 {
	switch n := v.(type) {
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%f", &f); err != nil {
			return 0
		}
		return f
	case float64:
		return n
	default:
		return 0
	}
}
