package rpc

import (
	"fmt"
	"net"
	"net/rpc"
	"time"

	"distributed-kv/types"
)

func SendRequestVote(address string, term int, candidateId int, lastLogIndex int, lastLogTerm int, nodeID int, peerID int) (succ bool, new_term int, err error) {

	// connect to the RPC server with timeout
	conn, err := net.DialTimeout("tcp", address, 1*time.Second)
	if err != nil {
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
		if err != nil {
			return false, 0, err
		}

		return reply.VoteGranted, reply.Term, nil

	case <-time.After(1 * time.Second):
		return false, 0, fmt.Errorf("RPC call timed out")
	}
}

func SendAppendEntries(address string, term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []types.LogEntry, nodeID int, followerID int) (success bool, new_term int, err error) {

	// connect to the RPC server with timeout
	conn, err := net.DialTimeout("tcp", address, 1*time.Second)
	if err != nil {
		return false, 0, fmt.Errorf("RPC connection failed")
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

		if err != nil {
			return false, 0, err
		}
		return reply.Success, reply.Term, nil

	case <-time.After(1 * time.Second):
		return false, 0, fmt.Errorf("RPC call timed out")
	}
}

// SendInstallSnapshot sends a snapshot to a follower that is too far behind
func SendInstallSnapshot(address string, term int, leaderId int, lastIncludedIndex int, lastIncludedTerm int, data map[string]any, nodeID int, followerID int) (replyTerm int, err error) {

	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return 0, fmt.Errorf("RPC connection failed")
	}
	defer conn.Close()

	client := rpc.NewClient(conn)
	defer client.Close()

	args := &types.InstallSnapshotArgs{
		Term:              term,
		LeaderID:          leaderId,
		LastIncludedIndex: lastIncludedIndex,
		LastIncludedTerm:  lastIncludedTerm,
		Data:              data,
	}
	var reply types.InstallSnapshotReply

	done := make(chan error, 1)
	go func() {
		done <- client.Call("RaftServer.InstallSnapshot", args, &reply)
	}()

	select {
	case err := <-done:
		if err != nil {
			return 0, err
		}
		return reply.Term, nil

	case <-time.After(5 * time.Second):
		// Snapshots can be large, allow longer timeout
		return 0, fmt.Errorf("InstallSnapshot RPC timed out")
	}
}
