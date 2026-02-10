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
type ConsensusModule interface {
	RequestVote(term int, candidateId int, lastLogIndex int, lastLogTerm int) (bool, int)
	AppendEntries(term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []LogEntry) error
	GetCurrentTerm() int
	Propose(command string) (index int, term int, isLeader bool)
}

type Persister struct {
	mu       sync.RWMutex
	filepath string // for state files
	basepath string // store the base directory
}

func NewPersister(nodeId int, basepath string) *Persister {
	stateDir := basepath + "/state"
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		log.Fatalf("Failed to create state directory: %v\n", err)
	}

	return &Persister{
		filepath: stateDir + "/node_" + fmt.Sprintf("%d", nodeId),
		basepath: basepath,
	}
}

type State struct {
	CurrentTerm int        `json:"current_term"`
	VotedFor    int        `json:"voted_for"`
	Log         []LogEntry `json:"log"`
}

func (p *Persister) SaveState(currentTerm int, votedFor int, log []LogEntry) error {
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

func (p *Persister) LoadState() (currentTerm int, votedFor int, log []LogEntry, err error) {
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

func (p *Persister) ClearState() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.Remove(p.filepath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("\nremove file error: %w", err)
	}
	return nil
}
