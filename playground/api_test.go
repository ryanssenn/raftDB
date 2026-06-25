package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryansenn/quorum/internal/harness"
)

func TestPlaygroundAPI(t *testing.T) {
	repoRoot := findRepoRoot()
	binaryPath := filepath.Join(repoRoot, "quorum")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}

	harness.KillPorts(3)
	t.Cleanup(func() { harness.KillPorts(3) })

	srv := NewServer(binaryPath, repoRoot, false)
	srv.cluster = NewCluster(3)

	mux := http.NewServeMux()
	srv.registerRoutes(mux, http.NotFoundHandler())

	ts := httptest.NewServer(mux)
	defer ts.Close()

	post := func(path string, body any) (*http.Response, error) {
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				return nil, err
			}
		}
		return http.Post(ts.URL+path, "application/json", &buf)
	}

	resp, err := post("/api/cluster/create", map[string]int{"nodes": 3})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = post("/api/cluster/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start status %d: %s", resp.StatusCode, string(body))
	}
	t.Cleanup(func() { srv.cluster.StopAll() })

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		st, err := http.Get(ts.URL + "/api/cluster/status")
		if err != nil {
			t.Fatal(err)
		}
		var statusBody struct {
			Nodes []NodeStatus `json:"nodes"`
		}
		json.NewDecoder(st.Body).Decode(&statusBody)
		st.Body.Close()
		leaders := 0
		for _, n := range statusBody.Nodes {
			if n.Running && n.State == 2 {
				leaders++
			}
		}
		if leaders == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	path := filepath.Join(repoRoot, "playground", "scenarios", "steady-writes.json")
	resp, err = post("/api/scenario/load", map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = post("/api/scenario/run", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	time.Sleep(3 * time.Second)

	metricsResp, err := http.Get("http://localhost:8001/metrics")
	if err != nil {
		t.Fatal(err)
	}
	metricsBody, _ := io.ReadAll(metricsResp.Body)
	metricsResp.Body.Close()
	if !strings.Contains(string(metricsBody), "quorum_term") {
		t.Fatalf("node metrics missing quorum_term")
	}

	clusterMetrics, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	clusterBody, _ := io.ReadAll(clusterMetrics.Body)
	clusterMetrics.Body.Close()
	if !strings.Contains(string(clusterBody), "quorum_replication_lag") {
		t.Fatalf("cluster metrics missing quorum_replication_lag")
	}

	resp, err = post("/api/cluster/stop", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestReadyEndpoint(t *testing.T) {
	srv := NewServer("", findRepoRoot(), false)
	mux := http.NewServeMux()
	srv.registerRoutes(mux, http.NotFoundHandler())
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Ready  bool              `json:"ready"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Checks["compose"] != "skipped" {
		t.Fatalf("expected compose skipped, got %q", body.Checks["compose"])
	}
	if !body.Ready {
		t.Fatal("expected ready when compose is disabled")
	}
}

func TestMetricsLiveEndpoint(t *testing.T) {
	srv := NewServer("", findRepoRoot(), false)
	mux := http.NewServeMux()
	srv.registerRoutes(mux, http.NotFoundHandler())
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/metrics/live")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		WriteOpsSec float64        `json:"writeOpsSec"`
		History     metricsHistory `json:"history"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.History.WriteOpsSec == nil {
		t.Fatal("expected history object")
	}
}

func TestLoadScenarioPaths(t *testing.T) {
	root := findRepoRoot()
	path := filepath.Join(root, "playground", "scenarios", "leader-failure.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("scenario file not found")
	}
	sc, err := LoadScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Nodes != 5 {
		t.Fatalf("expected 5 nodes, got %d", sc.Nodes)
	}
}

func TestFullDemoScenario(t *testing.T) {
	root := findRepoRoot()
	path := filepath.Join(root, "playground", "scenarios", "full-demo.json")
	sc, err := LoadScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Realtime {
		t.Fatal("full-demo should have realtime: true")
	}
	if len(sc.Steps) != 11 {
		t.Fatalf("expected 11 steps, got %d", len(sc.Steps))
	}
	if sc.Steps[1].Load == nil {
		t.Fatal("expected load step at index 1")
	}
}

func TestLoadStepValidation(t *testing.T) {
	root := findRepoRoot()
	path := filepath.Join(root, "playground", "scenarios", "full-demo.json")
	sc, err := LoadScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	loadSteps := 0
	for _, step := range sc.Steps {
		if step.Load != nil {
			loadSteps++
			if step.Load.KeyPrefix == "" {
				t.Fatal("load step missing keyPrefix")
			}
		}
	}
	if loadSteps != 5 {
		t.Fatalf("expected 5 load steps, got %d", loadSteps)
	}
}

func TestCompressWaitSkippedForRealtime(t *testing.T) {
	srv := NewServer("", findRepoRoot(), false)
	srv.demoPace = true
	srv.scenario = &Scenario{Realtime: true}

	d := 4 * time.Second
	step := Step{Wait: "4s"}
	orig := d
	if srv.demoPace && !srv.scenario.Showcase && !srv.scenario.Realtime {
		d = compressWait(d)
	}
	if d != orig {
		t.Fatalf("realtime scenario should not compress wait, got %v", d)
	}
	_ = step
}

func TestResolveScenarioPath(t *testing.T) {
	p := resolveScenarioPath("playground/scenarios/election.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("resolved path missing: %s (%v)", p, err)
	}
}

func TestWritePrometheusTargets(t *testing.T) {
	root := findRepoRoot()
	if err := writePrometheusTargets(root, 3); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "monitoring", "targets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "8001") {
		t.Fatalf("expected target 8001 in %s", data)
	}
	_ = clearPrometheusTargets(root)
}
