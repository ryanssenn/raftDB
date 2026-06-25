package core

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/ryansenn/quorum/proto/nodepb"
)

const rpcTimeout = 500 * time.Millisecond

func storeString(p *atomic.Pointer[string], s string) {
	v := new(string)
	*v = s
	p.Store(v)
}

func contextWithRPCTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), rpcTimeout)
}

func (n *Node) leaderID() string {
	p := n.LeaderId.Load()
	if p == nil {
		return ""
	}
	return *p
}

func (n *Node) voteFor() string {
	p := n.VoteFor.Load()
	if p == nil {
		return ""
	}
	return *p
}

type NodeState int

const (
	Follower  NodeState = 0
	Candidate NodeState = 1
	Leader    NodeState = 2
)

type Node struct {
	Id      string
	Peers   map[string]string
	Clients map[string]pb.NodeClient
	State   NodeState

	Term        atomic.Int64
	VoteFor     atomic.Pointer[string]
	CommitIndex atomic.Int64
	LastApplied atomic.Int64
	ApplyMu     sync.Mutex
	NextIndex   map[string]*atomic.Int64
	MatchIndex  map[string]*atomic.Int64
	Log         []*LogEntry
	LogMu       sync.Mutex
	CommitCond  *sync.Cond
	ApplyCond   *sync.Cond

	LeaderId           atomic.Pointer[string]
	BlockedPeers       sync.Map
	ResetElectionTimer chan struct{}
	ReplicateNotify    chan struct{}
	Logger             *Logger
	Storage            *Engine
	Events             *EventLog
}

func NewNode(id string, peers map[string]string) *Node {
	n := &Node{
		Id:                 id,
		Peers:              peers,
		Clients:            make(map[string]pb.NodeClient),
		State:              Follower,
		NextIndex:          make(map[string]*atomic.Int64),
		MatchIndex:         make(map[string]*atomic.Int64),
		Log:                make([]*LogEntry, 0),
		CommitCond:         sync.NewCond(&sync.Mutex{}),
		ApplyCond:          sync.NewCond(&sync.Mutex{}),
		ResetElectionTimer: make(chan struct{}, 1),
		ReplicateNotify:    make(chan struct{}, 1),
		Logger:             newLogger(id),
		Storage:            NewEngine(),
		Events:             NewEventLog(),
	}
	n.CommitIndex.Store(-1)
	n.LastApplied.Store(-1)
	storeString(&n.VoteFor, "")
	storeString(&n.LeaderId, "")

	for key, _ := range n.Peers {
		n.NextIndex[key] = &atomic.Int64{}
		n.MatchIndex[key] = &atomic.Int64{}
	}

	return n
}

func (n *Node) Init() {
	log.Printf("%s has been initialized.", n.Id)
	n.StartServer()
	n.StartClients()
	n.StartElectionTimer()
}

func (n *Node) RecoverState() {
	n.LogMu.Lock()
	n.Log = n.Logger.LoadLogs()
	n.LogMu.Unlock()

	term, votedFor := n.Logger.LoadMeta()
	n.Term.Store(term)
	storeString(&n.VoteFor, votedFor)

	if len(n.Log) > 0 {
		last := int64(len(n.Log) - 1)
		n.CommitIndex.Store(last)
		n.LastApplied.Store(-1)
		n.ApplyCommitted()
	}
}

func (n *Node) HandleCommand(cmd *Command) string {
	if n.State == Follower {
		return n.ForwardToLeader(cmd)
	}

	if n.State == Candidate {
		return "Error: election"
	}

	switch cmd.Op {
	case "get":
		return n.Get(cmd.Key)
	case "put":
		n.Commit(cmd)
		return "success"
	}

	return "unknown command"
}

func (n *Node) AppendLogs(prevLogIndex int64, entries []*LogEntry) {
	n.LogMu.Lock()
	defer n.LogMu.Unlock()

	n.Log = n.Log[:prevLogIndex+1]
	n.Log = append(n.Log, entries...)
	n.Logger.AppendLogs(entries, prevLogIndex+1)
}

func (n *Node) ApplyCommitted() {
	n.ApplyMu.Lock()

	for i := n.LastApplied.Load() + 1; i <= n.CommitIndex.Load(); i++ {
		n.ApplyLogEntry(i)
		n.LastApplied.Store(i)
	}

	n.ApplyMu.Unlock()
	n.ApplyCond.Broadcast()
}

func (n *Node) Get(key string) string {
	readIndex := n.CommitIndex.Load()

	n.ApplyCond.L.Lock()
	for n.LastApplied.Load() < readIndex {
		n.ApplyCond.Wait()
	}
	n.ApplyCond.L.Unlock()
	return n.Storage.Get(key)
}

func (n *Node) GetLogSize() int {
	n.LogMu.Lock()
	defer n.LogMu.Unlock()
	return len(n.Log)
}

func (n *Node) GetLogTerm(index int) int64 {
	n.LogMu.Lock()
	defer n.LogMu.Unlock()
	if index == -1 {
		if len(n.Log) > 0 {
			return n.Log[len(n.Log)-1].Term
		}
		return 0
	}

	return n.Log[index].Term
}

type LogEntryView struct {
	Index int64  `json:"index"`
	Term  int64  `json:"term"`
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

func (n *Node) GetLogTail(tail int) []LogEntryView {
	n.LogMu.Lock()
	defer n.LogMu.Unlock()
	if tail <= 0 {
		tail = 20
	}
	start := len(n.Log) - tail
	if start < 0 {
		start = 0
	}
	out := make([]LogEntryView, 0, len(n.Log)-start)
	for i := start; i < len(n.Log); i++ {
		e := n.Log[i]
		out = append(out, LogEntryView{
			Index: int64(i),
			Term:  e.Term,
			Op:    e.Command.Op,
			Key:   e.Command.Key,
			Value: e.Command.Value,
		})
	}
	return out
}

func (n *Node) StateName() string {
	switch n.State {
	case Follower:
		return "follower"
	case Candidate:
		return "candidate"
	case Leader:
		return "leader"
	default:
		return "unknown"
	}
}

func (n *Node) BlockPeer(peer string) {
	n.BlockedPeers.Store(peer, true)
}

func (n *Node) UnblockPeer(peer string) {
	n.BlockedPeers.Delete(peer)
}

func (n *Node) UnblockAllPeers() {
	n.BlockedPeers.Range(func(key, _ any) bool {
		n.BlockedPeers.Delete(key)
		return true
	})
}

func (n *Node) IsPeerBlocked(peer string) bool {
	_, ok := n.BlockedPeers.Load(peer)
	return ok
}

func (n *Node) ForwardToLeader(command *Command) string {
	serializedCommand, err := json.Marshal(*command)
	if err != nil {
		log.Print(err)
	}

	if n.leaderID() == "" {
		return "no leader elected yet"
	}

	client, ok := n.Clients[n.leaderID()]
	if !ok || client == nil {
		return "leader not accessible"
	}

	if err := n.checkPeerBlocked(n.leaderID()); err != nil {
		return "leader not accessible"
	}

	n.recordEvent(Event{
		Type: "forward_command",
		From: n.Id,
		To:   n.leaderID(),
		Term: n.Term.Load(),
		Op:   command.Op,
		Key:  command.Key,
	})
	ctx, cancel := contextWithRPCTimeout()
	defer cancel()
	response, err := client.ForwardToLeader(
		ctx,
		&pb.Command{Command: serializedCommand},
	)

	if err != nil {
		log.Print(err)
		return "leader not accessible"
	}

	return string(response.Result)
}

func (n *Node) StartElectionTimer() {
	randTimeout := func() time.Duration {
		return time.Duration(rand.Intn(151)+300) * time.Millisecond
	}
	timer := time.NewTimer(randTimeout())

	for {
		select {
		case <-timer.C:
			if n.State == Follower {
				n.StartElection()
			}
			timer.Reset(randTimeout())

		case <-n.ResetElectionTimer:
			if !timer.Stop() {
				<-timer.C //drain
			}
			timer.Reset(randTimeout())
		}
	}
}

func (n *Node) StartElection() {
	storeString(&n.VoteFor, n.Id)
	n.Term.Add(1)
	n.RecordElection()
	n.Logger.WriteMeta(n.Term.Load(), n.voteFor())
	n.State = Candidate
	n.recordEvent(Event{
		Type:   "state_change",
		From:   n.Id,
		To:     n.Id,
		Term:   n.Term.Load(),
		Detail: "candidate",
	})
	yesVote := 1

	log.Printf("%s started election for term %d", n.Id, n.Term.Load())

	for id, client := range n.Clients {
		if id != n.Id {
			LogSize := int64(n.GetLogSize())
			prevIndex := int64(LogSize - 1)
			prevTerm := int64(0)

			if prevIndex >= 0 && prevIndex < LogSize {
				prevTerm = n.GetLogTerm(int(prevIndex))
			}

			voteReq := pb.VoteRequest{
				Term:         n.Term.Load(),
				CandidateId:  n.Id,
				LastLogIndex: prevIndex,
				LastLogTerm:  prevTerm,
			}

			n.recordEvent(Event{
				Type: "request_vote",
				From: n.Id,
				To:   id,
				Term: n.Term.Load(),
			})

			if err := n.checkPeerBlocked(id); err != nil {
				continue
			}

			ctx, cancel := contextWithRPCTimeout()
			voteResp, err := client.RequestVote(ctx, &voteReq)
			cancel()

			if err != nil {
				log.Print(err)
				continue
			}

			if voteResp.VoteGranted {
				yesVote += 1
			}
		}
	}

	if yesVote > len(n.Peers)/2 {
		n.State = Leader
		n.recordEvent(Event{
			Type:   "state_change",
			From:   n.Id,
			To:     n.Id,
			Term:   n.Term.Load(),
			Detail: "leader",
		})
		go n.StartReplicationWorkers()
		log.Printf("%s becomes Leader for term %d", n.Id, n.Term.Load())
	} else {
		n.State = Follower
		n.recordEvent(Event{
			Type:   "state_change",
			From:   n.Id,
			To:     n.Id,
			Term:   n.Term.Load(),
			Detail: "follower",
		})
		log.Printf("%s becomes Follower for term %d", n.Id, n.Term.Load())
	}
}

func (n *Node) ReceiveHeartbeat() {
	select {
	case n.ResetElectionTimer <- struct{}{}:
		// sent successfully
	default:
		// channel full, skip
	}
}
