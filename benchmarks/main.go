// Command benchmarks runs a load-test suite against a live Quorum cluster and
// emits CSV/JSON results that the accompanying plot.py script turns into graphs.
//
// It is intentionally self-contained: it builds the quorum binary, launches a
// real multi-node cluster over HTTP/gRPC (the same way the integration tests
// do), drives it with closed-loop client workers, and records latency samples
// per request so percentiles are computed from raw data (not pre-averaged).
//
// Usage:
//
//	go run ./benchmarks                 # full suite, results in benchmarks/results/
//	go run ./benchmarks --quick         # shorter durations for a smoke run
//
// See benchmarks/README.md for details.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ryansenn/quorum/internal/harness"
)

// ---------------------------------------------------------------------------
// HTTP client (keep-alive, generous connection pool for concurrency sweeps)
// ---------------------------------------------------------------------------

var client = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        4096,
		MaxIdleConnsPerHost: 4096,
		MaxConnsPerHost:     4096,
		IdleConnTimeout:     90 * time.Second,
	},
}

func httpGet(url string) (string, error) {
	resp, err := client.Get(url)
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

func doPut(port int, key, value string) (string, error) {
	return httpGet(fmt.Sprintf("http://127.0.0.1:%d/put?key=%s&value=%s", port, key, value))
}

func doGet(port int, key string) (string, error) {
	return httpGet(fmt.Sprintf("http://127.0.0.1:%d/get?key=%s", port, key))
}

type status struct {
	Id    string `json:"id"`
	State int    `json:"state"` // 0=follower, 1=candidate, 2=leader
	Term  int64  `json:"term"`
}

func getStatus(port int) (*status, error) {
	body, err := httpGet(fmt.Sprintf("http://127.0.0.1:%d/status", port))
	if err != nil {
		return nil, err
	}
	var s status
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ---------------------------------------------------------------------------
// Cluster management
// ---------------------------------------------------------------------------

type proc struct {
	id       string
	httpPort int
	cmd      *exec.Cmd
	running  bool
}

type cluster struct {
	size     int
	procs    []*proc
	binary   string
	peers    string
	logDir   string
	noEvents bool
}

func newCluster(size int, binary, logDir string, noEvents bool) *cluster {
	c := &cluster{size: size, binary: binary, logDir: logDir, peers: harness.BuildPeers(size), noEvents: noEvents}
	for i := 0; i < size; i++ {
		c.procs = append(c.procs, &proc{
			id:       fmt.Sprintf("node%d", i+1),
			httpPort: 8001 + i,
		})
	}
	return c
}

func (c *cluster) startProc(p *proc, reset string) error {
	args := []string{
		"--id=" + p.id,
		"--port=" + strconv.Itoa(p.httpPort),
		"--peers=" + c.peers,
		"--reset=" + reset,
	}
	if c.noEvents {
		args = append(args, "--no-events=true")
	}
	cmd := exec.Command(c.binary, args...)
	logFile, err := os.Create(filepath.Join(c.logDir, p.id+".log"))
	if err != nil {
		return err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	p.running = true
	return waitReady(p, 30*time.Second)
}

func (c *cluster) start(reset string) error {
	harness.KillPorts(c.size)
	for _, p := range c.procs {
		if err := c.startProc(p, reset); err != nil {
			return fmt.Errorf("start %s: %w", p.id, err)
		}
	}
	if _, err := c.waitLeader(20 * time.Second); err != nil {
		return err
	}
	return nil
}

func (p *proc) stop() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		p.cmd = nil
	}
	p.running = false
}

func (c *cluster) stop() {
	for _, p := range c.procs {
		p.stop()
	}
}

func waitReady(p *proc, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := getStatus(p.httpPort); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s not ready after %s", p.id, timeout)
}

// leader returns the single elected leader, or an error if there is not exactly one.
func (c *cluster) leader() (*proc, error) {
	var leader *proc
	count := 0
	for _, p := range c.procs {
		if !p.running {
			continue
		}
		s, err := getStatus(p.httpPort)
		if err != nil {
			continue
		}
		if s.State == 2 {
			leader = p
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("expected 1 leader, found %d", count)
	}
	return leader, nil
}

func (c *cluster) waitLeader(timeout time.Duration) (*proc, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l, err := c.leader(); err == nil {
			return l, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("no single leader after %s", timeout)
}

func (c *cluster) followers(leader *proc) []*proc {
	var fs []*proc
	for _, p := range c.procs {
		if p.running && p != leader {
			fs = append(fs, p)
		}
	}
	return fs
}

// ---------------------------------------------------------------------------
// Load generation (closed-loop)
// ---------------------------------------------------------------------------

type benchResult struct {
	Experiment  string  `json:"experiment"`
	Op          string  `json:"op"`
	ClusterSize int     `json:"cluster_size"`
	Concurrency int     `json:"concurrency"`
	Target      string  `json:"target"` // leader | follower
	DurationSec float64 `json:"duration_sec"`
	Count       int     `json:"count"`
	Errors      int     `json:"errors"`
	Throughput  float64 `json:"throughput_ops_sec"`
	MeanMs      float64 `json:"mean_ms"`
	P50Ms       float64 `json:"p50_ms"`
	P95Ms       float64 `json:"p95_ms"`
	P99Ms       float64 `json:"p99_ms"`
	MaxMs       float64 `json:"max_ms"`
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// runLoad drives `concurrency` closed-loop workers against targetPort for `dur`,
// issuing `op` ("put" or "get"). For reads, keys are drawn from readKeys.
func runLoad(experiment, op string, clusterSize, concurrency int, target string, targetPort int, dur time.Duration, readKeys []string) benchResult {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		allLat   []float64
		errCount int64
	)
	deadline := time.Now().Add(dur)
	start := time.Now()

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			lat := make([]float64, 0, 4096)
			counter := 0
			for time.Now().Before(deadline) {
				counter++
				t0 := time.Now()
				var resp string
				var err error
				if op == "put" {
					key := fmt.Sprintf("k_%d_%d", worker, counter)
					resp, err = doPut(targetPort, key, "v")
				} else {
					key := readKeys[(worker+counter)%len(readKeys)]
					resp, err = doGet(targetPort, key)
				}
				elapsed := float64(time.Since(t0).Microseconds()) / 1000.0
				if err != nil || isErrResp(op, resp) {
					atomic.AddInt64(&errCount, 1)
					continue
				}
				lat = append(lat, elapsed)
			}
			mu.Lock()
			allLat = append(allLat, lat...)
			mu.Unlock()
		}(w)
	}
	wg.Wait()
	elapsedSec := time.Since(start).Seconds()

	sort.Float64s(allLat)
	sum := 0.0
	for _, v := range allLat {
		sum += v
	}
	mean := 0.0
	if len(allLat) > 0 {
		mean = sum / float64(len(allLat))
	}

	return benchResult{
		Experiment:  experiment,
		Op:          op,
		ClusterSize: clusterSize,
		Concurrency: concurrency,
		Target:      target,
		DurationSec: round(elapsedSec, 3),
		Count:       len(allLat),
		Errors:      int(errCount),
		Throughput:  round(float64(len(allLat))/elapsedSec, 1),
		MeanMs:      round(mean, 3),
		P50Ms:       round(percentile(allLat, 50), 3),
		P95Ms:       round(percentile(allLat, 95), 3),
		P99Ms:       round(percentile(allLat, 99), 3),
		MaxMs:       round(percentile(allLat, 100), 3),
	}
}

func isErrResp(op, resp string) bool {
	if op == "put" {
		return resp != "success"
	}
	return resp == "no leader elected yet" || resp == "leader not accessible" || strings.HasPrefix(resp, "Error:")
}

func round(v float64, places int) float64 {
	f := math.Pow(10, float64(places))
	return math.Round(v*f) / f
}

// preload writes n keys via the leader and returns them, so read benchmarks hit
// keys that actually exist.
func preload(leaderPort, n int) ([]string, error) {
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("pre_%d", i)
		resp, err := doPut(leaderPort, key, "v")
		if err != nil {
			return nil, err
		}
		if resp != "success" {
			return nil, fmt.Errorf("preload put failed: %q", resp)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// ---------------------------------------------------------------------------
// Failover (availability) experiment
// ---------------------------------------------------------------------------

type failoverResult struct {
	Trial          int     `json:"trial"`
	OldLeader      string  `json:"old_leader"`
	NewLeader      string  `json:"new_leader"`
	RecoveryMs     float64 `json:"recovery_ms"`
	WritesViaProbe string  `json:"probe_node"`
}

// measureFailover kills the current leader and times how long until a write
// (sent to a surviving follower, which forwards to the new leader) succeeds again.
func measureFailover(c *cluster, trial int) (failoverResult, error) {
	leader, err := c.waitLeader(15 * time.Second)
	if err != nil {
		return failoverResult{}, err
	}
	followers := c.followers(leader)
	if len(followers) == 0 {
		return failoverResult{}, fmt.Errorf("no follower to probe")
	}
	probe := followers[0]

	// Warm up: confirm the probe path works before we break things.
	if resp, err := doPut(probe.httpPort, fmt.Sprintf("warm_%d", trial), "v"); err != nil || resp != "success" {
		return failoverResult{}, fmt.Errorf("warmup write failed: resp=%q err=%v", resp, err)
	}

	t0 := time.Now()
	leader.stop()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := doPut(probe.httpPort, fmt.Sprintf("fo_%d_%d", trial, time.Now().UnixNano()), "v")
		if err == nil && resp == "success" {
			recovery := float64(time.Since(t0).Microseconds()) / 1000.0
			newLeader, _ := c.leader()
			newLeaderID := "unknown"
			if newLeader != nil {
				newLeaderID = newLeader.id
			}
			return failoverResult{
				Trial:          trial,
				OldLeader:      leader.id,
				NewLeader:      newLeaderID,
				RecoveryMs:     round(recovery, 1),
				WritesViaProbe: probe.id,
			}, nil
		}
		time.Sleep(15 * time.Millisecond)
	}
	return failoverResult{}, fmt.Errorf("cluster did not recover writes within deadline")
}

// ---------------------------------------------------------------------------
// Suite orchestration
// ---------------------------------------------------------------------------

func main() {
	quick := flag.Bool("quick", false, "shorter durations for a smoke run")
	durFlag := flag.Duration("dur", 5*time.Second, "duration per concurrency point")
	concStr := flag.String("concurrency", "1,4,8,16,32,64", "comma-separated concurrency levels")
	preloadN := flag.Int("preload", 2000, "number of keys to preload for read tests")
	outDir := flag.String("out", "", "output directory (default benchmarks/results)")
	noEvents := flag.Bool("no-events", false, "pass --no-events to quorum nodes")
	flag.Parse()

	dur := *durFlag
	if *quick {
		dur = 2 * time.Second
		*preloadN = 500
	}

	concLevels := parseInts(*concStr)

	repoRoot, err := findRepoRoot()
	if err != nil {
		log.Fatalf("locate repo root: %v", err)
	}
	binary := filepath.Join(repoRoot, "quorum")
	resultsDir := *outDir
	if resultsDir == "" {
		resultsDir = filepath.Join(repoRoot, "benchmarks", "results")
	}
	logDir := filepath.Join(resultsDir, "node-logs")
	mustMkdir(resultsDir)
	mustMkdir(logDir)

	log.Printf("building quorum binary...")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		log.Fatalf("build failed: %v\n%s", err, out)
	}

	var results []benchResult
	var failovers []failoverResult

	// ---- Experiment 1 & 2: write/read concurrency sweep on a 3-node cluster ----
	log.Printf("=== concurrency sweep (3-node cluster) ===")
	c3 := newCluster(3, binary, logDir, *noEvents)
	if err := c3.start("true"); err != nil {
		log.Fatalf("start 3-node cluster: %v", err)
	}
	leader, err := c3.waitLeader(15 * time.Second)
	if err != nil {
		c3.stop()
		log.Fatalf("no leader: %v", err)
	}
	log.Printf("leader is %s; preloading %d keys for reads...", leader.id, *preloadN)
	readKeys, err := preload(leader.httpPort, *preloadN)
	if err != nil {
		c3.stop()
		log.Fatalf("preload: %v", err)
	}

	for _, conc := range concLevels {
		log.Printf("  write sweep: concurrency=%d", conc)
		r := runLoad("write_sweep", "put", 3, conc, "leader", leader.httpPort, dur, nil)
		logResult(r)
		results = append(results, r)
	}
	for _, conc := range concLevels {
		log.Printf("  read sweep: concurrency=%d", conc)
		r := runLoad("read_sweep", "get", 3, conc, "leader", leader.httpPort, dur, readKeys)
		logResult(r)
		results = append(results, r)
	}

	// ---- Experiment 3: routing (leader-direct vs follower-forwarded writes) ----
	log.Printf("=== routing comparison (3-node, concurrency=16) ===")
	if l, err := c3.leader(); err == nil {
		rLeader := runLoad("routing", "put", 3, 16, "leader", l.httpPort, dur, nil)
		logResult(rLeader)
		results = append(results, rLeader)
		fs := c3.followers(l)
		if len(fs) > 0 {
			rFollower := runLoad("routing", "put", 3, 16, "follower", fs[0].httpPort, dur, nil)
			logResult(rFollower)
			results = append(results, rFollower)
		}
	}
	c3.stop()

	// ---- Experiment 4: cluster-size impact on write performance ----
	log.Printf("=== cluster size impact (write, concurrency=16) ===")
	for _, size := range []int{3, 5} {
		c := newCluster(size, binary, logDir, *noEvents)
		if err := c.start("true"); err != nil {
			log.Fatalf("start %d-node cluster: %v", size, err)
		}
		l, err := c.waitLeader(15 * time.Second)
		if err != nil {
			c.stop()
			log.Fatalf("no leader for size %d: %v", size, err)
		}
		log.Printf("  size=%d leader=%s", size, l.id)
		r := runLoad("cluster_size", "put", size, 16, "leader", l.httpPort, dur, nil)
		logResult(r)
		results = append(results, r)
		c.stop()
	}

	// ---- Experiment 5: failover / availability ----
	log.Printf("=== failover recovery (3-node) ===")
	cf := newCluster(3, binary, logDir, *noEvents)
	if err := cf.start("true"); err != nil {
		log.Fatalf("start failover cluster: %v", err)
	}
	for trial := 1; trial <= 3; trial++ {
		fr, err := measureFailover(cf, trial)
		if err != nil {
			log.Printf("  trial %d failed: %v", trial, err)
			break
		}
		log.Printf("  trial %d: %s died -> %s elected, writes recovered in %.1f ms",
			trial, fr.OldLeader, fr.NewLeader, fr.RecoveryMs)
		failovers = append(failovers, fr)

		// Restart the node we killed (keep its logs) so the next trial has a full cluster.
		var killed *proc
		for _, p := range cf.procs {
			if p.id == fr.OldLeader {
				killed = p
			}
		}
		if killed != nil {
			if err := cf.startProc(killed, "false"); err != nil {
				log.Printf("  could not restart %s: %v", killed.id, err)
				break
			}
		}
		if _, err := cf.waitLeader(15 * time.Second); err != nil {
			log.Printf("  cluster did not stabilize before next trial: %v", err)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	cf.stop()

	// ---- Write outputs ----
	writeResultsCSV(filepath.Join(resultsDir, "results.csv"), results)
	writeFailoverCSV(filepath.Join(resultsDir, "failover.csv"), failovers)
	writeJSON(filepath.Join(resultsDir, "results.json"), map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"load":         results,
		"failover":     failovers,
	})
	log.Printf("done. results written to %s", resultsDir)
}

// ---------------------------------------------------------------------------
// Output helpers
// ---------------------------------------------------------------------------

func logResult(r benchResult) {
	log.Printf("    -> %s/%s size=%d conc=%d target=%s: %.0f ops/s, p50=%.2fms p95=%.2fms p99=%.2fms (errors=%d)",
		r.Experiment, r.Op, r.ClusterSize, r.Concurrency, r.Target, r.Throughput, r.P50Ms, r.P95Ms, r.P99Ms, r.Errors)
}

func writeResultsCSV(path string, results []benchResult) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	fmt.Fprintln(f, "experiment,op,cluster_size,concurrency,target,duration_sec,count,errors,throughput_ops_sec,mean_ms,p50_ms,p95_ms,p99_ms,max_ms")
	for _, r := range results {
		fmt.Fprintf(f, "%s,%s,%d,%d,%s,%.3f,%d,%d,%.1f,%.3f,%.3f,%.3f,%.3f,%.3f\n",
			r.Experiment, r.Op, r.ClusterSize, r.Concurrency, r.Target, r.DurationSec,
			r.Count, r.Errors, r.Throughput, r.MeanMs, r.P50Ms, r.P95Ms, r.P99Ms, r.MaxMs)
	}
}

func writeFailoverCSV(path string, results []failoverResult) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	fmt.Fprintln(f, "trial,old_leader,new_leader,recovery_ms,probe_node")
	for _, r := range results {
		fmt.Fprintf(f, "%d,%s,%s,%.1f,%s\n", r.Trial, r.OldLeader, r.NewLeader, r.RecoveryMs, r.WritesViaProbe)
	}
}

func writeJSON(path string, v any) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Fatalf("encode json: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------------

func parseInts(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			log.Fatalf("invalid integer %q: %v", part, err)
		}
		out = append(out, n)
	}
	return out
}

func mustMkdir(path string) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", path, err)
	}
}

// findRepoRoot walks up from the current working directory looking for go.mod.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
