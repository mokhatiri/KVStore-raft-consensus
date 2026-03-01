package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"distributed-kv/types"

	_ "github.com/mattn/go-sqlite3"
)

// DatabasePersister implements the Persister interface using SQLite
type DatabasePersister struct {
	mu       sync.RWMutex
	db       *sql.DB
	nodeID   int
	dbPath   string
	basePath string
}

// NewDatabasePersister creates a new SQLite-backed persister for a node
func NewDatabasePersister(nodeID int, basePath string) (*DatabasePersister, error) {
	// Ensure state directory exists
	stateDir := filepath.Join(basePath, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	dbPath := filepath.Join(stateDir, fmt.Sprintf("node_%d.db", nodeID))

	// Open or create the SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	dp := &DatabasePersister{
		db:       db,
		nodeID:   nodeID,
		dbPath:   dbPath,
		basePath: basePath,
	}

	// Initialize schema
	if err := dp.initializeSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return dp, nil
}

// initializeSchema creates all necessary tables if they don't exist
func (dp *DatabasePersister) initializeSchema() error {
	schema := `
	-- Raft persistent state
	CREATE TABLE IF NOT EXISTS raft_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		current_term INTEGER NOT NULL DEFAULT 0,
		voted_for INTEGER NOT NULL DEFAULT -1,
		log_json TEXT NOT NULL DEFAULT '[]'
	);

	-- Log entries
	CREATE TABLE IF NOT EXISTS log_entries (
		entry_index INTEGER PRIMARY KEY,
		term INTEGER NOT NULL,
		command TEXT NOT NULL,
		key TEXT,
		value TEXT
	);

	-- Snapshots
	CREATE TABLE IF NOT EXISTS snapshots (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		last_included_index INTEGER NOT NULL DEFAULT 0,
		last_included_term INTEGER NOT NULL DEFAULT 0,
		data_json TEXT NOT NULL DEFAULT '{}'
	);

	-- Key-Value Store
	CREATE TABLE IF NOT EXISTS kv_store (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Ensure raft_state has a single row
	INSERT OR IGNORE INTO raft_state (id, current_term, voted_for, log_json) 
	VALUES (1, 0, -1, '[]');

	-- Ensure snapshots has a single row
	INSERT OR IGNORE INTO snapshots (id, last_included_index, last_included_term, data_json) 
	VALUES (1, 0, 0, '{}');
	`

	if _, err := dp.db.Exec(schema); err != nil {
		return fmt.Errorf("schema initialization failed: %w", err)
	}

	return nil
}

// SaveState persists Raft state (term, votedFor, log)
func (dp *DatabasePersister) SaveState(currentTerm int, votedFor int, log []types.LogEntry) error {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	logJSON, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("failed to marshal log: %w", err)
	}

	tx, err := dp.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Update raft_state
	_, err = tx.Exec(
		"UPDATE raft_state SET current_term = ?, voted_for = ?, log_json = ? WHERE id = 1",
		currentTerm, votedFor, string(logJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to update raft_state: %w", err)
	}

	// Clear old log entries table and repopulate
	_, err = tx.Exec("DELETE FROM log_entries")
	if err != nil {
		return fmt.Errorf("failed to clear log_entries: %w", err)
	}

	// Insert log entries for easy querying
	for idx, entry := range log {
		_, err = tx.Exec(
			"INSERT INTO log_entries (entry_index, term, command, key, value) VALUES (?, ?, ?, ?, ?)",
			idx+1, entry.Term, entry.Command, entry.Key, entry.Value,
		)
		if err != nil {
			return fmt.Errorf("failed to insert log entry %d: %w", idx, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// LoadState loads Raft state from the database
func (dp *DatabasePersister) LoadState() (currentTerm int, votedFor int, log []types.LogEntry, err error) {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	var logJSON string
	err = dp.db.QueryRow("SELECT current_term, voted_for, log_json FROM raft_state WHERE id = 1").
		Scan(&currentTerm, &votedFor, &logJSON)

	if err == sql.ErrNoRows {
		return 0, -1, nil, nil // No state yet
	}
	if err != nil {
		return 0, -1, nil, fmt.Errorf("failed to query raft_state: %w", err)
	}

	if err := json.Unmarshal([]byte(logJSON), &log); err != nil {
		return 0, -1, nil, fmt.Errorf("failed to unmarshal log: %w", err)
	}

	if log == nil {
		log = []types.LogEntry{}
	}

	return currentTerm, votedFor, log, nil
}

// ClearState clears all persisted state (for CLEAN command)
func (dp *DatabasePersister) ClearState() error {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	return dp.clearState()
}

func (dp *DatabasePersister) clearState() error {
	tx, err := dp.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Reset raft_state
	_, err = tx.Exec("UPDATE raft_state SET current_term = 0, voted_for = -1, log_json = '[]' WHERE id = 1")
	if err != nil {
		return fmt.Errorf("failed to reset raft_state: %w", err)
	}

	// Clear log entries
	_, err = tx.Exec("DELETE FROM log_entries")
	if err != nil {
		return fmt.Errorf("failed to clear log_entries: %w", err)
	}

	// Clear KV store
	_, err = tx.Exec("DELETE FROM kv_store")
	if err != nil {
		return fmt.Errorf("failed to clear kv_store: %w", err)
	}

	// Reset snapshots
	_, err = tx.Exec("UPDATE snapshots SET last_included_index = 0, last_included_term = 0, data_json = '{}' WHERE id = 1")
	if err != nil {
		return fmt.Errorf("failed to reset snapshots: %w", err)
	}

	return tx.Commit()
}

// SaveSnapshot persists a snapshot
func (dp *DatabasePersister) SaveSnapshot(snapshot *types.Snapshot) error {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	dataJSON, err := json.Marshal(snapshot.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot data: %w", err)
	}

	_, err = dp.db.Exec(
		"UPDATE snapshots SET last_included_index = ?, last_included_term = ?, data_json = ? WHERE id = 1",
		snapshot.LastIncludedIndex, snapshot.LastIncludedTerm, string(dataJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	return nil
}

// LoadSnapshot loads a snapshot from the database
func (dp *DatabasePersister) LoadSnapshot() (*types.Snapshot, error) {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	var lastIncludedIndex, lastIncludedTerm int
	var dataJSON string

	err := dp.db.QueryRow("SELECT last_included_index, last_included_term, data_json FROM snapshots WHERE id = 1").
		Scan(&lastIncludedIndex, &lastIncludedTerm, &dataJSON)

	if err == sql.ErrNoRows {
		return nil, nil // No snapshot
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query snapshots: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot data: %w", err)
	}

	return &types.Snapshot{
		LastIncludedIndex: lastIncludedIndex,
		LastIncludedTerm:  lastIncludedTerm,
		Data:              data,
	}, nil
}

// GetStoreFilepath returns a fake path (not used with SQLite)
// This is kept for backward compatibility
func (dp *DatabasePersister) GetStoreFilepath() string {
	return filepath.Join(dp.basePath, "state", fmt.Sprintf("store_%d.db", dp.nodeID))
}

// Close closes the database connection
func (dp *DatabasePersister) Close() error {
	dp.mu.Lock()
	defer dp.mu.Unlock()
	if dp.db != nil {
		return dp.db.Close()
	}
	return nil
}

// --- KV Store methods (for the Store to persist to DB) ---

// SaveKVStore persists the entire KV store to the database
func (dp *DatabasePersister) SaveKVStore(data map[string]any) error {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	tx, err := dp.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear existing KV store
	_, err = tx.Exec("DELETE FROM kv_store")
	if err != nil {
		return fmt.Errorf("failed to clear kv_store: %w", err)
	}

	// Insert all key-value pairs
	for key, value := range data {
		valueJSON, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal value for key %s: %w", key, err)
		}
		_, err = tx.Exec(
			"INSERT INTO kv_store (key, value) VALUES (?, ?)",
			key, string(valueJSON),
		)
		if err != nil {
			return fmt.Errorf("failed to insert key-value pair %s: %w", key, err)
		}
	}

	return tx.Commit()
}

// LoadKVStore loads the entire KV store from the database
func (dp *DatabasePersister) LoadKVStore() (map[string]any, error) {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	rows, err := dp.db.Query("SELECT key, value FROM kv_store")
	if err != nil {
		return nil, fmt.Errorf("failed to query kv_store: %w", err)
	}
	defer rows.Close()

	data := make(map[string]any)
	for rows.Next() {
		var key, valueJSON string
		if err := rows.Scan(&key, &valueJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var value any
		if err := json.Unmarshal([]byte(valueJSON), &value); err != nil {
			return nil, fmt.Errorf("failed to unmarshal value for key %s: %w", key, err)
		}
		data[key] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return data, nil
}
