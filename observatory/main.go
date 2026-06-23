package main

import (
	"context"
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
)

//go:embed static/*
var staticFiles embed.FS

const defaultScenario = "observatory/scenarios/full-demo.json"

func main() {
	port := flag.Int("port", 8080, "Observatory UI port")
	noBrowser := flag.Bool("no-browser", false, "skip opening browser")
	noCompose := flag.Bool("no-compose", false, "do not start Prometheus/Grafana via Docker")
	bootstrap := flag.Bool("bootstrap", false, "auto-start cluster on launch (demo still waits for Start Demo)")
	noBootstrap := flag.Bool("no-bootstrap", true, "do not auto-start cluster on launch")
	binary := flag.String("binary", "", "path to ryanDB binary")
	demoPace := flag.Bool("demo", true, "compress scenario waits")
	scenarioFlag := flag.String("scenario", "", "scenario JSON path (default: steady-writes.json when bootstrapping)")
	flag.Parse()

	repoRoot := findRepoRoot()
	binaryPath, err := ensureBinary(repoRoot, *binary)
	if err != nil {
		log.Fatalf("binary: %v", err)
	}

	composeEnabled := !*noCompose
	srv := NewServer(binaryPath, repoRoot, composeEnabled)

	scenarioPath := *scenarioFlag
	if flag.NArg() >= 1 {
		scenarioPath = flag.Arg(0)
	}
	shouldBootstrap := *bootstrap || !*noBootstrap || scenarioPath != ""
	if !shouldBootstrap {
		log.Printf("observatory ready; click Start Demo to launch the cluster")
		srv.appendLog("observatory ready — click Start Demo")
	}

	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static files: %v", err)
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux, http.FileServer(http.FS(static)))

	addr := fmt.Sprintf(":%d", *port)
	url := fmt.Sprintf("http://localhost:%d", *port)

	server := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("observatory at %s", url)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	if !*noBrowser {
		openBrowser(url)
	}

	if composeEnabled {
		go func() {
			log.Println("starting monitoring stack (requires Docker Desktop)...")
			if err := startComposeStack(repoRoot); err != nil {
				log.Printf("monitoring error: %v", err)
				srv.appendLog("ERROR: monitoring: " + err.Error())
				return
			}
			log.Println("monitoring stack ready (Prometheus :9090)")
			srv.appendLog("monitoring stack ready")
		}()
	}

	if shouldBootstrap {
		bootstrapPath := scenarioPath
		if bootstrapPath == "" {
			bootstrapPath = defaultScenario
		}
		go func() {
			log.Printf("bootstrapping cluster with %s...", bootstrapPath)
			if err := srv.bootstrapCluster(bootstrapPath, *demoPace); err != nil {
				log.Printf("bootstrap error: %v", err)
				srv.appendLog("ERROR: bootstrap: " + err.Error())
				return
			}
			log.Println("cluster bootstrapped")
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("shutting down...")
	srv.cluster.StopAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	if composeEnabled {
		stopComposeStack(repoRoot)
	}
}
