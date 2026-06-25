package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var playgroundHTTPClient = &http.Client{Timeout: 3 * time.Second}

type NodeStatus struct {
	ID           string           `json:"id"`
	Running      bool             `json:"running"`
	Reachable    bool             `json:"reachable,omitempty"`
	State        int              `json:"state,omitempty"`
	StateName    string           `json:"stateName,omitempty"`
	Term         int64            `json:"term,omitempty"`
	LeaderId     string           `json:"leaderId,omitempty"`
	CommitIndex  int64            `json:"commitIndex,omitempty"`
	LastApplied  int64            `json:"lastApplied,omitempty"`
	LogLength    int              `json:"logLength,omitempty"`
	BlockedPeers []string         `json:"blockedPeers,omitempty"`
}

func fetchStatus(port string) (*NodeStatus, error) {
	resp, err := playgroundHTTPClient.Get("http://localhost:" + port + "/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var s NodeStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	s.Reachable = true
	return &s, nil
}

type LogEntryView struct {
	Index int64  `json:"index"`
	Term  int64  `json:"term"`
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

func fetchLogTail(port string, tail int) ([]LogEntryView, error) {
	url := fmt.Sprintf("http://localhost:%s/log?tail=%d", port, tail)
	resp, err := playgroundHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		Entries []LogEntryView `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Entries, nil
}

func doPut(port, key, value, client string) (string, error) {
	params := url.Values{}
	params.Set("key", key)
	params.Set("value", value)
	if client != "" {
		params.Set("client", client)
	}
	resp, err := playgroundHTTPClient.Get("http://localhost:" + port + "/put?" + params.Encode())
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

func doGet(port, key, client string) (string, error) {
	params := url.Values{}
	params.Set("key", key)
	if client != "" {
		params.Set("client", client)
	}
	resp, err := playgroundHTTPClient.Get("http://localhost:" + port + "/get?" + params.Encode())
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

func blockPeer(port, peer string) error {
	params := url.Values{}
	params.Set("peer", peer)
	req, err := http.NewRequest(http.MethodPost, "http://localhost:"+port+"/simulate/block?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := playgroundHTTPClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("block peer on %s: status %d", port, resp.StatusCode)
	}
	return nil
}

func unblockAll(port string) error {
	req, err := http.NewRequest(http.MethodPost, "http://localhost:"+port+"/simulate/unblock", nil)
	if err != nil {
		return err
	}
	resp, err := playgroundHTTPClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("unblock on %s: status %d", port, resp.StatusCode)
	}
	return nil
}
