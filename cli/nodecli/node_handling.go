package nodecli

import (
	"bufio"
	"distributed-kv/consensus"
	"distributed-kv/handlers"
	"distributed-kv/storage"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Handling struct {
	consensus *consensus.RaftConsensus
}

func NewHandling(consensus *consensus.RaftConsensus) *Handling {
	return &Handling{
		consensus: consensus,
	}
}

func (h *Handling) HandleGet(parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: get <key>")
		return
	}

	key := parts[1]

	store := h.consensus.GetStore().(*storage.Store)
	value, exists := store.Get(key)
	if !exists {
		fmt.Printf("Key '%s' not found\n", key)
		return
	}

	fmt.Printf("%s = %v\n", key, value)
}

func (h *Handling) HandleSet(parts []string) {
	if len(parts) < 3 {
		fmt.Println("Usage: set <key> <value>")
		return
	}

	// Check if node is leader first
	key := parts[1]
	value := strings.Join(parts[2:], " ")

	msg, err := handlers.SetHandler(h.consensus, key, value)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(msg)
}

func (h *Handling) HandleDelete(parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: delete <key>")
		return
	}

	key := parts[1]

	msg, err := handlers.DeleteHandler(h.consensus, key)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(msg)
}

func (h *Handling) HandleAll() {
	store := h.consensus.GetStore().(*storage.Store)
	data := store.GetAll()
	if len(data) == 0 {
		fmt.Println("Store is empty")
		return
	}

	fmt.Println("Key-Value Store:")
	for key, value := range data {
		fmt.Printf("  %s = %v\n", key, value)
	}
}

func (h *Handling) HandleStatus() {
	h.consensus.PrintStatus()
}

func (h *Handling) HandleLog() {
	h.consensus.PrintLog()
}

func (h *Handling) HandleLogs(limit int) {
	logs := h.consensus.GetLogs(limit)

	if len(logs) == 0 {
		fmt.Println("No logs available")
		return
	}

	fmt.Println("\n--- Node Logs ---")
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

func (h *Handling) HandleClean() {
	// wait for confirmation before proposing to avoid multiple CLEAN commands being proposed at the same time
	fmt.Print("Are you sure you want to CLEAN all data? This action cannot be undone. (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	confirmation, _ := reader.ReadString('\n')
	confirmation = strings.TrimSpace(strings.ToLower(confirmation))

	if confirmation != "yes" {
		fmt.Println("CLEAN command aborted")
		return
	}

	msg, err := handlers.CleanHandler(h.consensus)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(msg)
}

func HandleHelp() {
	fmt.Println("\n--- Node CLI - Available Commands ---")
	fmt.Println()
	fmt.Println("--- Data Operations (Local Store) ---")
	fmt.Println(" get <key>                   - Get value from THIS node's store (any role)")
	fmt.Println(" set <key> <value>           - Set value in cluster (requires leader role)")
	fmt.Println(" delete <key>                - Delete key from cluster (requires leader role)")
	fmt.Println(" all                         - Show all key-value pairs on THIS node")
	fmt.Println()
	fmt.Println("--- State & Logs ---")
	fmt.Println(" status                      - Show node status (role, term, leader, log length)")
	fmt.Println(" log                         - Show the Raft replication log (committed entries)")
	fmt.Println(" logs [limit]                - Show node internal logs (errors, info, warnings)")
	fmt.Println()
	fmt.Println("--- Admin ---")
	fmt.Println(" clean                       - Delete all data from cluster (requires leader, prompts for confirmation)")
	fmt.Println()
	fmt.Println("--- Utilities ---")
	fmt.Println(" help                        - Show this help message")
	fmt.Println(" clear                       - Clear the console")
	fmt.Println(" exit                        - Exit the program")
	fmt.Println()
}

func (h *Handling) HandleCommand(input string) {
	parts := strings.Fields(input)
	command := parts[0]

	/*
		Commands:
		  get <key> - Get value from THIS node's local store (read-only, any role)
		  set <key> <value> - Set/update value (requires leader role)
		  delete <key> - Delete key (requires leader role)
		  clean - Delete all data (requires leader role, prompts for confirmation)
		  all - Show all key-value pairs in THIS node's store
		  status - Show node status (role, term, log length, leader info)
		  log - Show the Raft replication log (committed entries)
		  logs [limit] - Show node internal logs (errors, info, warnings)
		  help - Show this help message
		  clear - Clear the console
		  exit - Exit the program
	*/

	switch command {
	case "get":
		h.HandleGet(parts)

	case "set":
		h.HandleSet(parts)

	case "delete":
		h.HandleDelete(parts)

	case "all":
		h.HandleAll()

	case "status":
		h.HandleStatus()

	case "log":
		h.HandleLog()

	case "logs":
		limit := 20 // default
		if len(parts) > 1 {
			if l, err := strconv.Atoi(parts[1]); err == nil {
				limit = l
			}
		}
		h.HandleLogs(limit)

	case "clean":
		h.HandleClean()

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
