// Package consensus implements the Raft consensus algorithm.
package consensus

import (
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"distributed-kv/rpc"
	"distributed-kv/storage"
	"distributed-kv/types"
)

type RaftConsensus struct {
	mu          sync.Mutex
	currentTerm int
	votedFor    int
	lastApplied int

	nextIndex  map[int]int
	matchIndex map[int]int

	applyCh chan types.ApplyMsg

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	node      *types.Node
	persister types.Persister  // Now an interface
	store     *storage.Store   // Reference to the state machine
	logBuffer *types.LogBuffer // Buffer for node logs

	snapshot             *types.Snapshot // latest snapshot (nil if none)
	lastSnapshotIndex    int             // index of last entry included in snapshot
	lastSnapshotTerm     int             // term of last entry included in snapshot
	snapshotAppliedCount int             // entries applied since last snapshot

	configState *types.ConfigState // for cluster membership changes
}

func NewRaftConsensus(node *types.Node, store *storage.Store, persister types.Persister) *RaftConsensus {
	// Use provided persister, or create default JSON persister
	if persister == nil {
		persister = types.NewPersister(node.ID, ".") // default to JSON persister with current directory as base path
	}

	// Load persisted state if available
	term, votedFor, logEntries, err := persister.LoadState()
	logBuffer := types.NewLogBuffer(1000)
	if err != nil {
		logBuffer.AddLog("ERROR", fmt.Sprintf("Error reading persisted state: %v", err))
	}

	// Load persisted store state if available
	storeFilepath := persister.GetStoreFilepath()
	if err := store.LoadState(storeFilepath); err != nil {
		logBuffer.AddLog("ERROR", fmt.Sprintf("Error loading persisted store state: %v", err))
	}

	// put the log in place
	node.Mu.Lock()
	node.Log = logEntries
	node.Mu.Unlock()

	// Initialize nextIndex and matchIndex as maps keyed by peer ID
	nextIndex := make(map[int]int)
	matchIndex := make(map[int]int)
	for peerID := range node.Peers {
		nextIndex[peerID] = 1
		matchIndex[peerID] = 0
	}

	// Load snapshot if available
	var snapshot *types.Snapshot
	var lastSnapshotIndex, lastSnapshotTerm int
	snap, err := persister.LoadSnapshot()
	if err != nil {
		logBuffer.AddLog("ERROR", fmt.Sprintf("Error loading snapshot: %v", err))
	} else if snap != nil {
		snapshot = snap
		lastSnapshotIndex = snap.LastIncludedIndex
		lastSnapshotTerm = snap.LastIncludedTerm
		logBuffer.AddLog("INFO", fmt.Sprintf("Loaded snapshot at index %d, term %d", lastSnapshotIndex, lastSnapshotTerm))
		// Restore store from snapshot if log is empty (fresh recovery)
		if len(logEntries) == 0 && snap.Data != nil {
			store.RestoreSnapshot(snap.Data)
		}
		// Adjust nextIndex to account for snapshot offset
		for peerID := range node.Peers {
			if nextIndex[peerID] < lastSnapshotIndex+1 {
				nextIndex[peerID] = lastSnapshotIndex + 1
			}
		}
	}

	rc := &RaftConsensus{
		currentTerm: term,
		votedFor:    votedFor,
		lastApplied: 0,

		nextIndex:  nextIndex,
		matchIndex: matchIndex,

		applyCh: make(chan types.ApplyMsg), // channel to apply committed log entries

		electionTimer:  time.NewTimer(GetRandomElectionTimeout()),             // randomized election timeout
		heartbeatTimer: time.NewTimer(HeartbeatIntervalMs * time.Millisecond), // heartbeat interval

		node:      node,
		persister: persister,
		store:     store,
		logBuffer: logBuffer,

		snapshot:          snapshot,
		lastSnapshotIndex: lastSnapshotIndex,
		lastSnapshotTerm:  lastSnapshotTerm,

		configState: &types.ConfigState{
			OldConfig:    &types.Config{Nodes: make(map[int]string), Index: 0},
			NewConfig:    nil,
			InTransition: false,
		},
	}

	for id, http := range node.PeersHttp {
		rc.configState.OldConfig.Nodes[id] = http
	}

	// add the current node to the config
	rc.configState.OldConfig.Nodes[node.ID] = node.Address

	return rc
}

/* - prints - */

func (rc *RaftConsensus) PrintStatus() {
	// Read values without locking the consensus module to prevent hangs
	fmt.Println("\n--- node Status ---")

	rc.node.Mu.RLock()
	id := rc.node.ID
	address := rc.node.Address
	role := rc.node.Role
	logLen := len(rc.node.Log)
	commitIdx := rc.node.CommitIdx
	peers := rc.node.Peers
	rc.node.Mu.RUnlock()

	fmt.Printf("node ID:       %d\n", id)
	fmt.Printf("Address:       %s\n", address)
	fmt.Printf("Role:          %s\n", role)
	fmt.Printf("Current Term:  %d\n", rc.currentTerm)
	fmt.Printf("Voted For:     %d\n", rc.votedFor)
	fmt.Printf("Log Length:    %d\n", logLen)
	fmt.Printf("Commit Index:  %d\n", commitIdx)
	fmt.Printf("Peers:         %v\n", peers)
}

func (rc *RaftConsensus) PrintLog() {
	rc.node.Mu.RLock()
	defer rc.node.Mu.RUnlock()

	if len(rc.node.Log) == 0 {
		fmt.Println("Log is empty")
		return
	}

	fmt.Println("\n--- Replication Log ---")
	for i, entry := range rc.node.Log {
		fmt.Printf("[%d] Term: %d, Command: %s, Key: %s, Value: %v\n",
			i+1, entry.Term, entry.Command, entry.Key, entry.Value)
	}
}

/* - getters - */

func (rc *RaftConsensus) GetNodeStatus() (int, string, int, int) {
	rc.node.Mu.RLock()
	defer rc.node.Mu.RUnlock()
	return rc.node.ID, rc.node.Role, rc.node.CommitIdx, len(rc.node.Log)
}

func (rc *RaftConsensus) GetRole() string {
	rc.node.Mu.RLock()
	defer rc.node.Mu.RUnlock()
	return rc.node.Role
}

func (rc *RaftConsensus) GetLogs(limit int) []types.LogMessage {
	return rc.logBuffer.GetLogs(limit)
}

func (rc *RaftConsensus) GetLogBuffer() *types.LogBuffer {
	return rc.logBuffer
}

func (rc *RaftConsensus) GetStore() any {
	return rc.store
}

func (rc *RaftConsensus) GetLeader() (id int, httpAddress string) {
	rc.node.Mu.RLock()
	defer rc.node.Mu.RUnlock()

	// Look up leader's HTTP address by ID
	if rc.node.LeaderID >= 0 {
		if addr, ok := rc.node.PeersHttp[rc.node.LeaderID]; ok {
			return rc.node.LeaderID, addr
		}
	}

	return -1, ""
}

func (rc *RaftConsensus) GetCurrentTerm() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.currentTerm
}

func (rc *RaftConsensus) GetVotedFor() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.votedFor
}

func (rc *RaftConsensus) GetNodeID() int {
	rc.node.Mu.RLock()
	defer rc.node.Mu.RUnlock()
	return rc.node.ID
}

func (rc *RaftConsensus) GetSnapshot() *types.Snapshot {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.snapshot
}

func (rc *RaftConsensus) GetCurrentConfig() *types.Config {
	rc.configState.Mu.RLock()
	defer rc.configState.Mu.RUnlock()

	if rc.configState.InTransition {
		return rc.configState.NewConfig
	}

	return rc.configState.OldConfig
}

func (rc *RaftConsensus) GetConfigState() *types.ConfigState {
	rc.configState.Mu.RLock()
	defer rc.configState.Mu.RUnlock()
	return rc.configState
}

/* - functional - */

func (rc *RaftConsensus) IsInJointConsensus() bool {
	rc.configState.Mu.RLock()
	defer rc.configState.Mu.RUnlock()
	return rc.configState.InTransition
}

func (rc *RaftConsensus) IsLeader() bool {
	rc.node.Mu.RLock()
	defer rc.node.Mu.RUnlock()
	return rc.node.Role == "Leader"
}

func (rc *RaftConsensus) logIndex(globalIdx int) int {
	return globalIdx - rc.lastSnapshotIndex
}

/*
CanCommit checks if a log entry at a given index and term has been replicated to a majority of followers,
and thus can be safely committed.
*/
func (rc *RaftConsensus) CanCommit(index int, term int) bool {
	rc.configState.Mu.RLock()
	defer rc.configState.Mu.RUnlock()

	if term != rc.currentTerm {
		return false
	}

	oldQuorum := len(rc.configState.OldConfig.Nodes)/2 + 1
	oldReplicas := 1 // leader always counts as a replica
	for followerID := range rc.configState.OldConfig.Nodes {
		if followerID == rc.node.ID {
			continue
		}
		if rc.matchIndex[followerID] >= index {
			oldReplicas++
		}
	}

	if !rc.configState.InTransition {
		return oldReplicas >= oldQuorum
	}

	newQuorum := len(rc.configState.NewConfig.Nodes)/2 + 1
	newReplicas := 1
	for followerID := range rc.configState.NewConfig.Nodes {
		if followerID == rc.node.ID {
			continue
		}
		if rc.matchIndex[followerID] >= index {
			newReplicas++
		}
	}

	return oldReplicas >= oldQuorum && newReplicas >= newQuorum
}

/*
TakeSnapshot creates a snapshot of the current state machine at the given index,
then compacts the log by removing all entries up to and including that index.
This prevents unbounded log growth and speeds up startup/recovery.
*/
func (rc *RaftConsensus) TakeSnapshot() error {
	rc.mu.Lock()
	rc.node.Mu.Lock()

	commitIdx := rc.node.CommitIdx
	if commitIdx <= rc.lastSnapshotIndex {
		rc.node.Mu.Unlock()
		rc.mu.Unlock()
		return nil // nothing new to snapshot
	}

	snapshotIndex := commitIdx
	localIdx := rc.logIndex(snapshotIndex)
	if localIdx <= 0 || localIdx > len(rc.node.Log) {
		rc.node.Mu.Unlock()
		rc.mu.Unlock()
		return fmt.Errorf("invalid snapshot index %d (local=%d, logLen=%d)", snapshotIndex, localIdx, len(rc.node.Log))
	}

	snapshotTerm := rc.node.Log[localIdx-1].Term

	rc.node.Mu.Unlock()
	rc.mu.Unlock()

	// Create snapshot data from the store (this doesn't need Raft locks)
	storeData := rc.store.CreateSnapshot()

	snapshot := &types.Snapshot{
		LastIncludedIndex: snapshotIndex,
		LastIncludedTerm:  snapshotTerm,
		Data:              storeData,
	}

	// Persist snapshot to disk
	if err := rc.persister.SaveSnapshot(snapshot); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Now compact the log under lock
	rc.mu.Lock()
	rc.node.Mu.Lock()

	// Re-calculate localIdx in case log changed while we were saving
	localIdx = rc.logIndex(snapshotIndex)
	if localIdx > 0 && localIdx <= len(rc.node.Log) {
		// Keep only entries after the snapshot point
		rc.node.Log = rc.node.Log[localIdx:]
	}

	rc.snapshot = snapshot
	rc.lastSnapshotIndex = snapshotIndex
	rc.lastSnapshotTerm = snapshotTerm
	rc.snapshotAppliedCount = 0

	// Persist the compacted log
	if err := rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after snapshot: %v", err))
	}

	rc.node.Mu.Unlock()
	rc.mu.Unlock()

	rc.logBuffer.AddLog("INFO", fmt.Sprintf("Snapshot taken at index %d, term %d. Log compacted to %d entries",
		snapshotIndex, snapshotTerm, len(rc.node.Log)))

	return nil
}

/*
InstallSnapshot is called when the leader sends a snapshot to a follower
that is too far behind to catch up via normal AppendEntries.
The follower replaces its state machine and log with the snapshot.
*/
func (rc *RaftConsensus) InstallSnapshot(term int, leaderId int, lastIncludedIndex int, lastIncludedTerm int, data map[string]any) (int, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// If leader's term is older, reject
	if term < rc.currentTerm {
		return rc.currentTerm, fmt.Errorf("term %d is older than current term %d", term, rc.currentTerm)
	}

	// Reset election timer — leader is alive
	rc.electionTimer.Reset(GetRandomElectionTimeout())

	// Update term if needed
	if term > rc.currentTerm {
		rc.currentTerm = term
		rc.votedFor = -1
	}

	// Step down to follower
	rc.node.Mu.Lock()
	rc.node.Role = "Follower"
	rc.node.LeaderID = leaderId

	// If snapshot is not more recent than what we have, discard it
	if lastIncludedIndex <= rc.lastSnapshotIndex {
		rc.node.Mu.Unlock()
		return rc.currentTerm, nil
	}

	// If log has entries past the snapshot, keep them; otherwise discard all
	localIdx := lastIncludedIndex - rc.lastSnapshotIndex
	if localIdx < len(rc.node.Log) {
		// Keep entries after the snapshot point
		rc.node.Log = rc.node.Log[localIdx:]
	} else {
		// Discard entire log
		rc.node.Log = []types.LogEntry{}
	}

	rc.node.CommitIdx = lastIncludedIndex
	rc.node.Mu.Unlock()

	// Update snapshot metadata
	rc.lastSnapshotIndex = lastIncludedIndex
	rc.lastSnapshotTerm = lastIncludedTerm

	snapshot := &types.Snapshot{
		LastIncludedIndex: lastIncludedIndex,
		LastIncludedTerm:  lastIncludedTerm,
		Data:              data,
	}
	rc.snapshot = snapshot

	// Restore state machine from snapshot data
	rc.store.RestoreSnapshot(data)

	// Persist everything
	if err := rc.persister.SaveSnapshot(snapshot); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save snapshot: %v", err))
	}
	rc.node.Mu.RLock()
	if err := rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after snapshot install: %v", err))
	}
	rc.node.Mu.RUnlock()

	storeFilepath := rc.persister.GetStoreFilepath()
	if err := rc.store.SaveState(storeFilepath); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save store state: %v", err))
	}

	// Update lastApplied to snapshot point
	if rc.lastApplied < lastIncludedIndex {
		rc.lastApplied = lastIncludedIndex
	}

	rc.logBuffer.AddLog("INFO", fmt.Sprintf("Installed snapshot from leader %d at index %d, term %d",
		leaderId, lastIncludedIndex, lastIncludedTerm))

	return rc.currentTerm, nil
}

/*
	Propose : allows clients to propose new commands to be replicated.

- only the leader can accept proposals from clients.
- when a client proposes a command, the leader appends it to its log and initiates
replication to followers.
- the leader will return the index of the log entry, the current term,
and whether it is the leader.
- if the node is not the leader, it will reject the proposal and return false.
---
Propose allows clients to propose new commands to be replicated across the cluster.
Only the leader can accept proposals, and it will append the command to its log
and initiate replication to followers.
*/
func (rc *RaftConsensus) Propose(command string) (index int, term int, isLeader bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.node.Mu.Lock()
	defer rc.node.Mu.Unlock()

	if rc.node.Role != "Leader" {
		return -1, rc.currentTerm, false
	}

	// Append the new entry to leader's log
	parts := strings.SplitN(command, ":", 3)
	entry := types.LogEntry{
		Term:    rc.currentTerm,
		Index:   rc.lastSnapshotIndex + len(rc.node.Log) + 1,
		Command: parts[0], // Just the operation: "SET", "DELETE", "CLEAN", etc.
	}
	if len(parts) >= 2 {
		entry.Key = parts[1]
	}
	if len(parts) >= 3 {
		entry.Value = parts[2]
	}

	rc.node.Log = append(rc.node.Log, entry)
	// persister
	if err := rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after propose: %v", err))
	}

	// Return global index (snapshot offset + local length)
	index = rc.lastSnapshotIndex + len(rc.node.Log)

	return index, rc.currentTerm, true
}

/*
	RequestVote : used by candidates to gather votes.

- the point of voting is to ensure that
only one leader is elected
at a time in a given term.

---
RequestVote handles vote requests from candidates.
if the candidate's term is higher than
the current term, the node votes for the candidate.
if it's the same term, the node checks if it has already voted
and votes accordingly.

voting is base on three conditions:
- Term Check: The candidate's term is greater than or equal to the voter's current term.
- First-Come-First-Served: The voter has not already voted for another candidate in the
same term (i.e., votedFor is null or candidateId).
- Log Completeness: The candidate's log is at least as up-to-date as the voter's log.
*/
func (rc *RaftConsensus) RequestVote(term int, candidateId int, lastLogIndex int, lastLogTerm int) (bool, int) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// If candidate's term is higher, update our term and reset votedFor
	if term > rc.currentTerm {
		rc.currentTerm = term
		rc.votedFor = -1

		rc.node.Mu.Lock()
		// persist
		if err := rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log); err != nil {
			rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after term update: %v", err))
		}

		// Step down to follower if we were leader/candidate
		rc.node.Role = "Follower"

		rc.node.Mu.Unlock()
	}

	// Condition 1: Term Check - reject if candidate's term is older
	if term < rc.currentTerm {
		return false, rc.currentTerm
	}

	// Condition 2: First-Come-First-Served - check if we've already voted for someone else
	if rc.votedFor != -1 && rc.votedFor != candidateId {
		return false, rc.currentTerm
	}

	// Condition 3: Log Completeness - candidate's log must be at least as up-to-date
	rc.node.Mu.RLock()
	localLastIdx := len(rc.node.Log)
	globalLastIdx := rc.lastSnapshotIndex + localLastIdx
	localLastTerm := 0
	if localLastIdx > 0 {
		localLastTerm = rc.node.Log[localLastIdx-1].Term
	} else if rc.lastSnapshotTerm > 0 {
		localLastTerm = rc.lastSnapshotTerm
	}
	rc.node.Mu.RUnlock()

	// Up-to-date check: candidate's log is at least as up-to-date if:
	// - candidate's lastLogTerm > local lastLogTerm, OR
	// - terms are equal AND candidate's lastLogIndex >= local lastLogIndex (global)
	upToDate := lastLogTerm > localLastTerm || (lastLogTerm == localLastTerm && lastLogIndex >= globalLastIdx)
	if !upToDate {
		return false, rc.currentTerm
	}

	// All conditions passed - grant vote
	rc.votedFor = candidateId
	// persist
	if err := rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after granting vote: %v", err))
	}
	// Reset election timer when granting vote (prevents unnecessary elections)
	rc.electionTimer.Reset(GetRandomElectionTimeout())

	return true, rc.currentTerm
}

/*
	AppendEntries : used by leaders to replicate log entries.

- the point of the log is to ensure that
all nodes have the same sequence of
commands to apply to their state machines.

----
AppendEntries appends new log entries from
the leader to the follower's log.
if the term is older than the current term,
the entries are ignored.
if the leader is alive it will keep sending heartbeats, and don't start election
it basically tells the followers "i'm still here"
*/
func (rc *RaftConsensus) AppendEntries(term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []types.LogEntry) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// ignore entries from older term
	if term < rc.currentTerm {
		return fmt.Errorf("term %d is older than current term %d", term, rc.currentTerm)
	}

	// if leader alive - reset election timer with random value
	rc.electionTimer.Reset(GetRandomElectionTimeout())

	// if the term is higher, update current term and reset votedFor
	if term > rc.currentTerm {
		rc.currentTerm = term // update current term
		rc.votedFor = -1      // reset votedFor

		// save persister
		rc.node.Mu.RLock()
		if err := rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log); err != nil {
			rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state on AppendEntries: %v", err))
		}
		rc.node.Mu.RUnlock()
	}
	// if it's the same term, do nothing
	// because the node is already in sync with the leader

	// Step down if we're a candidate and receive AppendEntries from a valid leader
	rc.node.Mu.Lock()
	rc.node.Role = "Follower"
	rc.node.LeaderID = leaderId // Track the leader
	rc.node.Mu.Unlock()

	// Verify log consistency
	if !rc.verifyLogConsistency(prevLogIndex, prevLogTerm) {
		return fmt.Errorf("log mismatch at index %d, expected term %d", prevLogIndex, prevLogTerm)
	}

	// append new entries to the log
	if len(entries) > 0 {
		rc.appendNewEntries(prevLogIndex, entries)
	}

	// Update commitIndex if leader's commit is higher
	rc.updateCommitIndex(leaderCommit)

	return nil
}

// verifyLogConsistency checks if we have the prevLogIndex entry with prevLogTerm
func (rc *RaftConsensus) verifyLogConsistency(prevLogIndex int, prevLogTerm int) bool {
	if prevLogIndex == 0 {
		return true // No previous entry to verify
	}

	rc.node.Mu.RLock()
	defer rc.node.Mu.RUnlock()

	logLen := len(rc.node.Log)
	localPrevIdx := prevLogIndex - rc.lastSnapshotIndex

	// Check if entry is in log
	if localPrevIdx > 0 && logLen >= localPrevIdx {
		return rc.node.Log[localPrevIdx-1].Term == prevLogTerm
	}

	// Check if entry is at snapshot boundary
	if prevLogIndex == rc.lastSnapshotIndex {
		return rc.lastSnapshotTerm == prevLogTerm
	}

	// Entry is beyond our log
	return false
}

// appendNewEntries appends entries to the log and persists them
func (rc *RaftConsensus) appendNewEntries(prevLogIndex int, entries []types.LogEntry) {
	rc.node.Mu.Lock()
	defer rc.node.Mu.Unlock()

	logLen := len(rc.node.Log)
	localPrevIdx := prevLogIndex - rc.lastSnapshotIndex

	// If entries conflict with existing entries, overwrite them
	if localPrevIdx > 0 && logLen > localPrevIdx {
		rc.node.Log = rc.node.Log[:localPrevIdx]
	}

	rc.node.Log = append(rc.node.Log, entries...)

	if err := rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after appending entries: %v", err))
	}
}

// updateCommitIndex updates the commit index based on leader's commit
func (rc *RaftConsensus) updateCommitIndex(leaderCommit int) {
	if leaderCommit <= rc.node.CommitIdx {
		return
	}

	rc.node.Mu.Lock()
	defer rc.node.Mu.Unlock()

	globalLastIdx := rc.lastSnapshotIndex + len(rc.node.Log)
	if leaderCommit < globalLastIdx {
		rc.node.CommitIdx = leaderCommit
	} else {
		rc.node.CommitIdx = globalLastIdx
	}
}

/*
	calculateCommitIndex : calculates the commit index based on matchIndex of followers.

- the commit index is the index of the highest log entry known to be committed.

- A leader can advance commitIndex to an index N if a majority of servers have
replicated an entry at least up to N, and the entry at N is from the leader’s
current term.

- this ensures that only entries from the current term can be committed,
which prevents the system from committing entries that might be from
a previous leader that has been superseded.

---

calculateCommitIndex checks the matchIndex of all followers to determine
if a new log entry can be considered committed.
*/
func (rc *RaftConsensus) calculateCommitIndex() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Only leaders can advance commitIndex
	rc.node.Mu.RLock()
	if rc.node.Role != "Leader" {
		rc.node.Mu.RUnlock()
		return
	}
	rc.node.Mu.RUnlock()

	// matchIndex values are now global indices (accounting for snapshots)
	matchIndexes := make([]int, len(rc.matchIndex))
	i := 0
	for _, v := range rc.matchIndex {
		matchIndexes[i] = v
		i++
	}

	// add the leader's own global log index
	rc.node.Mu.RLock()
	globalLogLen := rc.lastSnapshotIndex + len(rc.node.Log)
	matchIndexes = append(matchIndexes, globalLogLen)
	rc.node.Mu.RUnlock()

	// sort matchIndexes in descending order
	for i := 0; i < len(matchIndexes)-1; i++ {
		for j := 0; j < len(matchIndexes)-i-1; j++ {
			if matchIndexes[j] < matchIndexes[j+1] {
				matchIndexes[j], matchIndexes[j+1] = matchIndexes[j+1], matchIndexes[j]
			}
		}
	}
	majorityIndex := matchIndexes[len(matchIndexes)/2] // majority index (global)

	rc.node.Mu.Lock()
	if majorityIndex > rc.node.CommitIdx && majorityIndex <= globalLogLen {
		// Check if this entry can be committed using CanCommit
		// CanCommit handles both regular quorum and joint consensus quorum requirements
		if majorityIndex == 0 || rc.CanCommit(majorityIndex, rc.currentTerm) {
			rc.node.CommitIdx = majorityIndex
		}
	}
	rc.node.Mu.Unlock()
}

func (rc *RaftConsensus) RequestAddServer(nodeID int, rpcAddr string, httpAddr string) error {
	rc.node.Mu.RLock()

	// 1. check if leader if not leader: reject
	if rc.node.Role != "Leader" {
		rc.node.Mu.RUnlock()
		return fmt.Errorf("only leader can add servers")
	}
	rc.node.Mu.RUnlock()

	rc.configState.Mu.RLock()
	if _, exists := rc.configState.OldConfig.Nodes[nodeID]; exists {
		rc.configState.Mu.RUnlock()
		return fmt.Errorf("node ID %d already exists in the cluster", nodeID)
	}

	// Start with a copy of the old config for the new config
	rc.configState.NewConfig = &types.Config{
		Nodes: make(map[int]string),
		Index: rc.configState.OldConfig.Index + 1,
	}

	maps.Copy(rc.configState.NewConfig.Nodes, rc.configState.OldConfig.Nodes) // copy existing nodes to new config

	// Add the new node to the new config
	rc.configState.NewConfig.Nodes[nodeID] = httpAddr // since the connection is made via HTTP, we track the HTTP address in the config
	rc.configState.InTransition = true
	rc.configState.JointConfigIndex = rc.lastSnapshotIndex + len(rc.node.Log) + 1 // global index of the next log entry
	rc.configState.TransitionStartTime = time.Now()

	rc.logBuffer.AddLog("INFO", fmt.Sprintf("Proposed adding node %d with address %s. New config index: %d",
		nodeID, httpAddr, rc.configState.NewConfig.Index))

	entry := types.LogEntry{
		Term:    rc.GetCurrentTerm(),
		Index:   rc.configState.JointConfigIndex,
		Command: "CONFIG_CHANGE",
		ConfigChange: &types.ConfigChangeEntry{
			Type:    "AddServer",
			NodeID:  nodeID,
			Address: httpAddr,
		},
	}

	rc.configState.Mu.RUnlock()

	rc.node.Mu.Lock()
	rc.node.Log = append(rc.node.Log, entry)
	if err := rc.persister.SaveState(rc.GetCurrentTerm(), rc.GetVotedFor(), rc.node.Log); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after adding server: %v", err))
	}
	rc.node.Peers[nodeID] = rpcAddr
	rc.node.PeersHttp[nodeID] = httpAddr
	rc.node.Mu.Unlock()

	// Initialize replication tracking for the new peer
	// The new peer will be caught up via AppendEntries in the next heartbeat
	rc.mu.Lock()
	globalLogLen := rc.lastSnapshotIndex + len(rc.node.Log)
	rc.nextIndex[nodeID] = globalLogLen + 1 // new peer starts after current log
	rc.matchIndex[nodeID] = 0               // hasn't replicated anything yet
	rc.mu.Unlock()

	rc.logBuffer.AddLog("INFO", fmt.Sprintf("Initialized replication tracking for node %d. Will be caught up via next heartbeat", nodeID))

	return nil
}

func (rc *RaftConsensus) RequestRemoveServer(nodeID int) error {
	rc.node.Mu.RLock()

	// 1. check if leader if not leader: reject
	if rc.node.Role != "Leader" {
		rc.node.Mu.RUnlock()
		return fmt.Errorf("only leader can remove servers")
	}
	rc.node.Mu.RUnlock()

	rc.configState.Mu.RLock()
	if _, exists := rc.configState.OldConfig.Nodes[nodeID]; !exists {
		rc.configState.Mu.RUnlock()
		return fmt.Errorf("node ID %d does not exist in the cluster", nodeID)
	}

	// check if self
	if nodeID == rc.node.ID {
		rc.configState.Mu.RUnlock()
		return fmt.Errorf("leader cannot remove itself from the cluster")
	}

	// enter joint consensus
	rc.configState.NewConfig = &types.Config{
		Nodes: make(map[int]string),
		Index: rc.configState.OldConfig.Index + 1,
	}

	maps.Copy(rc.configState.NewConfig.Nodes, rc.configState.OldConfig.Nodes) // copy existing nodes to new config

	// Remove the node from the new config
	delete(rc.configState.NewConfig.Nodes, nodeID)
	rc.configState.InTransition = true
	rc.configState.JointConfigIndex = rc.lastSnapshotIndex + len(rc.node.Log) + 1 // global index of the next log entry
	rc.configState.TransitionStartTime = time.Now()

	rc.logBuffer.AddLog("INFO", fmt.Sprintf("Proposed removing node %d. New config index: %d",
		nodeID, rc.configState.NewConfig.Index))

	entry := types.LogEntry{
		Term:    rc.GetCurrentTerm(),
		Index:   rc.configState.JointConfigIndex,
		Command: "CONFIG_CHANGE",
		ConfigChange: &types.ConfigChangeEntry{
			Type:   "RemoveServer",
			NodeID: nodeID,
		},
	}

	rc.configState.Mu.RUnlock()

	rc.node.Mu.Lock()
	rc.node.Log = append(rc.node.Log, entry)
	if err := rc.persister.SaveState(rc.GetCurrentTerm(), rc.GetVotedFor(), rc.node.Log); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after removing server: %v", err))
	}
	rc.node.Mu.Unlock()

	// Cleanup replication tracking for the removed peer (by next heartbeat, it will be excluded)
	// The peer will naturally fall out of Peers map, and replication will stop
	rc.mu.Lock()
	delete(rc.nextIndex, nodeID)
	delete(rc.matchIndex, nodeID)
	rc.mu.Unlock()

	rc.logBuffer.AddLog("INFO", fmt.Sprintf("Marked node %d for removal. Replication tracking cleaned up", nodeID))

	return nil
}

func (rc *RaftConsensus) FinaliseConfigChange() error {
	rc.configState.Mu.Lock()
	defer rc.configState.Mu.Unlock()

	if !rc.configState.InTransition {
		return fmt.Errorf("no config change in progress")
	}

	// phase 2: commit the new config
	rc.configState.FinalConfigIndex = rc.lastSnapshotIndex + len(rc.node.Log) // global index of the last log entry
	rc.configState.OldConfig = rc.configState.NewConfig
	rc.configState.InTransition = false
	rc.configState.NewConfig = nil
	rc.configState.JointConfigIndex = 0
	rc.configState.TransitionStartTime = time.Time{}

	rc.logBuffer.AddLog("INFO", fmt.Sprintf("Finalized config change. New config index: %d",
		rc.configState.OldConfig.Index))

	entry := types.LogEntry{
		Term:    rc.GetCurrentTerm(),
		Index:   rc.configState.FinalConfigIndex,
		Command: "CONFIG_CHANGE",
		ConfigChange: &types.ConfigChangeEntry{
			Type: "FinalizeConfig",
		},
	}

	rc.node.Mu.Lock()
	rc.node.Log = append(rc.node.Log, entry)
	if err := rc.persister.SaveState(rc.GetCurrentTerm(), rc.GetVotedFor(), rc.node.Log); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to save state after finalizing config change: %v", err))
	}
	rc.node.Mu.Unlock()

	return nil
}

func (rc *RaftConsensus) handleConfigChange(entry types.LogEntry) {
	if entry.ConfigChange == nil {
		rc.logBuffer.AddLog("ERROR", "CONFIG_CHANGE entry missing ConfigChangeEntry")
		return
	}

	switch entry.ConfigChange.Type {
	case "AddServer", "RemoveServer":
		rc.handleJointConsensusEntry()
	case "FinalizeConfig":
		rc.handleFinalizeConfigEntry()
	default:
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Unknown config change type: %s", entry.ConfigChange.Type))
	}
}

// handleJointConsensusEntry handles phase 1 of config change (joint consensus)
func (rc *RaftConsensus) handleJointConsensusEntry() {
	rc.node.Mu.RLock()
	isLeader := rc.node.Role == "Leader"
	rc.node.Mu.RUnlock()
	if isLeader {
		if err := rc.FinaliseConfigChange(); err != nil {
			rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to finalize config change: %v", err))
		}
	}
}

// handleFinalizeConfigEntry handles phase 2 of config change (applying new config)
func (rc *RaftConsensus) handleFinalizeConfigEntry() {
	currentConfig := rc.GetCurrentConfig()
	if currentConfig == nil {
		return
	}

	rc.logBuffer.AddLog("INFO", fmt.Sprintf("Applying finalized config change. Current config index: %d, Nodes: %v",
		currentConfig.Index, currentConfig.Nodes))

	rc.updateReplicationTracking(currentConfig)
}

// updateReplicationTracking updates nextIndex and matchIndex for new/removed nodes
func (rc *RaftConsensus) updateReplicationTracking(config *types.Config) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	globalLogLen := rc.lastSnapshotIndex + len(rc.node.Log)

	// Initialize tracking for new nodes in the config
	for peerID := range config.Nodes {
		if _, exists := rc.nextIndex[peerID]; !exists {
			rc.nextIndex[peerID] = globalLogLen + 1
		}
		if _, exists := rc.matchIndex[peerID]; !exists {
			rc.matchIndex[peerID] = 0
		}
	}

	// Cleanup tracking for removed nodes
	for peerID := range rc.matchIndex {
		if _, exists := config.Nodes[peerID]; !exists {
			delete(rc.matchIndex, peerID)
			delete(rc.nextIndex, peerID)
		}
	}
}

/* ---------- Starting the consensus --------- */

/*
	Start : starts the Raft consensus algorithm.

- RAFT works by having nodes transition between
three states: Follower, Candidate, and Leader.
Followers listen for messages from leaders.
If they don't hear from a leader within
a timeout, they become candidates
and start an election to become the new leader.

- Candidates request votes from other nodes.
If a candidate receives a majority of votes,
it becomes the leader.
Leaders send heartbeats to followers
to maintain their authority

- matchIndex and nextIndex are used by leaders to keep track of
the state of log replication on followers.
matchIndex tracks the highest log entry index known to be replicated on each follower,
while nextIndex tracks the next log entry index to send to each follower.

---
Start handles these state transitions
and message handling.

it's a go routine because it runs concurrently with other parts of the system.
*/
func (rc *RaftConsensus) Start(store *storage.Store) {
	// Start goroutine to handle state transitions and message handling
	go rc.runElectionAndHeartbeatLoop()

	// Start goroutine to apply committed log entries to state machine
	go rc.runApplyLoop()

	go rc.StartConsumption(store)
}

// runElectionAndHeartbeatLoop handles election and heartbeat logic
func (rc *RaftConsensus) runElectionAndHeartbeatLoop() {
	for {
		select {
		case <-rc.electionTimer.C:
			rc.handleElectionTimeout()
		case <-rc.heartbeatTimer.C:
			rc.handleHeartbeatTimeout()
		}
	}
}

// handleElectionTimeout processes election timeout events
func (rc *RaftConsensus) handleElectionTimeout() {
	rc.mu.Lock()
	rc.node.Mu.Lock()

	if rc.node.Role != "Leader" {
		rc.node.Role = "Candidate"
		rc.currentTerm++
		rc.votedFor = rc.node.ID

		votesGranted := rc.conductElection()

		peerCount := len(rc.node.Peers)
		totalnodes := peerCount + 1

		// if majority, become leader
		if votesGranted > totalnodes/2 {
			rc.becomeLeader()
		} else {
			rc.node.Role = "Follower"
		}
	}

	rc.node.Mu.Unlock()
	rc.mu.Unlock()

	// reset election timer after election timeout with random value
	rc.electionTimer.Reset(GetRandomElectionTimeout())
}

// conductElection sends RequestVote RPC to all peers and collects votes
func (rc *RaftConsensus) conductElection() int {
	votesCh := make(chan bool, len(rc.node.Peers))

	for _, peerAddr := range rc.node.Peers {
		go rc.sendRequestVoteToPeer(peerAddr, votesCh)
	}

	peerCount := len(rc.node.Peers)
	votesGranted := 1 // count self-vote
	responseCount := 0

	// Collect votes with timeout - stop early if we get majority
	voteTimeout := time.NewTimer(100 * time.Millisecond)
	totalnodes := peerCount + 1

	for responseCount < peerCount {
		select {
		case granted := <-votesCh:
			responseCount++
			if granted {
				votesGranted++
			}
			// Check if we have majority - elect immediately if so
			if votesGranted > totalnodes/2 {
				responseCount = peerCount // break out of loop
			}
		case <-voteTimeout.C:
			// Timeout waiting for responses from offline nodes
			responseCount = peerCount // break out of loop
		}
	}

	voteTimeout.Stop() // Stop the timer after vote collection
	return votesGranted
}

// sendRequestVoteToPeer sends a RequestVote RPC to a single peer
func (rc *RaftConsensus) sendRequestVoteToPeer(addr string, votesCh chan bool) {
	// Use global log index (snapshot offset + local length)
	lastLogIdx := rc.lastSnapshotIndex + len(rc.node.Log)
	lastLogTerm := 0
	if len(rc.node.Log) > 0 {
		lastLogTerm = rc.node.Log[len(rc.node.Log)-1].Term
	} else if rc.lastSnapshotTerm > 0 {
		lastLogTerm = rc.lastSnapshotTerm
	}
	granted, _, err := rpc.SendRequestVote(addr, rc.currentTerm, rc.node.ID, lastLogIdx, lastLogTerm)
	if err == nil {
		votesCh <- granted // true or false
	}
	// If error (node offline), don't send anything - ignore it
}

// becomeLeader transitions the node to leader and sends initial heartbeats
func (rc *RaftConsensus) becomeLeader() {
	rc.node.Role = "Leader"
	rc.node.LeaderID = rc.node.ID // Track this node as leader

	// Initialize nextIndex and matchIndex for all peers (global indices)
	globalLogLen := rc.lastSnapshotIndex + len(rc.node.Log)
	for peerID := range rc.node.Peers {
		rc.nextIndex[peerID] = globalLogLen + 1
		rc.matchIndex[peerID] = 0
	}

	// Immediately send heartbeats to all followers to establish leadership
	for peerID, peerAddr := range rc.node.Peers {
		go rc.sendInitialHeartbeat(peerID, peerAddr)
	}

	rc.heartbeatTimer.Reset(HeartbeatIntervalMs * time.Millisecond)
}

// sendInitialHeartbeat sends the initial heartbeat to a follower upon becoming leader
func (rc *RaftConsensus) sendInitialHeartbeat(peerID int, peerAddr string) {
	nextIdx := rc.nextIndex[peerID]
	prevLogIdx := nextIdx - 1
	prevLogTerm := rc.getPrevLogTerm(prevLogIdx)

	entries := rc.getEntriesForReplication(nextIdx)

	_, _, err := rpc.SendAppendEntries(peerAddr, rc.currentTerm, rc.node.ID, prevLogIdx, prevLogTerm, rc.node.CommitIdx, entries)
	if err != nil {
		rc.logBuffer.AddLog("DEBUG", fmt.Sprintf("SendAppendEntries to peer %d failed: %v", peerID, err))
	}
}

// handleHeartbeatTimeout processes heartbeat timeout events
func (rc *RaftConsensus) handleHeartbeatTimeout() {
	rc.node.Mu.RLock()
	isLeader := rc.node.Role == "Leader"
	rc.node.Mu.RUnlock()

	if isLeader {
		for followerID, peerAddr := range rc.node.Peers {
			go rc.replicateToFollower(followerID, peerAddr)
		}
		rc.heartbeatTimer.Reset(HeartbeatIntervalMs * time.Millisecond)
	}
}

// replicateToFollower replicates log entries to a single follower
func (rc *RaftConsensus) replicateToFollower(followerID int, followerAddr string) {
	// Snapshot state inside locks before RPC
	rc.mu.Lock()
	nextIdx := rc.nextIndex[followerID]
	currentTerm := rc.currentTerm
	snapshotIdx := rc.lastSnapshotIndex
	snapshotTrm := rc.lastSnapshotTerm
	snap := rc.snapshot
	rc.mu.Unlock()

	// If follower is behind our snapshot, send InstallSnapshot instead
	if nextIdx <= snapshotIdx && snap != nil {
		rc.sendSnapshotToFollower(followerID, followerAddr, currentTerm, snapshotIdx, snapshotTrm)
		return
	}

	rc.sendAppendEntriesToFollower(followerID, followerAddr, currentTerm, nextIdx, snapshotIdx, snapshotTrm)
}

// sendSnapshotToFollower sends a snapshot to a lagging follower
func (rc *RaftConsensus) sendSnapshotToFollower(followerID int, followerAddr string, currentTerm, snapshotIdx, snapshotTrm int) {
	replyTerm, err := rpc.SendInstallSnapshot(
		followerAddr, currentTerm, rc.node.ID,
		snapshotIdx, snapshotTrm, rc.snapshot.Data,
	)
	if err != nil {
		return
	}

	if replyTerm > currentTerm {
		rc.mu.Lock()
		rc.currentTerm = replyTerm
		rc.votedFor = -1
		rc.mu.Unlock()
		rc.node.Mu.Lock()
		rc.node.Role = "Follower"
		rc.node.Mu.Unlock()
		return
	}

	// Follower accepted snapshot, update tracking
	rc.mu.Lock()
	rc.matchIndex[followerID] = snapshotIdx
	rc.nextIndex[followerID] = snapshotIdx + 1
	rc.mu.Unlock()
}

// sendAppendEntriesToFollower sends AppendEntries RPC to a follower
func (rc *RaftConsensus) sendAppendEntriesToFollower(followerID int, followerAddr string, currentTerm, nextIdx, snapshotIdx, snapshotTrm int) {
	rc.node.Mu.RLock()
	leaderId := rc.node.ID
	commitIdx := rc.node.CommitIdx
	prevLogIdx := nextIdx - 1
	prevLogTerm := rc.getPrevLogTermLocked(prevLogIdx, snapshotIdx, snapshotTrm)
	entries := rc.getEntriesForReplicationLocked(nextIdx, snapshotIdx)
	rc.node.Mu.RUnlock()

	// RPC call with cached values
	success, newTerm, err := rpc.SendAppendEntries(followerAddr, currentTerm, leaderId, prevLogIdx, prevLogTerm, commitIdx, entries)

	if err != nil {
		return
	}

	if newTerm > currentTerm {
		// step down if term is higher
		rc.mu.Lock()
		rc.currentTerm = newTerm
		rc.votedFor = -1
		rc.mu.Unlock()
		rc.node.Mu.Lock()
		rc.node.Role = "Follower"
		rc.node.Mu.Unlock()
		return
	}

	// update the matchIndex and nextIndex for the follower (global indices)
	if success {
		rc.node.Mu.RLock()
		globalLogLen := snapshotIdx + len(rc.node.Log)
		rc.node.Mu.RUnlock()

		rc.mu.Lock()
		rc.matchIndex[followerID] = globalLogLen
		rc.nextIndex[followerID] = globalLogLen + 1
		rc.mu.Unlock()

		// calculate commit index based on matchIndex (outside lock)
		rc.calculateCommitIndex()
	} else {
		// replication failed, decrement nextIndex and retry
		rc.mu.Lock()
		if rc.nextIndex[followerID] > rc.lastSnapshotIndex+1 {
			rc.nextIndex[followerID]--
		}
		rc.mu.Unlock()
	}
}

// getPrevLogTerm calculates the term of the entry before nextIdx
func (rc *RaftConsensus) getPrevLogTerm(prevLogIdx int) int {
	prevLogTerm := 0
	localPrev := prevLogIdx - rc.lastSnapshotIndex
	if localPrev > 0 && localPrev <= len(rc.node.Log) {
		prevLogTerm = rc.node.Log[localPrev-1].Term
	} else if prevLogIdx == rc.lastSnapshotIndex && rc.lastSnapshotTerm > 0 {
		prevLogTerm = rc.lastSnapshotTerm
	}
	return prevLogTerm
}

// getPrevLogTermLocked calculates the term of the entry before nextIdx (assumes locks are held)
func (rc *RaftConsensus) getPrevLogTermLocked(prevLogIdx, snapshotIdx, snapshotTrm int) int {
	prevLogTerm := 0
	localPrev := prevLogIdx - snapshotIdx
	if localPrev > 0 && localPrev <= len(rc.node.Log) {
		prevLogTerm = rc.node.Log[localPrev-1].Term
	} else if prevLogIdx == snapshotIdx && snapshotTrm > 0 {
		prevLogTerm = snapshotTrm
	}
	return prevLogTerm
}

// getEntriesForReplication gets entries to replicate starting from nextIdx
func (rc *RaftConsensus) getEntriesForReplication(nextIdx int) []types.LogEntry {
	entries := []types.LogEntry{}
	localNext := nextIdx - rc.lastSnapshotIndex
	if localNext > 0 && localNext <= len(rc.node.Log) {
		entries = rc.node.Log[localNext-1:]
	}
	return entries
}

// getEntriesForReplicationLocked gets entries to replicate starting from nextIdx (assumes node lock is held)
func (rc *RaftConsensus) getEntriesForReplicationLocked(nextIdx, snapshotIdx int) []types.LogEntry {
	entries := []types.LogEntry{}
	localNext := nextIdx - snapshotIdx
	if localNext > 0 && localNext <= len(rc.node.Log) {
		original := rc.node.Log[localNext-1:]
		entries = make([]types.LogEntry, len(original))
		copy(entries, original)
	}
	return entries
}

// runApplyLoop applies committed log entries to the state machine
func (rc *RaftConsensus) runApplyLoop() {
	for {
		rc.mu.Lock()
		rc.node.Mu.RLock()
		commitIdx := rc.node.CommitIdx
		logLen := len(rc.node.Log)
		snapshotOffset := rc.lastSnapshotIndex
		rc.node.Mu.RUnlock()
		lastApplied := rc.lastApplied

		// lastApplied and commitIdx are global indices
		// Convert to local index for array access
		if lastApplied < commitIdx {
			rc.lastApplied++
			globalIdx := rc.lastApplied
			localIdx := globalIdx - snapshotOffset
			rc.mu.Unlock()

			// Fetch the entry to apply while holding node lock
			rc.node.Mu.RLock()
			if localIdx > 0 && localIdx <= logLen {
				entry := rc.node.Log[localIdx-1]
				rc.node.Mu.RUnlock()

				// Send to applychannel (may block)
				rc.applyCh <- types.ApplyMsg{
					CommandValid: true,
					Command:      entry,
					CommandIndex: globalIdx,
				}
			} else {
				rc.node.Mu.RUnlock()
			}
		} else {
			rc.mu.Unlock()
			time.Sleep(10 * time.Millisecond) // avoid busy waiting
		}
	}
}

/*
	Start consumption of applied log entries.

- this is where the state machine applies the committed log entries
to the actual key-value store.
- it listens on the applyCh for new committed entries, and when it receives one,
it executes the command on the store.

---
StartApplyConsumer runs a goroutine that listens for committed log entries
on the applyCh channel.
When it receives a new entry, it parses the command and applies it to
the state machine (key-value store).
*/
func (rc *RaftConsensus) StartConsumption(store *storage.Store) {
	rc.node.Mu.RLock()
	id := rc.node.ID
	rc.node.Mu.RUnlock()

	storeFilepath := rc.persister.GetStoreFilepath()

	for applyMsg := range rc.applyCh {
		if !applyMsg.CommandValid {
			continue
		}

		entry := applyMsg.Command

		rc.logBuffer.AddLog("INFO", fmt.Sprintf("[Node %d] Received committed entry: Term %d, Command: %s (index: %d)", id, entry.Term, entry.Command, applyMsg.CommandIndex))

		// Execute the command on the state machine
		rc.executeCommand(store, storeFilepath, entry, id, applyMsg.CommandIndex)

		// Trigger snapshot if enough entries have been applied since last snapshot
		rc.checkAndTakeSnapshot(id)
	}
}

// executeCommand applies a single command to the state machine
func (rc *RaftConsensus) executeCommand(store *storage.Store, storeFilepath string, entry types.LogEntry, nodeID int, cmdIndex int) {
	switch entry.Command {
	case "SET":
		rc.executeSetCommand(store, storeFilepath, entry, nodeID, cmdIndex)
	case "DELETE":
		rc.executeDeleteCommand(store, storeFilepath, entry, nodeID, cmdIndex)
	case "CLEAN":
		rc.executeCleanCommand(store, storeFilepath, nodeID, cmdIndex)
	case "CONFIG_CHANGE":
		rc.executeConfigChangeCommand(entry)
	default:
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("[Node %d] Unknown command type: %s", nodeID, entry.Command))
	}
}

// executeSetCommand handles the SET command
func (rc *RaftConsensus) executeSetCommand(store *storage.Store, storeFilepath string, entry types.LogEntry, nodeID int, cmdIndex int) {
	if entry.Key == "" {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("[Node %d] Invalid SET command: missing key", nodeID))
		return
	}
	store.Set(entry.Key, entry.Value)
	rc.logBuffer.AddLog("INFO", fmt.Sprintf("[Node %d] Applied SET %s = %v (index: %d)", nodeID, entry.Key, entry.Value, cmdIndex))
	if err := store.SaveState(storeFilepath); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("[Node %d] Failed to persist store state: %v", nodeID, err))
	}
}

// executeDeleteCommand handles the DELETE command
func (rc *RaftConsensus) executeDeleteCommand(store *storage.Store, storeFilepath string, entry types.LogEntry, nodeID int, cmdIndex int) {
	if entry.Key == "" {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("[Node %d] Invalid DELETE command: missing key", nodeID))
		return
	}
	store.Delete(entry.Key)
	rc.logBuffer.AddLog("INFO", fmt.Sprintf("[Node %d] Applied DELETE %s (index: %d)", nodeID, entry.Key, cmdIndex))
	if err := store.SaveState(storeFilepath); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("[Node %d] Failed to persist store state: %v", nodeID, err))
	}
}

// executeCleanCommand handles the CLEAN command
func (rc *RaftConsensus) executeCleanCommand(store *storage.Store, storeFilepath string, nodeID int, cmdIndex int) {
	if err := rc.persister.ClearState(); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("Failed to clear persister state: %v", err))
	}
	store.ClearAll()
	if err := store.SaveState(storeFilepath); err != nil {
		rc.logBuffer.AddLog("ERROR", fmt.Sprintf("[Node %d] Failed to persist store state after CLEAN: %v", nodeID, err))
	}
	rc.node.Log = []types.LogEntry{} // clear in-memory log as well
	rc.logBuffer.AddLog("INFO", fmt.Sprintf("[Node %d] Applied CLEAN all data (index: %d)", nodeID, cmdIndex))
}

// executeConfigChangeCommand handles the CONFIG_CHANGE command
func (rc *RaftConsensus) executeConfigChangeCommand(entry types.LogEntry) {
	if entry.ConfigChange != nil {
		rc.handleConfigChange(entry)
	}
}

// checkAndTakeSnapshot checks if a snapshot should be taken and takes it if needed
func (rc *RaftConsensus) checkAndTakeSnapshot(nodeID int) {
	rc.mu.Lock()
	rc.snapshotAppliedCount++
	shouldSnapshot := rc.snapshotAppliedCount >= SnapshotThreshold
	rc.mu.Unlock()

	if shouldSnapshot {
		if err := rc.TakeSnapshot(); err != nil {
			rc.logBuffer.AddLog("ERROR", fmt.Sprintf("[Node %d] Failed to take snapshot: %v", nodeID, err))
		}
	}
}
