package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"github.com/ryansenn/ryanDB/internal/harness"
)

//go:embed static/*
var staticFiles embed.FS

func main() {
	port := flag.Int("port", 8080, "UI port")
	noBrowser := flag.Bool("no-browser", false, "skip opening browser")
	binary := flag.String("binary", "", "path to ryanDB binary")
	demoPace := flag.Bool("demo", true, "compress scenario waits for presentation pacing")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: go run ./visualizer [flags] <scenario.json>\n")
		os.Exit(1)
	}

	scenario, err := LoadScenario(flag.Arg(0))
	if err != nil {
		log.Fatalf("load scenario: %v", err)
	}

	repoRoot := findRepoRoot()
	binaryPath, err := ensureBinary(repoRoot, *binary)
	if err != nil {
		log.Fatalf("binary: %v", err)
	}

	showcaseMode := scenario.Showcase
	if showcaseMode {
		*demoPace = false
	}

	harness.KillPorts(scenario.Nodes)
	cluster := NewCluster(scenario.Nodes)

	log.Printf("starting %d-node cluster...", scenario.Nodes)
	srv := &Server{
		cluster:    cluster,
		scenario:   scenario,
		binaryPath: binaryPath,
		demoPace:   *demoPace,
		eventSince: map[string]int64{},
	}

	if showcaseMode {
		srv.showcaseStart = time.Now()
		if err := cluster.StartStaggered(binaryPath, true, 500*time.Millisecond); err != nil {
			log.Fatalf("start cluster: %v", err)
		}
		log.Printf("showcase mode — staged boot, scenario begins immediately")
	} else {
		if err := cluster.StartAll(binaryPath, true); err != nil {
			log.Fatalf("start cluster: %v", err)
		}
		log.Printf("leader elected, running scenario: %s", scenario.Name)
	}
	srv.appendLog("cluster started (" + fmt.Sprintf("%d", scenario.Nodes) + " nodes)")
	srv.appendLog("running scenario: " + scenario.Name)
	go srv.runScenario()

	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static files: %v", err)
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux, http.FileServer(http.FS(static)))

	addr := fmt.Sprintf(":%d", *port)
	url := "http://localhost" + addr

	go func() {
		log.Printf("visualizer UI at %s", url)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	if !*noBrowser {
		openBrowser(url)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("shutting down...")
	cluster.StopAll()
}
