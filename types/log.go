package types

type LogEntry struct {
	Term    Term
	Command Command
}

type Command struct {
	Key   Key
	Value Value
}

type Key string
type Value string
