package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/ryansenn/quorum/core"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var node *core.Node

func clientID(r *http.Request) string {
	if id := strings.TrimSpace(r.URL.Query().Get("client")); id != "" {
		return id
	}
	return "client"
}

func leaderIDStr() string {
	if p := node.LeaderId.Load(); p != nil {
		return *p
	}
	return ""
}

func voteForStr() string {
	if p := node.VoteFor.Load(); p != nil {
		return *p
	}
	return ""
}

func get(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w.Header().Set("Content-Type", "text/plain")
	key := r.URL.Query().Get("key")
	cmd := core.NewCommand("get", key, "")
	if node.Events != nil {
		node.Events.Record(core.Event{
			Type: "client_request",
			From: clientID(r),
			To:   node.Id,
			Op:   "get",
			Key:  key,
		})
	}
	result := node.HandleCommand(cmd)
	node.ObserveClientRequest("get", core.ClassifyClientResult("get", result), time.Since(start))
	w.Write([]byte(result))
}

func put(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w.Header().Set("Content-Type", "text/plain")
	key := r.URL.Query().Get("key")
	value := r.URL.Query().Get("value")
	cmd := core.NewCommand("put", key, value)
	if node.Events != nil {
		node.Events.Record(core.Event{
			Type: "client_request",
			From: clientID(r),
			To:   node.Id,
			Op:   "put",
			Key:  key,
		})
	}
	result := node.HandleCommand(cmd)
	node.ObserveClientRequest("put", core.ClassifyClientResult("put", result), time.Since(start))
	w.Write([]byte(result))
}

func status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	matchIndex := map[string]int64{}
	nextIndex := map[string]int64{}
	for id, val := range node.MatchIndex {
		if id != node.Id {
			matchIndex[id] = val.Load()
		}
	}
	for id, val := range node.NextIndex {
		if id != node.Id {
			nextIndex[id] = val.Load()
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"id":           node.Id,
		"state":        node.State,
		"stateName":    node.StateName(),
		"term":         node.Term.Load(),
		"leaderId":     leaderIDStr(),
		"voteFor":      voteForStr(),
		"commitIndex":  node.CommitIndex.Load(),
		"lastApplied":  node.LastApplied.Load(),
		"logLength":    node.GetLogSize(),
		"matchIndex":   matchIndex,
		"nextIndex":    nextIndex,
		"blockedPeers": node.BlockedPeerList(),
	})
}

func logHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 {
		tail = 20
	}
	json.NewEncoder(w).Encode(map[string]any{
		"entries": node.GetLogTail(tail),
	})
}

func events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	evts, latest := node.Events.Since(since)
	json.NewEncoder(w).Encode(map[string]any{
		"events":    evts,
		"latestSeq": latest,
	})
}

func simulateBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	peer := strings.TrimSpace(r.URL.Query().Get("peer"))
	if peer == "" {
		http.Error(w, "peer required", http.StatusBadRequest)
		return
	}
	core.PeerBlockMu.Lock()
	node.BlockPeer(peer)
	core.PeerBlockMu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func simulateUnblock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	peer := strings.TrimSpace(r.URL.Query().Get("peer"))
	core.PeerBlockMu.Lock()
	if peer == "" {
		node.UnblockAllPeers()
	} else {
		node.UnblockPeer(peer)
	}
	core.PeerBlockMu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic: %v", r)
			debug.PrintStack()
		}
	}()

	id := flag.String("id", "", "Unique node ID")
	port := flag.String("port", "8000", "Port to listen on")
	peersStr := flag.String("peers", "", "Comma-separated list of id=addr pairs (e.g., node1=localhost:8001,node2=localhost:8002,node3=localhost:8003)")
	reset := flag.Bool("reset", false, "Reset logs and metadata")
	noEvents := flag.Bool("no-events", false, "Disable debug event recording")
	metrics := flag.Bool("metrics", true, "Expose Prometheus /metrics endpoint")

	flag.Parse()

	if *id == "" || *peersStr == "" {
		fmt.Println("Usage: go run main.go --id=node1 --port=8001 --peers=node1=localhost:8001,node2=localhost:8002,node3=localhost:8003")
		return
	}

	node = core.NewNode(*id, parsePeers(*peersStr))

	if *noEvents {
		node.Events = nil
	}

	if *reset {
		node.Logger.ClearData()
	} else {
		node.RecoverState()
	}

	go node.Init()

	http.HandleFunc("/get", get)
	http.HandleFunc("/put", put)
	http.HandleFunc("/status", status)
	http.HandleFunc("/log", logHandler)
	http.HandleFunc("/events", events)
	http.HandleFunc("/simulate/block", simulateBlock)
	http.HandleFunc("/simulate/unblock", simulateUnblock)

	if *metrics {
		core.RegisterNodeMetrics(node)
		http.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			node.RefreshMetrics()
			promhttp.Handler().ServeHTTP(w, r)
		}))
	}

	log.Fatalf("%s: %v", *id, http.ListenAndServe(":"+*port, nil))
}

func parsePeers(peersStr string) map[string]string {
	res := map[string]string{}

	for _, pair := range strings.Split(peersStr, ",") {
		kv := strings.Split(pair, "=")
		res[kv[0]] = kv[1]
	}

	return res
}
