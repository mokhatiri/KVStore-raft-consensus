package clustermanager

import (
	"time"
)

const (
	EventBufferSize    = 1000            // max number of events to keep in memory
	EventFlushInterval = 5 * time.Second // interval to flush events to file
)
