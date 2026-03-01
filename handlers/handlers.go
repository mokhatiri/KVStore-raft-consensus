package handlers

import (
	"fmt"

	"distributed-kv/types"
)

var notleader error = fmt.Errorf("Error: Not the leader")

func SetHandler(consensus types.ConsensusModule, key, value string) (string, error) {
	role := consensus.GetRole()
	if role != "Leader" {
		return "", notleader
	}

	// Format command as "SET:key:value"
	command := fmt.Sprintf("SET:%s:%s", key, value)

	// Propose the command to Raft consensus
	index, term, _ := consensus.Propose(command)
	return fmt.Sprintf("✓ Proposed SET %s = %s (index: %d, term: %d) - waiting for commitment...", key, value, index, term), nil
}

func DeleteHandler(consensus types.ConsensusModule, key string) (string, error) {
	role := consensus.GetRole()
	if role != "Leader" {
		return "", notleader
	}

	// Format command as "DELETE:key"
	command := fmt.Sprintf("DELETE:%s", key)

	// Propose the command to Raft consensus
	index, term, _ := consensus.Propose(command)
	return fmt.Sprintf("✓ Proposed DELETE %s (index: %d, term: %d) - waiting for commitment...", key, index, term), nil
}

func CleanHandler(consensus types.ConsensusModule) (string, error) {
	role := consensus.GetRole()
	if role != "Leader" {
		return "", notleader
	}

	// Format command as "CLEAN"
	command := "CLEAN"

	// Propose the command to Raft consensus
	index, term, _ := consensus.Propose(command)
	return fmt.Sprintf("✓ Proposed CLEAN (index: %d, term: %d) - waiting for commitment...", index, term), nil
}

func AddServerHandler(consensus types.ConsensusModule, nodeID int, httpaddress string, rpcaddress string) (string, error) {
	role := consensus.GetRole()
	if role != "Leader" {
		return "", notleader
	}

	// Attempt to add server to cluster via RequestAddServer
	if err := consensus.RequestAddServer(nodeID, rpcaddress, httpaddress); err != nil {
		return "", fmt.Errorf("Failed to add server: %v", err)
	}

	return fmt.Sprintf("✓ Initiated ADD_SERVER for node %d at %s - waiting for commitment...", nodeID, httpaddress), nil
}

func RemoveServerHandler(consensus types.ConsensusModule, nodeID int) (string, error) {
	role := consensus.GetRole()
	if role != "Leader" {
		return "", notleader
	}

	// Attempt to remove server from cluster via RequestRemoveServer
	if err := consensus.RequestRemoveServer(nodeID); err != nil {
		return "", fmt.Errorf("Failed to remove server: %v", err)
	}

	return fmt.Sprintf("✓ Initiated REMOVE_SERVER for node %d - waiting for commitment...", nodeID), nil
}
