package consensus

import (
	"distributed-kv/types"
	"testing"
)

func logOf(n int, term int) []types.LogEntry {
	out := make([]types.LogEntry, n)
	for i := range out {
		out[i] = types.LogEntry{Term: term, Index: i + 1, Command: "SET", Key: "k", Value: "v"}
	}
	return out
}

// A delayed, shorter AppendEntries (same term, no conflict) must not erase
// entries the follower already acknowledged.
func TestAppendEntriesStaleShorterRPCDoesNotTruncate(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	if err := rc.AppendEntries(1, 2, 0, 0, 0, logOf(3, 1)); err != nil {
		t.Fatalf(expectedUnexpErr, err)
	}
	// older RPC arrives late, carrying only the first entry
	if err := rc.AppendEntries(1, 2, 0, 0, 0, logOf(1, 1)); err != nil {
		t.Fatalf(expectedUnexpErr, err)
	}

	node.Mu.RLock()
	defer node.Mu.RUnlock()
	if len(node.Log) != 3 {
		t.Errorf("Expected log to keep 3 entries, got %d", len(node.Log))
	}
}

// A real conflict (same index, different term) truncates from that index.
func TestAppendEntriesConflictTruncates(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	if err := rc.AppendEntries(1, 2, 0, 0, 0, logOf(3, 1)); err != nil {
		t.Fatalf(expectedUnexpErr, err)
	}
	newer := []types.LogEntry{{Term: 2, Index: 2, Command: "SET", Key: "x", Value: "y"}}
	if err := rc.AppendEntries(2, 3, 1, 1, 0, newer); err != nil {
		t.Fatalf(expectedUnexpErr, err)
	}

	node.Mu.RLock()
	defer node.Mu.RUnlock()
	if len(node.Log) != 2 {
		t.Fatalf("Expected 2 entries after conflict, got %d", len(node.Log))
	}
	if node.Log[1].Term != 2 || node.Log[1].Key != "x" {
		t.Errorf("Expected entry 2 replaced by the new leader's entry, got %+v", node.Log[1])
	}
}

// A follower must not commit entries past what the current RPC vouched for:
// a stale suffix from an old leader is not committed by a later heartbeat.
func TestFollowerCommitBoundedByLastNewEntry(t *testing.T) {
	node, rc := setupTestRaft(t, 1)

	if err := rc.AppendEntries(1, 2, 0, 0, 0, logOf(3, 1)); err != nil {
		t.Fatalf(expectedUnexpErr, err)
	}
	// new leader (term 2) only vouches for index 1, but its commit index is 3
	if err := rc.AppendEntries(2, 3, 1, 1, 3, nil); err != nil {
		t.Fatalf(expectedUnexpErr, err)
	}

	node.Mu.RLock()
	defer node.Mu.RUnlock()
	if node.CommitIdx != 1 {
		t.Errorf("Expected commitIdx 1 (bounded by prevLogIndex+len(entries)), got %d", node.CommitIdx)
	}
}
