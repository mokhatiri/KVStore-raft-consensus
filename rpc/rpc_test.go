package rpc_test

import (
	"distributed-kv/consensus"
	"distributed-kv/rpc"
	"distributed-kv/storage"
	"distributed-kv/types"
	"net"
	netRpc "net/rpc"
	"os"
	"testing"
	"time"
)

// MockConsensus implements types.ConsensusModule for testing
type MockConsensus struct {
	requestVoteCalled   bool
	appendEntriesCalled bool
	currentTerm         int
	lastVoteGranted     bool
	lastAppendSucceeded bool
	store               any
}

func (mc *MockConsensus) GetRole() string {
	return "Follower" // Mock role
}

func (mc *MockConsensus) GetNodeStatus() (int, string, int, int) {
	return mc.currentTerm, "Follower", 0, 0 // Mock status
}

func (mc *MockConsensus) GetVotedFor() int {
	if mc.lastVoteGranted {
		return 1 // Mock candidate ID that was voted for
	}
	return -1 // No vote granted
}

func (mc *MockConsensus) GetStore() any {
	return mc.store
}

func (mc *MockConsensus) GetSnapshot() *types.Snapshot {
	return nil // No snapshot for mock
}

func (mc *MockConsensus) InstallSnapshot(term int, leaderId int, lastIncludedIndex int, lastIncludedTerm int, data map[string]any) (int, error) {
	return 0, nil // Mock implementation
}

func (mc *MockConsensus) RequestVote(term int, candidateId int, lastLogIndex int, lastLogTerm int) (bool, int) {
	mc.requestVoteCalled = true
	if term > mc.currentTerm {
		mc.currentTerm = term
		mc.lastVoteGranted = true
		return true, term
	}
	mc.lastVoteGranted = false
	return false, mc.currentTerm
}

func (mc *MockConsensus) AppendEntries(term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []types.LogEntry) error {
	mc.appendEntriesCalled = true
	if term > mc.currentTerm {
		mc.currentTerm = term
		mc.lastAppendSucceeded = true
		return nil
	}
	mc.lastAppendSucceeded = false
	return nil
}

func (mc *MockConsensus) Propose(command string) (index int, term int, isLeader bool) {
	// Mock implementation
	return 0, mc.currentTerm, false
}

func (mc *MockConsensus) RequestAddServer(nodeID int, rpcaddress string, httpaddress string) error {
	// Mock implementation
	return nil
}

func (mc *MockConsensus) RequestRemoveServer(nodeID int) error {
	// Mock implementation
	return nil
}

func (mc *MockConsensus) IsLeader() bool {
	return false // Default is not a leader
}

func (mc *MockConsensus) GetLeader() (int, string) {
	return 1, "localhost:9001" // Default leader
}

func (mc *MockConsensus) GetCurrentTerm() int {
	return mc.currentTerm
}

func (mc *MockConsensus) GetNodeID() int {
	return 1 // Mock node ID
}

func (mc *MockConsensus) Start() {
	// Mock implementation
}

// Helper function to start a test RPC server with its own registry
func startTestServer(consensus types.ConsensusModule, address string) error {
	raftServer := rpc.NewRaftServer(consensus, 1) // Use nodeID 1 for testing

	// Create a new RPC server instance to avoid conflicts with global registry
	server := netRpc.NewServer()
	err := server.Register(raftServer)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	go server.Accept(listener)
	time.Sleep(100 * time.Millisecond) // Give server time to start
	return nil
}

var expected_term = "Expected term %d, got %d"
var send_request_vote_failed = "SendRequestVote failed: %v"
var failed_to_start_server = "Failed to start test server: %v"

func TestRequestVoteRPC(t *testing.T) {
	mockConsensus := &MockConsensus{currentTerm: 1}
	address := "localhost:9001"

	// Start test server
	err := startTestServer(mockConsensus, address)
	if err != nil {
		t.Fatalf(failed_to_start_server, err)
	}

	// Make RPC call
	granted, term, err := rpc.SendRequestVote(address, 2, 1, 0, 0)

	if err != nil {
		t.Fatalf(send_request_vote_failed, err)
	}

	if !granted {
		t.Errorf("Expected vote to be granted, but it wasn't")
	}

	if term != 2 {
		t.Errorf(expected_term, 2, term)
	}

	if !mockConsensus.requestVoteCalled {
		t.Errorf("Expected RequestVote to be called on consensus")
	}
}

func TestRequestVoteRPCLowerTerm(t *testing.T) {
	mockConsensus := &MockConsensus{currentTerm: 5}
	address := "localhost:9002"

	// Start test server
	err := startTestServer(mockConsensus, address)
	if err != nil {
		t.Fatalf(failed_to_start_server, err)
	}

	// Make RPC call with lower term
	granted, term, err := rpc.SendRequestVote(address, 2, 1, 0, 0)

	if err != nil {
		t.Fatalf(send_request_vote_failed, err)
	}

	if granted {
		t.Errorf("Expected vote to be denied for lower term")
	}

	if term != 5 {
		t.Errorf(expected_term, 5, term)
	}
}

func TestAppendEntriesRPC(t *testing.T) {
	mockConsensus := &MockConsensus{currentTerm: 1}
	address := "localhost:9003"

	// Start test server
	err := startTestServer(mockConsensus, address)
	if err != nil {
		t.Fatalf(failed_to_start_server, err)
	}

	entries := []types.LogEntry{
		{
			Term:    2,
			Command: "SET",
			Key:     "key1",
			Value:   "value1",
		},
	}

	// Make RPC call
	success, term, err := rpc.SendAppendEntries(address, 2, 1, 0, 0, 0, entries)

	if err != nil {
		t.Fatalf("SendAppendEntries failed: %v", err)
	}

	if !success {
		t.Errorf("Expected AppendEntries to succeed")
	}

	if term != 2 {
		t.Errorf(expected_term, 2, term)
	}

	if !mockConsensus.appendEntriesCalled {
		t.Errorf("Expected AppendEntries to be called on consensus")
	}
}

func TestAppendEntriesHeartbeat(t *testing.T) {
	mockConsensus := &MockConsensus{currentTerm: 1}
	address := "localhost:9004"

	// Start test server
	err := startTestServer(mockConsensus, address)
	if err != nil {
		t.Fatalf(failed_to_start_server, err)
	}

	// Send heartbeat (empty entries)
	success, term, err := rpc.SendAppendEntries(address, 1, 1, 0, 0, 0, []types.LogEntry{})

	if err != nil {
		t.Fatalf("SendAppendEntries heartbeat failed: %v", err)
	}

	if !success {
		t.Errorf("Expected heartbeat to succeed")
	}

	if term != 1 {
		t.Errorf(expected_term, 1, term)
	}
}

func TestRequestVoteConnectionFailure(t *testing.T) {
	// Try to connect to non-existent server
	granted, term, err := rpc.SendRequestVote("localhost:9999", 1, 1, 0, 0)

	if err == nil {
		t.Errorf("Expected connection error, but got none")
	}

	if granted {
		t.Errorf("Expected vote to be denied on connection failure")
	}

	if term != 0 {
		t.Errorf(expected_term+" (on failure) ", 0, term)
	}
}

func TestAppendEntriesConnectionFailure(t *testing.T) {
	// Try to connect to non-existent server
	success, term, err := rpc.SendAppendEntries("localhost:9998", 1, 1, 0, 0, 0, []types.LogEntry{})

	if err == nil {
		t.Errorf("Expected connection error, but got none")
	}

	if success {
		t.Errorf("Expected AppendEntries to fail on connection error")
	}

	if term != 0 {
		t.Errorf(expected_term+" (on failure) ", 0, term)
	}
}

func TestIntegrationRaftConsensusWithRPC(t *testing.T) {
	// Clean persisted state so the test starts fresh
	os.RemoveAll("./state")
	t.Cleanup(func() { os.RemoveAll("./state") })

	// Create a real RaftConsensus instance
	peers := make(map[int]string)
	peers[2] = "localhost:9006"
	peers[3] = "localhost:9007"

	peersHttp := make(map[int]string)
	peersHttp[2] = "localhost:9006"
	peersHttp[3] = "localhost:9007"

	node := &types.Node{
		ID:        1,
		Address:   "localhost:9005",
		Peers:     peers,
		PeersHttp: peersHttp,
		Role:      "Follower",
		Log:       []types.LogEntry{},
		CommitIdx: 0,
	}
	store := storage.NewStore()
	raftConsensus := consensus.NewRaftConsensus(node, store, nil)

	address := "localhost:9005"

	// Start test server with real consensus
	err := startTestServer(raftConsensus, address)
	if err != nil {
		t.Fatalf(failed_to_start_server, err)
	}

	// Test RequestVote
	granted, term, err := rpc.SendRequestVote(address, 2, 2, 0, 0)
	if err != nil {
		t.Fatalf(send_request_vote_failed, err)
	}

	if !granted {
		t.Errorf("Expected vote to be granted for higher term")
	}

	if term != 2 {
		t.Errorf(expected_term, 2, term)
	}

	// Test AppendEntries
	entries := []types.LogEntry{
		{
			Term:    2,
			Command: "SET",
			Key:     "testkey",
			Value:   "testvalue",
		},
	}

	success, term, err := rpc.SendAppendEntries(address, 2, 2, 0, 0, 0, entries)
	if err != nil {
		t.Fatalf("SendAppendEntries failed: %v", err)
	}

	if !success {
		t.Errorf("Expected AppendEntries to succeed")
	}

	if term != 2 {
		t.Errorf(expected_term, 2, term)
	}
}
