package consensus

import (
	"distributed-kv/types"
	"os"
	"sync"
	"testing"
	"time"
)

// setupTestNode creates a node and a RaftConsensus instance using a temporary
// directory for the persister so that tests are fully isolated from each other
// and from any on-disk state left by previous runs.
func setupTestNode(id int) *types.Node {
	node := &types.Node{
		ID:        id,
		Address:   "localhost:8000",
		Peers:     map[int]string{1: "localhost:8001", 2: "localhost:8002"},
		PeersHttp: map[int]string{1: "localhost:9001", 2: "localhost:9002"},
		Role:      "Follower",
		Log:       []types.LogEntry{},
		CommitIdx: 0,
		Mu:        sync.RWMutex{},
	}
	return node
}

// setupTestRaft creates a RaftConsensus whose persister writes to an isolated
// temp directory, avoiding disk state leaking between tests.
func setupTestRaft(t *testing.T, id int) (*types.Node, *RaftConsensus) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "raft-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	node := setupTestNode(id)
	persister := types.NewPersister(id, tmpDir)

	rc := &RaftConsensus{
		currentTerm:    0,
		votedFor:       -1,
		lastApplied:    0,
		nextIndex:      make(map[int]int),
		matchIndex:     make(map[int]int),
		applyCh:        make(chan types.ApplyMsg),
		electionTimer:  time.NewTimer(GetRandomElectionTimeout()),
		heartbeatTimer: time.NewTimer(HeartbeatIntervalMs * time.Millisecond),
		node:           node,
		persister:      persister,
		logBuffer:      types.NewLogBuffer(100),
	}
	return node, rc
}

func TestRequestVoteHigherTerm(t *testing.T) {
	_, rc := setupTestRaft(t, 1)

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
	_, rc := setupTestRaft(t, 1)

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
	_, rc := setupTestRaft(t, 1)

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
	_, rc := setupTestRaft(t, 1)

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
	_, rc := setupTestRaft(t, 1)
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
	node, rc := setupTestRaft(t, 1)

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
	_, rc := setupTestRaft(t, 1)

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

/* --- New tests for Propose, getters, AppendEntries edge cases --- */

func TestProposeAsLeader(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	// Make node a leader
	rc.currentTerm = 1
	node.Mu.Lock()
	node.Role = "Leader"
	node.Mu.Unlock()

	index, term, isLeader := rc.Propose("SET:mykey:myvalue")

	if !isLeader {
		t.Errorf("Expected isLeader to be true")
	}
	if index != 1 {
		t.Errorf("Expected index 1, got %d", index)
	}
	if term != 1 {
		t.Errorf("Expected term 1, got %d", term)
	}

	// Verify log entry was appended
	node.Mu.RLock()
	logLen := len(node.Log)
	entry := node.Log[0]
	node.Mu.RUnlock()

	if logLen != 1 {
		t.Errorf("Expected 1 log entry, got %d", logLen)
	}
	if entry.Key != "mykey" {
		t.Errorf("Expected key 'mykey', got '%s'", entry.Key)
	}
	if entry.Value != "myvalue" {
		t.Errorf("Expected value 'myvalue', got '%v'", entry.Value)
	}
	if entry.Command != "SET" {
		t.Errorf("Expected command 'SET', got '%s'", entry.Command)
	}
}

func TestProposeAsFollower(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	// Node is a follower by default
	index, _, isLeader := rc.Propose("SET:key:value")

	if isLeader {
		t.Errorf("Expected isLeader to be false for follower")
	}
	if index != -1 {
		t.Errorf("Expected index -1 for non-leader, got %d", index)
	}

	// Verify no log entry was appended
	node.Mu.RLock()
	logLen := len(node.Log)
	node.Mu.RUnlock()

	if logLen != 0 {
		t.Errorf("Expected 0 log entries for non-leader proposal, got %d", logLen)
	}
}

func TestProposeMultipleEntries(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	rc.currentTerm = 1
	node.Mu.Lock()
	node.Role = "Leader"
	node.Mu.Unlock()

	rc.Propose("SET:k1:v1")
	rc.Propose("SET:k2:v2")
	index3, _, _ := rc.Propose("DELETE:k1")

	if index3 != 3 {
		t.Errorf("Expected third entry at index 3, got %d", index3)
	}

	node.Mu.RLock()
	logLen := len(node.Log)
	node.Mu.RUnlock()

	if logLen != 3 {
		t.Errorf("Expected 3 log entries, got %d", logLen)
	}
}

func TestGetNodeStatus(t *testing.T) {
	_, rc := setupTestRaft(t, 1)

	id, role, commitIdx, logLen := rc.GetNodeStatus()

	if id != 1 {
		t.Errorf("Expected node ID 1, got %d", id)
	}
	if role != "Follower" {
		t.Errorf("Expected role 'Follower', got '%s'", role)
	}
	if commitIdx != 0 {
		t.Errorf("Expected commitIdx 0, got %d", commitIdx)
	}
	if logLen != 0 {
		t.Errorf("Expected logLen 0, got %d", logLen)
	}
}

func TestGetRole(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	if rc.GetRole() != "Follower" {
		t.Errorf("Expected initial role 'Follower', got '%s'", rc.GetRole())
	}

	node.Mu.Lock()
	node.Role = "Leader"
	node.Mu.Unlock()

	if rc.GetRole() != "Leader" {
		t.Errorf("Expected role 'Leader', got '%s'", rc.GetRole())
	}
}

func TestGetNodeID(t *testing.T) {
	_, rc := setupTestRaft(t, 42)

	if rc.GetNodeID() != 42 {
		t.Errorf("Expected node ID 42, got %d", rc.GetNodeID())
	}
}

func TestAppendEntriesUpdatesCommitIndex(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	// First append some entries
	entries := []types.LogEntry{
		{Term: 1, Command: "SET", Key: "k1", Value: "v1"},
		{Term: 1, Command: "SET", Key: "k2", Value: "v2"},
	}
	err := rc.AppendEntries(1, 2, 0, 0, 0, entries)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Now send heartbeat with leaderCommit=2 to advance commit index
	err = rc.AppendEntries(1, 2, 2, 1, 2, []types.LogEntry{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	node.Mu.RLock()
	commitIdx := node.CommitIdx
	node.Mu.RUnlock()

	if commitIdx != 2 {
		t.Errorf("Expected commitIdx 2, got %d", commitIdx)
	}
}

func TestAppendEntriesStepsDownCandidate(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	// Make the node a candidate
	node.Mu.Lock()
	node.Role = "Candidate"
	node.Mu.Unlock()

	// Receive AppendEntries from a leader with same term
	err := rc.AppendEntries(1, 2, 0, 0, 0, []types.LogEntry{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	node.Mu.RLock()
	role := node.Role
	node.Mu.RUnlock()

	if role != "Follower" {
		t.Errorf("Expected candidate to step down to Follower, got '%s'", role)
	}
}

func TestRequestVoteLogCompleteness(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	// Give the node a log entry at term 2
	node.Mu.Lock()
	node.Log = append(node.Log, types.LogEntry{Term: 2, Command: "SET", Key: "k", Value: "v"})
	node.Mu.Unlock()

	// A candidate with an older log should be denied
	granted, _ := rc.RequestVote(3, 2, 1, 1) // lastLogTerm=1 is older than our term=2
	if granted {
		t.Errorf("Expected vote denied: candidate's log is less up-to-date")
	}

	// A candidate with a more up-to-date log should be granted
	granted2, _ := rc.RequestVote(3, 3, 1, 2) // lastLogTerm=2 matches
	if !granted2 {
		t.Errorf("Expected vote granted: candidate's log is at least as up-to-date")
	}
}

// ==============================
// Joint Consensus - Config Changes
// ==============================

func TestRequestAddServer(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = "Leader"
	rc.currentTerm = 1

	// Request to add server 2
	err := rc.RequestAddServer(2, "localhost:8002", "localhost:9002")
	if err != nil {
		t.Fatalf("RequestAddServer failed: %v", err)
	}

	// Check that a log entry was created with CONFIG_CHANGE command
	if len(node.Log) == 0 {
		t.Fatal("Expected log entry to be created")
	}

	lastEntry := node.Log[len(node.Log)-1]
	if lastEntry.Command != "CONFIG_CHANGE" {
		t.Errorf("Expected command 'CONFIG_CHANGE', got '%s'", lastEntry.Command)
	}

	if lastEntry.ConfigChange == nil {
		t.Fatal("Expected ConfigChange field to be set")
	}

	if lastEntry.ConfigChange.Type != "AddServer" {
		t.Errorf("Expected ConfigChange.Type 'AddServer', got '%s'", lastEntry.ConfigChange.Type)
	}

	if lastEntry.ConfigChange.NodeID != 2 {
		t.Errorf("Expected NodeID 2, got %d", lastEntry.ConfigChange.NodeID)
	}

	if lastEntry.ConfigChange.Address != "localhost:8002:localhost:9002" {
		t.Errorf("Expected Address 'localhost:8002:localhost:9002', got '%s'", lastEntry.ConfigChange.Address)
	}
}

func TestRequestAddServerNotLeader(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = "Follower" // Not leader

	err := rc.RequestAddServer(2, "localhost:8002", "localhost:9002")
	if err == nil {
		t.Fatal("Expected error when not leader")
	}
}

func TestRequestRemoveServer(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = "Leader"
	rc.currentTerm = 1

	// Request to remove server 2
	err := rc.RequestRemoveServer(2)
	if err != nil {
		t.Fatalf("RequestRemoveServer failed: %v", err)
	}

	// Check that a log entry was created with CONFIG_CHANGE command
	if len(node.Log) == 0 {
		t.Fatal("Expected log entry to be created")
	}

	lastEntry := node.Log[len(node.Log)-1]
	if lastEntry.Command != "CONFIG_CHANGE" {
		t.Errorf("Expected command 'CONFIG_CHANGE', got '%s'", lastEntry.Command)
	}

	if lastEntry.ConfigChange == nil {
		t.Fatal("Expected ConfigChange field to be set")
	}

	if lastEntry.ConfigChange.Type != "RemoveServer" {
		t.Errorf("Expected ConfigChangeType 'RemoveServer', got '%s'", lastEntry.ConfigChange.Type)
	}

	if lastEntry.ConfigChange.NodeID != 2 {
		t.Errorf("Expected NodeID 2, got %d", lastEntry.ConfigChange.NodeID)
	}
}

func TestRequestRemoveServerNotLeader(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = "Follower" // Not leader

	err := rc.RequestRemoveServer(2)
	if err == nil {
		t.Fatal("Expected error when not leader")
	}
}

func TestFinaliseConfigChange(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = "Leader"
	rc.currentTerm = 1

	// First add a server to set configState
	_ = rc.RequestAddServer(2, "localhost:8002", "localhost:9002")

	// Now finalize the config change
	err := rc.FinaliseConfigChange()
	if err != nil {
		t.Fatalf("FinaliseConfigChange failed: %v", err)
	}

	// Check that a finalize entry was created
	if len(node.Log) < 2 {
		t.Fatal("Expected at least 2 log entries (add + finalize)")
	}

	finalEntry := node.Log[len(node.Log)-1]
	if finalEntry.Command != "CONFIG_CHANGE" {
		t.Errorf("Expected command 'CONFIG_CHANGE', got '%s'", finalEntry.Command)
	}

	if finalEntry.ConfigChange == nil {
		t.Fatal("Expected ConfigChange field to be set")
	}

	if finalEntry.ConfigChange.Type != "FinalizeConfig" {
		t.Errorf("Expected ConfigChangeType 'FinalizeConfig', got '%s'", finalEntry.ConfigChange.Type)
	}
}

func TestFinaliseConfigChangeNotLeader(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = "Follower" // Not leader

	err := rc.FinaliseConfigChange()
	if err == nil {
		t.Fatal("Expected error when not leader")
	}
}
