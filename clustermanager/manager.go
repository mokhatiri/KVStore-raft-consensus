package clustermanager

import (
	"distributed-kv/types"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Manager struct {
	LogBuffer    *types.LogBuffer
	clusterState *types.ClusterState

	mu sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{
		LogBuffer:    types.NewLogBuffer(1000),
		clusterState: &types.ClusterState{Nodes: make(map[int]*types.NodeState)},
	}
}

// register a node in the cluster
func (m *Manager) RegisterNode(nodeId int, nodeHTTPAddress string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusterState.Nodes[nodeId] = &types.NodeState{
		ID:          nodeId,
		HTTPAddress: nodeHTTPAddress,
	}
}

func (m *Manager) UpdateNodeState(nodeState *types.NodeState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// update the cluster state with the new node state
	m.clusterState.Nodes[nodeState.ID] = nodeState

	// If this node is a leader, update cluster-level leader
	if nodeState.Role == "Leader" {
		m.clusterState.Leader = nodeState.ID
	}

	// Update current term to the maximum term seen
	if nodeState.Term > m.clusterState.CurrentTerm {
		m.clusterState.CurrentTerm = nodeState.Term
	}

	return nil
}

// get cluster status
func (m *Manager) GetClusterState() *types.ClusterState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clusterState
}

// unregister a node from the cluster
func (m *Manager) UnregisterNode(nodeID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clusterState.Nodes, nodeID)
}

// StartHealthCheck periodically checks if nodes are still alive
func (m *Manager) StartHealthCheck(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			m.checkNodeHealth()
		}
	}()
}

func (m *Manager) checkNodeHealth() {
	m.mu.RLock()
	nodes := make([]*types.NodeState, 0, len(m.clusterState.Nodes))
	for _, node := range m.clusterState.Nodes {
		nodes = append(nodes, node)
	}
	m.mu.RUnlock()

	for _, node := range nodes {
		go func(n *types.NodeState) {
			if err := m.fetchNodeState(n); err != nil {
				// Mark as dead if it's been down for 30+ seconds
				if time.Since(n.LastSeen) > 30*time.Second {
					n.IsAlive = false
					m.UpdateNodeState(n)
				}
			}
		}(node)
	}
}

func (m *Manager) fetchNodeState(node *types.NodeState) error {
	start := time.Now()

	url := fmt.Sprintf("http://%s/state", node.HTTPAddress)
	resp, err := http.Get(url)
	if err != nil {
		node.IsAlive = false
		return err
	}
	defer resp.Body.Close()

	node.ResponseLatency = time.Since(start)
	node.IsAlive = resp.StatusCode == http.StatusOK
	node.LastSeen = time.Now()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}

	if role, ok := data["role"].(string); ok {
		node.Role = role
	}
	if term, ok := data["term"].(float64); ok {
		node.Term = int(term)
	}
	if commitIndex, ok := data["commitIndex"].(float64); ok {
		node.CommitIndex = int(commitIndex)
	}
	if logLen, ok := data["logLength"].(float64); ok {
		node.LogLength = int(logLen)
	}

	return m.UpdateNodeState(node)
}

func (m *Manager) StartListening(interval time.Duration) {
	// call checkNodeHealth immediately and then start the periodic health check
	m.checkNodeHealth()
	m.StartHealthCheck(interval)

}
