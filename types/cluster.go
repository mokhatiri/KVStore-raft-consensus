package types

import "time"

type ClusterState struct {
	// Static
	Nodes map[int]*NodeState // ID → Node details

	// Dynamic (updates as events arrive)
	Leader      int
	CurrentTerm int
	ElectedAt   time.Time

	// Replication tracking
	ReplicationProgress map[int]map[int]int // [nodeID][followerID] = matchIndex

	// Health
	LastHeartbeat map[int]time.Time // [nodeID] = last contact time
	LiveNodes     int

	// Stats
	TotalEventsProcessed int
	CommittedEntries     int
}

type NodeState struct {
	ID          int
	Role        string // "Leader", "Follower", "Candidate"
	Term        int
	CommitIndex int
	LastApplied int
	LogLength   int

	// Health
	IsAlive         bool
	ResponseLatency time.Duration
	LastSeen        time.Time
}

// TODO: code to update the ClusterState based on incoming events, e.g. UpdateClusterState(event RPCEvent) that updates the leader, term, replication progress, etc.
