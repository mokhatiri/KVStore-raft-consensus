package managerapi

import (
	"distributed-kv/types"
	"net"
	"net/rpc"
	"time"
)

func (ms *ManagerServer) ReceiveNodeState(args *types.NodeState, reply *bool) error {
	// update ms.manager.clusterState.Nodes[args.ID] with the received node state
	// Mark node as alive since it's communicating with us
	args.IsAlive = true
	args.LastSeen = time.Now()
	err := ms.manager.UpdateNodeState(args)
	*reply = true
	return err
}

func (ms *ManagerServer) ReceiveRPCEvent(args *types.RPCEvent, reply *bool) error {
	err := ms.manager.SaveEvent(*args)
	*reply = true
	return err
}

// Ping is a health check RPC call that nodes can invoke
func (ms *ManagerServer) Ping(nodeID int, reply *bool) error {
	*reply = true
	return nil
}

func StartManagerRPCServer(address string, manager types.ManagerInterface, logger types.Logger) error {
	server := NewManagerServer(address, manager, logger)

	// register the RPC server using net/rpc
	err := rpc.Register(server)
	if err != nil {
		return err
	}

	// listen on the address
	listener, err := net.Listen("tcp", address) // listening on the given address
	if err != nil {
		return err
	}

	// Accept incoming connections in goroutine
	go rpc.Accept(listener)

	return nil

}
