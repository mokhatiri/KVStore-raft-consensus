package main

import (
	"bufio"
	"distributed-kv/handlers"
	"fmt"
	"log"
	"os"
	"strings"
)

func startManagerCLI() {
	// TODO later: add commands to view cluster state, view events, etc.
	select {} // block forever since the manager doesn't have a CLI yet, but we want to keep it running to serve the API and manage the cluster
}

func startCLI() {
	// Start Raft consensus
	nodeId, nodeRole, _, _ := raftConsensus.GetNodeStatus()
	log.Printf("Node %d started. Role: %s", nodeId, nodeRole)

	reader := bufio.NewReader(os.Stdin)

	for {
		nodeId, nodeRole, _, _ := raftConsensus.GetNodeStatus()
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
	key := parts[1]
	value := strings.Join(parts[2:], " ")

	err := handlers.SetHandler(raftConsensus, key, value)
	if err != nil {
		fmt.Println(err)
		return
	}
}

func handleDelete(parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: delete <key>")
		return
	}

	key := parts[1]

	err := handlers.DeleteHandler(raftConsensus, key)
	if err != nil {
		fmt.Println(err)
		return
	}
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
	// wait for confirmation before proposing to avoid multiple CLEAN commands being proposed at the same time
	fmt.Print("Are you sure you want to CLEAN all data? This action cannot be undone. (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	confirmation, _ := reader.ReadString('\n')
	confirmation = strings.TrimSpace(strings.ToLower(confirmation))

	if confirmation != "yes" {
		fmt.Println("CLEAN command aborted")
		return
	}

	err := handlers.CleanHandler(raftConsensus)
	if err != nil {
		fmt.Println(err)
		return
	}
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
