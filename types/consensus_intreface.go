package types

type ConsensusModule interface {
	RequestVote(term int, candidateId int, lastLogIndex int, lastLogTerm int) (bool, int)
	AppendEntries(term int, leaderId int, prevLogIndex int, prevLogTerm int, leaderCommit int, entries []LogEntry) error
	GetCurrentTerm() int
	Propose(command string) (index int, term int, isLeader bool)
}
