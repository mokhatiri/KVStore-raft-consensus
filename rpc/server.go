// Package rpc implements the RPC server for inter-node communication.
package rpc

import (
	"net"
	"net/rpc"

	"distributed-kv/types"
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
	nodeID    int
}

func NewRaftServer(consensus types.ConsensusModule, nodeID int) *RaftServer {
	return &RaftServer{
		consensus: consensus,
		nodeID:    nodeID,
	}
}

func (rs *RaftServer) RequestVote(args *types.RequestVoteArgs, reply *types.RequestVoteReply) error {
	voteGranted, term := rs.consensus.RequestVote(args.Term, args.CandidateID, args.LastLogIndex, args.LastLogTerm)

	reply.Term = term
	reply.VoteGranted = voteGranted

	return nil
}

func (rs *RaftServer) AppendEntries(args *types.AppendEntriesArgs, reply *types.AppendEntriesReply) error {
	err := rs.consensus.AppendEntries(args.Term, args.LeaderID, args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommit, args.Entries)

	reply.Term = rs.consensus.GetCurrentTerm()
	if err != nil {
		reply.Success = false
	} else {
		reply.Success = true
	}

	return err
}

// Ping is a simple health check endpoint that returns true if the node is alive
func (rs *RaftServer) Ping(nodeID int, reply *bool) error {
	*reply = true
	return nil
}

// InstallSnapshot handles snapshot installation from the leader
func (rs *RaftServer) InstallSnapshot(args *types.InstallSnapshotArgs, reply *types.InstallSnapshotReply) error {
	term, err := rs.consensus.InstallSnapshot(args.Term, args.LeaderID, args.LastIncludedIndex, args.LastIncludedTerm, args.Data)
	reply.Term = term

	return err
}

// StartServer registers the RPC server and listens for incoming requests
func StartServer(consensus types.ConsensusModule, address string) error {
	raftServer := NewRaftServer(consensus, consensus.GetNodeID()) // starts a new RPC server instance

	// register the RPC server using net/rpc
	err := rpc.Register(raftServer)
	if err != nil {
		return err
	}

	// listen on the address
	listener, err := net.Listen("tcp", address) // listening on the given address
	if err != nil {
		return err
	}

	// Accept incoming connections in goroutine
	go rpc.Accept(listener)

	return nil
}
