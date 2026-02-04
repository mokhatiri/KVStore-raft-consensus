package types

import (
	"strconv"
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

func (n *Node) PrintInfo() string {
	n.Mu.RLock()
	defer n.Mu.RUnlock()
	info := "Node ID: " + strconv.Itoa(n.ID) + "\n"
	info += "Address: " + n.Address + "\n"
	info += "Role: " + n.Role + "\n"
	info += "Peers: " + strconv.Itoa(len(n.Peers)) + "\n"
	info += "Log Length: " + strconv.Itoa(len(n.Log)) + "\n"
	info += "Commit Index: " + strconv.Itoa(n.CommitIdx) + "\n"
	return info
}
