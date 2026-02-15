package types

type LogEntry struct {
	Term         int                `json:"term"`
	Index        int                `json:"index,omitempty"` // informational global index, set on creation
	Command      string             `json:"command"`         // "SET", "DELETE", "CLEAN", "CONFIG_CHANGE"
	Key          string             `json:"key,omitempty"`
	Value        any                `json:"value,omitempty"`
	ConfigChange *ConfigChangeEntry `json:"config_change,omitempty"` // populated when Command == "CONFIG_CHANGE"
}
