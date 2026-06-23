package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func writePrometheusTargets(repoRoot string, nodeCount int) error {
	type targetGroup struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels"`
	}
	targets := make([]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		targets[i] = fmt.Sprintf("host.docker.internal:%d", 8001+i)
	}
	group := []targetGroup{{
		Targets: targets,
		Labels:  map[string]string{"cluster": "playground"},
	}}
	data, err := json.MarshalIndent(group, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(repoRoot, "monitoring", "targets.json")
	return os.WriteFile(path, data, 0644)
}

func clearPrometheusTargets(repoRoot string) error {
	return writePrometheusTargets(repoRoot, 0)
}
