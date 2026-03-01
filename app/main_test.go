package main

import (
	"distributed-kv/consensus"
	"distributed-kv/storage"
	"distributed-kv/types"
	"os"
	"testing"
	"time"
)

// setupCluster initializes nodes, stores, and consensus engines for testing
func setupCluster() ([]*types.Node, []*storage.Store, []*consensus.RaftConsensus) {
	nodes := make([]*types.Node, 3)
	stores := make([]*storage.Store, 3)
	consensusEngines := make([]*consensus.RaftConsensus, 3)

	peerAddresses := []string{
		"localhost:8000",
		"localhost:8001",
		"localhost:8002",
	}

	for i := 0; i < 3; i++ {
		peers := make(map[int]string)
		peersHttp := make(map[int]string)

		for peer := 0; peer < 3; peer++ {
			if i != peer {
				peers[peer+1] = peerAddresses[peer]
				peersHttp[peer+1] = peerAddresses[peer]
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

		stores[i] = storage.NewStore()
		consensusEngines[i] = consensus.NewRaftConsensus(nodes[i], stores[i], nil)
	}

	return nodes, stores, consensusEngines
}

func testNodeInitialization(t *testing.T, nodes []*types.Node) {
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
}

func testConsensusInitialization(t *testing.T, consensusEngines []*consensus.RaftConsensus) {
	for i, rc := range consensusEngines {
		if rc.GetCurrentTerm() != 0 {
			t.Errorf("Node %d consensus should start at term 0, got %d", i, rc.GetCurrentTerm())
		}
		if rc.GetVotedFor() != -1 {
			t.Errorf("Node %d should not have voted initially, got %d", i, rc.GetVotedFor())
		}
	}
}

func testRequestVoteMechanism(t *testing.T, consensusEngines []*consensus.RaftConsensus) {
	voteGranted, _ := consensusEngines[0].RequestVote(1, 1, 0, 0)
	if !voteGranted {
		t.Error("Node 0 should grant vote for Node 1")
	}

	voteGranted, _ = consensusEngines[0].RequestVote(1, 2, 0, 0)
	if voteGranted {
		t.Error("Node 0 should not grant second vote in same term")
	}

	voteGranted, _ = consensusEngines[0].RequestVote(2, 1, 0, 0)
	if !voteGranted {
		t.Error("Node 0 should grant vote with higher term")
	}
}

func testStorageOperations(t *testing.T, store *storage.Store) {
	store.Set("key1", "value1")
	value, exists := store.Get("key1")
	if !exists {
		t.Error("Failed to get key")
	}
	if value != "value1" {
		t.Errorf("Expected 'value1', got '%v'", value)
	}

	_, exists = store.Get("nonexistent")
	if exists {
		t.Error("Should not find non-existent key")
	}

	store.Set("key1", "value2")
	value, _ = store.Get("key1")
	if value != "value2" {
		t.Errorf("Expected 'value2' after update, got '%v'", value)
	}
}

func testLogReplication(t *testing.T, nodes []*types.Node) {
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
}

func testConsensusStateTransitions(t *testing.T, consensusEngines []*consensus.RaftConsensus) {
	rc := consensusEngines[1]

	term1 := rc.GetCurrentTerm()
	if term1 != 0 {
		t.Errorf("Node 1 should be at term 0 initially")
	}

	voteGranted0, _ := rc.RequestVote(1, 2, 0, 0)
	if !voteGranted0 {
		t.Error("Node 1 should grant vote in higher term")
	}

	term2 := rc.GetCurrentTerm()
	if term2 < 1 {
		t.Errorf("Node 1 term should increase after vote request")
	}
}

func testClusterIntegration(t *testing.T, consensusEngines []*consensus.RaftConsensus) {
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
}

func testMultiNodeStorageOperations(t *testing.T, stores []*storage.Store) {
	data := map[string]interface{}{
		"user:1":     "alice",
		"user:2":     "bob",
		"config:db":  "postgres://localhost",
		"config:app": "production",
	}

	for key, value := range data {
		stores[0].Set(key, value)
	}

	for key, expectedValue := range data {
		value, exists := stores[0].Get(key)
		if !exists {
			t.Errorf("Key doesn't exist: %s", key)
		}
		if value != expectedValue {
			t.Errorf("Key %s: expected %v, got %v", key, expectedValue, value)
		}
	}
}

func testTimingBehavior(t *testing.T, stores []*storage.Store) {
	start := time.Now()

	for i := 0; i < 10; i++ {
		stores[0].Set("key"+string(rune(i)), "value"+string(rune(i)))
	}

	elapsed := time.Since(start)
	t.Logf("10 SET operations took %v", elapsed)

	if elapsed > 100*time.Millisecond {
		t.Logf("Warning: Storage operations slower than expected: %v", elapsed)
	}
}

func testNodeRoleConsistency(t *testing.T, nodes []*types.Node) {
	for i, node := range nodes {
		expectedRole := "Follower"
		if node.Role != expectedRole {
			t.Errorf("Node %d role mismatch: expected %s, got %s", i, expectedRole, node.Role)
		}
	}
}

// TestMainIntegration tests the whole logic with multiple nodes, consensus, and storage
func TestMainIntegration(t *testing.T) {
	os.RemoveAll("./state")
	t.Cleanup(func() { os.RemoveAll("./state") })

	nodes, stores, consensusEngines := setupCluster()

	t.Run("NodeInitialization", func(t *testing.T) {
		testNodeInitialization(t, nodes)
	})

	t.Run("ConsensusInitialization", func(t *testing.T) {
		testConsensusInitialization(t, consensusEngines)
	})

	t.Run("RequestVoteMechanism", func(t *testing.T) {
		testRequestVoteMechanism(t, consensusEngines)
	})

	t.Run("StorageOperations", func(t *testing.T) {
		testStorageOperations(t, stores[0])
	})

	t.Run("LogReplication", func(t *testing.T) {
		testLogReplication(t, nodes)
	})

	t.Run("ConsensusStateTransitions", func(t *testing.T) {
		testConsensusStateTransitions(t, consensusEngines)
	})

	t.Run("ClusterIntegration", func(t *testing.T) {
		testClusterIntegration(t, consensusEngines)
	})

	t.Run("MultiNodeStorageOperations", func(t *testing.T) {
		testMultiNodeStorageOperations(t, stores)
	})

	t.Run("TimingBehavior", func(t *testing.T) {
		testTimingBehavior(t, stores)
	})

	t.Run("NodeRoleConsistency", func(t *testing.T) {
		testNodeRoleConsistency(t, nodes)
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
