package handlers

import (
	"strings"
	"testing"

	"distributed-kv/storage"
	"distributed-kv/types"
)

// --- Mock ConsensusModule ---

type MockConsensus struct {
	role     string
	term     int
	nodeID   int
	proposed []string // track proposed commands
	store    *storage.Store
}

func (m *MockConsensus) RequestVote(term int, candidateId int, lastLogIndex int, lastLogTerm int) (bool, int) {
	return false, m.term
}

func (m *MockConsensus) AppendEntries(term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []types.LogEntry) error {
	return nil
}

func (m *MockConsensus) GetCurrentTerm() int          { return m.term }
func (m *MockConsensus) GetNodeID() int               { return m.nodeID }
func (m *MockConsensus) GetVotedFor() int             { return -1 }
func (m *MockConsensus) GetRole() string              { return m.role }
func (m *MockConsensus) GetStore() any                { return m.store }
func (m *MockConsensus) GetSnapshot() *types.Snapshot { return nil }
func (m *MockConsensus) InstallSnapshot(term int, leaderId int, lastIncludedIndex int, lastIncludedTerm int, data map[string]any) (int, error) {
	return 0, nil
}
func (m *MockConsensus) RequestAddServer(nodeID int, rpcaddress string, httpaddress string) error {
	return nil
}
func (m *MockConsensus) RequestRemoveServer(nodeID int) error { return nil }

func (m *MockConsensus) GetNodeStatus() (int, string, int, int) {
	return m.nodeID, m.role, 0, 0
}

func (m *MockConsensus) Propose(command string) (int, int, bool) {
	if m.role != "Leader" {
		return -1, m.term, false
	}
	m.proposed = append(m.proposed, command)
	return len(m.proposed), m.term, true
}

func (m *MockConsensus) IsLeader() bool {
	return m.role == "Leader"
}

func (m *MockConsensus) GetLeader() (int, string) {
	if m.role == "Leader" {
		return m.nodeID, "localhost:9000"
	}
	return 1, "localhost:9001" // Assume node 1 is leader by default
}

// ==============================
// SetHandler
// ==============================

func TestSetHandlerAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1, store: storage.NewStore()}

	_, err := SetHandler(mock, "name", "alice")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(mock.proposed) != 1 {
		t.Fatalf("Expected 1 proposed command, got %d", len(mock.proposed))
	}
	expected := "SET:name:alice"
	if mock.proposed[0] != expected {
		t.Errorf("Expected proposed command '%s', got '%s'", expected, mock.proposed[0])
	}
}

func TestSetHandlerAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 2, store: storage.NewStore()}

	_, err := SetHandler(mock, "name", "alice")
	if err == nil {
		t.Fatal("Expected error for non-leader, got nil")
	}

	if len(mock.proposed) != 0 {
		t.Errorf("Expected no proposed commands for follower, got %d", len(mock.proposed))
	}
}

func TestSetHandlerAsCandidate(t *testing.T) {
	mock := &MockConsensus{role: "Candidate", term: 1, nodeID: 1, store: storage.NewStore()}

	_, err := SetHandler(mock, "key", "value")
	if err == nil {
		t.Fatal("Expected error for candidate, got nil")
	}
}

// ==============================
// DeleteHandler
// ==============================

func TestDeleteHandlerAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 2, nodeID: 1, store: storage.NewStore()}

	_, err := DeleteHandler(mock, "mykey")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(mock.proposed) != 1 {
		t.Fatalf("Expected 1 proposed command, got %d", len(mock.proposed))
	}
	expected := "DELETE:mykey"
	if mock.proposed[0] != expected {
		t.Errorf("Expected proposed command '%s', got '%s'", expected, mock.proposed[0])
	}
}

func TestDeleteHandlerAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 2, store: storage.NewStore()}

	_, err := DeleteHandler(mock, "mykey")
	if err == nil {
		t.Fatal("Expected error for non-leader, got nil")
	}

	if len(mock.proposed) != 0 {
		t.Errorf("Expected no proposed commands for follower, got %d", len(mock.proposed))
	}
}

// ==============================
// CleanHandler
// ==============================

func TestCleanHandlerAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1, store: storage.NewStore()}

	_, err := CleanHandler(mock)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(mock.proposed) != 1 {
		t.Fatalf("Expected 1 proposed command, got %d", len(mock.proposed))
	}
	if mock.proposed[0] != "CLEAN" {
		t.Errorf("Expected proposed command 'CLEAN', got '%s'", mock.proposed[0])
	}
}

func TestCleanHandlerAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 3, store: storage.NewStore()}

	_, err := CleanHandler(mock)
	if err == nil {
		t.Fatal("Expected error for non-leader, got nil")
	}
}

// ==============================
// Multiple operations
// ==============================

func TestMultipleProposals(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}

	_, _ = SetHandler(mock, "k1", "v1")
	_, _ = SetHandler(mock, "k2", "v2")
	_, _ = DeleteHandler(mock, "k1")
	_, _ = CleanHandler(mock)

	if len(mock.proposed) != 4 {
		t.Fatalf("Expected 4 proposed commands, got %d", len(mock.proposed))
	}

	expectedCmds := []string{"SET:k1:v1", "SET:k2:v2", "DELETE:k1", "CLEAN"}
	for i, expected := range expectedCmds {
		if mock.proposed[i] != expected {
			t.Errorf("Command %d: expected '%s', got '%s'", i, expected, mock.proposed[i])
		}
	}
}

// ==============================
// AddServerHandler
// ==============================

func TestAddServerHandlerAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1, store: storage.NewStore()}

	msg, err := AddServerHandler(mock, 2, "localhost:9002", "localhost:8002")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if msg == "" {
		t.Fatal("Expected success message")
	}

	if !strings.Contains(msg, "node 2") {
		t.Errorf("Expected message to contain 'node 2', got: %s", msg)
	}
}

func TestAddServerHandlerAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 3, store: storage.NewStore()}

	_, err := AddServerHandler(mock, 2, "localhost:9002", "localhost:8002")
	if err == nil {
		t.Fatal("Expected error for non-leader, got nil")
	}

	if !strings.Contains(err.Error(), "leader") {
		t.Errorf("Expected error to mention 'leader', got: %v", err)
	}
}

func TestAddServerHandlerAsCandidate(t *testing.T) {
	mock := &MockConsensus{role: "Candidate", term: 1, nodeID: 1, store: storage.NewStore()}

	_, err := AddServerHandler(mock, 2, "localhost:9002", "localhost:8002")
	if err == nil {
		t.Fatal("Expected error for non-leader candidate, got nil")
	}
}

// ==============================
// RemoveServerHandler
// ==============================

func TestRemoveServerHandlerAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1, store: storage.NewStore()}

	msg, err := RemoveServerHandler(mock, 2)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if msg == "" {
		t.Fatal("Expected success message")
	}

	if !strings.Contains(msg, "node 2") {
		t.Errorf("Expected message to contain 'node 2', got: %s", msg)
	}
}

func TestRemoveServerHandlerAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 3, store: storage.NewStore()}

	_, err := RemoveServerHandler(mock, 2)
	if err == nil {
		t.Fatal("Expected error for non-leader, got nil")
	}

	if !strings.Contains(err.Error(), "leader") {
		t.Errorf("Expected error to mention 'leader', got: %v", err)
	}
}

func TestRemoveServerHandlerAsCandidate(t *testing.T) {
	mock := &MockConsensus{role: "Candidate", term: 1, nodeID: 1, store: storage.NewStore()}

	_, err := RemoveServerHandler(mock, 2)
	if err == nil {
		t.Fatal("Expected error for non-leader candidate, got nil")
	}
}
