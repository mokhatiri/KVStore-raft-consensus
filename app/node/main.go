package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"distributed-kv/api"
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

var (
	raftConsensus *consensus.RaftConsensus
	store         *storage.Store
)

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
	default:
		NodeMain()
	}
}

// main entry point for the manager node
func ManagerMain() {
	log.Println("------------------- Starting Cluster Manager ------------------")
	dbPath, outputDir, address, clusterConfig := parseManagerArgs()

	manager := clustermanager.NewManager(dbPath, outputDir)
	err := manager.InitDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	for _, node := range clusterConfig.Nodes {
		log.Printf("Node ID: %d, RPC Address: %s, HTTP Address: %s, pprof Address: %s\n", node.ID, node.RPCAddr, node.HTTPAddr, node.PProfAddr)
		manager.RegisterNode(node.ID, node.RPCAddr, node.HTTPAddr, node.PProfAddr)
	}

	manager.StartAPI(address)
	startManagerCLI()
	log.Println("-------------------- Cluster Manager Running ------------------")
}

// Main entry point for the distributed key-value store node.
func NodeMain() {
	id, address, HTTPaddress, pprofServer, peers, managerAddress := parseArgs()

	// start the Raft
	start(id, address, pprofServer, peers, managerAddress)

	// start the HTTP API server before the CLI
	startAPI(HTTPaddress)

	// start CLI
	startCLI()
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

func parseManagerArgs() (string, string, string, Config) {
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

	return config.ManagerAddress.DBPath, config.ManagerAddress.OutputDir, config.ManagerAddress.HTTPAddr, config
}

func parseArgs() (int, string, string, string, []string, string) {
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
	found := false
	for _, node := range config.Nodes {
		if node.ID != *idPtr { // exclude self
			peers = append(peers, node.RPCAddr)
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

	return *idPtr, address, HTTPaddress, pprofServer, peers, config.ManagerAddress.RPCAddr
}

func start(id int, address string, pprofServer string, peers []string, managerAddress string) {
	// create the key-value store
	store = storage.NewStore()

	// create a new node and Raft consensus module
	newNode := types.NewNode(id, address, peers)
	raftConsensus = consensus.NewRaftConsensus(newNode)

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
		err := raftConsensus.ConnectToManager(managerAddress)
		if err != nil {
			log.Fatalf("Failed to connect to cluster manager: %v", err)
			log.Println("------------------- Cluster Manager Connection Failed ---------------------")
		} else {
			log.Println("Connected to cluster manager at", managerAddress)
			log.Println("------------------- Cluster Manager Connected ---------------------")
		}
	}
	// manager connection done
}

func startAPI(HTTPaddress string) {
	log.Println("------------------- Starting API Server ------------------")
	// check if HTTP address is provided
	if HTTPaddress == "" {
		log.Println("No HTTP address provided, skipping API server startup")
		log.Println("-------------------- API Server Skipped ------------------")
		return
	}

	log.Println("Starting HTTP API server on", HTTPaddress)

	httpServer := api.NewServer(store, raftConsensus, HTTPaddress)
	httpServer.Start()

	log.Println("HTTP API server started on", HTTPaddress)
	log.Println("-------------------- API Server Running ------------------")
}
