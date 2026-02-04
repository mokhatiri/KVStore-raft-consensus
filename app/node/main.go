package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"distributed-kv/consensus"
	"distributed-kv/rpc"
	"distributed-kv/storage"
	"distributed-kv/types"
)

// the general format of the command line arguments is as follows:
// go run main.go -id <node_id> -config <config_file>

/*
	Commands:
	get <key>           - Get value from THIS node's store
	set <key> <value>   - Set value (must be leader)
	delete <key>        - Delete key (must be leader)
	all                 - Show all data on THIS node
	status              - Show role, term, log length
	log                 - Show the replication log
	help                - Show commands
*/

var (
	node          *types.Node
	raftConsensus *consensus.RaftConsensus
	store         *storage.Store
)

// Main entry point for the distributed key-value store node.
func main() {
	id, address, peers := parseArgs()
	// start the Raft
	start(id, address, peers)
	// add interactive command line interface for user commands
	startRPC()
	// Give RPC server time to start before starting consensus
	time.Sleep(500 * time.Millisecond)
	log.Printf("RPC server started on %s", node.Address)
	// start CLI
	startCLI()
}

type Config struct {
	Nodes []struct {
		ID      int    `json:"id"`
		RPCAddr string `json:"rpcAddr"`
	} `json:"nodes"`
}

func parseArgs() (int, string, []string) {
	idPtr := flag.Int("id", 0, "Node ID")
	configPtr := flag.String("config", "cluster.json", "Path to config file")
	flag.Parse()

	// Load and parse
	data, err := os.ReadFile(*configPtr)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var config Config
	var address string
	err = json.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Failed to parse config JSON: %v", err)
	}

	var peers []string
	found := false
	for _, node := range config.Nodes {
		if node.ID != *idPtr { // exclude self
			peers = append(peers, node.RPCAddr)
		} else {
			address = node.RPCAddr
			found = true
		}
	}

	if !found {
		log.Fatalf("Node ID %d not found in config file", *idPtr)
	}

	if address == "" {
		log.Fatalf("Address for node %d is empty", *idPtr)
	}

	return *idPtr, address, peers
}

func startRPC() {
	// start the RPC server listening on the node's RPC address
	// handle incomming RPC calls from peers for Raft consensus
	rpc.StartServer(raftConsensus, node.Address)
}

func start(id int, address string, peers []string) {
	// create a new nod
	node = types.NewNode(id, address, peers)
	// create the key-value store
	store = storage.NewStore()
	// create Raft consensus module
	raftConsensus = consensus.NewRaftConsensus(node)
}

func startCLI() {
	// Start Raft consensus
	raftConsensus.Start()
	log.Printf("Node %d started. Role: %s", node.ID, node.Role)

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("\n[Node %d - %s] > ", node.ID, node.Role)
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

	// Check if leader
	node.Mu.RLock()
	role := node.Role
	node.Mu.RUnlock()

	if role != "Leader" {
		fmt.Printf("Error: Only leader can set values. This node is a %s\n", role)
		return
	}

	key := parts[1]
	value := strings.Join(parts[2:], " ")

	// Add to log
	entry := types.LogEntry{
		Term:    raftConsensus.GetCurrentTerm(),
		Command: "SET",
		Key:     key,
		Value:   value,
	}

	node.Mu.Lock()
	node.Log = append(node.Log, entry)
	node.Mu.Unlock()

	// Apply to store
	store.Set(key, value)

	fmt.Printf("✓ Set %s = %s (log index: %d)\n", key, value, len(node.Log))
}

func handleDelete(parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: delete <key>")
		return
	}

	// Check if leader
	node.Mu.RLock()
	role := node.Role
	node.Mu.RUnlock()

	if role != "Leader" {
		fmt.Printf("Error: Only leader can delete values. This node is a %s\n", role)
		return
	}

	key := parts[1]

	// Add to log
	entry := types.LogEntry{
		Term:    raftConsensus.GetCurrentTerm(),
		Command: "DELETE",
		Key:     key,
		Value:   nil,
	}

	node.Mu.Lock()
	node.Log = append(node.Log, entry)
	node.Mu.Unlock()

	// Apply to store
	store.Delete(key)

	fmt.Printf("✓ Deleted key '%s' (log index: %d)\n", key, len(node.Log))
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
	node.Mu.RLock()
	defer node.Mu.RUnlock()

	fmt.Println("\n--- Node Status ---")
	fmt.Printf("Node ID:       %d\n", node.ID)
	fmt.Printf("Address:       %s\n", node.Address)
	fmt.Printf("Role:          %s\n", node.Role)
	fmt.Printf("Current Term:  %d\n", raftConsensus.GetCurrentTerm())
	fmt.Printf("Voted For:     %d\n", raftConsensus.GetVotedFor())
	fmt.Printf("Log Length:    %d\n", len(node.Log))
	fmt.Printf("Commit Index:  %d\n", node.CommitIdx)
	fmt.Printf("Peers:         %v\n", node.Peers)
}

func handleLog() {
	node.Mu.RLock()
	defer node.Mu.RUnlock()

	if len(node.Log) == 0 {
		fmt.Println("Log is empty")
		return
	}

	fmt.Println("\n--- Replication Log ---")
	for i, entry := range node.Log {
		fmt.Printf("[%d] Term: %d, Command: %s, Key: %s, Value: %v\n",
			i+1, entry.Term, entry.Command, entry.Key, entry.Value)
	}
}

func handleHelp() {
	fmt.Println("\n--- Available Commands ---")
	fmt.Println("  get <key>           - Get value from THIS node's store")
	fmt.Println("  set <key> <value>   - Set value (must be leader)")
	fmt.Println("  delete <key>        - Delete key (must be leader)")
	fmt.Println("  all                 - Show all data on THIS node")
	fmt.Println("  status              - Show node status (role, term, log length)")
	fmt.Println("  log                 - Show the replication log")
	fmt.Println("  help                - Show this help message")
	fmt.Println("  exit                - Exit the program")
}
