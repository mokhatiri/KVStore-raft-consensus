package consensus

import (
	"distributed-kv/storage"
	"distributed-kv/types"
	"testing"
)

func setupTestNode(id int) (*types.Node, *storage.Store) {
	store := storage.NewStore()
	node := &types.Node{
		ID:        id,
		Address:   "localhost:8000",
		Peers:     []string{"localhost:8001", "localhost:8002"},
		Role:      "Follower",
		Log:       []types.LogEntry{},
		CommitIdx: 0,
	}
	return node, store
}

func TestRequestVoteHigherTerm(t *testing.T) {
	node, store := setupTestNode(1)
	rc := NewRaftConsensus(node, store)

	// Candidate from term 2 requests vote
	granted := rc.RequestVote(2, 2)

	if !granted {
		t.Errorf("Expected vote to be granted for higher term, but it wasn't")
	}

	if rc.currentTerm != 2 {
		t.Errorf("Expected currentTerm to be 2, got %d", rc.currentTerm)
	}

	if rc.votedFor != 2 {
		t.Errorf("Expected votedFor to be 2, got %d", rc.votedFor)
	}
}

func TestRequestVoteSameTerm(t *testing.T) {
	node, store := setupTestNode(1)
	rc := NewRaftConsensus(node, store)

	// First vote in term 1
	granted1 := rc.RequestVote(1, 2)
	if !granted1 {
		t.Errorf("Expected first vote to be granted")
	}

	// Second vote in term 1 from same candidate
	granted2 := rc.RequestVote(1, 2)
	if !granted2 {
		t.Errorf("Expected second vote from same candidate to be granted")
	}

	// Third vote in term 1 from different candidate
	granted3 := rc.RequestVote(1, 3)
	if granted3 {
		t.Errorf("Expected vote for different candidate in same term to be denied")
	}
}

func TestRequestVoteLowerTerm(t *testing.T) {
	node, store := setupTestNode(1)
	rc := NewRaftConsensus(node, store)

	// Set current term to 3
	rc.currentTerm = 3

	// Request vote with lower term
	granted := rc.RequestVote(2, 2)

	if granted {
		t.Errorf("Expected vote to be denied for lower term")
	}

	if rc.currentTerm != 3 {
		t.Errorf("Expected currentTerm to remain 3, got %d", rc.currentTerm)
	}
}

func TestAppendEntriesHigherTerm(t *testing.T) {
	node, store := setupTestNode(1)
	rc := NewRaftConsensus(node, store)

	entries := []types.LogEntry{
		{
			Term:    2,
			Command: "SET",
			Key:     "key1",
			Value:   "value1",
		},
	}

	err := rc.AppendEntries(2, 2, entries)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if rc.currentTerm != 2 {
		t.Errorf("Expected currentTerm to be 2, got %d", rc.currentTerm)
	}

	if rc.votedFor != -1 {
		t.Errorf("Expected votedFor to be reset to -1, got %d", rc.votedFor)
	}
}

func TestAppendEntriesLowerTerm(t *testing.T) {
	node, store := setupTestNode(1)
	rc := NewRaftConsensus(node, store)
	rc.currentTerm = 3

	entries := []types.LogEntry{}

	err := rc.AppendEntries(2, 2, entries)

	if err == nil {
		t.Errorf("Expected error for lower term, got nil")
	}

	if rc.currentTerm != 3 {
		t.Errorf("Expected currentTerm to remain 3, got %d", rc.currentTerm)
	}
}

func TestAppendEntriesAppendsToLog(t *testing.T) {
	node, store := setupTestNode(1)
	rc := NewRaftConsensus(node, store)

	entries := []types.LogEntry{
		{
			Term:    1,
			Command: "SET",
			Key:     "key1",
			Value:   "value1",
		},
	}

	rc.AppendEntries(1, 2, entries)

	// Check if entries were appended to node.Log
	node.Mu.RLock()
	logLen := len(node.Log)
	node.Mu.RUnlock()

	if logLen != 1 {
		t.Errorf("Expected 1 entry in log, got %d", logLen)
	}
}

func TestRaftConsensusInitialization(t *testing.T) {
	node, store := setupTestNode(1)
	rc := NewRaftConsensus(node, store)

	if rc.currentTerm != 0 {
		t.Errorf("Expected currentTerm to be 0, got %d", rc.currentTerm)
	}

	if rc.votedFor != -1 {
		t.Errorf("Expected votedFor to be -1, got %d", rc.votedFor)
	}

	if rc.lastApplied != 0 {
		t.Errorf("Expected lastApplied to be 0, got %d", rc.lastApplied)
	}
}
