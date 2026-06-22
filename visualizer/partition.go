package main

import (
	"fmt"
	"net/http"
	"net/url"
)

func blockPeer(port, peer string) error {
	params := url.Values{}
	params.Set("peer", peer)
	req, err := http.NewRequest(http.MethodPost, "http://localhost:"+port+"/simulate/block?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := visualizerHTTPClient.Do(req)
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
	resp, err := visualizerHTTPClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("unblock on %s: status %d", port, resp.StatusCode)
	}
	return nil
}

func (c *Cluster) SetPartition(isolated []string) error {
	isolatedSet := map[string]bool{}
	for _, id := range isolated {
		isolatedSet[id] = true
	}

	for _, node := range c.Nodes {
		if !node.Running {
			continue
		}
		if err := unblockAll(node.Port); err != nil {
			return err
		}
		for _, peer := range c.Nodes {
			if peer.ID == node.ID {
				continue
			}
			nodeIsolated := isolatedSet[node.ID]
			peerIsolated := isolatedSet[peer.ID]
			if nodeIsolated != peerIsolated {
				if err := blockPeer(node.Port, peer.ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Cluster) ClearPartition() error {
	for _, node := range c.Nodes {
		if !node.Running {
			continue
		}
		if err := unblockAll(node.Port); err != nil {
			return err
		}
	}
	return nil
}
