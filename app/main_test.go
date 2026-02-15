package main

import (
	"distributed-kv/consensus"
	"distributed-kv/storage"
	"distributed-kv/types"
	"os"
	"testing"
	"time"
)

// TestMainIntegration tests the whole logic with multiple nodes, consensus, and storage
func TestMainIntegration(t *testing.T) {
	// Clean persisted state so tests start fresh
	os.RemoveAll("./state")
	t.Cleanup(func() { os.RemoveAll("./state") })
	// Create 3 nodes for a Raft cluster
	nodes := make([]*types.Node, 3)
	stores := make([]*storage.Store, 3)
	consensusEngines := make([]*consensus.RaftConsensus, 3)

	// Initialize nodes with peer information
	peerAddresses := []string{
		"localhost:8000",
		"localhost:8001",
		"localhost:8002",
	}

	for i := 0; i < 3; i++ {
		// Create node with maps for peers and HTTP peers
		peers := make(map[int]string)
		peersHttp := make(map[int]string)

		// Add other nodes as peers
		for j := 0; j < 3; j++ {
			if i != j {
				peers[j+1] = peerAddresses[j]
				peersHttp[j+1] = peerAddresses[j]
			}
		}

		nodes[i] = &types.Node{
			ID:        i + 1,
			Address:   peerAddresses[i],
			Peers:     peers,
			PeersHttp: peersHttp,
			Role:      "Follower",
			Log:       []types.LogEntry{},
			CommitIdx: 0,
		}

		// Create storage
		stores[i] = storage.NewStore()

		// Create consensus engine
		consensusEngines[i] = consensus.NewRaftConsensus(nodes[i], stores[i], nil)
	}

	// Test 1: Verify nodes are initialized correctly
	t.Run("NodeInitialization", func(t *testing.T) {
		for i, node := range nodes {
			if node.ID != i+1 {
				t.Errorf("Node %d has incorrect ID, got %d", i, node.ID)
			}
			if node.Role != "Follower" {
				t.Errorf("Node %d should start as Follower, got %s", i, node.Role)
			}
			if node.CommitIdx != 0 {
				t.Errorf("Node %d should have CommitIdx 0, got %d", i, node.CommitIdx)
			}
			if len(node.Peers) != 2 {
				t.Errorf("Node %d should have 2 peers, got %d", i, len(node.Peers))
			}
		}
	})

	// Test 2: Verify consensus engines are initialized correctly
	t.Run("ConsensusInitialization", func(t *testing.T) {
		for i, rc := range consensusEngines {
			if rc.GetCurrentTerm() != 0 {
				t.Errorf("Node %d consensus should start at term 0, got %d", i, rc.GetCurrentTerm())
			}
			if rc.GetVotedFor() != -1 {
				t.Errorf("Node %d should not have voted initially, got %d", i, rc.GetVotedFor())
			}
		}
	})

	// Test 3: Test RequestVote mechanism
	t.Run("RequestVoteMechanism", func(t *testing.T) {
		// Node 1 requests vote from Node 0
		voteGranted, _ := consensusEngines[0].RequestVote(1, 1, 0, 0) // term 1, candidate ID 1
		if !voteGranted {
			t.Error("Node 0 should grant vote for Node 1")
		}

		// Node 2 requests vote from Node 0 in same term
		voteGranted, _ = consensusEngines[0].RequestVote(1, 2, 0, 0)
		if voteGranted {
			t.Error("Node 0 should not grant second vote in same term")
		}

		// Node 1 requests vote from Node 0 with higher term
		voteGranted, _ = consensusEngines[0].RequestVote(2, 1, 0, 0)
		if !voteGranted {
			t.Error("Node 0 should grant vote with higher term")
		}
	})

	// Test 4: Test storage operations (Get/Set)
	t.Run("StorageOperations", func(t *testing.T) {
		store := stores[0]

		// Set a key-value pair
		store.Set("key1", "value1")
		value, exists := store.Get("key1")
		if !exists {
			t.Error("Failed to get key")
		}
		if value != "value1" {
			t.Errorf("Expected 'value1', got '%v'", value)
		}

		// Get non-existent key
		_, exists = store.Get("nonexistent")
		if exists {
			t.Error("Should not find non-existent key")
		}

		// Overwrite value
		store.Set("key1", "value2")
		value, _ = store.Get("key1")
		if value != "value2" {
			t.Errorf("Expected 'value2' after update, got '%v'", value)
		}
	})

	// Test 5: Test log replication scenario
	t.Run("LogReplication", func(t *testing.T) {
		// Simulate adding log entries to node 0 (leader)
		logEntry := types.LogEntry{
			Term:    1,
			Key:     "key1",
			Value:   "value1",
			Command: "SET",
		}

		nodes[0].Log = append(nodes[0].Log, logEntry)

		if len(nodes[0].Log) != 1 {
			t.Errorf("Node 0 should have 1 log entry, got %d", len(nodes[0].Log))
		}

		if nodes[0].Log[0].Command != "SET" || nodes[0].Log[0].Key != "key1" {
			t.Errorf("Log entry command or key mismatch")
		}
	})

	// Test 6: Test consensus state transitions
	t.Run("ConsensusStateTransitions", func(t *testing.T) {
		// Simulate election: node 1 becomes candidate
		rc := consensusEngines[1]

		// Before election: term 0
		term1 := rc.GetCurrentTerm()
		if term1 != 0 {
			t.Errorf("Node 1 should be at term 0 initially")
		}

		// Request votes (simulate election)
		voteGranted0, _ := rc.RequestVote(1, 2, 0, 0) // higher term, node 1 votes for itself
		if !voteGranted0 {
			t.Error("Node 1 should grant vote in higher term")
		}

		// After election attempt
		term2 := rc.GetCurrentTerm()
		if term2 < 1 {
			t.Errorf("Node 1 term should increase after vote request")
		}
	})

	// Test 7: Integration test - simulating cluster behavior
	t.Run("ClusterIntegration", func(t *testing.T) {
		// Simulate election process
		// All nodes get vote request from Node 0 (potential leader)
		candidateTerm := 1
		candidateID := 1

		votes := 0
		for i := 0; i < 3; i++ {
			if granted, _ := consensusEngines[i].RequestVote(candidateTerm, candidateID, 0, 0); granted {
				votes++
			}
		}

		if votes < 2 {
			t.Logf("Warning: Node %d only got %d votes (need 2 for majority)", candidateID, votes)
		} else {
			t.Logf("Node %d election successful with %d votes", candidateID, votes)
		}
	})

	// Test 8: Test multiple KV operations across stores
	t.Run("MultiNodeStorageOperations", func(t *testing.T) {
		// Set values in all node stores
		data := map[string]interface{}{
			"user:1":     "alice",
			"user:2":     "bob",
			"config:db":  "postgres://localhost",
			"config:app": "production",
		}

		for key, value := range data {
			stores[0].Set(key, value)
		}

		// Verify all keys exist
		for key, expectedValue := range data {
			value, exists := stores[0].Get(key)
			if !exists {
				t.Errorf("Key doesn't exist: %s", key)
			}
			if value != expectedValue {
				t.Errorf("Key %s: expected %v, got %v", key, expectedValue, value)
			}
		}
	})

	// Test 9: Test timing and async behavior
	t.Run("TimingBehavior", func(t *testing.T) {
		start := time.Now()

		// Simulate some operations
		for i := 0; i < 10; i++ {
			stores[0].Set("key"+string(rune(i)), "value"+string(rune(i)))
		}

		elapsed := time.Since(start)
		t.Logf("10 SET operations took %v", elapsed)

		if elapsed > 100*time.Millisecond {
			t.Logf("Warning: Storage operations slower than expected: %v", elapsed)
		}
	})

	// Test 10: Test node role consistency
	t.Run("NodeRoleConsistency", func(t *testing.T) {
		for i, node := range nodes {
			expectedRole := "Follower"
			if node.Role != expectedRole {
				t.Errorf("Node %d role mismatch: expected %s, got %s", i, expectedRole, node.Role)
			}
		}
	})

	t.Logf("Integration test completed successfully with %d nodes", len(nodes))
}

// TestNodeStartup tests the basic startup flow of a single node
func TestNodeStartup(t *testing.T) {
	node := &types.Node{
		ID:        1,
		Address:   "localhost:8000",
		Peers:     map[int]string{1: "localhost:8001"},
		PeersHttp: map[int]string{1: "localhost:9001"},
		Role:      "Follower",
		Log:       []types.LogEntry{},
		CommitIdx: 0,
	}

	store := storage.NewStore()
	rc := consensus.NewRaftConsensus(node, store, nil)

	if rc == nil {
		t.Fatal("Failed to create RaftConsensus")
	}

	if node.ID != 1 {
		t.Errorf("Expected node ID 1, got %d", node.ID)
	}

	t.Log("Node startup test passed")
}
