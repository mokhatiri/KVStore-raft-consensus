package managercli

import (
	"distributed-kv/clustermanager"
	"distributed-kv/types"
	"os"
	"testing"
	"time"
)

func setupTestHandling(t *testing.T) (*Handling, func()) {
	t.Helper()
	dbPath := "test_cli_events.db"
	outputDir := "test_cli_output"

	manager := clustermanager.NewManager(dbPath, outputDir)
	err := manager.InitDB()
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	manager.StartEventAggregation()

	handling := NewHandling(manager)

	cleanup := func() {
		os.Remove(dbPath)
		os.RemoveAll(outputDir)
	}

	return handling, cleanup
}

func seedTestData(t *testing.T, h *Handling) {
	t.Helper()
	m := h.manager

	m.RegisterNode(1, "localhost:8001", "localhost:9001")
	m.RegisterNode(2, "localhost:8002", "localhost:9002")
	m.RegisterNode(3, "localhost:8003", "localhost:9003")

	m.UpdateNodeState(&types.NodeState{
		ID: 1, Role: "Leader", Term: 3, CommitIndex: 10, LastApplied: 10,
		LogLength: 15, RPCAddress: "localhost:8001", HTTPAddress: "localhost:9001",
		IsAlive: true, LastSeen: time.Now(),
	})
	m.UpdateNodeState(&types.NodeState{
		ID: 2, Role: "Follower", Term: 3, CommitIndex: 8, LastApplied: 8,
		LogLength: 12, RPCAddress: "localhost:8002", HTTPAddress: "localhost:9002",
		IsAlive: true, LastSeen: time.Now(),
	})
	m.UpdateNodeState(&types.NodeState{
		ID: 3, Role: "Follower", Term: 3, CommitIndex: 5, LastApplied: 5,
		LogLength: 10, RPCAddress: "localhost:8003", HTTPAddress: "localhost:9003",
		IsAlive: false, LastSeen: time.Now().Add(-30 * time.Second),
	})

	state := m.GetClusterState()
	state.Leader = 1
	state.CurrentTerm = 3
	state.ReplicationProgress = map[int]map[int]int{
		1: {2: 12, 3: 10},
	}

	m.SaveEvent(types.NewRPCEvent(1, 2, "AppendEntries", "", 5*time.Millisecond, ""))
	m.SaveEvent(types.NewRPCEvent(1, 3, "AppendEntries", "", 10*time.Millisecond, "timeout"))
	m.SaveEvent(types.NewRPCEvent(2, 1, "RequestVote", "", 3*time.Millisecond, ""))
}

func TestHandleClusterStatus_NoLeader(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	// Should not panic with no leader
	h.HandleClusterStatus()
}

func TestHandleClusterStatus_WithLeader(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	// Should not panic
	h.HandleClusterStatus()

	// Verify the state is correct
	state := h.manager.GetClusterState()

	// Count alive nodes
	aliveCount := 0
	for _, node := range state.Nodes {
		if node.IsAlive {
			aliveCount++
		}
	}

	// Should have 2 alive (nodes 1 and 2) and 1 dead (node 3)
	if aliveCount != 2 {
		t.Errorf("Expected 2 alive nodes, got %d", aliveCount)
	}
	if len(state.Nodes) != 3 {
		t.Errorf("Expected 3 total nodes, got %d", len(state.Nodes))
	}
}

func TestHandleEvents_Default(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	// Should not panic
	h.HandleEvents([]string{})
}

func TestHandleEvents_List(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleEvents([]string{"list", "2"})
}

func TestHandleEvents_Filter(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleEvents([]string{"filter", "AppendEntries"})
}

func TestHandleEvents_Stats(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleEvents([]string{"stats"})
}

func TestHandleEvents_Export(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)

	exportFile := "test_export_cli.csv"
	defer os.Remove(exportFile)

	h.HandleEvents([]string{"export", exportFile})

	if _, err := os.Stat(exportFile); os.IsNotExist(err) {
		t.Error("Export file was not created")
	}
}

func TestHandleNodes_List(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleNodes([]string{"list"})
}

func TestHandleNodes_Empty(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	h.HandleNodes([]string{"list"})
}

func TestHandleNode_Status(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleNode([]string{"status", "1"})
}

func TestHandleNode_StatusNotFound(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	h.HandleNode([]string{"status", "99"})
}

func TestHandleNode_Register(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	h.HandleNode([]string{"register", "5", "localhost:8005", "localhost:9005"})

	state := h.manager.GetClusterState()
	if _, ok := state.Nodes[5]; !ok {
		t.Error("Node 5 should have been registered")
	}
}

func TestHandleNode_Unregister(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleNode([]string{"unregister", "3"})

	state := h.manager.GetClusterState()
	if _, ok := state.Nodes[3]; ok {
		t.Error("Node 3 should have been unregistered")
	}
}

func TestHandleHealthCheck(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	// Node 3 is dead; should not panic
	h.HandleHealthCheck()
}

func TestHandleReplicationStatus(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleReplicationStatus()
}

func TestHandleReplicationStatus_NoLeader(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	// No leader set — Leader == 0
	h.HandleReplicationStatus()
}

func TestHandleLatencyStats(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleLatencyStats()
}

func TestHandleLatencyStats_NoEvents(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	h.HandleLatencyStats()
}

func TestCalculateAverage(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	tests := []struct {
		input    []int64
		expected int64
	}{
		{[]int64{10, 20, 30}, 20},
		{[]int64{5}, 5},
		{[]int64{}, 0},
		{[]int64{1, 2, 3, 4}, 2},
	}

	for _, tt := range tests {
		result := h.calculateAverage(tt.input)
		if result != tt.expected {
			t.Errorf("calculateAverage(%v) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestCalculatePercentile(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	latencies := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	p50 := h.calculatePercentile(latencies, 50)
	if p50 != 6 {
		t.Errorf("P50 = %d, want 6", p50)
	}

	p99 := h.calculatePercentile(latencies, 99)
	if p99 != 10 {
		t.Errorf("P99 = %d, want 10", p99)
	}

	empty := h.calculatePercentile([]int64{}, 50)
	if empty != 0 {
		t.Errorf("P50 of empty = %d, want 0", empty)
	}
}

func TestHandleCommand_Unknown(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	// Should not panic on unknown command
	h.HandleCommand("foobar")
}

func TestHandleCommand_Help(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	h.HandleCommand("help")
}

func TestHandleCommand_ClusterStatus(t *testing.T) {
	h, cleanup := setupTestHandling(t)
	defer cleanup()

	seedTestData(t, h)
	h.HandleCommand("cluster status")
}
