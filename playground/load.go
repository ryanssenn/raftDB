package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

var loadHTTPClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        4096,
		MaxIdleConnsPerHost: 4096,
		MaxConnsPerHost:     4096,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
}

type LoadStats struct {
	Active      bool    `json:"active"`
	Concurrency int     `json:"concurrency"`
	SendRate    float64 `json:"sendRate"`
	SuccessRate float64 `json:"successRate"`
	Attempts    int64   `json:"attempts"`
	Success     int64   `json:"success"`
	Errors      int64   `json:"errors"`
}

func (srv *Server) loadStatsSnapshot() LoadStats {
	srv.mu.RLock()
	stats := srv.loadStats
	srv.mu.RUnlock()
	if stats == nil {
		return LoadStats{}
	}
	return stats.snapshot()
}

type loadTracker struct {
	concurrency int
	started     time.Time
	attempts    atomic.Int64
	success     atomic.Int64
	errors      atomic.Int64
	sendRate    atomic.Uint64
	successRate atomic.Uint64
}

func newLoadTracker(concurrency int) *loadTracker {
	return &loadTracker{
		concurrency: concurrency,
		started:     time.Now(),
	}
}

func (t *loadTracker) snapshot() LoadStats {
	return LoadStats{
		Active:      true,
		Concurrency: t.concurrency,
		SendRate:    math.Float64frombits(t.sendRate.Load()),
		SuccessRate: math.Float64frombits(t.successRate.Load()),
		Attempts:    t.attempts.Load(),
		Success:     t.success.Load(),
		Errors:      t.errors.Load(),
	}
}

func (srv *Server) runConcurrentLoad(step LoadStep) error {
	duration, err := time.ParseDuration(step.Duration)
	if err != nil {
		return err
	}
	concurrency := step.Concurrency
	if concurrency <= 0 {
		if step.Interval != "" {
			concurrency = 1
		} else {
			concurrency = 32
		}
	}
	prefix := step.KeyPrefix
	if prefix == "" {
		prefix = "tx"
	}

	ports := srv.runningNodePorts()
	if len(ports) == 0 {
		srv.appendLog("load skipped: no running nodes")
		return nil
	}

	tracker := newLoadTracker(concurrency)
	srv.mu.Lock()
	srv.loadStats = tracker
	srv.mu.Unlock()
	defer func() {
		srv.mu.Lock()
		srv.loadStats = nil
		srv.mu.Unlock()
	}()

	srv.appendLog(fmt.Sprintf("load %s with %d workers (prefix %s)", step.Duration, concurrency, prefix))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.watchLoadCancel(cancel)

	var wg sync.WaitGroup
	deadline := time.Now().Add(duration)

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			counter := 0
			for {
				if ctx.Err() != nil || time.Now().After(deadline) {
					return
				}
				if !srv.waitIfPaused() {
					return
				}
				counter++
				port := ports[(worker+counter)%len(ports)]
				key := fmt.Sprintf("%s:%d:%d", prefix, worker, counter)
				tracker.attempts.Add(1)
				result, err := loadPut(port, key, "v")
				if err != nil || result != "success" {
					tracker.errors.Add(1)
				} else {
					tracker.success.Add(1)
					atomic.AddInt64(&srv.writeCount, 1)
				}
			}
		}(w)
	}

	rateDone := make(chan struct{})
	go srv.reportLoadRates(tracker, rateDone)
	wg.Wait()
	close(rateDone)

	snap := tracker.snapshot()
	elapsed := time.Since(tracker.started).Seconds()
	avg := 0.0
	if elapsed > 0 {
		avg = float64(snap.Success) / elapsed
	}
	srv.appendLog(fmt.Sprintf("load done: %d ok, %d err, avg %.0f req/s", snap.Success, snap.Errors, avg))
	return nil
}

func (srv *Server) runningNodePorts() []string {
	var ports []string
	for _, node := range srv.cluster.Nodes {
		if node.Running {
			ports = append(ports, node.Port)
		}
	}
	return ports
}

func (srv *Server) watchLoadCancel(cancel context.CancelFunc) {
	for {
		srv.mu.RLock()
		stop := srv.scenarioStop
		srv.mu.RUnlock()
		if stop != nil {
			select {
			case <-stop:
				cancel()
				return
			case <-time.After(100 * time.Millisecond):
			}
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (srv *Server) reportLoadRates(tracker *loadTracker, done <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastAttempts, lastSuccess int64
	lastAt := time.Now()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			now := time.Now()
			elapsed := now.Sub(lastAt).Seconds()
			if elapsed <= 0 {
				continue
			}
			attempts := tracker.attempts.Load()
			success := tracker.success.Load()
			sendRate := float64(attempts-lastAttempts) / elapsed
			successRate := float64(success-lastSuccess) / elapsed
			tracker.sendRate.Store(math.Float64bits(sendRate))
			tracker.successRate.Store(math.Float64bits(successRate))
			lastAttempts = attempts
			lastSuccess = success
			lastAt = now
		}
	}
}

func loadPut(port, key, value string) (string, error) {
	params := url.Values{}
	params.Set("key", key)
	params.Set("value", value)
	params.Set("client", "loadgen")
	resp, err := loadHTTPClient.Get("http://127.0.0.1:" + port + "/put?" + params.Encode())
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

func formatRate(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.1fk req/s", v/1000)
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f req/s", v)
	}
	return fmt.Sprintf("%.1f req/s", v)
}

func formatRateShort(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.1fk", v/1000)
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

func (srv *Server) runSequentialLoad(step LoadStep) error {
	duration, err := time.ParseDuration(step.Duration)
	if err != nil {
		return err
	}
	interval, err := time.ParseDuration(step.Interval)
	if err != nil {
		return err
	}
	prefix := step.KeyPrefix
	if prefix == "" {
		prefix = "key"
	}
	nodeCount := len(srv.cluster.Nodes)
	srv.appendLog(fmt.Sprintf("load %s @ %s (prefix %s)", step.Duration, step.Interval, prefix))
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
}
