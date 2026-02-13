package nodeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"distributed-kv/clustermanager"
	"distributed-kv/storage"
	"distributed-kv/types"
)

// --- Mock ConsensusModule ---

type MockConsensus struct {
	role        string
	term        int
	nodeID      int
	votedFor    int
	commitIndex int
	logLen      int
	proposeErr  bool
}

func (m *MockConsensus) RequestVote(term int, candidateId int, lastLogIndex int, lastLogTerm int) (bool, int) {
	return false, m.term
}

func (m *MockConsensus) AppendEntries(term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []types.LogEntry) error {
	return nil
}

func (m *MockConsensus) GetCurrentTerm() int { return m.term }
func (m *MockConsensus) GetNodeID() int      { return m.nodeID }
func (m *MockConsensus) GetVotedFor() int    { return m.votedFor }
func (m *MockConsensus) GetRole() string     { return m.role }

func (m *MockConsensus) GetNodeStatus() (int, string, int, int) {
	return m.nodeID, m.role, m.commitIndex, m.logLen
}

func (m *MockConsensus) Propose(command string) (int, int, bool) {
	if m.role != "Leader" {
		return -1, m.term, false
	}
	return 1, m.term, true
}

func (m *MockConsensus) EmitRPCEvent(event types.RPCEvent) {}

// --- Helper to build a test server with mux (not listening) ---

func setupTestServer(mock *MockConsensus) (*NodeServer, *http.ServeMux) {
	store := storage.NewStore()
	s := NewNodeServer(store, mock, ":0", clustermanager.NewLogBuffer(100))

	mux := http.NewServeMux()
	mux.HandleFunc("/kv/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if err := s.handleGet(w, r); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
			}
		case http.MethodPost:
			if err := s.handleSet(w, r); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
		case http.MethodDelete:
			if err := s.handleDelete(w, r); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
			}
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.handleHealth(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}
	})

	return s, mux
}

// ==============================
// GET /kv/{key}
// ==============================

func TestGetKeyExists(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	s, mux := setupTestServer(mock)

	// Pre-populate the store
	s.store.Set("greeting", "hello")

	req := httptest.NewRequest(http.MethodGet, "/kv/greeting", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if body["key"] != "greeting" {
		t.Errorf("Expected key 'greeting', got '%v'", body["key"])
	}
	if body["value"] != "hello" {
		t.Errorf("Expected value 'hello', got '%v'", body["value"])
	}
}

func TestGetKeyNotFound(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, "/kv/nonexistent", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d", rr.Code)
	}
}

func TestGetContentType(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	s, mux := setupTestServer(mock)
	s.store.Set("key1", "val1")

	req := httptest.NewRequest(http.MethodGet, "/kv/key1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", ct)
	}
}

// ==============================
// POST /kv/{key}
// ==============================

func TestSetKeyAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	body := `{"value": "world"}`
	req := httptest.NewRequest(http.MethodPost, "/kv/hello", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var respBody map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if respBody["key"] != "hello" {
		t.Errorf("Expected key 'hello', got '%v'", respBody["key"])
	}
}

func TestSetKeyAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 2}
	_, mux := setupTestServer(mock)

	body := `{"value": "world"}`
	req := httptest.NewRequest(http.MethodPost, "/kv/hello", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 (not leader), got %d", rr.Code)
	}
}

func TestSetKeyInvalidBody(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, "/kv/hello", strings.NewReader("not json"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for invalid body, got %d", rr.Code)
	}
}

func TestSetKeyEmptyBody(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, "/kv/hello", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for empty body, got %d", rr.Code)
	}
}

// ==============================
// DELETE /kv/{key}
// ==============================

func TestDeleteKeyAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodDelete, "/kv/somekey", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteKeyAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 2}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodDelete, "/kv/somekey", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 (not leader goes through error path), got %d", rr.Code)
	}
}

// ==============================
// Method Not Allowed
// ==============================

func TestKVMethodNotAllowed(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPatch, "/kv/hello", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 405, got %d", rr.Code)
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 405, got %d", rr.Code)
	}
}

// ==============================
// GET /health
// ==============================

func TestHealthEndpoint(t *testing.T) {
	mock := &MockConsensus{
		role:        "Leader",
		term:        3,
		nodeID:      1,
		votedFor:    1,
		commitIndex: 5,
		logLen:      10,
	}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	if body["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%v'", body["status"])
	}
	if body["role"] != "Leader" {
		t.Errorf("Expected role 'Leader', got '%v'", body["role"])
	}
	// JSON numbers decode as float64
	if body["term"] != float64(3) {
		t.Errorf("Expected term 3, got %v", body["term"])
	}
	if body["commitIndex"] != float64(5) {
		t.Errorf("Expected commitIndex 5, got %v", body["commitIndex"])
	}
	if body["logLength"] != float64(10) {
		t.Errorf("Expected logLength 10, got %v", body["logLength"])
	}
}

func TestHealthEndpointFollowerStatus(t *testing.T) {
	mock := &MockConsensus{
		role:     "Follower",
		term:     2,
		nodeID:   2,
		votedFor: 1,
	}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	var body map[string]any
	json.Unmarshal(rr.Body.Bytes(), &body)
	if body["role"] != "Follower" {
		t.Errorf("Expected role 'Follower', got '%v'", body["role"])
	}
}

func TestHealthContentType(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", ct)
	}
}

// ==============================
// Server lifecycle
// ==============================

func TestNewServer(t *testing.T) {
	store := storage.NewStore()
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	s := NewNodeServer(store, mock, ":9090", clustermanager.NewLogBuffer(100))

	if s.addr != ":9090" {
		t.Errorf("Expected addr ':9090', got '%s'", s.addr)
	}
	if s.store != store {
		t.Errorf("Store reference mismatch")
	}
}

func TestServerStartStop(t *testing.T) {
	store := storage.NewStore()
	mock := &MockConsensus{role: "Follower", term: 0, nodeID: 1}
	s := NewNodeServer(store, mock, ":0", clustermanager.NewLogBuffer(100)) // port 0 = random available port

	s.Start()
	defer s.Stop()

	// Verify the http_server was created
	if s.http_server == nil {
		t.Fatal("Expected http_server to be initialized after Start()")
	}
}

// ==============================
// Multiple operations flow
// ==============================

func TestGetAfterSet(t *testing.T) {
	// Even though SET goes through consensus (Propose), the store is populated
	// for reads independently. This tests the full GET read path with pre-populated data.
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	s, mux := setupTestServer(mock)

	// Simulate data that has been committed and applied to the store
	s.store.Set("name", "raft")

	req := httptest.NewRequest(http.MethodGet, "/kv/name", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	var body map[string]any
	json.Unmarshal(rr.Body.Bytes(), &body)
	if body["value"] != "raft" {
		t.Errorf("Expected 'raft', got '%v'", body["value"])
	}
}

func TestMultipleKeyOperations(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	s, mux := setupTestServer(mock)

	// Set several keys
	keys := []string{"a", "b", "c"}
	for _, k := range keys {
		body := fmt.Sprintf(`{"value": "%s_value"}`, k)
		req := httptest.NewRequest(http.MethodPost, "/kv/"+k, strings.NewReader(body))
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("POST /kv/%s: Expected 200, got %d", k, rr.Code)
		}
	}

	// Populate store to simulate committed data for reads
	for _, k := range keys {
		s.store.Set(k, k+"_value")
	}

	// GET each key
	for _, k := range keys {
		req := httptest.NewRequest(http.MethodGet, "/kv/"+k, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /kv/%s: Expected 200, got %d", k, rr.Code)
		}
	}

	// DELETE one key (goes through consensus)
	delReq := httptest.NewRequest(http.MethodDelete, "/kv/b", nil)
	delRR := httptest.NewRecorder()
	mux.ServeHTTP(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("DELETE /kv/b: Expected 204, got %d", delRR.Code)
	}
}
