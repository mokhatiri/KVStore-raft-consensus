package types

import "sync"

type Node struct {
	ID        int        `json:"id"`
	Address   string     `json:"address"` // e.g., "localhost:8080"
	Peers     []string   `json:"peers"`
	Role      string     `json:"role"` // Leader, Follower, Candidate
	Log       []LogEntry `json:"log"`
	CommitIdx int        `json:"commit_index"`
	mu        sync.RWMutex
}
