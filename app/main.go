package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"distributed-kv/clustermanager"
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

func main() {
	// Determine subcommand by checking the first argument before any flags
	subcommand := "node"
	if len(os.Args) > 1 && os.Args[1] != "" && os.Args[1][0] != '-' {
		subcommand = os.Args[1]
	}

	switch subcommand {
	case "manager":
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		ManagerMain()
	case "node":
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		NodeMain()
	default:
		log.Fatalf("Unknown subcommand: %s. Use 'manager' or 'node'.", subcommand)
	}
}

// main entry point for the manager node
func ManagerMain() {
	dbPath, outputDir, rpcAddress, httpAddress, clusterConfig := parseManagerArgs()

	// start the cluster manager and its RPC server
	manager := Managerstart(dbPath, outputDir, rpcAddress, clusterConfig)

	// start the manager's HTTP API server
	startManagerAPI(httpAddress, manager)

	// start the manager CLI
	startManagerCLI(manager)
}

func NodeMain() {
	id, address, HTTPaddress, pprofServer, peers, peerIDs, managerAddress := parseNodeArgs()

	// start the Raft
	raftconsensus, store := Nodestart(id, address, pprofServer, peers, peerIDs, managerAddress, HTTPaddress)

	// start the HTTP API server before the CLI
	startNodeAPI(HTTPaddress, raftconsensus, store)

	// start CLI
	startNodeCLI(raftconsensus, store)
}

type Config struct {
	Nodes []struct {
		ID        int    `json:"id"`
		RPCAddr   string `json:"rpcAddr"`
		HTTPAddr  string `json:"httpAddr"`
		PProfAddr string `json:"pprofAddr"`
	} `json:"nodes"`

	ManagerAddress struct {
		RPCAddr   string `json:"rpcAddr"`
		HTTPAddr  string `json:"httpAddr"`
		PProfAddr string `json:"pprofAddr"`
		DBPath    string `json:"dbPath"`
		OutputDir string `json:"outputDir"`
	} `json:"manager"`
}

func parseManagerArgs() (string, string, string, string, Config) {
	// get the config file path from command line arguments
	configPtr := flag.String("config", "cluster.json", "Path to config file")
	flag.Parse()

	// Load and parse
	data, err := os.ReadFile(*configPtr)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Failed to parse config JSON: %v", err)
	}

	return config.ManagerAddress.DBPath, config.ManagerAddress.OutputDir, config.ManagerAddress.RPCAddr, config.ManagerAddress.HTTPAddr, config
}

func parseNodeArgs() (int, string, string, string, []string, []int, string) {
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
	var HTTPaddress string
	var pprofServer string
	err = json.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Failed to parse config JSON: %v", err)
	}

	var peers []string
	var peerIDs []int
	found := false
	for _, node := range config.Nodes {
		if node.ID != *idPtr { // exclude self
			peers = append(peers, node.RPCAddr)
			peerIDs = append(peerIDs, node.ID)
		} else {
			address = node.RPCAddr
			HTTPaddress = node.HTTPAddr
			pprofServer = node.PProfAddr
			found = true
		}
	}

	if !found {
		log.Fatalf("Node ID %d not found in config file", *idPtr)
	}

	if address == "" {
		log.Fatalf("Address for node %d is empty", *idPtr)
	}

	return *idPtr, address, HTTPaddress, pprofServer, peers, peerIDs, config.ManagerAddress.RPCAddr
}

func Nodestart(id int, address string, pprofServer string, peers []string, peerIDs []int, managerAddress string, apiHTTPAddress string) (*consensus.RaftConsensus, *storage.Store) {
	// create the key-value store
	store := storage.NewStore()

	// create a new node and Raft consensus module
	newNode := types.NewNode(id, address, peers)
	newNode.PeerIDs = peerIDs
	raftConsensus := consensus.NewRaftConsensus(newNode)

	// start the RPC server
	log.Println("------------------- Starting RPC Server ---------------------")
	rpc.StartServer(raftConsensus, address)

	time.Sleep(500 * time.Millisecond)
	log.Println("RPC server started on", address)
	log.Println("------------------- RPC Server Started ---------------------")
	// rpc done

	// Start pprof server for debugging goroutines/deadlocks
	// check if the pprof server address is provided
	log.Println("------------------- Starting pprof Server ---------------------")
	if pprofServer == "" {
		log.Println("[Node", id, "] No pprof server address provided, skipping pprof server startup")
		log.Println("------------------- pprof Server Skipped ---------------------")
	} else {
		go func() {
			log.Println("[Node", id, "] pprof server started on http://"+pprofServer+"/debug/pprof/")
			if err := http.ListenAndServe(pprofServer, nil); err != nil {
				log.Println("[Node", id, "] pprof server error:", err)
			}
		}()
		log.Println("------------------- pprof Server Started ---------------------")
	}
	time.Sleep(100 * time.Millisecond)
	// pprof done

	// start the Raft consensus module
	log.Println("------------------- Starting Raft Consensus ---------------------")
	raftConsensus.Start(store)
	log.Println("Raft consensus started on node", id)
	log.Println("------------------- Raft Consensus Started ---------------------")
	// wait a moment for the Raft consensus to stabilize
	time.Sleep(500 * time.Millisecond)
	// raft done

	// connect to the cluster manager
	log.Println("------------------- Connecting to Cluster Manager ---------------------")
	if managerAddress != "" {
		raftConsensus.ConnectToManager(managerAddress, apiHTTPAddress)
		log.Println("Connected to cluster manager at", managerAddress)
		log.Println("------------------- Connected to Cluster Manager ---------------------")
	} else {
		log.Println("No cluster manager address provided, skipping connection")
		log.Println("------------------- Cluster Manager Connection Skipped ---------------------")
	}
	// manager connection done

	return raftConsensus, store
}

func Managerstart(dbPath string, outputDir string, address string, clusterConfig Config) *clustermanager.Manager {
	manager := clustermanager.NewManager(dbPath, outputDir)

	log.Println("------------------- Initializing Cluster Manager Database ------------------")
	err := manager.InitDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	log.Println("Cluster manager database initialized successfully on path:", dbPath)
	log.Println("------------------- Cluster Manager Database Initialized ------------------")

	log.Println("------------------- Registering Nodes with Cluster Manager ------------------")
	for _, node := range clusterConfig.Nodes {
		log.Printf("Node ID: %d, RPC Address: %s, HTTP Address: %s, pprof Address: %s\n", node.ID, node.RPCAddr, node.HTTPAddr, node.PProfAddr)
		manager.RegisterNode(node.ID, node.RPCAddr, node.HTTPAddr)
	}
	log.Println("------------------- Nodes Registered with Cluster Manager ------------------")

	// start the RPC server for the manager to receive events and node state updates
	log.Println("------------------- Starting Manager RPC Server ---------------------")
	manager.Start(address)
	log.Println("Manager RPC server started on", address)
	log.Println("------------------- Manager RPC Server Started ---------------------")

	// start the manager's event aggregation
	log.Println("------------------- Starting Cluster Manager ------------------")
	manager.StartEventAggregation()
	log.Println("Cluster manager started and aggregating events")
	log.Println("------------------- Cluster Manager Started ------------------")

	// start the manager's health check
	log.Println("------------------- Starting Node Health Check ------------------")
	manager.StartHealthCheck(5 * time.Second)
	log.Println("Node health check started (checking every 5 seconds)")
	log.Println("------------------- Node Health Check Started ------------------")

	return manager
}
