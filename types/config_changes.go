package types

import (
	"sync"
	"time"
)

type ConfigState struct {
	OldConfig *Config
	NewConfig *Config

	InTransition     bool
	JointConfigIndex int
	FinalConfigIndex int

	TransitionStartTime time.Time

	Mu sync.RWMutex
}

type Config struct {
	Nodes map[int]string // addresses
	Index int            // log index when this config was committed
}

// ConfigChangeEntry represents a membership change in the log
type ConfigChangeEntry struct {
	Type    string `json:"type"`    // "AddServer", "RemoveServer", "FinalizeConfig"
	NodeID  int    `json:"node_id"` // ID of the node being added/removed
	Address string `json:"address"` // Address of the node being added (empty for removals)
}

// RPC calls for config changes

// AddServerArgs is the RPC request to add a new node
type AddServerArgs struct {
	NodeID  int    `json:"node_id"`
	Address string `json:"address"`
	Term    int    `json:"term"`
}

// AddServerReply is the response to AddServer RPC
type AddServerReply struct {
	Term        int    `json:"term"`         // current term (for leader detection)
	LeaderID    int    `json:"leader_id"`    // current leader
	WrongLeader bool   `json:"wrong_leader"` // true if requester sent to non-leader
	Err         string `json:"err"`          // error message if any
}

// RemoveServerArgs is the RPC request to remove a node
type RemoveServerArgs struct {
	NodeID int `json:"node_id"` // ID of server to remove
	Term   int `json:"term"`    // leader's current term
}

// RemoveServerReply is the response to RemoveServer RPC
type RemoveServerReply struct {
	Term        int    `json:"term"`         // current term (for leader detection)
	LeaderID    int    `json:"leader_id"`    // current leader
	WrongLeader bool   `json:"wrong_leader"` // true if requester sent to non-leader
	Err         string `json:"err"`          // error message if any
}

// MembershipChangeResult tracks the outcome of a config change request
type MembershipChangeResult struct {
	Success   bool   `json:"success"`    // true if change was applied
	LeaderID  int    `json:"leader_id"`  // where to retry if not leader
	Err       string `json:"err"`        // error details
	AppliedAt int    `json:"applied_at"` // log index where change takes effect
}
