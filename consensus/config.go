// this file is used to mark the manage package
package consensus

import (
	"math/rand"
	"time"
)

const (
	ElectionTimeoutMinMs = 150  // minimum milliseconds before election triggered
	ElectionTimeoutMaxMs = 300  // maximum milliseconds before election triggered
	HeartbeatIntervalMs  = 50   // milliseconds between leader heartbeats
	SnapshotThreshold    = 1000 // number of applied entries before taking a snapshot
)

// GetRandomElectionTimeout returns a random election timeout between min and max
func GetRandomElectionTimeout() time.Duration {
	randomMs := ElectionTimeoutMinMs + rand.Intn(ElectionTimeoutMaxMs-ElectionTimeoutMinMs)
	return time.Duration(randomMs) * time.Millisecond
}
