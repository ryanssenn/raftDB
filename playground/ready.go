package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

type readyChecks struct {
	Compose    string `json:"compose"`
	Prometheus string `json:"prometheus"`
	Grafana    string `json:"grafana"`
	Cluster    string `json:"cluster"`
	Leader     string `json:"leader"`
	Targets    string `json:"targets"`
}

type readyResponse struct {
	Ready  bool        `json:"ready"`
	Checks readyChecks `json:"checks"`
}

func (srv *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	checks := readyChecks{
		Compose:    "skipped",
		Prometheus: "skipped",
		Grafana:    "skipped",
		Cluster:    "pending",
		Leader:     "pending",
		Targets:    "pending",
	}

	if srv.composeEnabled {
		checks.Compose = "ok"
		if prometheusReady() {
			checks.Prometheus = "ok"
		} else {
			checks.Prometheus = "pending"
		}
		if grafanaReady() {
			checks.Grafana = "ok"
		} else {
			checks.Grafana = "pending"
		}
	}

	srv.mu.RLock()
	started := srv.clusterStarted
	nodeCount := len(srv.cluster.Nodes)
	statuses := srv.clusterStatusLocked()
	srv.mu.RUnlock()

	if started {
		checks.Cluster = "ok"
	} else {
		checks.Cluster = "pending"
	}

	leaders := 0
	for _, ns := range statuses {
		if ns.Running && ns.Reachable && (ns.State == 2 || ns.StateName == "leader") {
			leaders++
		}
	}
	if leaders == 1 {
		checks.Leader = "ok"
	} else if started {
		checks.Leader = "pending"
	}

	if srv.composeEnabled && started && nodeCount > 0 {
		up, err := prometheusTargetCount()
		if err != nil {
			checks.Targets = "pending"
		} else if up >= quorumNeeded(nodeCount) {
			checks.Targets = "ok"
		} else {
			checks.Targets = "pending"
		}
	} else if !srv.composeEnabled && started {
		checks.Targets = "skipped"
	}

	ready := true
	if srv.composeEnabled {
		ready = checks.Prometheus == "ok" && checks.Grafana == "ok"
	}

	json.NewEncoder(w).Encode(readyResponse{Ready: ready, Checks: checks})
}

func quorumNeeded(nodeCount int) int {
	return nodeCount/2 + 1
}

func prometheusTargetCount() (int, error) {
	q := url.Values{}
	q.Set("query", `count(up{job="ryanDB"}==1)`)
	resp, err := http.Get("http://localhost:9090/api/v1/query?" + q.Encode())
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
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
		return 0, err
	}
	if result.Status != "success" || len(result.Data.Result) == 0 {
		return 0, nil
	}
	if len(result.Data.Result[0].Value) < 2 {
		return 0, nil
	}
	return int(parsePromValue(result.Data.Result[0].Value[1])), nil
}
