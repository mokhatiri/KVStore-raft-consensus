package main

import (
	"bufio"
	managercli "distributed-kv/cli/managercli"
	"distributed-kv/cli/nodecli"
	"distributed-kv/clustermanager"
	"distributed-kv/consensus"
	"distributed-kv/storage"
	"fmt"
	"log"
	"os"
	"strings"
)

func startManagerCLI(manager *clustermanager.Manager) {
	log.Println("------------------- Starting Manager CLI ------------------")
	defer log.Println("------------------- Manager CLI Exited ------------------")

	handlers := managercli.NewHandling(manager)

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\n[Manager] > ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		handlers.HandleCommand(input)
	}
}

func startNodeCLI(raftConsensus *consensus.RaftConsensus, store *storage.Store) {
	// Start Raft consensus
	nodeId, nodeRole, _, _ := raftConsensus.GetNodeStatus()
	log.Printf("Node %d started. Role: %s", nodeId, nodeRole)

	reader := bufio.NewReader(os.Stdin)

	log.Println("------------------- Starting Node CLI ------------------")
	defer log.Println("------------------- Node CLI Exited ------------------")

	handlers := nodecli.NewHandling(raftConsensus, store)

	for {
		nodeId, nodeRole, _, _ := raftConsensus.GetNodeStatus()
		fmt.Printf("\n[Node %d - %s] > ", nodeId, nodeRole)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		handlers.HandleCommand(input)
	}
}
