package main

import (
	"flag"
	"log"

	"distributed-kv/config"
	"distributed-kv/types"
)

// example
func main() {
	// Parse command-line flags.
	nodeID := flag.String("id", "node1", "Node ID")
	address := flag.String("address", "localhost:5000", "Bind address for RPC server")
	dataDir := flag.String("data-dir", "./data", "Directory for persistent state")
	flag.Parse()

	// Create configuration.
	cfg := config.NewConfig(
		types.NodeID(*nodeID),
		types.Address(*address),
		*dataDir,
	)
	cfg.DataDir = *dataDir

	// TODO: Create a Node.
	// TODO: Start RPC server.
	// TODO: Start HTTP API.
	// TODO: Block forever.

	log.Printf("Starting node %s on %s\n", cfg.NodeID, cfg.Address)
	log.Printf("Data directory: %s\n", cfg.DataDir)
	log.Printf("Election timeout: %v-%v\n", cfg.ElectionTimeoutMin, cfg.ElectionTimeoutMax)
	log.Printf("Heartbeat interval: %v\n", cfg.HeartbeatInterval)
}
