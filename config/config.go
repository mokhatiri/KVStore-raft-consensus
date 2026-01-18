package config

import (
	"time"

	"distributed-kv/types"
)

// Config holds the configuration for a Raft node.
type Config struct {
	NodeID  types.NodeID
	Address types.Address

	Peers              []types.Address
	DataDir            string // where the node stores its persistent state
	ElectionTimeoutMin time.Duration
	ElectionTimeoutMax time.Duration
	HeartbeatInterval  time.Duration // how often a leader sends heartbeats
}

// NewConfig creates a Config with sensible defaults.
func NewConfig(nodeID types.NodeID, address types.Address, dataDir string) *Config {
	if dataDir == "" {
		dataDir = "./data"
	}

	return &Config{
		NodeID:             nodeID,
		Address:            address,
		Peers:              []types.Address{},
		DataDir:            dataDir,
		ElectionTimeoutMin: 150 * time.Millisecond,
		ElectionTimeoutMax: 300 * time.Millisecond,
		HeartbeatInterval:  50 * time.Millisecond,
	}
}
func (c *Config) AddPeer(peer types.Address) {
	c.Peers = append(c.Peers, peer)
}

func (c *Config) RemovePeer(peer types.Address) {
	for i, p := range c.Peers {
		if p == peer {
			c.Peers = append(c.Peers[:i], c.Peers[i+1:]...)
			return
		}
	}
}

func (c *Config) SetElectionTimeout(min, max time.Duration) {
	c.ElectionTimeoutMin = min
	c.ElectionTimeoutMax = max
}

func (c *Config) SetHeartbeatInterval(interval time.Duration) {
	c.HeartbeatInterval = interval
}
