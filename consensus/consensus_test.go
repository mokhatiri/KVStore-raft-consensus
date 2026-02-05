package consensus

import (
	"distributed-kv/types"
	"testing"
)

func setupTestNode(id int) *types.Node {
	node := &types.Node{
		ID:        id,
		Address:   "localhost:8000",
		Peers:     []string{"localhost:8001", "localhost:8002"},
		Role:      "Follower",
		Log:       []types.LogEntry{},
		CommitIdx: 0,
	}
	return node
}

func TestRequestVoteHigherTerm(t *testing.T) {
	node := setupTestNode(1)
	rc := NewRaftConsensus(node)

	// Candidate from term 2 requests vote
	granted, _ := rc.RequestVote(2, 2, 0, 0)

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
	node := setupTestNode(1)
	rc := NewRaftConsensus(node)

	// First vote in term 1
	granted1, _ := rc.RequestVote(1, 2, 0, 0)
	if !granted1 {
		t.Errorf("Expected first vote to be granted")
	}

	// Second vote in term 1 from same candidate
	granted2, _ := rc.RequestVote(1, 2, 0, 0)
	if !granted2 {
		t.Errorf("Expected second vote from same candidate to be granted")
	}

	// Third vote in term 1 from different candidate
	granted3, _ := rc.RequestVote(1, 3, 0, 0)
	if granted3 {
		t.Errorf("Expected vote for different candidate in same term to be denied")
	}
}

func TestRequestVoteLowerTerm(t *testing.T) {
	node := setupTestNode(1)
	rc := NewRaftConsensus(node)

	// Set current term to 3
	rc.currentTerm = 3

	// Request vote with lower term
	granted, _ := rc.RequestVote(2, 2, 0, 0)

	if granted {
		t.Errorf("Expected vote to be denied for lower term")
	}

	if rc.currentTerm != 3 {
		t.Errorf("Expected currentTerm to remain 3, got %d", rc.currentTerm)
	}
}

func TestAppendEntriesHigherTerm(t *testing.T) {
	node := setupTestNode(1)
	rc := NewRaftConsensus(node)

	entries := []types.LogEntry{
		{
			Term:    2,
			Command: "SET",
			Key:     "key1",
			Value:   "value1",
		},
	}

	err := rc.AppendEntries(2, 2, 0, 0, 0, entries)

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
	node := setupTestNode(1)
	rc := NewRaftConsensus(node)
	rc.currentTerm = 3

	entries := []types.LogEntry{}

	err := rc.AppendEntries(2, 2, 0, 0, 0, entries)

	if err == nil {
		t.Errorf("Expected error for lower term, got nil")
	}

	if rc.currentTerm != 3 {
		t.Errorf("Expected currentTerm to remain 3, got %d", rc.currentTerm)
	}
}

func TestAppendEntriesAppendsToLog(t *testing.T) {
	node := setupTestNode(1)
	rc := NewRaftConsensus(node)

	entries := []types.LogEntry{
		{
			Term:    1,
			Command: "SET",
			Key:     "key1",
			Value:   "value1",
		},
	}

	rc.AppendEntries(1, 2, 0, 0, 0, entries)

	// Check if entries were appended to node.Log
	node.Mu.RLock()
	logLen := len(node.Log)
	node.Mu.RUnlock()

	if logLen != 1 {
		t.Errorf("Expected 1 entry in log, got %d", logLen)
	}
}

func TestRaftConsensusInitialization(t *testing.T) {
	node := setupTestNode(1)
	rc := NewRaftConsensus(node)

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
