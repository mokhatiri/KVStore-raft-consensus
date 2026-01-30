// Package consensus implements the Raft consensus algorithm.
package consensus

import (
	"fmt"
	"sync"
	"time"

	"distributed-kv/storage"
	"distributed-kv/types"
	// "distributed-kv/rpc"
)

type Consensus interface {
	RequestVote(term int, candidateId int) (granted bool)
	AppendEntries(term int, leaderId int, entries []types.LogEntry) error
	Start()
}

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
	store   *storage.Store
	// persister *Persister // persistent storage for Raft state

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	node *types.Node
	// the node has:
	// ID, Address, Peers, Role, Log, CommitIdx
}

func NewRaftConsensus(node *types.Node, store *storage.Store) *RaftConsensus {
	return &RaftConsensus{
		currentTerm: 0,
		votedFor:    -1,
		lastApplied: 0,

		nextIndex:  make([]int, len(node.Peers)),
		matchIndex: make([]int, len(node.Peers)),

		applyCh: make(chan ApplyMsg), // channel to apply committed log entries
		store:   store,               // persistent storage to store Raft state

		electionTimer:  time.NewTimer(ElectionTimeoutMs * time.Millisecond),   // can be updated
		heartbeatTimer: time.NewTimer(HeartbeatIntervalMs * time.Millisecond), // heartbeat interval

		node: node,
	}
}

/* RequestVote : used by candidates to gather votes.

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

func (rc *RaftConsensus) RequestVote(term int, candidateId int) (granted bool) {
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

/* AppendEntries : used by leaders to replicate log entries.

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

func (rc *RaftConsensus) AppendEntries(term int, leaderId int, entries []types.LogEntry) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// ignore entries from older term
	if term < rc.currentTerm {
		return fmt.Errorf("term %d is older than current term %d", term, rc.currentTerm)
	}

	// if leader alive
	rc.electionTimer.Reset(150 * time.Millisecond)

	// if the term is higher, update current term and reset votedFor
	if term > rc.currentTerm {
		rc.currentTerm = term // update current term
		rc.votedFor = -1      // reset votedFor
	}
	// if it's the same term, do nothing
	// because the node is already in sync with the leader

	// append new entries to the log
	if len(entries) > 0 {
		rc.node.Mu.Lock()
		rc.node.Log = append(rc.node.Log, entries...) // append new entries to the log
		rc.node.Mu.Unlock()
	}

	// TODO : verify log consistency before updating commit index
	rc.lastApplied += len(entries)

	return nil
}

/* Start : starts the Raft consensus algorithm.

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

					// TODO : uncomment when RPC is implemented
					// votesCh := make(chan bool, len(rc.node.Peers))

					// TODO : uncomment when RPC is implemented
					// ---
					// for _, peerAddr := range rc.node.Peers {
					// 	// for every peer, send RequestVote RPC
					// 	go func(addr string) {
					// 		// TODO : implement RPC client to send RequestVote
					// 		_ = addr
					// 		granted := rpc.SendRequestVote(addr, rc.currentTerm, rc.node.ID)
					// 		votesCh <- granted
					// 	}(peerAddr)
					// }

					// TODO : Uncomment the following lines when RPC is implemented
					// ---
					// votesGranted := 1 // count self-vote
					// for range rc.node.Peers {
					// 	granted := <-votesCh
					// 	if granted {
					// 		votesGranted += 1
					// 	}
					// }
					// // if majority, become leader
					// if votesGranted > len(rc.node.Peers)/2 {
					// 	rc.node.Role = "Leader"
					// }

					// TODO : delete when RPC is implemented
					votesGranted := len(rc.node.Peers)/2 + 1 // simulate majority
					if votesGranted > len(rc.node.Peers)/2 {
						rc.node.Role = "Leader"
					}

					// TODO : if appendEntries received, become follower
				}
				rc.node.Mu.Unlock()
				rc.mu.Unlock()

				// reset election timer after election timeout
				rc.electionTimer.Reset(ElectionTimeoutMs * time.Millisecond)

			case <-rc.heartbeatTimer.C:
				// send AppendEntries (heartbeats) to followers if leader
				rc.mu.Lock()
				rc.node.Mu.Lock()

				if rc.node.Role == "Leader" {
					// TODO : send AppendEntries RPCs to peers
				}

				rc.node.Mu.Unlock()
				rc.mu.Unlock()

				rc.heartbeatTimer.Reset(HeartbeatIntervalMs * time.Millisecond)
			}
		}
	}()
}
