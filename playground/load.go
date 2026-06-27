package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
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
	Active          bool    `json:"active"`
	Concurrency     int     `json:"concurrency"`
	ReadConcurrency int     `json:"readConcurrency"`
	SendRate        float64 `json:"sendRate"`
	SuccessRate     float64 `json:"successRate"`
	ReadSendRate    float64 `json:"readSendRate"`
	ReadSuccessRate float64 `json:"readSuccessRate"`
	WriteP50Ms      float64 `json:"writeP50Ms"`
	WriteP99Ms      float64 `json:"writeP99Ms"`
	ReadP99Ms       float64 `json:"readP99Ms"`
	Attempts        int64   `json:"attempts"`
	Success         int64   `json:"success"`
	Errors          int64   `json:"errors"`
	ReadAttempts    int64   `json:"readAttempts"`
	ReadSuccess     int64   `json:"readSuccess"`
	ReadErrors      int64   `json:"readErrors"`
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

const latencySampleCap = 512

type latencySamples struct {
	mu      sync.Mutex
	samples []float64
}

func (l *latencySamples) record(d time.Duration) {
	ms := float64(d.Microseconds()) / 1000.0
	l.mu.Lock()
	l.samples = append(l.samples, ms)
	if len(l.samples) > latencySampleCap {
		l.samples = l.samples[len(l.samples)-latencySampleCap:]
	}
	l.mu.Unlock()
}

func (l *latencySamples) percentile(p float64) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) == 0 {
		return 0
	}
	sorted := append([]float64(nil), l.samples...)
	sort.Float64s(sorted)
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (l *latencySamples) p50() float64 { return l.percentile(0.50) }

func (l *latencySamples) p99() float64 { return l.percentile(0.99) }

type loadTracker struct {
	concurrency     int
	readConcurrency int
	started         time.Time
	attempts        atomic.Int64
	success         atomic.Int64
	errors          atomic.Int64
	readAttempts    atomic.Int64
	readSuccess     atomic.Int64
	readErrors      atomic.Int64
	sendRate        atomic.Uint64
	successRate     atomic.Uint64
	readSendRate    atomic.Uint64
	readSuccessRate atomic.Uint64
	writeLatency    latencySamples
	readLatency     latencySamples
}

func newLoadTracker(concurrency, readConcurrency int) *loadTracker {
	return &loadTracker{
		concurrency:     concurrency,
		readConcurrency: readConcurrency,
		started:         time.Now(),
	}
}

func (t *loadTracker) snapshot() LoadStats {
	return LoadStats{
		Active:          true,
		Concurrency:     t.concurrency,
		ReadConcurrency: t.readConcurrency,
		SendRate:        math.Float64frombits(t.sendRate.Load()),
		SuccessRate:     math.Float64frombits(t.successRate.Load()),
		ReadSendRate:    math.Float64frombits(t.readSendRate.Load()),
		ReadSuccessRate: math.Float64frombits(t.readSuccessRate.Load()),
		WriteP50Ms:      t.writeLatency.p50(),
		WriteP99Ms:      t.writeLatency.p99(),
		ReadP99Ms:       t.readLatency.p99(),
		Attempts:        t.attempts.Load(),
		Success:         t.success.Load(),
		Errors:          t.errors.Load(),
		ReadAttempts:    t.readAttempts.Load(),
		ReadSuccess:     t.readSuccess.Load(),
		ReadErrors:      t.readErrors.Load(),
	}
}

func parseLoadDuration(s string) (deadline time.Time, infinite bool, err error) {
	switch s {
	case "", "forever", "0", "infinite":
		return time.Time{}, true, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, false, err
	}
	return time.Now().Add(d), false, nil
}

func rateDelay(rps, workers int) time.Duration {
	if rps <= 0 || workers <= 0 {
		return 0
	}
	perWorker := float64(rps) / float64(workers)
	if perWorker <= 0 {
		return 0
	}
	return time.Duration(float64(time.Second) / perWorker)
}

func stressWorkerCount(rps, min, max int) int {
	if rps <= 0 {
		return min
	}
	n := rps / 50
	if n < min {
		n = min
	}
	if n > max {
		n = max
	}
	return n
}

func (srv *Server) runConcurrentLoad(step LoadStep) error {
	deadline, infinite, err := parseLoadDuration(step.Duration)
	if err != nil {
		return err
	}

	concurrency := step.Concurrency
	if concurrency <= 0 {
		concurrency = stressWorkerCount(step.WriteRPS, 16, 64)
		if step.Interval != "" && concurrency > 1 {
			concurrency = 1
		}
	}
	readConcurrency := step.ReadConcurrency
	if readConcurrency <= 0 {
		if step.ReadRPS > 0 {
			readConcurrency = stressWorkerCount(step.ReadRPS, 8, 32)
		} else {
			readConcurrency = concurrency / 2
			if readConcurrency < 8 {
				readConcurrency = 8
			}
		}
	}
	if step.ReadRPS <= 0 {
		readConcurrency = 0
	}

	writeDelay := rateDelay(step.WriteRPS, concurrency)
	readDelay := rateDelay(step.ReadRPS, readConcurrency)

	prefix := step.KeyPrefix
	if prefix == "" {
		prefix = "tx"
	}

	ports := srv.runningNodePorts()
	if len(ports) == 0 {
		srv.appendLog("load skipped: no running nodes")
		return nil
	}

	tracker := newLoadTracker(concurrency, readConcurrency)
	srv.mu.Lock()
	srv.loadStats = tracker
	srv.mu.Unlock()
	defer func() {
		srv.mu.Lock()
		srv.loadStats = nil
		srv.mu.Unlock()
	}()

	loadLabel := step.Duration
	if infinite {
		loadLabel = "continuous"
	}
	rateLabel := ""
	if step.WriteRPS > 0 || step.ReadRPS > 0 {
		rateLabel = fmt.Sprintf(" target %d write/s %d read/s", step.WriteRPS, step.ReadRPS)
	}
	srv.appendLog(fmt.Sprintf("load %s with %d write + %d read workers%s",
		loadLabel, concurrency, readConcurrency, rateLabel))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.watchLoadCancel(cancel)

	var wg sync.WaitGroup
	written := make([]atomic.Int64, concurrency)

	loadDone := func() bool {
		if ctx.Err() != nil {
			return true
		}
		return !infinite && time.Now().After(deadline)
	}

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			counter := 0
			for {
				if loadDone() {
					return
				}
				if !srv.waitIfPaused() {
					return
				}
				counter++
				port := ports[(worker+counter)%len(ports)]
				key := fmt.Sprintf("%s:%d:%d", prefix, worker, counter)
				tracker.attempts.Add(1)
				start := time.Now()
				result, err := loadPut(port, key, "v")
				elapsed := time.Since(start)
				if err != nil || result != "success" {
					tracker.errors.Add(1)
				} else {
					tracker.success.Add(1)
					tracker.writeLatency.record(elapsed)
					written[worker].Store(int64(counter))
					atomic.AddInt64(&srv.writeCount, 1)
				}
				if writeDelay > 0 {
					if sleep := writeDelay - time.Since(start); sleep > 0 {
						time.Sleep(sleep)
					}
				}
			}
		}(w)
	}

	for w := 0; w < readConcurrency; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			counter := 0
			for {
				if loadDone() {
					return
				}
				if !srv.waitIfPaused() {
					return
				}
				counter++
				writeWorker := counter % concurrency
				maxKey := written[writeWorker].Load()
				if maxKey < 1 {
					time.Sleep(2 * time.Millisecond)
					continue
				}
				readKey := counter % int(maxKey)
				if readKey == 0 {
					readKey = 1
				}
				port := ports[(worker+counter)%len(ports)]
				key := fmt.Sprintf("%s:%d:%d", prefix, writeWorker, readKey)
				tracker.readAttempts.Add(1)
				start := time.Now()
				result, err := loadGet(port, key)
				elapsed := time.Since(start)
				if err != nil || result == "" || result == "key not found" {
					tracker.readErrors.Add(1)
				} else {
					tracker.readSuccess.Add(1)
					tracker.readLatency.record(elapsed)
				}
				if readDelay > 0 {
					if sleep := readDelay - time.Since(start); sleep > 0 {
						time.Sleep(sleep)
					}
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
	avgWrite := 0.0
	avgRead := 0.0
	if elapsed > 0 {
		avgWrite = float64(snap.Success) / elapsed
		avgRead = float64(snap.ReadSuccess) / elapsed
	}
	srv.appendLog(fmt.Sprintf("load done: %d write ok, %d read ok, avg %.0f w/s %.0f r/s, w p99 %.1fms",
		snap.Success, snap.ReadSuccess, avgWrite, avgRead, snap.WriteP99Ms))
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
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var lastAttempts, lastSuccess, lastReadAttempts, lastReadSuccess int64
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
			readAttempts := tracker.readAttempts.Load()
			readSuccess := tracker.readSuccess.Load()
			tracker.sendRate.Store(math.Float64bits(float64(attempts-lastAttempts) / elapsed))
			tracker.successRate.Store(math.Float64bits(float64(success-lastSuccess) / elapsed))
			tracker.readSendRate.Store(math.Float64bits(float64(readAttempts-lastReadAttempts) / elapsed))
			tracker.readSuccessRate.Store(math.Float64bits(float64(readSuccess-lastReadSuccess) / elapsed))
			lastAttempts = attempts
			lastSuccess = success
			lastReadAttempts = readAttempts
			lastReadSuccess = readSuccess
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

func loadGet(port, key string) (string, error) {
	params := url.Values{}
	params.Set("key", key)
	params.Set("client", "loadgen")
	resp, err := loadHTTPClient.Get("http://127.0.0.1:" + port + "/get?" + params.Encode())
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
