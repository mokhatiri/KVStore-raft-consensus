// Package api implements the HTTP API for client requests.
package api

import (
	"distributed-kv/consensus"
	"distributed-kv/storage"
	"distributed-kv/types"
)

// TODO : Implement the http api to handle client requests (GET, SET, DELETE).

// the server struct
type Server struct {
	store     *storage.Store
	consensus *consensus.RaftConsensus
	node      *types.Node
	addr      string
}

func NewServer(store *storage.Store, consensus *consensus.RaftConsensus, node *types.Node, addr string) *Server {
	return &Server{
		store:     store,
		consensus: consensus,
		node:      node,
		addr:      addr,
	}
}

