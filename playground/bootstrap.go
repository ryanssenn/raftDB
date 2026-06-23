package main

import (
	"fmt"
	"log"

	"github.com/ryansenn/ryanDB/internal/harness"
)

func (srv *Server) bootstrapCluster(scenarioPath string, demoPace bool) error {
	scenario, err := LoadScenario(resolveScenarioPath(scenarioPath))
	if err != nil {
		return fmt.Errorf("load scenario: %w", err)
	}

	srv.mu.Lock()
	srv.scenario = scenario
	srv.demoPace = demoPace
	if scenario.Showcase || scenario.Realtime {
		srv.demoPace = false
	}
	srv.mu.Unlock()

	harness.KillPorts(scenario.Nodes)
	srv.mu.Lock()
	srv.cluster = NewCluster(scenario.Nodes)
	srv.mu.Unlock()
	_ = writePrometheusTargets(srv.repoRoot, scenario.Nodes)

	log.Printf("starting %d-node cluster...", scenario.Nodes)
	if err := srv.cluster.StartAll(srv.binaryPath, true); err != nil {
		return fmt.Errorf("start cluster: %w", err)
	}

	srv.mu.Lock()
	srv.clusterStarted = true
	srv.appendLog(fmt.Sprintf("cluster started (%d nodes)", scenario.Nodes))
	srv.appendLog("scenario loaded: " + scenario.Name + " (click Run stress test)")
	srv.mu.Unlock()

	return nil
}
