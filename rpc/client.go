package rpc

import (
	"fmt"
	"net"
	"net/rpc"
	"time"

	"distributed-kv/types"
)

func SendRequestVote(address string, term int, candidateId int, lastLogIndex int, lastLogTerm int, nodeID int, rpcEventCh chan<- types.RPCEvent) (succ bool, new_term int, err error) {
	startTime := time.Now()

	// wrap to emit an event to the rpc channel
	// connect to the RPC server with timeout
	conn, err := net.DialTimeout("tcp", address, 1*time.Second)
	if err != nil {
		// Emit an event for the failed RPC call
		if rpcEventCh != nil {
			rpcEventCh <- types.EmitRequestVoteEvent(
				nodeID,
				0,
				types.RequestVoteArgs{Term: term, CandidateID: candidateId, LastLogIndex: lastLogIndex, LastLogTerm: lastLogTerm},
				types.RequestVoteReply{VoteGranted: false, Term: 0},
				time.Since(startTime),
				err.Error(),
			)
		}
		return false, 0, err
	}
	defer conn.Close()

	client := rpc.NewClient(conn)
	defer client.Close()

	// prepare the arguments for the RequestVote RPC call
	args := &types.RequestVoteArgs{
		Term:         term,
		CandidateID:  candidateId,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	var reply types.RequestVoteReply

	// make the RPC call with timeout
	done := make(chan error, 1)
	go func() {
		done <- client.Call("RaftServer.RequestVote", args, &reply)
	}()

	select {
	case err := <-done:
		duration := time.Since(startTime)

		// Emit an event for the completed RPC call
		if rpcEventCh != nil {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			rpcEventCh <- types.EmitRequestVoteEvent(
				nodeID,
				reply.Term,
				types.RequestVoteArgs{Term: term, CandidateID: candidateId, LastLogIndex: lastLogIndex, LastLogTerm: lastLogTerm},
				reply,
				duration,
				errStr,
			)
		}

		if err != nil {
			return false, 0, err
		}

		return reply.VoteGranted, reply.Term, nil

	case <-time.After(1 * time.Second):
		duration := time.Since(startTime)

		if rpcEventCh != nil {
			rpcEventCh <- types.EmitRequestVoteEvent(
				nodeID,
				0,
				types.RequestVoteArgs{Term: term, CandidateID: candidateId, LastLogIndex: lastLogIndex, LastLogTerm: lastLogTerm},
				types.RequestVoteReply{VoteGranted: false, Term: 0},
				duration,
				"RPC call timed out",
			)
		}
		return false, 0, fmt.Errorf("RPC call timed out")
	}
}

func SendAppendEntries(address string, term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []types.LogEntry, nodeID int, rpcEventCh chan<- types.RPCEvent) (success bool, new_term int, err error) {
	startTime := time.Now()

	// connect to the RPC server with timeout
	conn, err := net.DialTimeout("tcp", address, 1*time.Second)
	if err != nil {
		if rpcEventCh != nil {
			rpcEventCh <- types.EmitAppendEntriesEvent(
				nodeID,
				0,
				types.AppendEntriesArgs{Term: term, LeaderID: leaderId, PrevLogIndex: prevLogIndex, PrevLogTerm: prevLogTerm, LeaderCommit: leaderCommit, Entries: entries},
				types.AppendEntriesReply{Success: false, Term: 0},
				time.Since(startTime),
				err.Error(),
			)
		}
		return false, 0, fmt.Errorf("RPC call timed out")
	}
	defer conn.Close()

	client := rpc.NewClient(conn)
	defer client.Close()

	// prepare the arguments for the AppendEntries RPC call
	args := &types.AppendEntriesArgs{
		Term:         term,
		LeaderID:     leaderId,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		LeaderCommit: leaderCommit,
		Entries:      entries,
	}
	var reply types.AppendEntriesReply

	// make the RPC call with timeout
	done := make(chan error, 1)
	go func() {
		done <- client.Call("RaftServer.AppendEntries", args, &reply)
	}()

	select {
	case err := <-done:
		duration := time.Since(startTime)

		// Emit an event for the completed RPC call
		if rpcEventCh != nil {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			rpcEventCh <- types.EmitAppendEntriesEvent(
				nodeID,
				reply.Term,
				types.AppendEntriesArgs{Term: term, LeaderID: leaderId, PrevLogIndex: prevLogIndex, PrevLogTerm: prevLogTerm, LeaderCommit: leaderCommit, Entries: entries},
				reply,
				duration,
				errStr,
			)
		}

		if err != nil {
			return false, 0, err
		}
		return reply.Success, reply.Term, nil

	case <-time.After(1 * time.Second):
		duration := time.Since(startTime)

		if rpcEventCh != nil {
			rpcEventCh <- types.EmitAppendEntriesEvent(
				nodeID,
				0,
				types.AppendEntriesArgs{Term: term, LeaderID: leaderId, PrevLogIndex: prevLogIndex, PrevLogTerm: prevLogTerm, LeaderCommit: leaderCommit, Entries: entries},
				types.AppendEntriesReply{Success: false, Term: 0},
				duration,
				"RPC call timed out",
			)
		}
		return false, 0, fmt.Errorf("RPC call timed out")
	}
}
