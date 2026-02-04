package rpc_test

import (
	"distributed-kv/consensus"
	"distributed-kv/rpc"
	"distributed-kv/types"
	"net"
	netRpc "net/rpc"
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
}

func (mc *MockConsensus) RequestVote(term int, candidateId int) bool {
	mc.requestVoteCalled = true
	if term > mc.currentTerm {
		mc.currentTerm = term
		mc.lastVoteGranted = true
		return true
	}
	mc.lastVoteGranted = false
	return false
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

func (mc *MockConsensus) GetCurrentTerm() int {
	return mc.currentTerm
}

func (mc *MockConsensus) Start() {
	// Mock implementation
}

// Helper function to start a test RPC server with its own registry
func startTestServer(consensus types.ConsensusModule, address string) error {
	raftServer := rpc.NewRaftServer(consensus)

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

func TestRequestVoteRPC(t *testing.T) {
	mockConsensus := &MockConsensus{currentTerm: 1}
	address := "localhost:9001"

	// Start test server
	err := startTestServer(mockConsensus, address)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}

	// Make RPC call
	granted, term, err := rpc.SendRequestVote(address, 2, 1, 0, 0)

	if err != nil {
		t.Fatalf("SendRequestVote failed: %v", err)
	}

	if !granted {
		t.Errorf("Expected vote to be granted, but it wasn't")
	}

	if term != 2 {
		t.Errorf("Expected term 2, got %d", term)
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
		t.Fatalf("Failed to start test server: %v", err)
	}

	// Make RPC call with lower term
	granted, term, err := rpc.SendRequestVote(address, 2, 1, 0, 0)

	if err != nil {
		t.Fatalf("SendRequestVote failed: %v", err)
	}

	if granted {
		t.Errorf("Expected vote to be denied for lower term")
	}

	if term != 5 {
		t.Errorf("Expected term 5, got %d", term)
	}
}

func TestAppendEntriesRPC(t *testing.T) {
	mockConsensus := &MockConsensus{currentTerm: 1}
	address := "localhost:9003"

	// Start test server
	err := startTestServer(mockConsensus, address)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
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
		t.Errorf("Expected term 2, got %d", term)
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
		t.Fatalf("Failed to start test server: %v", err)
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
		t.Errorf("Expected term 1, got %d", term)
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
		t.Errorf("Expected term 0 on failure, got %d", term)
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
		t.Errorf("Expected term 0 on failure, got %d", term)
	}
}

func TestIntegrationRaftConsensusWithRPC(t *testing.T) {
	// Create a real RaftConsensus instance
	node := &types.Node{
		ID:        1,
		Address:   "localhost:9005",
		Peers:     []string{"localhost:9006", "localhost:9007"},
		Role:      "Follower",
		Log:       []types.LogEntry{},
		CommitIdx: 0,
	}
	raftConsensus := consensus.NewRaftConsensus(node)

	address := "localhost:9005"

	// Start test server with real consensus
	err := startTestServer(raftConsensus, address)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}

	// Test RequestVote
	granted, term, err := rpc.SendRequestVote(address, 2, 2, 0, 0)
	if err != nil {
		t.Fatalf("SendRequestVote failed: %v", err)
	}

	if !granted {
		t.Errorf("Expected vote to be granted for higher term")
	}

	if term != 2 {
		t.Errorf("Expected term 2, got %d", term)
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
		t.Errorf("Expected term 2, got %d", term)
	}
}
