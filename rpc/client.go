package rpc

import (
	"distributed-kv/types"
	"net/rpc"
)

func SendRequestVote(address string, term int, candidateId int, lastLogIndex int, lastLogTerm int) (succ bool, new_term int, err error) {
	// connect to the RPC server
	client, err := rpc.Dial("tcp", address)
	if err != nil {
		return false, 0, err
	}
	defer client.Close()

	// prepare the arguments for the RequestVote RPC call
	args := &RequestVoteArgs{
		Term:         term,
		CandidateID:  candidateId,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	var reply RequestVoteReply

	// make the RPC call
	err = client.Call("RaftServer.RequestVote", args, &reply)
	if err != nil {
		return false, 0, err
	}

	// return the result
	return reply.VoteGranted, reply.Term, nil
}

func SendAppendEntries(address string, term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []types.LogEntry) (success bool, new_term int, err error) {
	// connect to the RPC server
	client, err := rpc.Dial("tcp", address)
	if err != nil {
		return false, 0, err
	}
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

	// make the RPC call
	err = client.Call("RaftServer.AppendEntries", args, &reply)
	if err != nil {
		return false, 0, err
	}

	// return the result
	return reply.Success, reply.Term, nil
}
