// Package api implements the HTTP API for client requests.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

// TODO : impliment the manager

// the server struct
type Server struct {
	store       *storage.Store
	consensus   types.ConsensusModule
	addr        string
	http_server *http.Server
}

func NewServer(store *storage.Store, consensus types.ConsensusModule, addr string) *Server {
	return &Server{
		store:     store,
		consensus: consensus,
		addr:      addr,
	}
}

func (s *Server) Start() {
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

	s.http_server.Handler = mux

	go func() {
		if err := s.http_server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()
}

func (s *Server) Stop() {
	// signal the server to stop gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.http_server.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) error {
	// Handle GET request to retrieve the value for a given key.

	select {
	case <-r.Context().Done():
		return fmt.Errorf("request cancelled")
	default:
		// decode the key from the request URL
		key := r.URL.Path[len("/kv/"):]

		// fast read from the store
		// no need to check for leadership or consensus for reads, as we can serve stale reads from followers
		value, exists := s.store.Get(key)
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

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) error {
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
		if err := handlers.SetHandler(s.consensus, key, fmt.Sprintf("%v", requestBody.Value)); err != nil {
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

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) error {
	// Handle DELETE request to remove the value for a given key.

	select {
	case <-r.Context().Done():
		return fmt.Errorf("request cancelled")

	default:
		// decode the key from the request URL
		key := r.URL.Path[len("/kv/"):]

		// use delete handler
		if err := handlers.DeleteHandler(s.consensus, key); err != nil {
			return fmt.Errorf("failed to delete value: %w", err)
		}

		// write the response
		w.WriteHeader(http.StatusNoContent) // 204 No Content, successful deletion with no content to return
	}

	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) error {
	_, role, commitIndex, logLen := s.consensus.GetNodeStatus()
	term := s.consensus.GetCurrentTerm()
	votedFor := s.consensus.GetVotedFor()
	healthStatus := "healthy"

	if term == 0 && votedFor == 1 {
		healthStatus = "unhealthy"
	}

	statusCode := http.StatusOK

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
