package managercli

import (
	"distributed-kv/clustermanager"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Handling struct {
	manager *clustermanager.Manager
}

func NewHandling(manager *clustermanager.Manager) *Handling {
	return &Handling{
		manager: manager,
	}
}

func (h *Handling) HandleCommand(input string) {
	parts := strings.Fields(input)
	command := parts[0]

	/*
		Commands:
		  cluster status - Show cluster status (leader ID, term, live nodes, replication status)
		  nodes list - Show status of all nodes in the cluster
		  node status <nodeID> - Show detailed status of a specific node
		  node register <nodeID> <RPCAddress> <HTTPAddress> - Register a new node
		  node unregister <nodeID> - Unregister a node
		  health check - Perform health check on all nodes
		  replication status - Show replication status (match/next indices) for all followers
		  log [limit] - Show manager logs (errors, info, warnings)
		  help - Show this help message
		  clear - Clear the console
		  exit - Exit the program
	*/

	switch command {
	case "cluster":
		if len(parts) < 2 {
			fmt.Println("Usage: cluster <status>")
			return
		}
		subcommand := parts[1]
		switch subcommand {
		case "status":
			h.HandleClusterStatus()
		default:
			fmt.Println("Unknown cluster command. Available: status")
		}

	case "nodes":
		if len(parts) < 2 || parts[1] != "list" {
			fmt.Println("Usage: nodes list")
			return
		}
		h.HandleNodes(parts[1:])

	case "node":
		if len(parts) < 2 {
			fmt.Println("Usage: node <subcommand> [args]")
			return
		}
		subcommand := parts[1:]
		h.HandleNode(subcommand)

	case "health":
		if len(parts) < 2 || parts[1] != "check" {
			fmt.Println("Usage: health check")
			return
		}
		h.HandleHealthCheck()

	case "replication":
		if len(parts) < 2 || parts[1] != "status" {
			fmt.Println("Usage: replication status")
			return
		}
		h.HandleReplicationStatus()

	case "log":
		limit := 20 // default
		if len(parts) > 1 {
			if l, err := strconv.Atoi(parts[1]); err == nil {
				limit = l
			}
		}
		h.HandleLog(limit)

	case "help":
		HandleHelp()

	case "clear":
		fmt.Print("\033[H\033[2J") // clear console

	case "exit":
		fmt.Println("Exiting...")
		os.Exit(0) // exit the program

	default:
		fmt.Println("Unknown command. Type 'help' for available commands.")
	}
}

func (h *Handling) HandleClusterStatus() {
	fmt.Println("\n--- Cluster Status ---")
	clusterState := h.manager.GetClusterState()
	fmt.Printf("Leader ID: %d\n", clusterState.Leader)
	fmt.Printf("Current Term: %d\n", clusterState.CurrentTerm)

	// Count alive nodes vs total nodes
	aliveCount := 0
	for _, node := range clusterState.Nodes {
		if node.IsAlive {
			aliveCount++
		}
	}
	totalCount := len(clusterState.Nodes)
	fmt.Printf("Live Nodes: %d/%d\n", aliveCount, totalCount)

	if leader, ok := clusterState.Nodes[clusterState.Leader]; ok && leader != nil {
		fmt.Printf("Leader Log Length: %d\n", leader.LogLength)
	} else {
		fmt.Println("Leader Log Length: N/A (no leader elected)")
	}

	fmt.Printf("Replication Progress:\n")
	for nodeId, followers := range clusterState.ReplicationProgress {
		fmt.Printf("  Node %d:\n", nodeId)
		for followerId, matchIndex := range followers {
			fmt.Printf("    Follower %d: Match Index = %d\n", followerId, matchIndex)
		}
	}
}

func (h *Handling) HandleNode(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: node <status|register|unregister> [args]")
		return
	}

	subcommand := args[0]

	switch subcommand {
	case "status":
		if len(args) < 2 {
			fmt.Println("Usage: node status <nodeID>")
			return
		}
		nodeID, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Println("Invalid node ID")
			return
		}
		h.printNodeStatus(nodeID)

	case "register":
		if len(args) < 4 {
			fmt.Println("Usage: node register <nodeID> <HTTPAddress>")
			return
		}
		nodeID, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Println("Invalid node ID")
			return
		}
		httpAddr := args[3]
		h.manager.RegisterNode(nodeID, httpAddr)
		fmt.Printf("Node %d registered with HTTP Address: %s\n", nodeID, httpAddr)

	case "unregister":
		if len(args) < 2 {
			fmt.Println("Usage: node unregister <nodeID>")
			return
		}
		nodeID, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Println("Invalid node ID")
			return
		}
		h.manager.UnregisterNode(nodeID)
		fmt.Printf("Node %d unregistered\n", nodeID)

	default:
		fmt.Println("Usage: node <status|register|unregister> [args]")
	}
}

func (h *Handling) HandleNodes(args []string) {
	if len(args) == 0 || args[0] != "list" {
		fmt.Println("Usage: nodes list")
		return
	}

	fmt.Println("\n--- Cluster Nodes ---")
	clusterState := h.manager.GetClusterState()

	if len(clusterState.Nodes) == 0 {
		fmt.Println("No nodes registered")
		return
	}

	for nodeID, nodeState := range clusterState.Nodes {
		fmt.Printf("\nNode ID: %d\n", nodeID)
		fmt.Printf("  Role: %s\n", nodeState.Role)
		fmt.Printf("  Term: %d\n", nodeState.Term)
		fmt.Printf("  HTTP Address: %s\n", nodeState.HTTPAddress)
		fmt.Printf("  State: %v\n", map[bool]string{true: "Alive", false: "Dead"}[nodeState.IsAlive])
		fmt.Printf("  Last Seen: %v\n", nodeState.LastSeen)
		fmt.Printf("  Latency: %v\n", nodeState.ResponseLatency)
	}
	fmt.Printf("\nTotal Nodes: %d\n", len(clusterState.Nodes))
}

func (h *Handling) HandleHealthCheck() {
	fmt.Println("\n--- Health Check ---")
	clusterState := h.manager.GetClusterState()

	aliveCount := 0
	deadCount := 0

	for nodeID, nodeState := range clusterState.Nodes {
		status := "Alive"
		if !nodeState.IsAlive {
			status = "Dead"
			deadCount++
		} else {
			aliveCount++
		}
		fmt.Printf("Node %d: %s (Last seen: %v ago)\n", nodeID, status, time.Since(nodeState.LastSeen).Round(time.Second))
	}

	fmt.Printf("\nCluster Health: %d alive, %d dead\n", aliveCount, deadCount)
	if deadCount > 0 {
		fmt.Printf("WARNING: %d nodes are unreachable\n", deadCount)
	} else {
		fmt.Println("All nodes are healthy!")
	}
}

func (h *Handling) HandleReplicationStatus() {
	fmt.Println("\n--- Replication Status ---")
	clusterState := h.manager.GetClusterState()

	if clusterState.Leader == 0 {
		fmt.Println("No leader elected yet")
		return
	}

	fmt.Printf("Leader: Node %d\n\n", clusterState.Leader)

	for nodeID, followers := range clusterState.ReplicationProgress {
		fmt.Printf("Node %d (Leader):\n", nodeID)
		if len(followers) == 0 {
			fmt.Println("  No followers")
			continue
		}

		for followerId, matchIndex := range followers {
			var status string
			if followerNode, ok := clusterState.Nodes[followerId]; ok {
				if followerNode.IsAlive {
					status = "Alive"
				} else {
					status = "Dead"
				}
			} else {
				status = "Unknown"
			}
			fmt.Printf("  Follower %d: Match Index = %d (%s)\n", followerId, matchIndex, status)
		}
	}
}

func HandleHelp() {
	fmt.Println("\n--- Manager CLI - Available Commands ---")
	fmt.Println()
	fmt.Println("--- Cluster ---")
	fmt.Println(" cluster status                              - Show cluster status (leader ID, term, live nodes)")
	fmt.Println()
	fmt.Println("--- Nodes ---")
	fmt.Println(" nodes list                                  - Show status of all registered nodes")
	fmt.Println(" node status <nodeID>                        - Show detailed status of a specific node")
	fmt.Println(" node register <nodeID> <RPC> <HTTP>         - Register a new node in cluster")
	fmt.Println(" node unregister <nodeID>                    - Unregister a node from cluster")
	fmt.Println()
	fmt.Println("--- Diagnostics ---")
	fmt.Println(" health check                                - Health check on all nodes (connectivity, state)")
	fmt.Println(" replication status                          - Show replication progress (match index, next index)")
	fmt.Println()
	fmt.Println("--- Manager Logs ---")
	fmt.Println(" log [limit]                                 - Show manager internal logs (default: last 20)")
	fmt.Println()
	fmt.Println("--- Utilities ---")
	fmt.Println(" help                                        - Show this help message")
	fmt.Println(" clear                                       - Clear the console")
	fmt.Println(" exit                                        - Exit the program")
	fmt.Println()
}

func (h *Handling) printNodeStatus(nodeID int) {
	clusterState := h.manager.GetClusterState()
	nodeState, ok := clusterState.Nodes[nodeID]
	if !ok {
		fmt.Printf("Node %d not found\n", nodeID)
		return
	}

	fmt.Printf("\n--- Node %d Status ---\n", nodeID)
	fmt.Printf("ID: %d\n", nodeState.ID)
	fmt.Printf("Role: %s\n", nodeState.Role)
	fmt.Printf("Term: %d\n", nodeState.Term)
	fmt.Printf("Commit Index: %d\n", nodeState.CommitIndex)
	fmt.Printf("Last Applied: %d\n", nodeState.LastApplied)
	fmt.Printf("Log Length: %d\n", nodeState.LogLength)
	fmt.Printf("HTTP Address: %s\n", nodeState.HTTPAddress)
	fmt.Printf("Status: %s\n", map[bool]string{true: "Alive", false: "Dead"}[nodeState.IsAlive])
	fmt.Printf("Response Latency: %v\n", nodeState.ResponseLatency)
	fmt.Printf("Last Seen: %v\n", nodeState.LastSeen)
}

func (h *Handling) HandleLog(limit int) {
	logs := h.manager.LogBuffer.GetLogs(limit)

	if len(logs) == 0 {
		fmt.Println("No logs available")
		return
	}

	fmt.Println("\n--- Manager Logs ---")
	fmt.Printf("%-35s | %-5s | %s\n", "Timestamp", "Level", "Message")
	fmt.Println(strings.Repeat("-", 100))

	for _, log := range logs {
		fmt.Printf("%-35s | %-5s | %s\n",
			log.Timestamp.Format("2006-01-02 15:04:05.000"),
			log.Level,
			log.Message,
		)
	}
	fmt.Println()
}
