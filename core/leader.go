package core

import (
	"context"
	"fmt"
	"time"

	pb "github.com/ryansenn/quorum/proto/nodepb"
)

// maxEntriesPerAppend bounds how many log entries the leader sends in a single
// AppendEntries RPC. Without a cap, a follower that restarts far behind would be
// sent its entire missing tail at once; under heavy load that payload exceeds the
// RPC deadline, the call errors out, and the follower never catches up. Sending in
// bounded batches lets a recovering follower make steady forward progress.
const maxEntriesPerAppend = 256

func (n *Node) notifyReplicators() {
	select {
	case n.ReplicateNotify <- struct{}{}:
	default:
	}
}

func (n *Node) AppendLog(cmd *Command) int {
	entry := NewLogEntry(n.Term.Load(), cmd)
	n.LogMu.Lock()
	defer n.LogMu.Unlock()
	n.Logger.AppendLog(entry)
	n.Log = append(n.Log, entry)
	if n.State == Leader {
		n.notifyReplicators()
	}
	return len(n.Log) - 1
}

func (n *Node) Commit(cmd *Command) {
	index := int64(n.AppendLog(cmd))
	n.CommitCond.L.Lock()
	for index > n.CommitIndex.Load() {
		n.CommitCond.Wait()
	}
	n.CommitCond.L.Unlock()

	n.ApplyCond.L.Lock()
	for index > n.LastApplied.Load() {
		n.ApplyCond.Wait()
	}
	n.ApplyCond.L.Unlock()
}

func (n *Node) StartReplicationWorkers() {
	for key, _ := range n.MatchIndex {
		n.MatchIndex[key].Store(0)
	}

	for key, _ := range n.NextIndex {
		n.NextIndex[key].Store(int64(n.GetLogSize()))
	}

	for id := range n.Peers {
		if id != n.Id {
			go n.ReplicateToFollower(id)
		}
	}
}

func (n *Node) ReplicateToFollower(id string) {
	var lastHeartbeatEvent time.Time
	for n.State == Leader {
		startIndex := n.NextIndex[id].Load()
		prevIndex := startIndex - 1
		prevTerm := int64(0)
		var snapshot []*LogEntry
		n.LogMu.Lock()
		if startIndex < int64(len(n.Log)) {
			end := startIndex + maxEntriesPerAppend
			if end > int64(len(n.Log)) {
				end = int64(len(n.Log))
			}
			snapshot = append(snapshot, n.Log[startIndex:end]...)
		}
		if prevIndex >= 0 && prevIndex < int64(len(n.Log)) {
			prevTerm = int64(n.Log[prevIndex].Term)
		}
		n.LogMu.Unlock()

		var entries []*pb.LogEntry

		for _, entry := range snapshot {
			entries = append(entries, &pb.LogEntry{Term: entry.Term, Command: entry.Serialized})
		}

		req := pb.AppendRequest{
			Term:         n.Term.Load(),
			LeaderId:     n.Id,
			PrevLogIndex: prevIndex,
			PrevLogTerm:  prevTerm,
			Entries:      entries,
			LeaderCommit: n.CommitIndex.Load(),
		}

		sendHBEvent := false
		if len(entries) > 0 {
			n.recordEvent(Event{
				Type:    "append_entries",
				From:    n.Id,
				To:      id,
				Term:    n.Term.Load(),
				Entries: len(entries),
			})
		} else if time.Since(lastHeartbeatEvent) >= 2*time.Second {
			sendHBEvent = true
			lastHeartbeatEvent = time.Now()
			n.recordEvent(Event{
				Type:    "append_entries",
				From:    n.Id,
				To:      id,
				Term:    n.Term.Load(),
				Entries: 0,
			})
		}

		if len(entries) > 0 {
			n.Logger.Sync()
		}

		if err := n.checkPeerBlocked(id); err != nil {
			if len(entries) == 0 {
				select {
				case <-n.ReplicateNotify:
				case <-time.After(10 * time.Millisecond):
				}
			}
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		resp, err := n.Clients[id].AppendEntries(ctx, &req)
		cancel()

		if err == nil && resp.Success {
			n.RecordAppendEntries("success")
			if len(entries) > 0 {
				n.recordEvent(Event{
					Type:    "append_response",
					From:    id,
					To:      n.Id,
					Term:    n.Term.Load(),
					Entries: len(entries),
				})
				added := int64(len(req.Entries))
				n.NextIndex[id].Add(added)
				n.MatchIndex[id].Store(n.NextIndex[id].Load() - 1)
				n.UpdateCommitIndex()
			} else if sendHBEvent {
				n.recordEvent(Event{
					Type:    "append_response",
					From:    id,
					To:      n.Id,
					Term:    n.Term.Load(),
					Entries: 0,
				})
			}
		} else if err == nil && len(entries) > 0 {
			n.RecordAppendEntries("failure")
			if n.NextIndex[id].Load() > 0 {
				n.NextIndex[id].Add(-1)
			}
			continue
		}

		if err != nil && len(entries) > 0 {
			n.RecordAppendEntries("error")
		}

		if len(entries) == 0 {
			select {
			case <-n.ReplicateNotify:
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

func (n *Node) UpdateCommitIndex() {
	for i := int64(n.GetLogSize()) - 1; i > n.CommitIndex.Load(); i-- {
		if n.GetLogTerm(int(i)) != n.Term.Load() {
			continue
		}
		count := 1

		for id, val := range n.MatchIndex {
			if id != n.Id && val.Load() >= i {
				count++
			}
		}

		if i > n.CommitIndex.Load() && count > len(n.MatchIndex)/2 {
			n.CommitCond.L.Lock()
			n.CommitIndex.Store(i)
			n.CommitCond.L.Unlock()
			n.RecordCommit()
			n.recordEvent(Event{
				Type:   "commit",
				From:   n.Id,
				To:     n.Id,
				Term:   n.Term.Load(),
				Detail: fmt.Sprintf("%d", i),
			})
			n.ApplyCommitted()
			n.CommitCond.L.Lock()
			n.CommitCond.Broadcast()
			n.CommitCond.L.Unlock()
			return
		}
	}
}

func (n *Node) ApplyLogEntry(index int64) {
	n.LogMu.Lock()
	cmd := n.Log[index].Command
	switch cmd.Op {
	case "put":
		n.Storage.Put(cmd.Key, cmd.Value)
	}
	n.LogMu.Unlock()
}
