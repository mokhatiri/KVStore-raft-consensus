package clustermanager

import (
	"distributed-kv/types"
	"os"
	"testing"
	"time"
)

func setupTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()
	dbPath := "test_events.db"
	outputDir := "test_output"

	manager := NewManager(dbPath, outputDir)
	err := manager.InitDB()
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	cleanup := func() {
		manager.db.Close()
		os.Remove(dbPath)
		os.RemoveAll(outputDir)
	}

	return manager, cleanup
}

func TestNewManager(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

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
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	manager.RegisterNode(1, "localhost:8001", "localhost:9001")
	manager.RegisterNode(2, "localhost:8002", "localhost:9002")

	state := manager.GetClusterState()
	if len(state.Nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(state.Nodes))
	}

	node1 := state.Nodes[1]
	if node1.ID != 1 {
		t.Errorf("Expected node ID 1, got %d", node1.ID)
	}
	if node1.RPCAddress != "localhost:8001" {
		t.Errorf("Expected RPC address localhost:8001, got %s", node1.RPCAddress)
	}
	if node1.HTTPAddress != "localhost:9001" {
		t.Errorf("Expected HTTP address localhost:9001, got %s", node1.HTTPAddress)
	}
}

func TestUnregisterNode(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	manager.RegisterNode(1, "localhost:8001", "localhost:9001")
	manager.RegisterNode(2, "localhost:8002", "localhost:9002")

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

func TestSaveAndGetEvents(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	events := []types.RPCEvent{
		types.NewRPCEvent(1, 2, "AppendEntries", "", 5*time.Millisecond, ""),
		types.NewRPCEvent(1, 3, "AppendEntries", "", 10*time.Millisecond, ""),
		types.NewRPCEvent(2, 1, "RequestVote", "", 3*time.Millisecond, ""),
	}

	// Start event aggregation so the channel is consumed
	manager.StartEventAggregation()

	for _, event := range events {
		err := manager.SaveEvent(event)
		if err != nil {
			t.Fatalf("Failed to save event: %v", err)
		}
	}

	// Get last 2 events
	retrieved := manager.GetEvents(2)
	if len(retrieved) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(retrieved))
	}

	// Get all events
	all := manager.GetAllEvents()
	if len(all) != 3 {
		t.Fatalf("Expected 3 events, got %d", len(all))
	}
}

func TestGetEventStats(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	manager.StartEventAggregation()

	events := []types.RPCEvent{
		types.NewRPCEvent(1, 2, "AppendEntries", "", 10*time.Millisecond, ""),
		types.NewRPCEvent(1, 3, "AppendEntries", "", 20*time.Millisecond, ""),
		types.NewRPCEvent(2, 1, "RequestVote", "", 5*time.Millisecond, ""),
		types.NewRPCEvent(1, 2, "AppendEntries", "", 15*time.Millisecond, "timeout"),
	}

	for _, event := range events {
		manager.SaveEvent(event)
	}

	stats := manager.GetEventStats()

	if stats["total_events"].(int) != 4 {
		t.Errorf("Expected 4 total events, got %v", stats["total_events"])
	}

	if stats["error_count"].(int) != 1 {
		t.Errorf("Expected 1 error, got %v", stats["error_count"])
	}

	typeCount := stats["event_types"].(map[string]int)
	if typeCount["AppendEntries"] != 3 {
		t.Errorf("Expected 3 AppendEntries events, got %d", typeCount["AppendEntries"])
	}
	if typeCount["RequestVote"] != 1 {
		t.Errorf("Expected 1 RequestVote event, got %d", typeCount["RequestVote"])
	}
}

func TestUpdateNodeState(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	manager.RegisterNode(1, "localhost:8001", "localhost:9001")

	updatedState := &types.NodeState{
		ID:          1,
		Role:        "Leader",
		Term:        5,
		CommitIndex: 10,
		LastApplied: 10,
		LogLength:   15,
		RPCAddress:  "localhost:8001",
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

func TestExportEventsToCSV(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	manager.StartEventAggregation()

	manager.SaveEvent(types.NewRPCEvent(1, 2, "AppendEntries", "test", 10*time.Millisecond, ""))
	manager.SaveEvent(types.NewRPCEvent(2, 1, "RequestVote", "vote", 5*time.Millisecond, ""))

	filename := "test_export.csv"
	defer os.Remove(filename)

	err := manager.ExportEventsToCSV(filename)
	if err != nil {
		t.Fatalf("Failed to export events: %v", err)
	}

	// Verify file exists and has content
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("Export file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("Export file is empty")
	}
}

func TestGetClusterState(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	state := manager.GetClusterState()
	if state == nil {
		t.Fatal("GetClusterState returned nil")
	}

	// Empty state initially
	if len(state.Nodes) != 0 {
		t.Errorf("Expected 0 nodes initially, got %d", len(state.Nodes))
	}

	// Register and check
	manager.RegisterNode(1, "localhost:8001", "localhost:9001")
	state = manager.GetClusterState()
	if len(state.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(state.Nodes))
	}
}

func TestConcurrentRegisterUnregister(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	done := make(chan bool)

	// Concurrently register nodes
	go func() {
		for i := 0; i < 100; i++ {
			manager.RegisterNode(i, "localhost:8000", "localhost:9000")
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
