package managerapi

import (
	"distributed-kv/types"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Mock Logger ---
type MockLogger struct{}

func (l *MockLogger) AddLog(level string, message string) {}

// --- Mock Manager ---

type MockManager struct {
	clusterState *types.ClusterState
	events       []types.RPCEvent
}

func newMockManager() *MockManager {
	return &MockManager{
		clusterState: &types.ClusterState{
			Leader:      1,
			CurrentTerm: 3,
			Nodes: map[int]*types.NodeState{
				1: {ID: 1, Role: "Leader", Term: 3, RPCAddress: "localhost:8001", HTTPAddress: "localhost:9001", IsAlive: true, LastSeen: time.Now()},
				2: {ID: 2, Role: "Follower", Term: 3, RPCAddress: "localhost:8002", HTTPAddress: "localhost:9002", IsAlive: true, LastSeen: time.Now()},
			},
			ReplicationProgress: map[int]map[int]int{
				1: {2: 5},
			},
		},
		events: []types.RPCEvent{
			types.NewRPCEvent(1, 2, "AppendEntries", "", 5*time.Millisecond, ""),
			types.NewRPCEvent(2, 1, "RequestVote", "", 3*time.Millisecond, ""),
		},
	}
}

func (m *MockManager) SaveEvent(args types.RPCEvent) error {
	m.events = append(m.events, args)
	return nil
}

func (m *MockManager) UpdateNodeState(args *types.NodeState) error {
	m.clusterState.Nodes[args.ID] = args
	return nil
}

func (m *MockManager) GetClusterState() *types.ClusterState {
	return m.clusterState
}

func (m *MockManager) GetEvents(limit int) []types.RPCEvent {
	if limit > len(m.events) {
		limit = len(m.events)
	}
	return m.events[len(m.events)-limit:]
}

// --- Tests ---

func TestHandleClusterStatus(t *testing.T) {
	mock := newMockManager()
	server := NewManagerServer(":0", mock, &MockLogger{})

	req := httptest.NewRequest(http.MethodGet, "/cluster/status", nil)
	w := httptest.NewRecorder()

	err := server.handleClusterStatus(w, req)
	if err != nil {
		t.Fatalf("handleClusterStatus returned error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result["leader"].(float64) != 1 {
		t.Errorf("Expected leader 1, got %v", result["leader"])
	}
	if result["currentTerm"].(float64) != 3 {
		t.Errorf("Expected term 3, got %v", result["currentTerm"])
	}
	if result["liveNodes"].(float64) != 2 {
		t.Errorf("Expected 2 live nodes, got %v", result["liveNodes"])
	}
	if result["totalNodes"].(float64) != 2 {
		t.Errorf("Expected 2 total nodes, got %v", result["totalNodes"])
	}
}

func TestHandleClusterEvents(t *testing.T) {
	mock := newMockManager()
	server := NewManagerServer(":0", mock, &MockLogger{})

	t.Run("default limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/cluster/events", nil)
		w := httptest.NewRecorder()

		err := server.handleClusterEvents(w, req)
		if err != nil {
			t.Fatalf("handleClusterEvents returned error: %v", err)
		}

		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["count"].(float64) != 2 {
			t.Errorf("Expected 2 events, got %v", result["count"])
		}
	})

	t.Run("with limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/cluster/events?limit=1", nil)
		w := httptest.NewRecorder()

		err := server.handleClusterEvents(w, req)
		if err != nil {
			t.Fatalf("handleClusterEvents returned error: %v", err)
		}

		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["count"].(float64) != 1 {
			t.Errorf("Expected 1 event, got %v", result["count"])
		}
	})
}

func TestHandleClusterNodes(t *testing.T) {
	mock := newMockManager()
	server := NewManagerServer(":0", mock, &MockLogger{})

	req := httptest.NewRequest(http.MethodGet, "/cluster/nodes", nil)
	w := httptest.NewRecorder()

	err := server.handleClusterNodes(w, req)
	if err != nil {
		t.Fatalf("handleClusterNodes returned error: %v", err)
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["count"].(float64) != 2 {
		t.Errorf("Expected 2 nodes, got %v", result["count"])
	}
}

func TestMethodNotAllowed(t *testing.T) {
	mock := newMockManager()
	server := NewManagerServer(":0", mock, &MockLogger{})
	server.Start()
	defer server.Stop()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// The mux handlers check for GET only — test with POST
	req := httptest.NewRequest(http.MethodPost, "/cluster/status", nil)
	w := httptest.NewRecorder()

	// Access the handler directly through the mux
	server.http_server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for POST, got %d", w.Code)
	}
}
