package managerapi

import (
	"context"
	"distributed-kv/types"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type ManagerServer struct {
	manager     types.ManagerInterface
	addr        string
	http_server *http.Server
	logger      *types.LogBuffer
}

func NewManagerServer(addr string, manager types.ManagerInterface, logger *types.LogBuffer) *ManagerServer {
	return &ManagerServer{
		manager: manager,
		addr:    addr,
		logger:  logger,
	}
}

func (s *ManagerServer) Start() {
	s.http_server = &http.Server{
		Addr:         s.addr,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	mux := http.NewServeMux()

	// cluster/status route
	mux.HandleFunc("/cluster/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.handleClusterStatus(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError) // 500 Internal Server Error if something goes wrong
		}
	})

	// /cluster/nodes
	mux.HandleFunc("/cluster/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.handleClusterNodes(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError) // 500 Internal Server Error if something goes wrong
		}
	})

	s.http_server.Handler = mux

	go func() {
		if err := s.http_server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.AddLog("ERROR", fmt.Sprintf("HTTP server ListenAndServe error: %v", err))
		}
	}()
}

func (s *ManagerServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.http_server.Shutdown(ctx); err != nil {
		s.logger.AddLog("ERROR", fmt.Sprintf("HTTP server shutdown error: %v", err))
	}
}

func (s *ManagerServer) handleClusterStatus(w http.ResponseWriter, r *http.Request) error {
	clusterState := s.manager.GetClusterState()

	aliveCount := 0
	for _, node := range clusterState.Nodes {
		if node.IsAlive {
			aliveCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{
		"leader":              clusterState.Leader,
		"currentTerm":         clusterState.CurrentTerm,
		"liveNodes":           aliveCount,
		"totalNodes":          len(clusterState.Nodes),
		"electedAt":           clusterState.ElectedAt,
		"replicationProgress": clusterState.ReplicationProgress,
	})
}

func (s *ManagerServer) handleClusterNodes(w http.ResponseWriter, r *http.Request) error {
	clusterState := s.manager.GetClusterState()

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{
		"count": len(clusterState.Nodes),
		"nodes": clusterState.Nodes,
	})
}
