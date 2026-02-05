// Package consensus implements the Raft consensus algorithm.
package consensus

import (
	"fmt"
	"sync"
	"time"

	"distributed-kv/rpc"
	"distributed-kv/types"
)

type ApplyMsg struct {
	CommandValid bool
	Command      types.LogEntry
	CommandIndex int
}
type RaftConsensus struct {
	mu          sync.Mutex
	currentTerm int
	votedFor    int
	lastApplied int

	nextIndex  []int // For leaders, next log entry to send to each follower
	matchIndex []int // For leaders, index of highest log entry known to be replicated on each follower

	applyCh chan ApplyMsg

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	node *types.Node
	// the node has:
	// ID, Address, Peers, Role, Log, CommitIdx
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

// GetApplyCh returns the channel for applied log entries
func (rc *RaftConsensus) GetApplyCh() <-chan ApplyMsg {
	return rc.applyCh
}

// Propose submits a new command to the Raft log (only works if this node is the leader)
// Returns the index at which the command will appear if committed, the current term, and whether this node is the leader
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
	rc.node.Log = append(rc.node.Log, entry)
	index = len(rc.node.Log)

	return index, rc.currentTerm, true
}

func NewRaftConsensus(node *types.Node) *RaftConsensus {
	return &RaftConsensus{
		currentTerm: 0,
		votedFor:    -1,
		lastApplied: 0,

		nextIndex:  make([]int, len(node.Peers)),
		matchIndex: make([]int, len(node.Peers)),

		applyCh: make(chan ApplyMsg), // channel to apply committed log entries

		electionTimer:  time.NewTimer(GetRandomElectionTimeout()),             // randomized election timeout
		heartbeatTimer: time.NewTimer(HeartbeatIntervalMs * time.Millisecond), // heartbeat interval

		node: node,
	}
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
- First-Come-First-Served: The voter has not already voted for another candidate in the same term (i.e., votedFor is null or candidateId).
- Log Completeness: The candidate's log is at least as up-to-date as the voter's log.
*/
func (rc *RaftConsensus) RequestVote(term int, candidateId int, lastLogIndex int, lastLogTerm int) (bool, int) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// If candidate's term is higher, update our term and reset votedFor
	if term > rc.currentTerm {
		rc.currentTerm = term
		rc.votedFor = -1
		// Step down to follower if we were leader/candidate
		rc.node.Mu.Lock()
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
	}
	// if it's the same term, do nothing
	// because the node is already in sync with the leader

	// Step down if we're a candidate and receive AppendEntries from a valid leader
	rc.node.Mu.Lock()
	if rc.node.Role != "Follower" {
		rc.node.Role = "Follower"
	}
	rc.node.Mu.Unlock()

	// Verify log consistency: check if we have the prevLogIndex entry with prevLogTerm
	rc.node.Mu.RLock()
	logLen := len(rc.node.Log)
	rc.node.Mu.RUnlock()

	if prevLogIndex > 0 && (logLen < prevLogIndex || (logLen >= prevLogIndex && rc.node.Log[prevLogIndex-1].Term != prevLogTerm)) {
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
		rc.node.Mu.Unlock()
	}

	// Update commitIndex if leader's commit is higher
	// commitIndex = min(leaderCommit, index of last new entry)
	if leaderCommit > rc.node.CommitIdx {
		rc.node.Mu.RLock()
		lastNewIdx := len(rc.node.Log)
		rc.node.Mu.RUnlock()
		if leaderCommit < lastNewIdx {
			rc.node.CommitIdx = leaderCommit
		} else {
			rc.node.CommitIdx = lastNewIdx
		}
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
func (rc *RaftConsensus) Start() {
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
								votesCh <- granted
							} else {
								votesCh <- false
							}
						}(peerAddr)
					}

					votesGranted := 1 // count self-vote
					for range rc.node.Peers {
						granted := <-votesCh
						if granted {
							votesGranted += 1
						}
					}
					// if majority, become leader (majority = more than half of total nodes including self)
					totalNodes := len(rc.node.Peers) + 1
					if votesGranted > totalNodes/2 {
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
							rc.mu.Lock()
							nextIdx := rc.nextIndex[followerIdx]
							rc.mu.Unlock()

							rc.node.Mu.RLock()
							prevLogIdx := nextIdx - 1
							prevLogTerm := 0
							if prevLogIdx > 0 && prevLogIdx <= len(rc.node.Log) {
								prevLogTerm = rc.node.Log[prevLogIdx-1].Term
							}
							var entries []types.LogEntry
							if nextIdx > 0 && nextIdx <= len(rc.node.Log)+1 {
								entries = rc.node.Log[nextIdx-1:]
							}
							rc.node.Mu.RUnlock()

							// call rpc to send AppendEntries
							success, newTerm, err := rpc.SendAppendEntries(followerAddr, rc.currentTerm, rc.node.ID, prevLogIdx, prevLogTerm, rc.node.CommitIdx, entries)

							if err != nil {
								return
							}

							if newTerm > rc.currentTerm {
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
								rc.mu.Lock()
								rc.matchIndex[followerIdx] = len(rc.node.Log)
								rc.nextIndex[followerIdx] = len(rc.node.Log) + 1
								rc.mu.Unlock()

								// calculate commit index based on matchIndex
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
			lastApplied := rc.lastApplied
			rc.node.Mu.RUnlock()
			rc.mu.Unlock()

			// apply all the committed but not yet applied entries
			if lastApplied < commitIdx {
				rc.mu.Lock()
				rc.lastApplied += 1
				rc.node.Mu.RLock()
				entry := rc.node.Log[rc.lastApplied-1]
				rc.node.Mu.RUnlock()
				rc.mu.Unlock()

				// Send to applychannel
				rc.applyCh <- ApplyMsg{
					CommandValid: true,
					Command:      entry,
					CommandIndex: rc.lastApplied,
				}
			} else {
				time.Sleep(10 * time.Millisecond) // avoid busy waiting
			}
		}
	}()

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
