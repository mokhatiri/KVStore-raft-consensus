// data types for the RPC
package rpc

import "distributed-kv/types"

// for RequestVote RPC
type RequestVoteArgs struct {
	Term         int // candidate's term
	CandidateID  int // candidate requesting vote
	LastLogIndex int // index of candidate's last log entry
	LastLogTerm  int // term of candidate's last log entry
}
type RequestVoteReply struct {
	Term        int  // current term, for candidate to update itself
	VoteGranted bool // true means candidate received vote
}

// for AppendEntries RPC
type AppendEntriesArgs struct {
	Term         int              // leader's term
	LeaderID     int              // so follower can redirect clients
	PrevLogIndex int              // index of log entry immediately preceding new ones
	PrevLogTerm  int              // term of prevLogIndex entry
	LeaderCommit int              // leader's commitIndex
	Entries      []types.LogEntry // log entries to store (empty for heartbeat)
}
type AppendEntriesReply struct {
	Term    int  // current term, for leader to update itself
	Success bool // true if follower contained entry matching prevLogIndex and prevLogTerm
}
