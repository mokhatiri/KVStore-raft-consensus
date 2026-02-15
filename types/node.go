package types

import (
	"sync"
)

type Node struct {
	ID        int            `json:"id"`
	Address   string         `json:"address"`    // e.g., "localhost:8080"
	Peers     map[int]string `json:"peers"`      // Map of peer node IDs to their addresses (RPC)
	PeersHttp map[int]string `json:"peers_http"` // HTTP addresses of peers for health checks
	Role      string         `json:"role"`       // Leader, Follower, Candidate
	Log       []LogEntry     `json:"log"`
	CommitIdx int            `json:"commit_index"`
	LeaderID  int            `json:"leader_id"` // ID of current leader (-1 if unknown)
	Mu        sync.RWMutex
}

func NewNode(id int, address string, peers map[int]string, peersHttp map[int]string) *Node {
	return &Node{
		ID:        id,
		Address:   address,
		Peers:     peers,
		PeersHttp: peersHttp,
		Role:      "Follower",
		Log:       []LogEntry{},
		CommitIdx: 0,
		Mu:        sync.RWMutex{},
	}
}
