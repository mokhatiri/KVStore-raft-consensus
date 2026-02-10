// Package consensus implements the Raft consensus algorithm.
package consensus

import (
	"fmt"
	"log"
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

	nextIndex  []int // For leaders, next log entry to send to each follower
	matchIndex []int // For leaders, index of highest log entry known to be replicated on each follower

	applyCh chan types.ApplyMsg

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	node      *types.Node
	persister *types.Persister
	// the node has:
	// ID, Address, Peers, Role, Log, CommitIdx
}

func NewRaftConsensus(node *types.Node) *RaftConsensus {
	persister := types.NewPersister(node.ID, ".")

	// Load persisted state if available
	term, votedFor, logEntries, err := persister.LoadState()
	if err != nil {
		log.Printf("Error reading persisted state: %v", err)
	}

	// put the log in place
	node.Mu.Lock()
	node.Log = logEntries
	node.Mu.Unlock()

	return &RaftConsensus{
		currentTerm: term,
		votedFor:    votedFor,
		lastApplied: 0,

		nextIndex:  make([]int, len(node.Peers)),
		matchIndex: make([]int, len(node.Peers)),

		applyCh: make(chan types.ApplyMsg), // channel to apply committed log entries

		electionTimer:  time.NewTimer(GetRandomElectionTimeout()),             // randomized election timeout
		heartbeatTimer: time.NewTimer(HeartbeatIntervalMs * time.Millisecond), // heartbeat interval

		node:      node,
		persister: persister,
	}
}

/* - getters, prints - */

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

func (rc *RaftConsensus) GetNodeInfo() (int, string) {
	rc.node.Mu.RLock()
	defer rc.node.Mu.RUnlock()
	return rc.node.ID, rc.node.Role
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

/* ------------------- */

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
	entry := types.LogEntry{
		Term:    rc.currentTerm,
		Command: command,
	}

	parts := strings.SplitN(command, ":", 3)
	if len(parts) >= 2 {
		entry.Key = parts[1]
	}
	if len(parts) >= 3 {
		entry.Value = parts[2]
	}

	rc.node.Log = append(rc.node.Log, entry)
	// persister
	rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log)

	index = len(rc.node.Log)

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
		rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log)

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
	localLastTerm := 0
	if localLastIdx > 0 {
		localLastTerm = rc.node.Log[localLastIdx-1].Term
	}
	rc.node.Mu.RUnlock()

	// Up-to-date check: candidate's log is at least as up-to-date if:
	// - candidate's lastLogTerm > local lastLogTerm, OR
	// - terms are equal AND candidate's lastLogIndex >= local lastLogIndex
	upToDate := lastLogTerm > localLastTerm || (lastLogTerm == localLastTerm && lastLogIndex >= localLastIdx)
	if !upToDate {
		return false, rc.currentTerm
	}

	// All conditions passed - grant vote
	rc.votedFor = candidateId
	// persist
	rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log)
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
		rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log)
		rc.node.Mu.RUnlock()
	}
	// if it's the same term, do nothing
	// because the node is already in sync with the leader

	// Step down if we're a candidate and receive AppendEntries from a valid leader
	rc.node.Mu.Lock()
	rc.node.Role = "Follower"
	rc.node.Mu.Unlock()

	// Verify log consistency: check if we have the prevLogIndex entry with prevLogTerm
	rc.node.Mu.RLock()
	logLen := len(rc.node.Log)
	var logValid bool = true
	if prevLogIndex > 0 && logLen >= prevLogIndex {
		logValid = (rc.node.Log[prevLogIndex-1].Term == prevLogTerm)
	} else if prevLogIndex > 0 {
		logValid = false
	}
	rc.node.Mu.RUnlock()

	if prevLogIndex > 0 && !logValid {
		// Log doesn't match at prevLogIndex, reject the append
		return fmt.Errorf("log mismatch at index %d, expected term %d", prevLogIndex, prevLogTerm)
	}

	// append new entries to the log
	if len(entries) > 0 {
		rc.node.Mu.Lock()
		// If entries conflict with existing entries, overwrite them
		if prevLogIndex > 0 && logLen > prevLogIndex {
			// Remove conflicting entries
			rc.node.Log = rc.node.Log[:prevLogIndex]
		}
		rc.node.Log = append(rc.node.Log, entries...) // append new entries to the log
		rc.persister.SaveState(rc.currentTerm, rc.votedFor, rc.node.Log)
		rc.node.Mu.Unlock()
	}

	// Update commitIndex if leader's commit is higher
	// commitIndex = min(leaderCommit, index of last new entry)
	if leaderCommit > rc.node.CommitIdx {
		rc.node.Mu.Lock()
		lastNewIdx := len(rc.node.Log)
		if leaderCommit < lastNewIdx {
			rc.node.CommitIdx = leaderCommit
		} else {
			rc.node.CommitIdx = lastNewIdx
		}
		rc.node.Mu.Unlock()
	}

	return nil
}

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
	go func() {
		for {
			select {
			case <-rc.electionTimer.C:
				rc.mu.Lock()
				rc.node.Mu.Lock()

				// check the role
				if rc.node.Role != "Leader" {
					rc.node.Role = "Candidate"
					rc.currentTerm += 1
					rc.votedFor = rc.node.ID

					votesCh := make(chan bool, len(rc.node.Peers))

					for _, peerAddr := range rc.node.Peers {
						// for every peer, send RequestVote RPC
						go func(addr string) {
							lastLogIdx := len(rc.node.Log)
							lastLogTerm := 0
							if lastLogIdx > 0 {
								lastLogTerm = rc.node.Log[lastLogIdx-1].Term
							}
							granted, _, err := rpc.SendRequestVote(addr, rc.currentTerm, rc.node.ID, lastLogIdx, lastLogTerm)
							if err == nil {
								votesCh <- granted // true or false
							}
							// If error (node offline), don't send anything - ignore it
						}(peerAddr)
					}

					votesGranted := 1 // count self-vote
					responseCount := 0
					peerCount := len(rc.node.Peers)
					totalnodes := peerCount + 1

					// Collect votes with timeout - stop early if we get majority
					voteTimeout := time.NewTimer(100 * time.Millisecond)

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

					// if majority, become leader
					if votesGranted > totalnodes/2 {
						rc.node.Role = "Leader"
						// Initialize nextIndex and matchIndex for all peers
						logLen := len(rc.node.Log)
						for i := range rc.node.Peers {
							rc.nextIndex[i] = logLen + 1 // nextIndex should start at log length + 1
							rc.matchIndex[i] = 0
						}
						// Immediately send heartbeats to all followers to establish leadership
						for i, peerAddr := range rc.node.Peers {
							nextIdx := rc.nextIndex[i]
							prevLogIdx := nextIdx - 1
							prevLogTerm := 0
							if prevLogIdx > 0 && prevLogIdx <= len(rc.node.Log) {
								prevLogTerm = rc.node.Log[prevLogIdx-1].Term
							}
							entries := []types.LogEntry{}
							if nextIdx <= len(rc.node.Log) {
								entries = rc.node.Log[nextIdx-1:]
							}
							go rpc.SendAppendEntries(peerAddr, rc.currentTerm, rc.node.ID, prevLogIdx, prevLogTerm, rc.node.CommitIdx, entries)
						}
						rc.heartbeatTimer.Reset(HeartbeatIntervalMs * time.Millisecond)
					} else {
						rc.node.Role = "Follower"
					}
				}
				rc.node.Mu.Unlock()
				rc.mu.Unlock()

				// reset election timer after election timeout with random value
				rc.electionTimer.Reset(GetRandomElectionTimeout())

			case <-rc.heartbeatTimer.C:
				// send AppendEntries (heartbeats) to followers if leader
				rc.node.Mu.RLock()
				isLeader := rc.node.Role == "Leader"
				rc.node.Mu.RUnlock()
				if isLeader {
					for i, peerAddr := range rc.node.Peers {
						go func(followerAddr string, followerIdx int) {
							// Snapshot state inside locks before RPC
							rc.mu.Lock()
							nextIdx := rc.nextIndex[followerIdx]
							currentTerm := rc.currentTerm
							rc.mu.Unlock()

							rc.node.Mu.RLock()
							leaderId := rc.node.ID
							commitIdx := rc.node.CommitIdx
							prevLogIdx := nextIdx - 1
							prevLogTerm := 0
							if prevLogIdx > 0 && prevLogIdx <= len(rc.node.Log) {
								prevLogTerm = rc.node.Log[prevLogIdx-1].Term
							}
							var entries []types.LogEntry
							if nextIdx > 0 && nextIdx <= len(rc.node.Log) {
								original := rc.node.Log[nextIdx-1:]
								entries = make([]types.LogEntry, len(original))
								copy(entries, original)
							}
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

							// update the matchIndex and nextIndex for the follower
							if success {
								rc.node.Mu.RLock()
								logLen := len(rc.node.Log)
								rc.node.Mu.RUnlock()

								rc.mu.Lock()
								rc.matchIndex[followerIdx] = logLen
								rc.nextIndex[followerIdx] = logLen + 1
								rc.mu.Unlock()

								// calculate commit index based on matchIndex (outside lock)
								rc.calculateCommitIndex()
							} else {
								// replication failed, decrement nextIndex and retry
								rc.mu.Lock()
								if rc.nextIndex[followerIdx] > 1 {
									rc.nextIndex[followerIdx] -= 1
								}
								rc.mu.Unlock()
							}
						}(peerAddr, i)
					}

					rc.heartbeatTimer.Reset(HeartbeatIntervalMs * time.Millisecond)
				}
			}
		}
	}()

	// Start goroutine to apply committed log entries to state machine
	go func() {
		for {
			rc.mu.Lock()
			rc.node.Mu.RLock()
			commitIdx := rc.node.CommitIdx
			logLen := len(rc.node.Log)
			rc.node.Mu.RUnlock()
			lastApplied := rc.lastApplied

			// Check if we have entries to apply
			if lastApplied < commitIdx && lastApplied < logLen {
				rc.lastApplied += 1
				idx := rc.lastApplied
				rc.mu.Unlock()

				// Fetch the entry to apply while holding node lock
				rc.node.Mu.RLock()
				if idx > 0 && idx <= logLen {
					entry := rc.node.Log[idx-1]
					rc.node.Mu.RUnlock()

					// Send to applychannel (may block)
					rc.applyCh <- types.ApplyMsg{
						CommandValid: true,
						Command:      entry,
						CommandIndex: idx,
					}
				} else {
					rc.node.Mu.RUnlock()
				}
			} else {
				rc.mu.Unlock()
				time.Sleep(10 * time.Millisecond) // avoid busy waiting
			}
		}
	}()

	go rc.StartConsumption(store)
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

	for applyMsg := range rc.applyCh {
		if !applyMsg.CommandValid {
			continue
		}

		entry := applyMsg.Command
		commandStr := entry.Command

		// for the cli visualisation!
		log.Printf("\n[Node %d] Received committed entry: Term %d, Command: %s (index: %d)", id, entry.Term, commandStr, applyMsg.CommandIndex)

		// Parse the command string
		parts := strings.SplitN(commandStr, ":", 3)
		if len(parts) < 1 {
			log.Printf("[Node %d] Invalid command format: %s", id, commandStr)
			continue
		}

		commandType := parts[0]
		// Execute the command on the state machine
		switch commandType {
		case "SET":
			if len(parts) < 3 {
				log.Printf("[Node %d] Invalid SET command format: %s", id, commandStr)
				continue
			}
			key := parts[1]
			value := parts[2]
			store.Set(key, value)
			log.Printf("[Node %d] Applied SET %s = %s (index: %d)", id, key, value, applyMsg.CommandIndex)

		case "DELETE":
			if len(parts) < 2 {
				log.Printf("[Node %d] Invalid DELETE command format: %s", id, commandStr)
				continue
			}
			key := parts[1]
			store.Delete(key)
			log.Printf("[Node %d] Applied DELETE %s (index: %d)", id, key, applyMsg.CommandIndex)

		case "CLEAN":
			rc.persister.ClearState()
			store.ClearAll()
			rc.node.Log = []types.LogEntry{} // clear in-memory log as well
			log.Printf("[Node %d] Applied CLEAN all data (index: %d)", id, applyMsg.CommandIndex)

		default:
			log.Printf("[Node %d] Unknown command type: %s", id, commandType)
		}
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

	// using matchIndex, we sort, calculate the majority index,
	// and check if the entry at that index is from current term
	matchIndexes := make([]int, len(rc.matchIndex))
	copy(matchIndexes, rc.matchIndex)

	// add the leader's own log index as well
	rc.node.Mu.RLock()
	matchIndexes = append(matchIndexes, len(rc.node.Log))
	rc.node.Mu.RUnlock()

	// sort matchIndexes in descending order
	for i := 0; i < len(matchIndexes)-1; i++ {
		for j := 0; j < len(matchIndexes)-i-1; j++ {
			if matchIndexes[j] < matchIndexes[j+1] {
				matchIndexes[j], matchIndexes[j+1] = matchIndexes[j+1], matchIndexes[j]
			}
		}
	}
	majorityIndex := matchIndexes[len(matchIndexes)/2] // majority index

	rc.node.Mu.RLock()
	if majorityIndex > rc.node.CommitIdx && majorityIndex <= len(rc.node.Log) {
		// check if the entry at majorityIndex is from current term
		if majorityIndex == 0 || (majorityIndex > 0 && rc.node.Log[majorityIndex-1].Term == rc.currentTerm) {
			rc.node.CommitIdx = majorityIndex
		}
	}
	rc.node.Mu.RUnlock()

}
