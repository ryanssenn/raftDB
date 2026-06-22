package core

import (
	"errors"
	"sync"
)

var ErrPeerBlocked = errors.New("peer blocked by simulation")

func (n *Node) checkPeerBlocked(peer string) error {
	if n.IsPeerBlocked(peer) {
		return ErrPeerBlocked
	}
	return nil
}

func (n *Node) BlockedPeerList() []string {
	var peers []string
	n.BlockedPeers.Range(func(key, _ any) bool {
		if id, ok := key.(string); ok {
			peers = append(peers, id)
		}
		return true
	})
	return peers
}

func (n *Node) SetBlockedPeers(peers []string) {
	n.UnblockAllPeers()
	for _, p := range peers {
		n.BlockPeer(p)
	}
}

// PeerBlockMu serializes simulation updates from HTTP handlers.
var PeerBlockMu sync.Mutex
