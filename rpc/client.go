package rpc

import (
	"distributed-kv/types"
	"net"
	"net/rpc"
	"time"
)

func SendRequestVote(address string, term int, candidateId int, lastLogIndex int, lastLogTerm int) (succ bool, new_term int, err error) {
	// connect to the RPC server with timeout
	conn, err := net.DialTimeout("tcp", address, 1*time.Second)
	if err != nil {
		return false, 0, err
	}
	defer conn.Close()

	client := rpc.NewClient(conn)
	defer client.Close()

	// prepare the arguments for the RequestVote RPC call
	args := &RequestVoteArgs{
		Term:         term,
		CandidateID:  candidateId,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	var reply RequestVoteReply

	// make the RPC call with timeout
	done := make(chan error, 1)
	go func() {
		done <- client.Call("RaftServer.RequestVote", args, &reply)
	}()

	select {
	case err := <-done:
		if err != nil {
			return false, 0, err
		}
		return reply.VoteGranted, reply.Term, nil
	case <-time.After(1 * time.Second):
		return false, 0, err
	}
}

func SendAppendEntries(address string, term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []types.LogEntry) (success bool, new_term int, err error) {
	// connect to the RPC server with timeout
	conn, err := net.DialTimeout("tcp", address, 1*time.Second)
	if err != nil {
		return false, 0, err
	}
	defer conn.Close()

	client := rpc.NewClient(conn)
	defer client.Close()

	// prepare the arguments for the AppendEntries RPC call
	args := &AppendEntriesArgs{
		Term:         term,
		LeaderID:     leaderId,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		LeaderCommit: leaderCommit,
		Entries:      entries,
	}
	var reply AppendEntriesReply

	// make the RPC call with timeout
	done := make(chan error, 1)
	go func() {
		done <- client.Call("RaftServer.AppendEntries", args, &reply)
	}()

	select {
	case err := <-done:
		if err != nil {
			return false, 0, err
		}
		return reply.Success, reply.Term, nil
	case <-time.After(1 * time.Second):
		return false, 0, err
	}
}
