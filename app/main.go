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

var ErrorMsg = "Failed to parse config file: %v"

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
		log.Fatalf(ErrorMsg, err)
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
		log.Fatalf(ErrorMsg, err)
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
	store := storage.NewStore()
	persister := createPersister(id, store, useDB)
	peers, peershttp := loadPeers(id, configFile)

	newNode := types.NewNode(id, address, peers, peershttp)
	raftConsensus := consensus.NewRaftConsensus(newNode, store, persister)

	startRPCServer(raftConsensus, address)
	startPprofServer(id, pprofServer)
	startRaftConsensus(id, raftConsensus, store)

	return raftConsensus
}

func createPersister(id int, store *storage.Store, useDB bool) types.Persister {
	if useDB {
		log.Println("Using SQLite database for persistence")
		dbPersister, err := storage.NewDatabasePersister(id, ".")
		if err != nil {
			log.Fatalf("Failed to create database persister: %v", err)
		}
		store.SetPersister(dbPersister)
		return dbPersister
	}
	log.Println("Using JSON files for persistence")
	return nil
}

func loadPeers(id int, configFile string) (map[int]string, map[int]string) {
	peers := make(map[int]string)
	peershttp := make(map[int]string)

	if configFile == "" {
		log.Println("Starting in dynamic mode, peers will be fetched from manager after registration")
		return peers, peershttp
	}

	log.Println("Loading peers from config file:", configFile)
	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf(ErrorMsg, err)
	}

	for _, node := range config.Nodes {
		if node.ID != id {
			peers[node.ID] = node.RPCAddr
			peershttp[node.ID] = node.HTTPAddr
		}
	}

	return peers, peershttp
}

func startRPCServer(raftConsensus *consensus.RaftConsensus, address string) {
	log.Println("------------------- Starting RPC Server ---------------------")
	if err := appRpc.StartServer(raftConsensus, address); err != nil {
		log.Fatalf("Failed to start RPC server: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	log.Println("RPC server started on", address)
	log.Println("------------------- RPC Server Started ---------------------")
}

func startPprofServer(id int, pprofServer string) {
	log.Println("------------------- Starting pprof Server ---------------------")
	if pprofServer == "" {
		log.Println("[Node", id, "] No pprof server address provided, skipping pprof server startup")
		log.Println("------------------- pprof Server Skipped ---------------------")
		return
	}

	go func() {
		log.Println("[Node", id, "] pprof server started on http://"+pprofServer+"/debug/pprof/")
		if err := http.ListenAndServe(pprofServer, nil); err != nil {
			log.Println("[Node", id, "] pprof server error:", err)
		}
	}()
	log.Println("------------------- pprof Server Started ---------------------")
	time.Sleep(100 * time.Millisecond)
}

func startRaftConsensus(id int, raftConsensus *consensus.RaftConsensus, store *storage.Store) {
	log.Println("------------------- Starting Raft Consensus ---------------------")
	raftConsensus.Start(store)
	log.Println("Raft consensus started on node", id)
	log.Println("------------------- Raft Consensus Started ---------------------")
	time.Sleep(500 * time.Millisecond)
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
