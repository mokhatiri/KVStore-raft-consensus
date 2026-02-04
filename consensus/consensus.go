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
*/
func (rc *RaftConsensus) RequestVote(term int, candidateId int) bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if term > rc.currentTerm { // candidate's term is higher
		rc.currentTerm = term     // update current term
		rc.votedFor = candidateId // vote for the candidate

		return true // vote granted
	} else if term == rc.currentTerm { // same term
		if rc.votedFor == -1 || rc.votedFor == candidateId {
			rc.votedFor = candidateId // vote for the candidate
			return true               // vote granted
		}
	}
	return false // vote not granted
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
	if leaderCommit > rc.node.CommitIdx {
		rc.node.CommitIdx = leaderCommit
	}

	rc.lastApplied += len(entries)

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
					// if majority, become leader
					if votesGranted > len(rc.node.Peers)/2 {
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
				rc.mu.Lock()
				rc.node.Mu.Lock()

				if rc.node.Role == "Leader" {
					for i, peerAddr := range rc.node.Peers {
						// Calculate nextIndex (next log entry to send to this follower)
						nextIdx := rc.nextIndex[i]
						prevLogIdx := nextIdx - 1
						prevLogTerm := 0
						if prevLogIdx > 0 && prevLogIdx <= len(rc.node.Log) {
							prevLogTerm = rc.node.Log[prevLogIdx-1].Term
						}

						// Send entries starting from nextIndex
						entries := []types.LogEntry{}
						if nextIdx <= len(rc.node.Log) {
							entries = rc.node.Log[nextIdx-1:]
						}

						go rpc.SendAppendEntries(peerAddr, rc.currentTerm, rc.node.ID, prevLogIdx, prevLogTerm, rc.node.CommitIdx, entries)
					}

					rc.heartbeatTimer.Reset(HeartbeatIntervalMs * time.Millisecond)
				}
				rc.node.Mu.Unlock()
				rc.mu.Unlock()
			}
		}
	}()
}
