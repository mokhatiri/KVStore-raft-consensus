package types

import "time"

type RPCEvent struct {
	Timestamp time.Time
	From      int
	To        int
	Type      string
	Details   any
	Duration  time.Duration
	Error     string
}

func NewRPCEvent(from, to int, rpcType string, details any, duration time.Duration, err string) RPCEvent {
	return RPCEvent{
		Timestamp: time.Now(),
		From:      from,
		To:        to,
		Type:      rpcType,
		Details:   details,
		Duration:  duration,
		Error:     err,
	}
}

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
	Term         int        // leader's term
	LeaderID     int        // so follower can redirect clients
	PrevLogIndex int        // index of log entry immediately preceding new ones
	PrevLogTerm  int        // term of prevLogIndex entry
	LeaderCommit int        // leader's commitIndex
	Entries      []LogEntry // log entries to store (empty for heartbeat)
}
type AppendEntriesReply struct {
	Term    int  // current term, for leader to update itself
	Success bool // true if follower contained entry matching prevLogIndex and prevLogTerm
}

// methodes to emit RPC events
func EmitRequestVoteEvent(from, to int, args RequestVoteArgs, reply RequestVoteReply, duration time.Duration, err string) RPCEvent {
	return NewRPCEvent(from, to, "RequestVote", map[string]any{
		"args":  args,
		"reply": reply,
	}, duration, err)
}

func EmitAppendEntriesEvent(from, to int, args AppendEntriesArgs, reply AppendEntriesReply, duration time.Duration, err string) RPCEvent {
	return NewRPCEvent(from, to, "AppendEntries", map[string]any{
		"args":  args,
		"reply": reply,
	}, duration, err)
}
