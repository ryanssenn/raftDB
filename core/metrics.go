package core

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	termGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "quorum_term",
		Help: "Current Raft term.",
	}, []string{"node"})

	commitIndexGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "quorum_commit_index",
		Help: "Highest committed log index.",
	}, []string{"node"})

	lastAppliedGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "quorum_last_applied",
		Help: "Highest applied log index.",
	}, []string{"node"})

	applyLagGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "quorum_apply_lag",
		Help: "Commit index minus last applied index.",
	}, []string{"node"})

	logLengthGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "quorum_log_length",
		Help: "Number of log entries.",
	}, []string{"node"})

	stateGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "quorum_state",
		Help: "Raft state: 0=follower, 1=candidate, 2=leader.",
	}, []string{"node"})

	isLeaderGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "quorum_is_leader",
		Help: "1 if this node is the leader.",
	}, []string{"node"})

	electionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "quorum_elections_total",
		Help: "Election attempts started by this node.",
	}, []string{"node"})

	commitsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "quorum_commits_total",
		Help: "Log entries committed by this node as leader.",
	}, []string{"node"})

	appendEntriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "quorum_append_entries_total",
		Help: "AppendEntries RPC results.",
	}, []string{"node", "result"})

	requestVoteTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "quorum_requestvote_total",
		Help: "RequestVote RPC results.",
	}, []string{"node", "result"})

	clientRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "quorum_client_requests_total",
		Help: "Client HTTP requests.",
	}, []string{"node", "op", "result"})

	clientRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "quorum_client_request_duration_seconds",
		Help:    "Client HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"node", "op"})
)

func RegisterNodeMetrics(n *Node) {
	id := n.Id
	termGauge.WithLabelValues(id).Set(float64(n.Term.Load()))
	commit := n.CommitIndex.Load()
	applied := n.LastApplied.Load()
	commitIndexGauge.WithLabelValues(id).Set(float64(commit))
	lastAppliedGauge.WithLabelValues(id).Set(float64(applied))
	applyLagGauge.WithLabelValues(id).Set(float64(commit - applied))
	logLengthGauge.WithLabelValues(id).Set(float64(n.GetLogSize()))
	stateGauge.WithLabelValues(id).Set(float64(n.State))
	if n.State == Leader {
		isLeaderGauge.WithLabelValues(id).Set(1)
	} else {
		isLeaderGauge.WithLabelValues(id).Set(0)
	}
}

func (n *Node) RefreshMetrics() {
	RegisterNodeMetrics(n)
}

func (n *Node) RecordElection() {
	electionsTotal.WithLabelValues(n.Id).Inc()
}

func (n *Node) RecordCommit() {
	commitsTotal.WithLabelValues(n.Id).Inc()
}

func (n *Node) RecordAppendEntries(result string) {
	appendEntriesTotal.WithLabelValues(n.Id, result).Inc()
}

func (n *Node) RecordRequestVote(result string) {
	requestVoteTotal.WithLabelValues(n.Id, result).Inc()
}

func (n *Node) ObserveClientRequest(op, result string, d time.Duration) {
	clientRequestsTotal.WithLabelValues(n.Id, op, result).Inc()
	clientRequestDuration.WithLabelValues(n.Id, op).Observe(d.Seconds())
}

func ClassifyClientResult(op, body string) string {
	if strings.HasPrefix(body, "Error:") || strings.HasPrefix(body, "unknown") {
		return "error"
	}
	if op == "put" && body == "success" {
		return "success"
	}
	if op == "get" && body != "" {
		return "success"
	}
	return "error"
}
