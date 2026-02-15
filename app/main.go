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
	appRpc "distributed-kv/rpc"
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
	httpAddress, clusterConfig := parseManagerArgs()

	// start the cluster manager and its RPC server
	manager := Managerstart(clusterConfig)

	// start the manager's HTTP API server
	startManagerAPI(httpAddress, manager)

	// start the manager CLI
	startManagerCLI(manager)
}

func NodeMain() {
	id, address, HTTPaddress, pprofServer, configFile, useDB := parseNodeArgs()

	// start the Raft
	raftconsensus := Nodestart(id, address, pprofServer, HTTPaddress, configFile, useDB)

	// start the HTTP API server before the CLI
	startNodeAPI(HTTPaddress, raftconsensus)

	// start CLI
	startNodeCLI(raftconsensus)
}

type Config struct {
	Nodes []struct {
		ID        int    `json:"id"`
		RPCAddr   string `json:"rpcAddr"`
		HTTPAddr  string `json:"httpAddr"`
		PProfAddr string `json:"pprofAddr"`
	} `json:"nodes"`

	ManagerAddress struct {
		HTTPAddr  string `json:"httpAddr"`
		PProfAddr string `json:"pprofAddr"`
	} `json:"manager"`
}

func parseManagerArgs() (string, Config) {
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

	return config.ManagerAddress.HTTPAddr, config
}

func parseNodeArgs() (int, string, string, string, string, bool) {
	// Command-line flags for standalone mode
	idPtr := flag.Int("id", 0, "Node ID")
	configPtr := flag.String("config", "cluster.json", "Path to config file (used as fallback if flags not provided)")
	dbPtr := flag.Bool("db", false, "Use SQLite database for persistence (default: JSON files)")
	flag.Parse()

	data, err := os.ReadFile(*configPtr)
	if err != nil {
		log.Fatalf("Failed to read config file and no flags provided: %v", err)
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Failed to parse config JSON: %v", err)
	}

	// Find this node's configuration in the config file
	var address, httpAddress, pprofServer string
	found := false
	for _, node := range config.Nodes {
		if node.ID == *idPtr {
			address = node.RPCAddr
			httpAddress = node.HTTPAddr
			pprofServer = node.PProfAddr
			found = true
			break
		}
	}

	if !found {
		log.Fatalf("Node ID %d not found in config file", *idPtr)
	}

	if address == "" {
		log.Fatalf("Address for node %d is empty", *idPtr)
	}

	return *idPtr, address, httpAddress, pprofServer, *configPtr, *dbPtr
	// Return: id, rpcAddr, httpAddr, pprofAddr, configFile, useDB
}

func Nodestart(id int, address string, pprofServer string, apiHTTPAddress string, configFile string, useDB bool) *consensus.RaftConsensus {
	// create the key-value store
	store := storage.NewStore()

	// Create persister based on flag
	var persister types.Persister
	if useDB {
		log.Println("Using SQLite database for persistence")
		dbPersister, err := storage.NewDatabasePersister(id, ".")
		if err != nil {
			log.Fatalf("Failed to create database persister: %v", err)
		}
		persister = dbPersister
		// Update store to use database persister
		store.SetPersister(dbPersister)
	} else {
		log.Println("Using JSON files for persistence")
		persister = nil // will default to JSONPersister in NewRaftConsensus
	}

	// Determine initial peers list using maps
	peers := make(map[int]string)     // PeerID -> RPC address
	peershttp := make(map[int]string) // PeerID -> HTTP address

	if configFile != "" {
		// Static mode: load peers from config file
		log.Println("Loading peers from config file:", configFile)
		data, err := os.ReadFile(configFile)
		if err != nil {
			log.Fatalf("Failed to read config file: %v", err)
		}

		var config Config
		err = json.Unmarshal(data, &config)
		if err != nil {
			log.Fatalf("Failed to parse config JSON: %v", err)
		}

		for _, node := range config.Nodes {
			if node.ID != id { // exclude self
				peers[node.ID] = node.RPCAddr
				peershttp[node.ID] = node.HTTPAddr
			}
		}
	} else {
		// Dynamic mode: start with empty peers, will fetch from manager after registering
		log.Println("Starting in dynamic mode, peers will be fetched from manager after registration")
	}

	// create a new node and Raft consensus module
	newNode := types.NewNode(id, address, peers, peershttp)
	raftConsensus := consensus.NewRaftConsensus(newNode, store, persister)

	// start the RPC server early so it's ready to receive connections
	log.Println("------------------- Starting RPC Server ---------------------")
	appRpc.StartServer(raftConsensus, address)

	time.Sleep(500 * time.Millisecond)
	log.Println("RPC server started on", address)
	log.Println("------------------- RPC Server Started ---------------------")
	// rpc done

	// Start pprof server for debugging goroutines/deadlocks
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
	time.Sleep(500 * time.Millisecond)
	// raft done

	return raftConsensus
}

func Managerstart(clusterConfig Config) *clustermanager.Manager {
	manager := clustermanager.NewManager()

	log.Println("------------------- Registering Nodes with Cluster Manager ------------------")
	for _, node := range clusterConfig.Nodes {
		log.Printf("Node ID: %d, RPC Address: %s, HTTP Address: %s, pprof Address: %s\n", node.ID, node.RPCAddr, node.HTTPAddr, node.PProfAddr)
		manager.RegisterNode(node.ID, node.HTTPAddr)
	}
	log.Println("------------------- Nodes Registered with Cluster Manager ------------------")
	log.Println("------------------- Starting Node Health Check ------------------")
	manager.StartListening(5 * time.Millisecond)
	log.Println("Node health check started (checking every 5 seconds)")
	log.Println("------------------- Node Health Check Started ------------------")

	return manager
}
