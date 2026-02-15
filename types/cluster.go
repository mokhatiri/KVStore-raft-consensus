package types

import "time"

type ClusterState struct {
	// Static
	Nodes map[int]*NodeState // ID → Node details

	// Dynamic
	Leader      int
	CurrentTerm int
	ElectedAt   time.Time

	// Replication tracking
	ReplicationProgress map[int]map[int]int // [nodeID][followerID] = matchIndex

	// Health
	LastHeartbeat map[int]time.Time // [nodeID] = last contact time
	LiveNodes     int

	CommittedEntries int
}

type NodeState struct {
	ID          int
	Role        string // "Leader", "Follower", "Candidate"
	Term        int
	CommitIndex int
	LastApplied int
	LogLength   int
	HTTPAddress string

	// Health
	IsAlive         bool
	ResponseLatency time.Duration
	LastSeen        time.Time
}

type ManagerInterface interface {
	UpdateNodeState(args *NodeState) error
	GetClusterState() *ClusterState
}
