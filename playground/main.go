package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

const defaultScenario = "playground/scenarios/full-demo.json"

func main() {
	port := flag.Int("port", 8080, "UI port")
	noBrowser := flag.Bool("no-browser", false, "skip opening browser")
	noCompose := flag.Bool("no-compose", false, "do not start Prometheus/Grafana via Docker")
	keepMonitoring := flag.Bool("keep-monitoring", false, "leave Prometheus/Grafana running after exit")
	bootstrap := flag.Bool("bootstrap", false, "auto-start cluster on launch (stress test still waits for Run stress test)")
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

	if err := portAvailable(*port); err != nil {
		log.Fatalf("%v", err)
	}

	composeEnabled := !*noCompose
	srv := NewServer(binaryPath, repoRoot, composeEnabled)

	scenarioPath := *scenarioFlag
	if flag.NArg() >= 1 {
		scenarioPath = flag.Arg(0)
	}
	shouldBootstrap := *bootstrap || !*noBootstrap || scenarioPath != ""
	if !shouldBootstrap {
		log.Printf("ready; click Run stress test")
		srv.appendLog("ready; click Run stress test")
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
		log.Printf("UI at %s", url)
		log.Printf("exit cleanly: Ctrl+C here, or Stop & quit in the UI")
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
			log.Println("monitoring stack ready (Prometheus :9090, Grafana :3000)")
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
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	select {
	case <-sig:
	case <-srv.ShutdownRequested():
	}

	log.Println("shutting down...")
	srv.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	if composeEnabled && !*keepMonitoring {
		stopComposeStack(repoRoot)
	}
	log.Println("playground stopped")
}

func portAvailable(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf(
			"port %d already in use — another playground may still be running (Ctrl+C in its terminal, or: lsof -ti :%d | xargs kill)",
			port, port,
		)
	}
	ln.Close()
	return nil
}
