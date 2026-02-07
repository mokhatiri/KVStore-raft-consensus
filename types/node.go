package types

import (
	"sync"
)

type Node struct {
	ID        int        `json:"id"`
	Address   string     `json:"address"` // e.g., "localhost:8080"
	Peers     []string   `json:"peers"`
	Role      string     `json:"role"` // Leader, Follower, Candidate
	Log       []LogEntry `json:"log"`
	CommitIdx int        `json:"commit_index"`
	Mu        sync.RWMutex
}

func NewNode(id int, address string, peers []string) *Node {
	return &Node{
		ID:        id,
		Address:   address,
		Peers:     peers,
		Role:      "Follower",
		Log:       []LogEntry{},
		CommitIdx: 0,
		Mu:        sync.RWMutex{},
	}
}
