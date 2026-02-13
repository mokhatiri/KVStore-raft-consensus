package handlers

import (
	"fmt"

	"distributed-kv/types"
)

func SetHandler(raftConsensus types.ConsensusModule, key, value string) (string, error) {
	role := raftConsensus.GetRole()
	if role != "Leader" {
		return "", fmt.Errorf("Error: Not the leader")
	}

	// Format command as "SET:key:value"
	command := fmt.Sprintf("SET:%s:%s", key, value)

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose(command)
	return fmt.Sprintf("✓ Proposed SET %s = %s (index: %d, term: %d) - waiting for commitment...", key, value, index, term), nil
}

func DeleteHandler(raftConsensus types.ConsensusModule, key string) (string, error) {
	role := raftConsensus.GetRole()
	if role != "Leader" {
		return "", fmt.Errorf("Error: Not the leader")
	}

	// Format command as "DELETE:key"
	command := fmt.Sprintf("DELETE:%s", key)

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose(command)
	return fmt.Sprintf("✓ Proposed DELETE %s (index: %d, term: %d) - waiting for commitment...", key, index, term), nil
}

func CleanHandler(raftConsensus types.ConsensusModule) (string, error) {
	role := raftConsensus.GetRole()
	if role != "Leader" {
		return "", fmt.Errorf("Error: Not the leader")
	}

	// Format command as "CLEAN"
	command := "CLEAN"

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose(command)
	return fmt.Sprintf("✓ Proposed CLEAN (index: %d, term: %d) - waiting for commitment...", index, term), nil
}
