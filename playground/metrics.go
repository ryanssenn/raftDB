package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	replicationLag = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "raftdb_replication_lag",
		Help: "Commit index lag vs current leader.",
	}, []string{"node"})

	leaderCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "raftdb_leader_count",
		Help: "Number of nodes reporting leader state.",
	})

	clusterNodes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "raftdb_cluster_nodes",
		Help: "Configured cluster size.",
	})

	nodesRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "raftdb_nodes_running",
		Help: "Number of node processes currently running.",
	})

	scenarioStep = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "raftdb_scenario_step",
		Help: "Current scenario step index.",
	}, []string{"scenario"})

	scenarioRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "raftdb_scenario_running",
		Help: "1 if a scenario is running.",
	})
)

func (srv *Server) refreshClusterMetrics() {
	srv.mu.RLock()
	statuses := srv.clusterStatusLocked()
	scenarioName := ""
	if srv.scenario != nil {
		scenarioName = srv.scenario.Name
	}
	step := srv.stepIndex
	running := srv.scenarioRunning
	nodeCount := len(srv.cluster.Nodes)
	runningCount := srv.cluster.RunningCount()
	srv.mu.RUnlock()

	var leaderCommit int64 = -1
	leaders := 0
	for _, ns := range statuses {
		if !ns.Running || !ns.Reachable {
			replicationLag.WithLabelValues(ns.ID).Set(0)
			continue
		}
		if ns.State == 2 || ns.StateName == "leader" {
			leaders++
			leaderCommit = ns.CommitIndex
		}
	}

	leaderCount.Set(float64(leaders))
	clusterNodes.Set(float64(nodeCount))
	nodesRunning.Set(float64(runningCount))

	for _, ns := range statuses {
		if !ns.Running || !ns.Reachable {
			continue
		}
		lag := float64(0)
		if leaderCommit >= 0 {
			lag = float64(leaderCommit - ns.CommitIndex)
			if lag < 0 {
				lag = 0
			}
		}
		replicationLag.WithLabelValues(ns.ID).Set(lag)
	}

	if scenarioName != "" {
		scenarioStep.WithLabelValues(scenarioName).Set(float64(step))
	}
	if running {
		scenarioRunning.Set(1)
	} else {
		scenarioRunning.Set(0)
	}
}

func (srv *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	srv.refreshClusterMetrics()
	promhttp.Handler().ServeHTTP(w, r)
}
