// Package consensus implements the Raft consensus algorithm.
package consensus

import (
	"distributed-kv/types"
)

type Consensus interface {
	RequestVote(term int, candidateId int) (granted bool)
	AppendEntries(term int, leaderId int, entries []types.LogEntry) error
	Start()
}

type RaftConsensus struct {
	currentTerm int
	votedFor    int
	log         []types.LogEntry
	commitIndex int
	lastApplied int

	node *types.Node
}

func NewRaftConsensus(node *types.Node) *RaftConsensus {
	return &RaftConsensus{
		currentTerm: 0,
		votedFor:    -1,
		log:         make([]types.LogEntry, 0),
		commitIndex: 0,
		lastApplied: 0,
		node:        node,
	}
}

func (rc *RaftConsensus) RequestVote(term int, candidateId int) (granted bool) {
	// TODO: Implement RequestVote logic
	return false
}

func (rc *RaftConsensus) AppendEntries(term int, leaderId int, entries []types.LogEntry) error {
	// TODO: Implement AppendEntries logic
	return nil
}

func (rc *RaftConsensus) Start() {
	// TODO : Implement the main consensus loop
}
