package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"
)

const composeWaitTimeout = 120 * time.Second

func checkDocker() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Docker is not running — start Docker Desktop and retry\n%s", out)
	}
	return nil
}

func startComposeStack(repoRoot string) error {
	if err := checkDocker(); err != nil {
		return err
	}

	log.Println("pulling/starting Prometheus + Grafana (first run may take a minute)...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", "monitoring/docker-compose.yml", "up", "-d")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up: %w\n%s", err, out)
	}
	return waitForMonitoringStack()
}

func waitForMonitoringStack() error {
	deadline := time.Now().Add(composeWaitTimeout)
	promOK := false
	grafOK := false
	lastLog := time.Time{}
	for time.Now().Before(deadline) {
		if !promOK {
			promOK = prometheusReady()
		}
		if !grafOK {
			grafOK = grafanaReady()
		}
		if promOK && grafOK {
			return nil
		}
		if time.Since(lastLog) >= 5*time.Second {
			log.Printf("waiting for monitoring stack... prometheus=%t grafana=%t", promOK, grafOK)
			lastLog = time.Now()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("monitoring stack did not become ready within %s", composeWaitTimeout)
}

func grafanaReady() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:3000/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func prometheusReady() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:9090/-/ready")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func stopComposeStack(repoRoot string) {
	cmd := exec.Command("docker", "compose", "-f", "monitoring/docker-compose.yml", "down")
	cmd.Dir = repoRoot
	_ = cmd.Run()
}
