package main

import (
	"distributed-kv/consensus"
	"distributed-kv/rpc"
	"distributed-kv/storage"
	"distributed-kv/types"
	"flag"
	"log"
)

// Main entry point for the distributed key-value store node.
func main() {
	// Parse command-line flags
	nodeID := flag.Int("id", 1, "Node ID")
	address := flag.String("addr", "localhost:8000", "Node address")
	// peers := flag.String("peers", "localhost:8001,localhost:8002", "Comma-separated list of peer addresses")
	flag.Parse()

	// Create a Node
	node := &types.Node{
		ID:        *nodeID,
		Address:   *address,
		Peers:     []string{}, // Parse peers from flag
		Role:      "Follower",
		Log:       []types.LogEntry{},
		CommitIdx: 0,
	}

	// Create storage
	store := storage.NewStore()

	// Create RaftConsensus instance
	raftConsensus := consensus.NewRaftConsensus(node, store)

	// Start RPC server
	err := rpc.StartServer(raftConsensus, *address)
	if err != nil {
		log.Fatalf("Failed to start RPC server: %v", err)
	}
	log.Printf("RPC server started on %s", *address)

	// Start Raft consensus
	raftConsensus.Start()
	log.Printf("Raft consensus started for node %d", *nodeID)

	// Block forever
	select {}
}
