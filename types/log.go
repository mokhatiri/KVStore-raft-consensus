package types

type LogEntry struct {
	Term    int    `json:"term"`
	Command string `json:"command"` // e.g., "SET key value"
	Key     string `json:"key"`
	Value   any    `json:"value"`
}
