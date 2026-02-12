package handlers

import (
	"fmt"

	"distributed-kv/types"
)

func SetHandler(raftConsensus types.ConsensusModule, key, value string) error {
	role := raftConsensus.GetRole()
	if role != "Leader" {
		return fmt.Errorf("Error: Not the leader")
	}

	// Format command as "SET:key:value"
	command := fmt.Sprintf("SET:%s:%s", key, value)

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose(command)
	fmt.Printf("✓ Proposed SET %s = %s (index: %d, term: %d) - waiting for commitment...\n", key, value, index, term)
	return nil
}

func DeleteHandler(raftConsensus types.ConsensusModule, key string) error {
	role := raftConsensus.GetRole()
	if role != "Leader" {
		return fmt.Errorf("Error: Not the leader")
	}

	// Format command as "DELETE:key"
	command := fmt.Sprintf("DELETE:%s", key)

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose(command)
	fmt.Printf("✓ Proposed DELETE %s (index: %d, term: %d) - waiting for commitment...\n", key, index, term)
	return nil
}

func CleanHandler(raftConsensus types.ConsensusModule) error {
	role := raftConsensus.GetRole()
	if role != "Leader" {
		return fmt.Errorf("Error: Not the leader")
	}

	// Format command as "CLEAN"
	command := "CLEAN"

	// Propose the command to Raft consensus
	index, term, _ := raftConsensus.Propose(command)
	fmt.Printf("✓ Proposed CLEAN (index: %d, term: %d) - waiting for commitment...\n", index, term)
	return nil
}
