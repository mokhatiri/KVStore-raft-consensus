package main

import (
	managerapi "distributed-kv/api/manager_api"
	nodeapi "distributed-kv/api/node_api"
	"distributed-kv/clustermanager"
	"distributed-kv/consensus"
	"distributed-kv/storage"
	"log"
)

func startNodeAPI(HTTPaddress string, raftConsensus *consensus.RaftConsensus, store *storage.Store) {
	log.Println("------------------- Starting API Server ------------------")
	// check if HTTP address is provided
	if HTTPaddress == "" {
		log.Println("No HTTP address provided, skipping API server startup")
		log.Println("-------------------- API Server Skipped ------------------")
		return
	}

	log.Println("Starting HTTP API server on", HTTPaddress)

	httpServer := nodeapi.NewNodeServer(store, raftConsensus, HTTPaddress, raftConsensus.GetLogBuffer())
	httpServer.Start()

	log.Println("HTTP API server started on", HTTPaddress)
	log.Println("-------------------- API Server Running ------------------")
}

func startManagerAPI(HTTPaddress string, manager *clustermanager.Manager) {
	log.Println("------------------- Starting Manager API Server ------------------")

	if HTTPaddress == "" {
		log.Println("No HTTP address provided, skipping API server startup")
		log.Println("-------------------- API Server Skipped ------------------")
		return
	}

	log.Println("Starting HTTP API server on", HTTPaddress)

	httpServer := managerapi.NewManagerServer(HTTPaddress, manager, manager.GetLogBuffer())
	httpServer.Start()

	log.Println("HTTP API server started on", HTTPaddress)
	log.Println("-------------------- API Server Running ------------------")
}
