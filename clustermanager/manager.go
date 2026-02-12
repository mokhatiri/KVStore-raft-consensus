package clustermanager

import (
	"database/sql"
	"distributed-kv/types"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

type Manager struct {
	eventBuffer *EventBuffer
	db          *sql.DB
	fileWriter  *EventFileWriter
	eventsCh    chan types.RPCEvent

	clusterState      *types.ClusterState
	nodeSubscriptions map[int]<-chan types.RPCEvent

	mu sync.RWMutex
}

func NewManager(dbPath string, outputDir string) *Manager {
	db, err := sql.Open("sqlite3", dbPath)

	if err != nil {
		panic(fmt.Sprintf("Failed to open database: %v", err))
	}

	// make the node subscriptions map
	nodeSubscriptions := make(map[int]<-chan types.RPCEvent)
	eventsCh := make(chan types.RPCEvent, EventBufferSize)

	return &Manager{
		eventBuffer: NewEventBuffer(EventBufferSize),
		db:          db,
		fileWriter:  NewEventFileWriter(outputDir, eventsCh),
		eventsCh:    eventsCh,

		clusterState:      &types.ClusterState{},
		nodeSubscriptions: nodeSubscriptions,
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

// SubscribeToNode allows a node to subscribe to cluster events. The manager will send relevant events to the provided channel.
func (m *Manager) subscribeToNode(nodeID int, eventChan <-chan types.RPCEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeSubscriptions[nodeID] = eventChan
}

// UnsubscribeFromNode removes a node's subscription to cluster events.
func (m *Manager) unsubscribeFromNode(nodeID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodeSubscriptions, nodeID)
}

// register a node in the cluster and subscribe to its events
func (m *Manager) RegisterNode(nodeId int, nodeRPCAddress string, nodeHTTPAddress string, nodePProfAddress string) {
	// TODO
}

// start event aggregation : spawns goroutines for each subcription + file writer
func (m *Manager) StartEventAggregation() {
	// start the file writer goroutine
	m.fileWriter.Start()

	// start a goroutine for each node subscription to listen for events and process them
	for nodeID, eventChan := range m.nodeSubscriptions {
		go func(id int, ch <-chan types.RPCEvent) {
			for event := range ch {
				m.saveEvent(event)
			}
		}(nodeID, eventChan)
	}
}

// save event to backends
func (m *Manager) saveEvent(event types.RPCEvent) {
	// save to in-memory buffer
	m.eventBuffer.AddEvent(event) // add lock

	// save to sqlite db
	_, err := m.db.Exec(`
	INSERT INTO rpc_events (from_id, to_id, event_type, details, duration, error)
	VALUES (?, ?, ?, ?, ?, ?);
	`, event.From, event.To, event.Type, event.Details, event.Duration.Milliseconds(), event.Error)

	if err != nil {
		fmt.Printf("\n----\n-Error saving event to database : %v-\n----\n", err)
	}

	// send to events channel to save to file
	m.eventsCh <- event

	m.updateClusterState(event)
}

// update cluster state based on event
func (m *Manager) updateClusterState(event types.RPCEvent) {
	// updates the cluster state based on the event type and details
	// TODO ...
}

// get cluster status
func (m *Manager) GetClusterStatus() *types.ClusterState {
	return m.clusterState
}

// get events
func (m *Manager) GetEvents(limit int) []types.RPCEvent {
	return m.eventBuffer.GetLast(limit)
}

// start the API server to serve cluster status and events
func (m *Manager) StartAPI(address string) {
	// TODO : start API server to serve cluster status and events
	/*
		- Create routes for manager API:
			GET /cluster/status
			GET /cluster/events?limit=100
			GET /cluster/nodes
			etc.
			- Pass manager to API handler so it can query data
	*/
}
