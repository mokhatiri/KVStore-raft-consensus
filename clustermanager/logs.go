package clustermanager

import (
	"sync"
	"time"
)

type LogMessage struct {
	Timestamp time.Time
	Level     string // "INFO", "ERROR", "WARN"
	Message   string
}

type LogBuffer struct {
	logs    []LogMessage
	maxSize int
	mu      sync.RWMutex
}

func NewLogBuffer(maxSize int) *LogBuffer {
	return &LogBuffer{
		logs:    make([]LogMessage, 0, maxSize),
		maxSize: maxSize,
	}
}

func (lb *LogBuffer) AddLog(level string, message string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// evict oldest log if we exceed max size
	if len(lb.logs) >= lb.maxSize {
		lb.logs = lb.logs[1:]
	}

	lb.logs = append(lb.logs, LogMessage{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	})
}

func (lb *LogBuffer) GetLogs(limit int) []LogMessage {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if limit <= 0 || limit > len(lb.logs) {
		limit = len(lb.logs)
	}

	// return last `limit` logs
	return lb.logs[len(lb.logs)-limit:]
}

func (lb *LogBuffer) GetAllLogs() []LogMessage {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	result := make([]LogMessage, len(lb.logs))
	copy(result, lb.logs)
	return result
}

func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.logs = make([]LogMessage, 0, lb.maxSize)
}
