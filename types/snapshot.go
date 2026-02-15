package types

// Snapshot represents a point-in-time capture of the state machine
// plus enough Raft metadata to know which log entries it replaces.
type Snapshot struct {
	LastIncludedIndex int            `json:"last_included_index"` // index of last log entry in snapshot
	LastIncludedTerm  int            `json:"last_included_term"`  // term of last log entry in snapshot
	Data              map[string]any `json:"data"`                // state machine data (KV store)
}

// InstallSnapshotArgs is sent by the leader to followers that are too far behind
// to catch up via normal AppendEntries (their nextIndex points to a compacted entry).
type InstallSnapshotArgs struct {
	Term              int            `json:"term"`
	LeaderID          int            `json:"leader_id"`
	LastIncludedIndex int            `json:"last_included_index"`
	LastIncludedTerm  int            `json:"last_included_term"`
	Data              map[string]any `json:"data"` // full state machine snapshot
}

// InstallSnapshotReply is the response to InstallSnapshot.
type InstallSnapshotReply struct {
	Term int `json:"term"` // currentTerm, for leader to update itself
}
