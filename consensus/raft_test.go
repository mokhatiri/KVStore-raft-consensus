package consensus

import (
	"distributed-kv/types"
	"os"
	"sync"
	"testing"
	"time"
)

// Test constants to avoid duplication
const (
	localhost8001           = "localhost:8001"
	localhost8002           = "localhost:8002"
	localhost9001           = "localhost:9001"
	localhost9002           = "localhost:9002"
	localhost8004           = "localhost:8004"
	localhost9004           = "localhost:9004"
	expectedUnexpErr        = "Unexpected error: %v"
	expectedNilError        = "Expected error when not leader"
	expectedAddServer       = "RequestAddServer failed: %v"
	expectedRemoveServ      = "RequestRemoveServer failed: %v"
	expectedFinalize        = "FinalizeConfigChange failed: %v"
	expectedTerm            = "Expected term %d, got %d"
	expectedVotedFor        = "Expected votedFor to be %d, got %d"
	expectedRole            = "Expected role '%s', got '%s'"
	expectedLogLen          = "Expected %d log entries, got %d"
	expectedCommand         = "Expected command '%s', got '%s'"
	expectedConfigChangeSet = "Expected ConfigChange field to be set"
	addServerCmd            = "AddServer"
	removeServerCmd         = "RemoveServer"
	configChangeCmd         = "CONFIG_CHANGE"
	finalizeConfigCmd       = "FinalizeConfig"
	followerRole            = "Follower"
	leaderRole              = "Leader"
	candidateRole           = "Candidate"
)

// setupTestNode creates a node and a RaftConsensus instance using a temporary
// directory for the persister so that tests are fully isolated from each other
// and from any on-disk state left by previous runs.
func setupTestNode(id int) *types.Node {
	node := &types.Node{
		ID:        id,
		Address:   "localhost:8000",
		Peers:     map[int]string{1: localhost8001, 2: localhost8002},
		PeersHttp: map[int]string{1: localhost9001, 2: localhost9002},
		Role:      followerRole,
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

	// Initialize configState similar to NewRaftConsensus
	configState := &types.ConfigState{
		OldConfig:    &types.Config{Nodes: make(map[int]string), Index: 0},
		NewConfig:    nil,
		InTransition: false,
		Mu:           sync.RWMutex{},
	}
	for peerId := range node.Peers {
		configState.OldConfig.Nodes[peerId] = node.PeersHttp[peerId]
	}

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
		configState:    configState,
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

	if err := rc.AppendEntries(1, 2, 0, 0, 0, entries); err != nil {
		t.Fatalf("AppendEntries failed: %v", err)
	}

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
	node.Role = leaderRole
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
		t.Errorf(expectedCommand, "SET", entry.Command)
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
	node.Role = leaderRole
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
	if role != followerRole {
		t.Errorf(expectedRole, followerRole, role)
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

	if rc.GetRole() != followerRole {
		t.Errorf("Expected initial role '%s', got '%s'", followerRole, rc.GetRole())
	}

	node.Mu.Lock()
	node.Role = leaderRole
	node.Mu.Unlock()

	if rc.GetRole() != leaderRole {
		t.Errorf(expectedRole, leaderRole, rc.GetRole())
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
		t.Fatalf(expectedUnexpErr, err)
	}

	// Now send heartbeat with leaderCommit=2 to advance commit index
	err = rc.AppendEntries(1, 2, 2, 1, 2, []types.LogEntry{})
	if err != nil {
		t.Fatalf(expectedUnexpErr, err)
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
	node.Role = candidateRole
	node.Mu.Unlock()

	// Receive AppendEntries from a leader with same term
	err := rc.AppendEntries(1, 2, 0, 0, 0, []types.LogEntry{})
	if err != nil {
		t.Fatalf(expectedUnexpErr, err)
	}

	node.Mu.RLock()
	role := node.Role
	node.Mu.RUnlock()

	if role != followerRole {
		t.Errorf("Expected candidate to step down to %s, got '%s'", followerRole, role)
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
	node.Role = leaderRole
	rc.currentTerm = 1

	// Request to add server 4 (not in the initial peer list)
	err := rc.RequestAddServer(4, localhost8004, localhost9004)
	if err != nil {
		t.Fatalf(expectedAddServer, err)
	}

	// Check that a log entry was created with CONFIG_CHANGE command
	if len(node.Log) == 0 {
		t.Fatal("Expected log entry to be created")
	}

	lastEntry := node.Log[len(node.Log)-1]
	if lastEntry.Command != configChangeCmd {
		t.Errorf(expectedCommand, configChangeCmd, lastEntry.Command)
	}

	if lastEntry.ConfigChange == nil {
		t.Fatal(expectedConfigChangeSet)
	}

	if lastEntry.ConfigChange.Type != addServerCmd {
		t.Errorf("Expected ConfigChange.Type '%s', got '%s'", addServerCmd, lastEntry.ConfigChange.Type)
	}

	if lastEntry.ConfigChange.NodeID != 4 {
		t.Errorf("Expected NodeID 4, got %d", lastEntry.ConfigChange.NodeID)
	}

	if lastEntry.ConfigChange.Address != localhost9004 {
		t.Errorf("Expected Address '%s', got '%s'", localhost9004, lastEntry.ConfigChange.Address)
	}
}

func TestRequestAddServerNotLeader(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = followerRole // Not leader

	err := rc.RequestAddServer(2, localhost8002, localhost9002)
	if err == nil {
		t.Fatal(expectedNilError)
	}
}

func TestRequestRemoveServer(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = leaderRole
	rc.currentTerm = 1

	// Request to remove server 2
	err := rc.RequestRemoveServer(2)
	if err != nil {
		t.Fatalf(expectedRemoveServ, err)
	}

	// Check that a log entry was created with CONFIG_CHANGE command
	if len(node.Log) == 0 {
		t.Fatal("Expected log entry to be created")
	}

	lastEntry := node.Log[len(node.Log)-1]
	if lastEntry.Command != configChangeCmd {
		t.Errorf(expectedCommand, configChangeCmd, lastEntry.Command)
	}

	if lastEntry.ConfigChange == nil {
		t.Fatal(expectedConfigChangeSet)
	}

	if lastEntry.ConfigChange.Type != removeServerCmd {
		t.Errorf("Expected ConfigChangeType '%s', got '%s'", removeServerCmd, lastEntry.ConfigChange.Type)
	}

	if lastEntry.ConfigChange.NodeID != 2 {
		t.Errorf("Expected NodeID 2, got %d", lastEntry.ConfigChange.NodeID)
	}
}

func TestRequestRemoveServerNotLeader(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = followerRole // Not leader

	err := rc.RequestRemoveServer(2)
	if err == nil {
		t.Fatal(expectedNilError)
	}
}

func TestFinaliseConfigChange(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = leaderRole
	rc.currentTerm = 1

	// First add a server to set configState (using node 4 which is not in the initial config)
	err := rc.RequestAddServer(4, localhost8004, localhost9004)
	if err != nil {
		t.Fatalf(expectedAddServer, err)
	}

	// Now finalize the config change
	err = rc.FinaliseConfigChange()
	if err != nil {
		t.Fatalf(expectedFinalize, err)
	}

	// Check that a finalize entry was created
	if len(node.Log) < 2 {
		t.Fatal("Expected at least 2 log entries (add + finalize)")
	}

	finalEntry := node.Log[len(node.Log)-1]
	if finalEntry.Command != configChangeCmd {
		t.Errorf(expectedCommand, configChangeCmd, finalEntry.Command)
	}

	if finalEntry.ConfigChange == nil {
		t.Fatal(expectedConfigChangeSet)
	}

	if finalEntry.ConfigChange.Type != finalizeConfigCmd {
		t.Errorf("Expected ConfigChangeType '%s', got '%s'", finalizeConfigCmd, finalEntry.ConfigChange.Type)
	}
}

func TestFinaliseConfigChangeNotLeader(t *testing.T) {
	node, rc := setupTestRaft(t, 1)
	node.Role = followerRole // Not leader

	err := rc.FinaliseConfigChange()
	if err == nil {
		t.Fatal(expectedNilError)
	}
}
