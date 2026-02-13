package types

import (
	"encoding/json"
	"time"
)

type RPCEvent struct {
	Timestamp   time.Time
	From        int
	To          int
	Type        string
	Details     string // Changed from interface{} to string to be gob-serializable
	Duration    time.Duration
	Error       string
	IsHeartbeat bool // true if this is an AppendEntries heartbeat
}

func NewRPCEvent(from, to int, rpcType string, details string, duration time.Duration, err string) RPCEvent {
	return RPCEvent{
		Timestamp:   time.Now(),
		From:        from,
		To:          to,
		Type:        rpcType,
		Details:     details,
		Duration:    duration,
		Error:       err,
		IsHeartbeat: false,
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
	details := map[string]any{
		"args":  args,
		"reply": reply,
	}
	detailsJSON, _ := json.Marshal(details)
	return NewRPCEvent(from, to, "RequestVote", string(detailsJSON), duration, err)
}

func EmitAppendEntriesEvent(from, to int, args AppendEntriesArgs, reply AppendEntriesReply, duration time.Duration, err string) RPCEvent {
	details := map[string]any{
		"args":  args,
		"reply": reply,
	}
	detailsJSON, _ := json.Marshal(details)
	event := NewRPCEvent(from, to, "AppendEntries", string(detailsJSON), duration, err)
	// Mark as heartbeat if no entries to replicate
	event.IsHeartbeat = len(args.Entries) == 0
	return event
}

func EmitPingEvent(from, to int, duration time.Duration, err string) RPCEvent {
	return NewRPCEvent(from, to, "Ping", "", duration, err)
}
