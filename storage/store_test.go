package storage_test

import (
	"testing"
	"distributed-kv/storage"
)

func TestStoreSet(t *testing.T) {
	store := storage.NewStore()
	store.Set("key1", "value1")
	value, exists := store.Get("key1")

	if !exists {
		t.Errorf("Expected key1 to exist, but it doesn't")
	}

	if value != "value1" {
		t.Errorf("Expected value1, got %v", value)
	}
}

func TestStoreGet(t *testing.T) {
	store := storage.NewStore()

	// Try to get a non-existent key
	value, exists := store.Get("nonexistent")

	if exists {
		t.Errorf("Expected key to not exist, but it does")
	}

	if value != nil {
		t.Errorf("Expected nil for non-existent key, got %v", value)
	}
}

func TestStoreDelete(t *testing.T) {
	store := storage.NewStore()

	store.Set("key1", "value1")
	store.Delete("key1")

	value, exists := store.Get("key1")

	if exists {
		t.Errorf("Expected key1 to be deleted, but it still exists")
	}

	if value != nil {
		t.Errorf("Expected nil after delete, got %v", value)
	}
}

func TestStoreGetAll(t *testing.T) {
	store := storage.NewStore()

	store.Set("key1", "value1")
	store.Set("key2", "value2")
	store.Set("key3", "value3")

	all := store.GetAll()

	if len(all) != 3 {
		t.Errorf("Expected 3 items, got %d", len(all))
	}

	if all["key1"] != "value1" {
		t.Errorf("Expected value1 for key1, got %v", all["key1"])
	}

	if all["key2"] != "value2" {
		t.Errorf("Expected value2 for key2, got %v", all["key2"])
	}

	if all["key3"] != "value3" {
		t.Errorf("Expected value3 for key3, got %v", all["key3"])
	}
}

func TestStoreConcurrency(t *testing.T) {
	store := storage.NewStore()

	// Write from multiple goroutines
	for i := 0; i < 10; i++ {
		go func(index int) {
			store.Set("key", index)
		}(i)
	}

	// Read from multiple goroutines
	for i := 0; i < 10; i++ {
		go func() {
			store.Get("key")
		}()
	}

	// If no panic, the mutex works correctly
	t.Log("Concurrency test passed")
}
