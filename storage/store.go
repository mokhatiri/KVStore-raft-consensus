// Package storage implements the persistent storage layer for the Raft node.
package storage

import "sync"

type Store struct {
	data map[string]any
	mu   sync.RWMutex
}

func NewStore() *Store {
	return &Store{
		data: make(map[string]any),
	}
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
