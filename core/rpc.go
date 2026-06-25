package core

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"time"

	pb "github.com/ryansenn/quorum/proto/nodepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
)

type server struct {
	pb.UnimplementedNodeServer
	node *Node
}

func (n *Node) StartServer() {
	lis, err := net.Listen("tcp", n.Peers[n.Id])

	if err != nil {
		log.Fatalf(n.Id+"failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterNodeServer(grpcServer, &server{node: n})
	go grpcServer.Serve(lis)
}

func (n *Node) StartClients() {
	n.Clients = map[string]pb.NodeClient{}

	for key, addr := range n.Peers {
		if key == n.Id {
			continue
		}

		conn, err := grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithConnectParams(grpc.ConnectParams{
				Backoff: backoff.Config{
					BaseDelay:  100 * time.Millisecond,
					Multiplier: 1.2,
					MaxDelay:   240 * time.Millisecond,
				},
				MinConnectTimeout: 100 * time.Millisecond,
			}),
		)
		if err != nil {
			log.Fatalf("%s dial: %v", n.Id, err)
		}
		client := pb.NewNodeClient(conn)
		n.Clients[key] = client

		for {
			dummyReq := pb.VoteRequest{
				Term:         -1,
				CandidateId:  n.Id,
				LastLogIndex: -1,
				LastLogTerm:  -1,
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, err = client.RequestVote(ctx, &dummyReq)
			cancel()

			if err == nil {
				break
			}

			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (s *server) AppendEntries(ctx context.Context, req *pb.AppendRequest) (*pb.AppendResponse, error) {
	term := s.node.Term.Load()
	resp := pb.AppendResponse{Term: term, Success: false}

	if s.node.Term.Load() > req.Term {
		return &resp, nil
	}

	s.node.ReceiveHeartbeat()
	storeString(&s.node.LeaderId, req.LeaderId)

	if req.Term > term {
		s.node.Term.Store(req.Term)
		s.node.Logger.WriteMeta(s.node.Term.Load(), s.node.voteFor())
		s.node.State = Follower
	}

	if s.node.GetLogSize()-1 < int(req.PrevLogIndex) {
		return &resp, nil
	}

	if s.node.GetLogTerm(int(req.PrevLogIndex)) != req.PrevLogTerm {
		return &resp, nil
	}

	var entries []*LogEntry

	for _, entry := range req.Entries {
		var cmd Command
		json.Unmarshal(entry.Command, &cmd)
		entries = append(entries, NewLogEntry(entry.Term, &cmd))
	}

	if len(entries) > 0 {
		s.node.AppendLogs(req.PrevLogIndex, entries)
	}

	if req.LeaderCommit > s.node.CommitIndex.Load() {
		s.node.CommitIndex.Store(min(req.LeaderCommit, int64(s.node.GetLogSize()-1)))
		s.node.ApplyCommitted()
	}

	resp.Success = true
	resp.Term = s.node.Term.Load()
	return &resp, nil
}

func (s *server) RequestVote(ctx context.Context, req *pb.VoteRequest) (*pb.VoteResponse, error) {
	if s.node.Term.Load() < req.Term {
		s.node.ReceiveHeartbeat()
		s.node.State = Follower
		s.node.Term.Store(req.Term)
		storeString(&s.node.VoteFor, "")
		s.node.Logger.WriteMeta(s.node.Term.Load(), "")
	}

	resp := pb.VoteResponse{Term: s.node.Term.Load(), VoteGranted: false}

	if s.node.voteFor() != "" && s.node.voteFor() != req.CandidateId {
		s.node.RecordRequestVote("denied")
		return &resp, nil
	}

	if s.node.Term.Load() > req.Term {
		s.node.RecordRequestVote("denied")
		return &resp, nil
	}

	if s.node.GetLogTerm(-1) > req.LastLogTerm || (s.node.GetLogTerm(-1) == req.LastLogTerm && int64(len(s.node.Log))-1 > req.LastLogIndex) {
		s.node.RecordRequestVote("denied")
		return &resp, nil
	}

	resp.VoteGranted = true
	storeString(&s.node.VoteFor, req.CandidateId)
	s.node.Logger.WriteMeta(s.node.Term.Load(), req.CandidateId)
	s.node.ReceiveHeartbeat()
	s.node.RecordRequestVote("granted")
	s.node.recordEvent(Event{
		Type:   "request_vote",
		From:   s.node.Id,
		To:     req.CandidateId,
		Term:   s.node.Term.Load(),
		Detail: "granted",
	})
	return &resp, nil
}

func (s *server) ForwardToLeader(ctx context.Context, command *pb.Command) (*pb.CommandResponse, error) {
	if s.node.State == Follower {
		leaderID := s.node.leaderID()
		if leaderID == "" {
			return &pb.CommandResponse{Result: []byte("no leader elected yet")}, nil
		}
		client := s.node.Clients[leaderID]
		if client == nil {
			return &pb.CommandResponse{Result: []byte("leader not accessible")}, nil
		}
		ctx, cancel := contextWithRPCTimeout()
		defer cancel()
		return client.ForwardToLeader(ctx, command)
	}

	var cmd Command
	var res pb.CommandResponse
	res.Success = true

	if s.node.State == Candidate {
		res.Success = false
		return &res, nil
	}

	err := json.Unmarshal(command.Command, &cmd)

	if err != nil {
		res.Success = false
		return &res, err
	}

	res.Result = []byte(s.node.HandleCommand(NewCommand(cmd.Op, cmd.Key, cmd.Value)))

	return &res, nil
}
