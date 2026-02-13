package clustermanager

import (
	"database/sql"
	managerapi "distributed-kv/api/manager_api"
	"distributed-kv/types"
	"encoding/csv"
	"fmt"
	"log"
	"net/rpc"
	"os"
	"strconv"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Manager struct {
	eventBuffer *EventBuffer
	logBuffer   *LogBuffer
	db          *sql.DB
	fileWriter  *EventFileWriter
	eventsCh    chan types.RPCEvent

	clusterState *types.ClusterState

	mu sync.RWMutex
}

func NewManager(dbPath string, outputDir string) *Manager {
	db, err := sql.Open("sqlite3", dbPath)

	if err != nil {
		panic(fmt.Sprintf("Failed to open database: %v", err))
	}

	eventsCh := make(chan types.RPCEvent, EventBufferSize)
	logBuffer := NewLogBuffer(1000)

	return &Manager{
		eventBuffer:  NewEventBuffer(EventBufferSize),
		logBuffer:    logBuffer,
		db:           db,
		fileWriter:   NewEventFileWriter(outputDir, eventsCh, logBuffer),
		eventsCh:     eventsCh,
		clusterState: &types.ClusterState{Nodes: make(map[int]*types.NodeState)},
	}
}

// initialize the db
func (m *Manager) InitDB() error {
	_, err := m.db.Exec(`
	CREATE TABLE IF NOT EXISTS rpc_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		from_id INTEGER,
		to_id INTEGER,
		event_type TEXT,
		details TEXT,
		duration INTEGER,
		error TEXT
	);
	`)

	return err
}

// register a node in the cluster and subscribe to its events
func (m *Manager) RegisterNode(nodeId int, nodeRPCAddress string, nodeHTTPAddress string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusterState.Nodes[nodeId] = &types.NodeState{
		ID:          nodeId,
		RPCAddress:  nodeRPCAddress,
		HTTPAddress: nodeHTTPAddress,
	}
}

func (m *Manager) StartEventAggregation() {
	// start the file writer goroutine
	m.fileWriter.Start()
}

// save event to backends
func (m *Manager) SaveEvent(event types.RPCEvent) error {
	// Skip heartbeat events - don't add to buffer, but still save to DB and file
	if !event.IsHeartbeat {
		// save to in-memory buffer only for meaningful events
		m.eventBuffer.AddEvent(event)
		m.logBuffer.AddLog("DEBUG", fmt.Sprintf("Event saved: From=%d, To=%d, Type=%s, IsHeartbeat=%v", event.From, event.To, event.Type, event.IsHeartbeat))
	}

	// Always save to sqlite db (for full history)
	_, err := m.db.Exec(`
	INSERT INTO rpc_events (from_id, to_id, event_type, details, duration, error)
	VALUES (?, ?, ?, ?, ?, ?);
	`, event.From, event.To, event.Type, event.Details, event.Duration.Milliseconds(), event.Error)

	if err != nil {
		m.logBuffer.AddLog("ERROR", fmt.Sprintf("Error saving event to database: %v", err))
	}

	// Always send to file writer (for full event log)
	m.eventsCh <- event

	return nil
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
	return m.clusterState
}

// get events (heartbeats already filtered from buffer)
func (m *Manager) GetEvents(limit int) []types.RPCEvent {
	return m.eventBuffer.GetLast(limit)
}

// get all events from the buffer
func (m *Manager) GetAllEvents() []types.RPCEvent {
	return m.eventBuffer.GetAllEvents()
}

// get event statistics (heartbeats already filtered from buffer)
func (m *Manager) GetEventStats() map[string]interface{} {
	events := m.eventBuffer.GetAllEvents()
	stats := make(map[string]interface{})

	typeCount := make(map[string]int)
	errorCount := 0
	totalDuration := int64(0)

	for _, event := range events {
		typeCount[event.Type]++
		totalDuration += event.Duration.Milliseconds()
		if event.Error != "" {
			errorCount++
		}
	}

	stats["total_events"] = len(events)
	stats["event_types"] = typeCount
	stats["error_count"] = errorCount
	stats["avg_duration_ms"] = int64(0)
	if len(events) > 0 {
		stats["avg_duration_ms"] = totalDuration / int64(len(events))
	}

	return stats
}

// get logs
func (m *Manager) GetLogs(limit int) []LogMessage {
	return m.logBuffer.GetLogs(limit)
}

// add log entry
func (m *Manager) AddLog(level string, message string) {
	m.logBuffer.AddLog(level, message)
}

// get log buffer reference
func (m *Manager) GetLogBuffer() *LogBuffer {
	return m.logBuffer
}

// export events to CSV format (heartbeats already filtered from buffer)
func (m *Manager) ExportEventsToCSV(filename string) error {
	events := m.eventBuffer.GetAllEvents()

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	if err := writer.Write([]string{"Timestamp", "From", "To", "Type", "Details", "Duration(ms)", "Error"}); err != nil {
		return fmt.Errorf("failed to write header: %v", err)
	}

	// Write events
	for _, event := range events {
		details := fmt.Sprintf("%v", event.Details)
		row := []string{
			event.Timestamp.String(),
			strconv.Itoa(event.From),
			strconv.Itoa(event.To),
			event.Type,
			details,
			strconv.Itoa(int(event.Duration.Milliseconds())),
			event.Error,
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("failed to write row: %v", err)
		}
	}

	return nil
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
			if m.pingNode(n) {
				// Node responded — mark as alive
				n.IsAlive = true
				n.LastSeen = time.Now()
				m.UpdateNodeState(n)
			} else {
				// Node didn't respond — mark as dead if it's been down for 30+ seconds
				if time.Since(n.LastSeen) > 30*time.Second {
					n.IsAlive = false
					m.UpdateNodeState(n)
				}
			}
		}(node)
	}
}

func (m *Manager) pingNode(node *types.NodeState) bool {
	client, err := rpc.Dial("tcp", node.RPCAddress)
	if err != nil {
		return false
	}
	defer client.Close()

	var reply bool
	err = client.Call("ConsensusRPC.Ping", node.ID, &reply)
	return err == nil && reply
}

// start the manager RPC server
func (m *Manager) Start(address string) {
	err := managerapi.StartManagerRPCServer(address, m, m.logBuffer)
	if err != nil {
		log.Fatalf("Failed to start manager RPC server: %v", err)
	}
}
