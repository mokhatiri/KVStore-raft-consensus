package clustermanager

import (
	"time"
)

const (
	EventBufferSize    = 5000             // max number of events to keep in memory (increased to handle ~100 events/sec x 5 flush intervals)
	EventFlushInterval = 30 * time.Second // interval to flush events to file (reduced from 5s to batch more events per file)
)
