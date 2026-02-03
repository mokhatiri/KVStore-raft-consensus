package types

type ConsensusModule interface {
	RequestVote(term int, candidateId int) bool
	AppendEntries(term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []LogEntry) error
	GetCurrentTerm() int
}
