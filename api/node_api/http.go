// Package api implements the HTTP API for client requests.
package nodeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"distributed-kv/handlers"
	"distributed-kv/storage"
	"distributed-kv/types"
)

// OPEN API SPECIFICATION:
/*
1. GET /kv/{key} - Retrieve the value for a given key.
   - Response: 200 OK with the value in the body, or 404 Not Found if the key does not exist.
2. POST /kv/{key} - Set the value for a given key.
	- Request Body: JSON object with a "value" field.
	- Response: 200 OK if the value is set successfully, or 400 Bad Request if the request is invalid.
3. DELETE /kv/{key} - Remove the value for a given key.
	- Response: 200 OK if the key is deleted successfully, or 404 Not Found if the key does not exist.
4. GET /health - Check the health of the server.
	- Response: 200 OK if the server is healthy, or 503 Service Unavailable if the server is not healthy.
*/

// the server struct
type NodeServer struct {
	consensus   types.ConsensusModule
	addr        string
	http_server *http.Server
	logBuffer   *types.LogBuffer
}

func NewNodeServer(consensus types.ConsensusModule, addr string, logBuffer *types.LogBuffer) *NodeServer {
	return &NodeServer{
		consensus: consensus,
		addr:      addr,
		logBuffer: logBuffer,
	}
}

func (s *NodeServer) Start() {
	s.http_server = &http.Server{
		Addr:         s.addr,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// set up the HTTP handlers for the API routes
	mux := http.NewServeMux()

	// Start the HTTP server and handle routes for GET, SET, DELETE, and health check.
	mux.HandleFunc("/kv/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// if it's a GET, check if it's a health check or a key retrieval
			if err := s.handleGet(w, r); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound) // 404 Not Found if the key does not exist
			}
		case http.MethodPost:
			if err := s.handleSet(w, r); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest) // 400 Bad Request if the request is invalid
			}
		case http.MethodDelete:
			if err := s.handleDelete(w, r); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound) // 404 Not Found if the key does not exist
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
			http.Error(w, err.Error(), http.StatusServiceUnavailable) // 503 Service Unavailable if the server is not healthy
		}
	})

	// Observability endpoints for manager HTTP polling
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleState(w, r)
	})

	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePeers(w, r)
	})

	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleLogs(w, r)
	})

	mux.HandleFunc("/raftinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRaftInfo(w, r)
	})

	mux.HandleFunc("/cluster/add-server/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleAddServer(w, r)
	})

	mux.HandleFunc("/cluster/remove-server/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRemoveServer(w, r)
	})

	s.http_server.Handler = mux

	go func() {
		if err := s.http_server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logBuffer.AddLog("ERROR", fmt.Sprintf("HTTP server error: %v", err))
		}
	}()
}

func (s *NodeServer) Stop() {
	// signal the server to stop gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.http_server.Shutdown(ctx); err != nil {
		s.logBuffer.AddLog("ERROR", fmt.Sprintf("HTTP server shutdown error: %v", err))
	}
}

func (s *NodeServer) handleGet(w http.ResponseWriter, r *http.Request) error {
	// Handle GET request to retrieve the value for a given key.

	select {
	case <-r.Context().Done():
		return fmt.Errorf("request cancelled")
	default:
		// decode the key from the request URL
		key := r.URL.Path[len("/kv/"):]

		// fast read from the store
		// no need to check for leadership or consensus for reads, as we can serve stale reads from followers
		store := s.consensus.GetStore().(*storage.Store)
		value, exists := store.Get(key)
		if !exists {
			return fmt.Errorf("key not found")
		}

		// to send the value
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"key":   key,
			"value": value,
		}); err != nil {
			return fmt.Errorf("failed to encode response body: %w", err)
		}
	}

	return nil
}

func (s *NodeServer) handleAddServer(w http.ResponseWriter, r *http.Request) error {
	// Handle POST request to add a new server to the cluster.

	select {
	case <-r.Context().Done():
		return fmt.Errorf("request cancelled")

	default:
		node := r.URL.Path[len("/cluster/add-server/"):]
		// the key to int
		var key int
		_, err := fmt.Sscanf(node, "%d", &key)
		if err != nil {
			http.Error(w, "Invalid node ID", http.StatusBadRequest)
			return fmt.Errorf("invalid node ID: %w", err)
		}

		// decode the value from the request body
		var requestBody struct {
			RPCAddress  string `json:"rpc_address"`
			HTTPAddress string `json:"http_address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			http.Error(w, "Failed to decode request body", http.StatusBadRequest)
			return fmt.Errorf("failed to decode request body: %w", err)
		}

		// use the add server handler to add the new server to the cluster

		if _, err := handlers.AddServerHandler(s.consensus, key, requestBody.HTTPAddress, requestBody.RPCAddress); err != nil {
			http.Error(w, "Failed to add server", http.StatusInternalServerError)
			return fmt.Errorf("failed to add server: %w", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"key":          key,
			"rpc_address":  requestBody.RPCAddress,
			"http_address": requestBody.HTTPAddress,
		}); err != nil {
			return fmt.Errorf("failed to encode response body: %w", err)
		}

		return nil
	}
}

func (s *NodeServer) handleRemoveServer(w http.ResponseWriter, r *http.Request) error {
	// Handle POST request to remove a server from the cluster.

	select {
	case <-r.Context().Done():
		return fmt.Errorf("request cancelled")

	default:
		nodeID := r.URL.Path[len("/cluster/remove-server/"):]
		var id int
		_, err := fmt.Sscanf(nodeID, "%d", &id)
		if err != nil {
			http.Error(w, "Invalid node ID", http.StatusBadRequest)
			return fmt.Errorf("invalid node ID: %w", err)
		}

		// use the remove server handler to remove the server from the cluster
		if _, err := handlers.RemoveServerHandler(s.consensus, id); err != nil {
			http.Error(w, "Failed to remove server", http.StatusInternalServerError)
			return fmt.Errorf("failed to remove server: %w", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"nodeID": id,
		}); err != nil {
			return fmt.Errorf("failed to encode response body: %w", err)
		}

		return nil
	}
}

func (s *NodeServer) handleSet(w http.ResponseWriter, r *http.Request) error {
	// Handle POST request to set the value for a given key.

	select {
	case <-r.Context().Done():
		return fmt.Errorf("request cancelled")
	default:
		key := r.URL.Path[len("/kv/"):]

		// decode the value from the request body
		var requestBody struct {
			Value any `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			return fmt.Errorf("failed to decode request body: %w", err)
		}

		// use set handler
		if _, err := handlers.SetHandler(s.consensus, key, fmt.Sprintf("%v", requestBody.Value)); err != nil {
			return fmt.Errorf("failed to set value: %w", err)
		}

		// write the response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"key":   key,
			"value": requestBody.Value,
		}); err != nil {
			return fmt.Errorf("failed to encode response body: %w", err)
		}
	}

	return nil
}

func (s *NodeServer) handleDelete(w http.ResponseWriter, r *http.Request) error {
	// Handle DELETE request to remove the value for a given key.

	select {
	case <-r.Context().Done():
		return fmt.Errorf("request cancelled")

	default:
		// decode the key from the request URL
		key := r.URL.Path[len("/kv/"):]

		// use delete handler
		if _, err := handlers.DeleteHandler(s.consensus, key); err != nil {
			return fmt.Errorf("failed to delete value: %w", err)
		}

		// write the response
		w.WriteHeader(http.StatusNoContent) // 204 No Content, successful deletion with no content to return
	}

	return nil
}

func (s *NodeServer) handleHealth(w http.ResponseWriter, r *http.Request) error {
	_, role, commitIndex, logLen := s.consensus.GetNodeStatus()
	term := s.consensus.GetCurrentTerm()
	votedFor := s.consensus.GetVotedFor()
	healthStatus := "healthy"

	// A node is unhealthy if it has no role or is stuck at term 0 with no vote
	if role == "" || (term == 0 && votedFor == -1) {
		healthStatus = "unhealthy"
	}

	statusCode := http.StatusOK
	if healthStatus == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	json.NewEncoder(w).Encode(map[string]any{
		"status":      healthStatus,
		"role":        role,
		"term":        term,
		"votedFor":    votedFor,
		"commitIndex": commitIndex,
		"logLength":   logLen,
	})

	return nil
}

// handleState returns the current node state (used by manager for polling)
func (s *NodeServer) handleState(w http.ResponseWriter, r *http.Request) {
	nodeID, role, commitIndex, logLen := s.consensus.GetNodeStatus()
	term := s.consensus.GetCurrentTerm()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"id":          nodeID,
		"role":        role,
		"term":        term,
		"commitIndex": commitIndex,
		"logLength":   logLen,
		"isAlive":     true,
		"lastSeen":    time.Now(),
	})
}

// handlePeers returns the list of peers (used by manager to discover cluster topology)
func (s *NodeServer) handlePeers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"peers": []map[string]any{},
	})
}

// handleLogs returns recent node logs (used by manager for debugging)
func (s *NodeServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 20 // default limit
	if limitStr != "" {
		if l, err := parseIntQuery(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	logs := s.logBuffer.GetLogs(limit)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"count": len(logs),
		"logs":  logs,
	})
}

// handleRaftInfo returns Raft consensus information
func (s *NodeServer) handleRaftInfo(w http.ResponseWriter, r *http.Request) {
	nodeID, role, commitIndex, logLen := s.consensus.GetNodeStatus()
	term := s.consensus.GetCurrentTerm()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"nodeID":       nodeID,
		"term":         term,
		"role":         role,
		"commitIndex":  commitIndex,
		"lastLogIndex": logLen,
	})
}

// parseIntQuery parses a query string parameter as an integer
func parseIntQuery(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
