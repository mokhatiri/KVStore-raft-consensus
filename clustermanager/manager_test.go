package clustermanager

import (
	"distributed-kv/types"
	"testing"
	"time"
)

func setupTestManager(t *testing.T) *Manager {
	t.Helper()

	manager := NewManager()

	return manager
}

func TestNewManager(t *testing.T) {
	manager := setupTestManager(t)

	if manager == nil {
		t.Fatal("NewManager returned nil")
	}
	if manager.clusterState == nil {
		t.Fatal("ClusterState is nil")
	}
	if manager.clusterState.Nodes == nil {
		t.Fatal("Nodes map is nil")
	}
}

func TestRegisterNode(t *testing.T) {
	manager := setupTestManager(t)

	manager.RegisterNode(1, "localhost:9001")
	manager.RegisterNode(2, "localhost:9002")

	state := manager.GetClusterState()
	if len(state.Nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(state.Nodes))
	}

	node1 := state.Nodes[1]
	if node1.ID != 1 {
		t.Errorf("Expected node ID 1, got %d", node1.ID)
	}
	if node1.HTTPAddress != "localhost:9001" {
		t.Errorf("Expected HTTP address localhost:9001, got %s", node1.HTTPAddress)
	}
}

func TestUnregisterNode(t *testing.T) {
	manager := setupTestManager(t)

	manager.RegisterNode(1, "localhost:9001")
	manager.RegisterNode(2, "localhost:9002")

	manager.UnregisterNode(1)

	state := manager.GetClusterState()
	if len(state.Nodes) != 1 {
		t.Fatalf("Expected 1 node after unregister, got %d", len(state.Nodes))
	}
	if _, ok := state.Nodes[1]; ok {
		t.Error("Node 1 should have been unregistered")
	}
	if _, ok := state.Nodes[2]; !ok {
		t.Error("Node 2 should still be registered")
	}
}

func TestUpdateNodeState(t *testing.T) {
	manager := setupTestManager(t)

	manager.RegisterNode(1, "localhost:9001")

	updatedState := &types.NodeState{
		ID:          1,
		Role:        "Leader",
		Term:        5,
		CommitIndex: 10,
		LastApplied: 10,
		LogLength:   15,
		HTTPAddress: "localhost:9001",
		IsAlive:     true,
		LastSeen:    time.Now(),
	}

	err := manager.UpdateNodeState(updatedState)
	if err != nil {
		t.Fatalf("Failed to update node state: %v", err)
	}

	state := manager.GetClusterState()
	node := state.Nodes[1]
	if node.Role != "Leader" {
		t.Errorf("Expected role Leader, got %s", node.Role)
	}
	if node.Term != 5 {
		t.Errorf("Expected term 5, got %d", node.Term)
	}
	if !node.IsAlive {
		t.Error("Expected node to be alive")
	}
}

func TestGetClusterState(t *testing.T) {
	manager := setupTestManager(t)

	state := manager.GetClusterState()
	if state == nil {
		t.Fatal("GetClusterState returned nil")
	}

	// Empty state initially
	if len(state.Nodes) != 0 {
		t.Errorf("Expected 0 nodes initially, got %d", len(state.Nodes))
	}

	// Register and check
	manager.RegisterNode(1, "localhost:9001")
	state = manager.GetClusterState()
	if len(state.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(state.Nodes))
	}
}

func TestConcurrentRegisterUnregister(t *testing.T) {
	manager := setupTestManager(t)

	done := make(chan bool)

	// Concurrently register nodes
	go func() {
		for i := 0; i < 100; i++ {
			manager.RegisterNode(i, "localhost:9000")
		}
		done <- true
	}()

	// Concurrently unregister nodes
	go func() {
		for i := 0; i < 50; i++ {
			manager.UnregisterNode(i)
		}
		done <- true
	}()

	<-done
	<-done
	// If we get here without a panic, the locks are working
}
