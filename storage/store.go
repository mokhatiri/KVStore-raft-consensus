// Package storage implements the persistent storage layer for the Raft node.
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type Store struct {
	data      map[string]any
	mu        sync.RWMutex
	persister *DatabasePersister // Optional SQLite persister
}

// NewStore creates a new in-memory store without persistence (used for testing or simple setups)
func NewStore() *Store {
	return &Store{
		data: make(map[string]any),
	}
}

// SetPersister sets or updates the DatabasePersister for this store
func (s *Store) SetPersister(persister *DatabasePersister) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persister = persister
}

// NewStoreWithPersister creates a Store linked to a DatabasePersister for SQLite persistence
func NewStoreWithPersister(persister *DatabasePersister) (*Store, error) {
	store := &Store{
		data:      make(map[string]any),
		persister: persister,
	}

	// Load existing KV data from database
	if persister != nil {
		data, err := persister.LoadKVStore()
		if err != nil {
			return nil, fmt.Errorf("failed to load store from database: %w", err)
		}
		if data != nil {
			store.data = data
		}
	}

	return store, nil
}

// locks are necessary for concurrent access

func (s *Store) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, exists := s.data[key]
	return value, exists
}

func (s *Store) Set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
}

// GetAll returns a copy of all key-value pairs in the store.
func (s *Store) GetAll() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copiedData := make(map[string]any)
	for k, v := range s.data {
		copiedData[k] = v
	}
	return copiedData
}

func (s *Store) ClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]any)
}

// SaveState persists the store data to disk (or database if persister is set)
func (s *Store) SaveState(filepath string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// If using database persister, save to database
	if s.persister != nil {
		return s.persister.SaveKVStore(s.data)
	}

	// Otherwise, fall back to JSON file persistence
	data, err := json.Marshal(s.data)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("write file error: %w", err)
	}

	return nil
}

// LoadState restores the store data from disk (or database if persister is set)
func (s *Store) LoadState(filepath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If using database persister, load from database (already done in NewStoreWithPersister)
	if s.persister != nil {
		return nil // Data already loaded in constructor
	}

	// Otherwise, fall back to JSON file persistence
	data, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file, use empty store
		}
		return fmt.Errorf("read file error: %w", err)
	}

	var storeData map[string]any
	if err := json.Unmarshal(data, &storeData); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	s.data = storeData
	return nil
}

// CreateSnapshot returns a deep copy of the store data for snapshotting
func (s *Store) CreateSnapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make(map[string]any, len(s.data))
	for k, v := range s.data {
		snapshot[k] = v
	}
	return snapshot
}

// RestoreSnapshot replaces the store data entirely with the snapshot data
func (s *Store) RestoreSnapshot(data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]any, len(data))
	for k, v := range data {
		s.data[k] = v
	}
}
