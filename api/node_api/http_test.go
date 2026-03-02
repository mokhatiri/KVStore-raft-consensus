package nodeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"distributed-kv/storage"
	"distributed-kv/types"
)

// Test constants to avoid duplication
const (
	contentTypeHeader     = "Content-Type"
	expectedStatus200     = "Expected 200, got %d"
	expectedStatus204     = "Expected 204, got %d"
	expectedStatus400     = "Expected 400, got %d"
	expectedStatus404     = "Expected 404, got %d"
	expectedStatus405     = "Expected 405, got %d"
	expectedStatus500     = "Expected 500, got %d"
	roleLeader            = "Leader"
	roleFollower          = "Follower"
	healthEndpoint        = "/health"
	kvEndpoint            = "/kv/"
	addServerEndpoint     = "/cluster/add-server/"
	removeServerEndpoint  = "/cluster/remove-server/"
	failedParseHealthResp = "Failed to parse health response: %v"
	failedParseAddResp    = "Failed to parse response: %v"
	jsonUnmarshalFailed   = "json.Unmarshal failed: %v"
	expectedHealthy       = "Expected status 'healthy', got '%v'"
	expectedRoleLeader    = "Expected role 'Leader', got '%v'"
	expectedRoleFollower  = "Expected role 'Follower', got '%v'"
	expectedTerm3         = "Expected term 3, got %v"
	expectedCommitIdx5    = "Expected commitIndex 5, got %v"
	expectedLogLen10      = "Expected logLength 10, got %v"
	expectedAddr9090      = "Expected addr ':9090', got '%s'"
	expectedKey2          = "Expected key 2, got %v"
	expectedRPCAddr       = "Expected rpc_address 'localhost:8002', got %v"
	expectedHTTPAddr      = "Expected http_address 'localhost:9002', got %v"
	expectedNodeID2       = "Expected nodeID 2, got %v"
	expectedStatusOK      = "http.StatusOK"
	statusBadRequest      = "http.StatusBadRequest"
	statusInternalErr     = "http.StatusInternalServerError"
	rpcAddress            = "localhost:8002"
	httpAddress           = "localhost:9002"
	invalidNodeIDMsg      = "Invalid node ID"
)

// --- Mock ConsensusModule ---

type MockConsensus struct {
	role        string
	term        int
	nodeID      int
	votedFor    int
	commitIndex int
	logLen      int
	store       *storage.Store
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
func (m *MockConsensus) GetStore() any       { return m.store }

func (m *MockConsensus) GetNodeStatus() (int, string, int, int) {
	return m.nodeID, m.role, m.commitIndex, m.logLen
}

func (m *MockConsensus) Propose(command string) (int, int, bool) {
	if m.role != "Leader" {
		return -1, m.term, false
	}
	return 1, m.term, true
}

func (m *MockConsensus) GetSnapshot() *types.Snapshot {
	return nil // No snapshot for mock
}

func (m *MockConsensus) InstallSnapshot(term int, leaderId int, lastIncludedIndex int, lastIncludedTerm int, data map[string]any) (int, error) {
	return 0, nil // Mock implementation
}

func (m *MockConsensus) RequestAddServer(nodeID int, rpcaddress string, httpaddress string) error {
	return nil // Mock implementation
}

func (m *MockConsensus) RequestRemoveServer(nodeID int) error {
	return nil // Mock implementation
}

func (m *MockConsensus) IsLeader() bool {
	return m.role == "Leader"
}

func (m *MockConsensus) GetLeader() (int, string) {
	if m.role == "Leader" {
		return m.nodeID, "localhost:9000"
	}
	return 1, "localhost:9001" // Assume node 1 is leader by default
}

// --- Helper to build a test server with mux (not listening) ---

func setupKVHandler(s *NodeServer) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
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
			http.Error(w, methodNotAllowed, http.StatusMethodNotAllowed)
		}
	}
}

func setupHealthHandler(s *NodeServer) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, methodNotAllowed, http.StatusMethodNotAllowed)
			return
		}
		if err := s.handleHealth(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}
	}
}

func setupAddServerHandler(s *NodeServer) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, methodNotAllowed, http.StatusMethodNotAllowed)
			return
		}
		if err := s.handleAddServer(w, r); err != nil {
			http.Error(w, fmt.Sprintf("Failed to add server: %v", err), http.StatusInternalServerError)
		}
	}
}

func setupRemoveServerHandler(s *NodeServer) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, methodNotAllowed, http.StatusMethodNotAllowed)
			return
		}
		if err := s.handleRemoveServer(w, r); err != nil {
			http.Error(w, fmt.Sprintf("Failed to remove server: %v", err), http.StatusInternalServerError)
		}
	}
}

func setupTestServer(mock *MockConsensus) (*NodeServer, *http.ServeMux) {
	store := storage.NewStore()
	mock.store = store
	s := NewNodeServer(mock, ":0", types.NewLogBuffer(100))

	mux := http.NewServeMux()
	mux.HandleFunc(kvEndpoint, setupKVHandler(s))
	mux.HandleFunc(healthEndpoint, setupHealthHandler(s))
	mux.HandleFunc(addServerEndpoint, setupAddServerHandler(s))
	mux.HandleFunc(removeServerEndpoint, setupRemoveServerHandler(s))

	return s, mux
}

// ==============================
// GET /kv/{key}
// ==============================

func TestGetKeyExists(t *testing.T) {
	mock := &MockConsensus{role: roleLeader, term: 1, nodeID: 1}
	s, mux := setupTestServer(mock)

	// Pre-populate the store
	store := s.consensus.GetStore().(*storage.Store)
	store.Set("greeting", "hello")

	req := httptest.NewRequest(http.MethodGet, kvEndpoint+"greeting", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf(expectedStatus200, rr.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf(failedParseAddResp, err)
	}
	if body["key"] != "greeting" {
		t.Errorf("Expected key 'greeting', got '%v'", body["key"])
	}
	if body["value"] != "hello" {
		t.Errorf("Expected value 'hello', got '%v'", body["value"])
	}
}

func TestGetKeyNotFound(t *testing.T) {
	mock := &MockConsensus{role: roleFollower, term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, kvEndpoint+"nonexistent", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d", rr.Code)
	}
}

func TestGetContentType(t *testing.T) {
	mock := &MockConsensus{role: roleLeader, term: 1, nodeID: 1}
	s, mux := setupTestServer(mock)
	store := s.consensus.GetStore().(*storage.Store)
	store.Set("key1", "val1")

	req := httptest.NewRequest(http.MethodGet, kvEndpoint+"key1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	ct := rr.Header().Get(contentTypeHeader)
	if ct != contentTypeJSON {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", ct)
	}
}

// ==============================
// POST /kv/{key}
// ==============================

func TestSetKeyAsLeader(t *testing.T) {
	mock := &MockConsensus{role: roleLeader, term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	body := `{"value": "world"}`
	req := httptest.NewRequest(http.MethodPost, kvEndpoint+"hello", strings.NewReader(body))
	req.Header.Set(contentTypeHeader, contentTypeJSON)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf(expectedStatus200, rr.Code)
	}

	var respBody map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &respBody); err != nil {
		t.Fatalf(failedParseAddResp, err)
	}
	if respBody["key"] != "hello" {
		t.Errorf("Expected key 'hello', got '%v'", respBody["key"])
	}
}

func TestSetKeyAsFollower(t *testing.T) {
	mock := &MockConsensus{role: roleFollower, term: 1, nodeID: 2}
	_, mux := setupTestServer(mock)

	body := `{"value": "world"}`
	req := httptest.NewRequest(http.MethodPost, kvEndpoint+"hello", strings.NewReader(body))
	req.Header.Set(contentTypeHeader, contentTypeJSON)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 (not leader), got %d", rr.Code)
	}
}

func TestSetKeyInvalidBody(t *testing.T) {
	mock := &MockConsensus{role: roleLeader, term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, kvEndpoint+"hello", strings.NewReader("not json"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf(expectedStatus400, rr.Code)
	}
}

func TestSetKeyEmptyBody(t *testing.T) {
	mock := &MockConsensus{role: roleLeader, term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, kvEndpoint+"hello", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf(expectedStatus400, rr.Code)
	}
}

// ==============================
// DELETE /kv/{key}
// ==============================

func TestDeleteKeyAsLeader(t *testing.T) {
	mock := &MockConsensus{role: roleLeader, term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodDelete, kvEndpoint+"somekey", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf(expectedStatus204, rr.Code)
	}
}

func TestDeleteKeyAsFollower(t *testing.T) {
	mock := &MockConsensus{role: roleFollower, term: 1, nodeID: 2}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodDelete, kvEndpoint+"somekey", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf(expectedStatus404, rr.Code)
	}
}

// ==============================
// Method Not Allowed
// ==============================

func TestKVMethodNotAllowed(t *testing.T) {
	mock := &MockConsensus{role: roleLeader, term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPatch, kvEndpoint+"hello", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf(expectedStatus405, rr.Code)
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	mock := &MockConsensus{role: roleLeader, term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, healthEndpoint, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf(expectedStatus405, rr.Code)
	}
}

// ==============================
// GET /health
// ==============================

func TestHealthEndpoint(t *testing.T) {
	mock := &MockConsensus{
		role:        roleLeader,
		term:        3,
		nodeID:      1,
		votedFor:    1,
		commitIndex: 5,
		logLen:      10,
	}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, healthEndpoint, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf(expectedStatus200, rr.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf(failedParseHealthResp, err)
	}

	if body["status"] != "healthy" {
		t.Errorf(expectedHealthy, body["status"])
	}
	if body["role"] != roleLeader {
		t.Errorf("Expected role '%s', got '%v'", roleLeader, body["role"])
	}
	// JSON numbers decode as float64
	if body["term"] != float64(3) {
		t.Errorf(expectedTerm3, body["term"])
	}
	if body["commitIndex"] != float64(5) {
		t.Errorf(expectedCommitIdx5, body["commitIndex"])
	}
	if body["logLength"] != float64(10) {
		t.Errorf(expectedLogLen10, body["logLength"])
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
		t.Fatalf(expectedStatus200, rr.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf(jsonUnmarshalFailed, err)
	}
	if body["role"] != roleFollower {
		t.Errorf("Expected role '%s', got '%v'", roleFollower, body["role"])
	}
}

func TestHealthContentType(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, healthEndpoint, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	ct := rr.Header().Get(contentTypeHeader)
	if ct != contentTypeJSON {
		t.Errorf("Expected Content-Type '%s', got '%s'", contentTypeJSON, ct)
	}
}

// ==============================
// Server lifecycle
// ==============================

func TestNewServer(t *testing.T) {
	store := storage.NewStore()
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1, store: store}
	s := NewNodeServer(mock, ":9090", types.NewLogBuffer(100))

	if s.addr != ":9090" {
		t.Errorf("Expected addr ':9090', got '%s'", s.addr)
	}
	if s.consensus.GetStore() != store {
		t.Errorf("Store reference mismatch")
	}
}

func TestServerStartStop(t *testing.T) {
	store := storage.NewStore()
	mock := &MockConsensus{role: "Follower", term: 0, nodeID: 1, store: store}
	s := NewNodeServer(mock, ":0", types.NewLogBuffer(100)) // port 0 = random available port

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
	store := s.consensus.GetStore().(*storage.Store)
	store.Set("name", "raft")

	req := httptest.NewRequest(http.MethodGet, "/kv/name", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf(expectedStatus200, rr.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf(jsonUnmarshalFailed, err)
	}
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
	store := s.consensus.GetStore().(*storage.Store)
	for _, k := range keys {
		store.Set(k, k+"_value")
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

// ==============================
// POST /cluster/add-server/{nodeID}
// ==============================

func TestAddServerAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	// Prepare request body
	reqBody := strings.NewReader(`{
		"rpc_address": "localhost:8002",
		"http_address": "localhost:9002"
	}`)

	req := httptest.NewRequest(http.MethodPost, addServerEndpoint+"2", reqBody)
	req.Header.Set(contentTypeHeader, contentTypeJSON)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf(failedParseAddResp, err)
	}

	if body["key"] != float64(2) {
		t.Errorf("Expected key 2, got %v", body["key"])
	}

	if body["rpc_address"] != "localhost:8002" {
		t.Errorf("Expected rpc_address 'localhost:8002', got %v", body["rpc_address"])
	}

	if body["http_address"] != "localhost:9002" {
		t.Errorf("Expected http_address 'localhost:9002', got %v", body["http_address"])
	}
}

func TestAddServerAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 3}
	_, mux := setupTestServer(mock)

	reqBody := strings.NewReader(`{
		"rpc_address": "localhost:8002",
		"http_address": "localhost:9002"
	}`)

	req := httptest.NewRequest(http.MethodPost, addServerEndpoint+"2", reqBody)
	req.Header.Set(contentTypeHeader, contentTypeJSON)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf(expectedStatus500, rr.Code)
	}
}

func TestAddServerInvalidNodeID(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	reqBody := strings.NewReader(`{
		"rpc_address": "localhost:8002",
		"http_address": "localhost:9002"
	}`)

	req := httptest.NewRequest(http.MethodPost, addServerEndpoint+"invalid", reqBody)
	req.Header.Set(contentTypeHeader, contentTypeJSON)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf(expectedStatus400, rr.Code)
	}
}

func TestAddServerInvalidJSON(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	reqBody := strings.NewReader(`{invalid json}`)

	req := httptest.NewRequest(http.MethodPost, addServerEndpoint+"2", reqBody)
	req.Header.Set(contentTypeHeader, contentTypeJSON)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf(expectedStatus400, rr.Code)
	}
}

func TestAddServerMethodNotAllowed(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, addServerEndpoint+"2", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf(expectedStatus405, rr.Code)
	}
}

// ==============================
// POST /cluster/remove-server/{nodeID}
// ==============================

func TestRemoveServerAsLeader(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, removeServerEndpoint+"2", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf(failedParseAddResp, err)
	}

	if body["nodeID"] != float64(2) {
		t.Errorf("Expected nodeID 2, got %v", body["nodeID"])
	}
}

func TestRemoveServerAsFollower(t *testing.T) {
	mock := &MockConsensus{role: "Follower", term: 1, nodeID: 3}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, removeServerEndpoint+"2", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf(expectedStatus500, rr.Code)
	}
}

func TestRemoveServerInvalidNodeID(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodPost, removeServerEndpoint+"invalid", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf(expectedStatus400, rr.Code)
	}
}

func TestRemoveServerMethodNotAllowed(t *testing.T) {
	mock := &MockConsensus{role: "Leader", term: 1, nodeID: 1}
	_, mux := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, removeServerEndpoint+"2", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf(expectedStatus405, rr.Code)
	}
}
