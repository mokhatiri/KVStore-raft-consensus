package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
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
	raftConsensus *consensus.RaftConsensus
	store         *storage.Store
)

// Main entry point for the distributed key-value store node.
func main() {
	id, address, peers := parseArgs()
	// start the Raft
	start(id, address, peers)
	// add interactive command line interface for user commands
	startRPC(address)
	// Give RPC server time to start before starting consensus
	time.Sleep(500 * time.Millisecond)
	log.Printf("RPC server started on %s", address)

	// Start pprof server for debugging goroutines/deadlocks
	go func() {
		log.Printf("[Node %d] pprof server started on http://localhost:6060/debug/pprof/", id)
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			log.Printf("[Node %d] pprof server error: %v", id, err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// start apply consumer (applies committed entries to store)
	startApplyConsumer()
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

func startRPC(address string) {
	// start the RPC server listening on the node's RPC address
	// handle incomming RPC calls from peers for Raft consensus
	rpc.StartServer(raftConsensus, address)
}

// startApplyConsumer consumes committed log entries and applies them to the state machine (store)
func startApplyConsumer() {
	raftConsensus.StartConsumption(store)
}

func start(id int, address string, peers []string) {
	// create the key-value store
	store = storage.NewStore()
	// create a new node and Raft consensus module
	newNode := types.NewNode(id, address, peers)
	raftConsensus = consensus.NewRaftConsensus(newNode)
}
