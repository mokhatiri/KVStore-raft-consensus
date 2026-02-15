package types

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
)

type ApplyMsg struct {
	CommandValid bool
	Command      LogEntry
	CommandIndex int
}

// Persister is an interface for persisting Raft state
type Persister interface {
	SaveState(currentTerm int, votedFor int, log []LogEntry) error
	LoadState() (currentTerm int, votedFor int, log []LogEntry, err error)
	SaveSnapshot(snapshot *Snapshot) error
	LoadSnapshot() (*Snapshot, error)
	GetStoreFilepath() string
	ClearState() error
}

type ConsensusModule interface {
	RequestVote(term int, candidateId int, lastLogIndex int, lastLogTerm int) (bool, int)
	AppendEntries(term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []LogEntry) error
	InstallSnapshot(term int, leaderId int, lastIncludedIndex int, lastIncludedTerm int, data map[string]any) (int, error)
	GetCurrentTerm() int
	GetNodeID() int
	GetNodeStatus() (int, string, int, int)
	GetVotedFor() int
	GetRole() string
	Propose(command string) (index int, term int, isLeader bool)
	RequestAddServer(nodeID int, rpcaddress string, httpaddress string) error
	RequestRemoveServer(nodeID int) error
	GetStore() any // Returns the state machine (key-value store)
	IsLeader() bool
	GetLeader() (id int, address string)
	GetSnapshot() *Snapshot // Returns the latest snapshot (nil if none)
}

type State struct {
	CurrentTerm int        `json:"current_term"`
	VotedFor    int        `json:"voted_for"`
	Log         []LogEntry `json:"log"`
}

// JSONPersister implements Persister using JSON files (default/fallback implementation)
// this was the original implementation before we added SQLite support, and is still used in tests and as a fallback for simplicity.
type JSONPersister struct {
	mu            sync.RWMutex
	filepath      string // for state files (log, term, votedFor)
	storeFilepath string // for state machine (key-value store)
	basepath      string // store the base directory
}

// NewPersister creates a JSON-based persister (factory function for backward compatibility)
func NewPersister(nodeId int, basepath string) Persister {
	stateDir := basepath + "/state"
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		log.Fatalf("Failed to create state directory: %v\n", err)
	}

	nodeIdStr := fmt.Sprintf("%d", nodeId)
	return &JSONPersister{
		filepath:      stateDir + "/node_" + nodeIdStr,
		storeFilepath: stateDir + "/store_" + nodeIdStr,
		basepath:      basepath,
	}
}

func (p *JSONPersister) SaveState(currentTerm int, votedFor int, log []LogEntry) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	state := State{
		CurrentTerm: currentTerm,
		VotedFor:    votedFor,
		Log:         log,
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("\nmarshal error: %w", err)
	}

	if err := os.WriteFile(p.filepath, data, 0644); err != nil {
		return fmt.Errorf("\nwrite file error: %w", err)
	}

	return nil
}

func (p *JSONPersister) LoadState() (currentTerm int, votedFor int, log []LogEntry, err error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	data, err := os.ReadFile(p.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, -1, nil, nil // No state file, return defaults
		}
		return 0, -1, nil, fmt.Errorf("\nread file error: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, -1, nil, fmt.Errorf("\nunmarshal error: %w", err)
	}

	return state.CurrentTerm, state.VotedFor, state.Log, nil
}

func (p *JSONPersister) ClearState() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.Remove(p.filepath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("\nremove file error: %w", err)
	}
	return nil
}

// GetStoreFilepath returns the filepath where store state should be persisted
func (p *JSONPersister) GetStoreFilepath() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.storeFilepath
}

// SaveSnapshot persists a snapshot to disk
func (p *JSONPersister) SaveSnapshot(snapshot *Snapshot) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	snapshotPath := p.basepath + "/state/snapshot_" + p.filepath[len(p.basepath)+len("/state/node_"):]
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("snapshot marshal error: %w", err)
	}
	if err := os.WriteFile(snapshotPath, data, 0644); err != nil {
		return fmt.Errorf("snapshot write error: %w", err)
	}
	return nil
}

// LoadSnapshot loads a snapshot from disk (returns nil if none exists)
func (p *JSONPersister) LoadSnapshot() (*Snapshot, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snapshotPath := p.basepath + "/state/snapshot_" + p.filepath[len(p.basepath)+len("/state/node_"):]
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No snapshot file
		}
		return nil, fmt.Errorf("snapshot read error: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("snapshot unmarshal error: %w", err)
	}
	return &snapshot, nil
}
