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
	"testing"
	"time"

	"github.com/ryansenn/ryanDB/internal/harness"
)

func TestControlAPI(t *testing.T) {
	repoRoot := findRepoRoot()
	binaryPath := filepath.Join(repoRoot, "ryanDB")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}
	binary := binaryPath

	harness.KillPorts(3)
	t.Cleanup(func() { harness.KillPorts(3) })

	srv := NewServer(binary, true)
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status %d", resp.StatusCode)
	}

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
		var body struct {
			Nodes []NodeStatus `json:"nodes"`
		}
		json.NewDecoder(st.Body).Decode(&body)
		st.Body.Close()
		leaders := 0
		for _, n := range body.Nodes {
			if n.Running && n.State == 2 {
				leaders++
			}
		}
		if leaders == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	resp, err = post("/api/request", map[string]string{
		"client": "test",
		"op":     "put",
		"key":    "api-test",
		"value":  "ok",
		"node":   "node1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var reqResult struct {
		Result string `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&reqResult)
	resp.Body.Close()
	if reqResult.Result != "success" {
		t.Fatalf("put result %q", reqResult.Result)
	}

	resp, err = post("/api/cluster/nodes/node2/kill", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("kill status %d", resp.StatusCode)
	}

	resp, err = post("/api/cluster/nodes/node2/restart", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restart status %d", resp.StatusCode)
	}
	time.Sleep(500 * time.Millisecond)

	resp, err = post("/api/cluster/partition", map[string][]string{
		"isolated": {"node1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	partBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("partition status %d: %s", resp.StatusCode, string(partBody))
	}

	resp, err = post("/api/cluster/partition/clear", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear partition status %d", resp.StatusCode)
	}

	resp, err = post("/api/cluster/stop", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestLoadScenarioPaths(t *testing.T) {
	root := findRepoRoot()
	path := filepath.Join(root, "visualizer", "scenarios", "election.json")
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

func TestResolveScenarioPath(t *testing.T) {
	p := resolveScenarioPath("visualizer/scenarios/election.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("resolved path missing: %s (%v)", p, err)
	}
}
