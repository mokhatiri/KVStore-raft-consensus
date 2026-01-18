package types

type NodeID string
type Address string
type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

type Term uint64
type CommitIndex uint64
