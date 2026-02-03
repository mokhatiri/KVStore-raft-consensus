// Package rpc implements the RPC server for inter-node communication.
package rpc

import (
	"distributed-kv/types"
	"net"
	"net/rpc"
)

/*
to explain the RPC is necessary for the distributed KV store using Raft consensus.
without it, the nodes are isolated and cannot coordinate to maintain consistency.

The RPC is devided into two:
1. the RPC Serve : listen for incoming RPC calls from other nodes
2. the RPC Client : make outgoing RPC calls to other nodes

---

example RPC calls:

Node A → Network (RPC call) → Node B
"RequestVote(term=2, candidateId=1)"
         ↓
Node B receives → runs RequestVote() locally → gets response
         ↓
Node B → Network (RPC response) → Node A
         "VoteGranted: true"

*/

type RaftServer struct {
	consensus types.ConsensusModule
}

func NewRaftServer(consensus types.ConsensusModule) *RaftServer {
	return &RaftServer{
		consensus: consensus,
	}
}

func (rs *RaftServer) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
	VoteGranted := rs.consensus.RequestVote(args.Term, args.CandidateID)

	reply.Term = rs.consensus.GetCurrentTerm()
	reply.VoteGranted = VoteGranted

	return nil
}

func (rs *RaftServer) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	err := rs.consensus.AppendEntries(args.Term, args.LeaderID, args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommit, args.Entries)

	reply.Term = rs.consensus.GetCurrentTerm()
	if err != nil {
		reply.Success = false
		return nil // Don't return error, Raft expects false Success
	}
	reply.Success = true
	return nil
}

// StartServer registers the RPC server and listens for incoming requests
func StartServer(consensus types.ConsensusModule, address string) error {
	raftServer := NewRaftServer(consensus)

	// register the RPC server using net/rpc
	err := rpc.Register(raftServer)
	if err != nil {
		return err
	}

	// listen on the address
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	// Accept incoming connections in goroutine
	go rpc.Accept(listener)

	return nil
}
