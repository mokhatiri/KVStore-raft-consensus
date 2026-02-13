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
	RPCAddress  string
	HTTPAddress string

	// Health
	IsAlive         bool
	ResponseLatency time.Duration
	LastSeen        time.Time
}

type ManagerInterface interface {
	SaveEvent(args RPCEvent) error
	UpdateNodeState(args *NodeState) error
	GetClusterState() *ClusterState
	GetEvents(limit int) []RPCEvent
}

// Logger is a simple logging interface to avoid import cycles
type Logger interface {
	AddLog(level string, message string)
}
