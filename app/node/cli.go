package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

func startCLI() {
	// Start Raft consensus
	nodeId, nodeRole := raftConsensus.GetNodeInfo()
	log.Printf("Node %d started. Role: %s", nodeId, nodeRole)

	reader := bufio.NewReader(os.Stdin)

	for {
		nodeId, nodeRole := raftConsensus.GetNodeInfo()
		fmt.Printf("\n[Node %d - %s] > ", nodeId, nodeRole)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		command := parts[0]

		switch command {
		case "get":
			handleGet(parts)

		case "set":
			handleSet(parts)

		case "delete":
			handleDelete(parts)

		case "all":
			handleAll()

		case "status":
			handleStatus()

		case "log":
			handleLog()

		case "clean":
			handleClean()

		case "help":
			handleHelp()

		case "exit":
			fmt.Println("Exiting...")
			os.Exit(0)

		default:
			fmt.Println("Unknown command. Type 'help' for available commands.")
		}
	}
}

func handleGet(parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: get <key>")
		return
	}

	key := parts[1]
	value, exists := store.Get(key)
	if !exists {
		fmt.Printf("Key '%s' not found\n", key)
		return
	}

	fmt.Printf("%s = %v\n", key, value)
}

func handleSet(parts []string) {
	if len(parts) < 3 {
		fmt.Println("Usage: set <key> <value>")
		return
	}

	// Check if node is leader first
	_, role := raftConsensus.GetNodeInfo()
	if role != "Leader" {
		fmt.Println("Error: Not the leader")
		return
	}

	key := parts[1]
	value := strings.Join(parts[2:], " ")

	// Format command as "SET:key:value"
	command := fmt.Sprintf("SET:%s:%s", key, value)

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose(command)
	fmt.Printf("✓ Proposed SET %s = %s (index: %d, term: %d) - waiting for commitment...\n", key, value, index, term)
}

func handleDelete(parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: delete <key>")
		return
	}

	// Check if node is leader first
	_, role := raftConsensus.GetNodeInfo()
	if role != "Leader" {
		fmt.Println("Error: Not the leader")
		return
	}

	key := parts[1]

	// Format command as "DELETE:key"
	command := fmt.Sprintf("DELETE:%s", key)

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose(command)
	fmt.Printf("✓ Proposed DELETE %s (index: %d, term: %d) - waiting for commitment...\n", key, index, term)
}

func handleAll() {
	data := store.GetAll()
	if len(data) == 0 {
		fmt.Println("Store is empty")
		return
	}

	fmt.Println("Key-Value Store:")
	for key, value := range data {
		fmt.Printf("  %s = %v\n", key, value)
	}
}

func handleStatus() {
	raftConsensus.PrintStatus()
}

func handleLog() {
	raftConsensus.PrintLog()
}

func handleClean() {
	// Check if node is leader first
	_, role := raftConsensus.GetNodeInfo()
	if role != "Leader" {
		fmt.Println("Error: Not the leader")
		return
	}

	// wait for confirmation before proposing to avoid multiple CLEAN commands being proposed at the same time
	fmt.Print("Are you sure you want to CLEAN all data? This action cannot be undone. (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	confirmation, _ := reader.ReadString('\n')
	confirmation = strings.TrimSpace(strings.ToLower(confirmation))

	if confirmation != "yes" {
		fmt.Println("CLEAN command aborted")
		return
	}

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose("CLEAN")
	fmt.Printf("✓ Proposed CLEAN all data (index: %d, term: %d) - waiting for commitment...\n", index, term)
}

func handleHelp() {
	fmt.Println("\n--- Available Commands ---")
	fmt.Println("  get <key>           - Get value from THIS node's store")
	fmt.Println("  set <key> <value>   - Set value (must be leader)")
	fmt.Println("  delete <key>        - Delete key (must be leader)")
	fmt.Println("  clean               - Clean all data (must be leader)")
	fmt.Println("  all                 - Show all data on THIS node")
	fmt.Println("  status              - Show node status (role, term, log length)")
	fmt.Println("  log                 - Show the replication log")
	fmt.Println("  help                - Show this help message")
	fmt.Println("  exit                - Exit the program")
}
