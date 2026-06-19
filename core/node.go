package core

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/ryansenn/ryanDB/proto/nodepb"
)

type NodeState int

const (
	Follower  NodeState = 0
	Candidate NodeState = 1
	Leader    NodeState = 2
)

type Node struct {
	Id      string
	Port    string
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
	ResetElectionTimer chan struct{}
	Logger             *Logger
	Storage            *Engine
	Events             *EventLog
}

func NewNode(id, port string, peers map[string]string) *Node {
	n := &Node{
		Id:                 id,
		Port:               port,
		Peers:              peers,
		Clients:            make(map[string]pb.NodeClient),
		State:              Follower,
		NextIndex:          make(map[string]*atomic.Int64),
		MatchIndex:         make(map[string]*atomic.Int64),
		Log:                make([]*LogEntry, 0),
		CommitCond:         sync.NewCond(&sync.Mutex{}),
		ApplyCond:          sync.NewCond(&sync.Mutex{}),
		ResetElectionTimer: make(chan struct{}, 1),
		Logger:             newLogger(id),
		Storage:            NewEngine(),
		Events:             NewEventLog(),
	}
	n.CommitIndex.Store(-1)
	n.LastApplied.Store(-1)
	empty := ""
	n.VoteFor.Store(&empty)
	n.LeaderId.Store(&empty)

	for key, _ := range n.Peers {
		n.NextIndex[key] = &atomic.Int64{}
		n.MatchIndex[key] = &atomic.Int64{}
	}

	return n
}

func (n *Node) Init() {
	log.Printf(n.Id + " has been initialized.")
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
	n.VoteFor.Store(&votedFor)
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

// Used by followers to update/write entries from leader
func (n *Node) AppendLogs(PrevLogIndex int64, entries []*LogEntry) {
	n.LogMu.Lock()
	defer n.LogMu.Unlock()

	// in memory
	n.Log = n.Log[:PrevLogIndex+1]
	n.Log = append(n.Log, entries...)

	//persistent
	n.Logger.AppendLogs(entries, PrevLogIndex+1)

	log.Printf(n.Id+" has appended %d new log", len(entries))
}

func (n *Node) ApplyCommitted() {
	n.ApplyMu.Lock()

	for i := n.LastApplied.Load() + 1; i <= n.CommitIndex.Load(); i++ {
		n.ApplyLogEntry(i)
		n.LastApplied.Store(i)
	}

	n.ApplyMu.Unlock()
}

// Provide linearizable reads
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

func (n *Node) ForwardToLeader(command *Command) string {
	serializedCommand, err := json.Marshal(*command)
	if err != nil {
		log.Print(err)
	}

	if *n.LeaderId.Load() == "" {
		return "no leader elected yet"
	}

	log.Printf(n.Id + " has forwarded command to leader " + *n.LeaderId.Load())
	n.recordEvent(Event{
		Type: "forward_command",
		From: n.Id,
		To:   *n.LeaderId.Load(),
		Term: n.Term.Load(),
		Op:   command.Op,
		Key:  command.Key,
	})
	response, err := n.Clients[*n.LeaderId.Load()].ForwardToLeader(context.Background(), &pb.Command{Command: serializedCommand})

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
	n.VoteFor.Store(&n.Id)
	n.Term.Add(1)
	n.Logger.WriteTerm(n.Term.Load())
	n.Logger.WriteVotedFor(*n.VoteFor.Load())
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

			voteResp, err := client.RequestVote(context.Background(), &voteReq)

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
