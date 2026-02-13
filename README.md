# Distributed Key-Value Store with Raft Consensus

A complete implementation of the [Raft consensus algorithm](https://raft.github.io/) for a distributed key-value store. Ensures data consistency and durability across multiple nodes with automatic leader election and log replication.

## Features

✅ **Full Raft Implementation**
- Leader election with randomized timeouts
- Log replication to followers
- Commit index management and state machine application
- Safety properties enforced

✅ **Cluster Management**
- Multi-node cluster support
- Real-time cluster state monitoring
- Node health checks and liveness detection
- Replication progress tracking

✅ **Event Tracking & Monitoring**
- RPC event logging (RequestVote, AppendEntries)
- Automatic heartbeat filtering (skip idle maintains)
- Performance metrics (latency statistics, error tracking)
- Event export to CSV for analysis

✅ **Dual CLI Interfaces**
- **Node CLI**: Local node operations (get/set/delete, status, logs)
- **Manager CLI**: Cluster-wide monitoring and diagnostics

✅ **HTTP APIs**
- Node API: Key-value operations and health checks
- Manager API: Cluster status, events, node management

## Architecture

```
KVStore-raft-consensus/
├── app/                    # Application entry points
│   ├── main.go            # Node/Manager startup
│   ├── cli.go             # CLI initialization
│   └── api_start.go       # HTTP server setup
├── consensus/             # Raft consensus logic
│   └── raft.go            # Election, replication, commit
├── rpc/                   # RPC communication
│   ├── server.go          # RPC handler endpoints
│   └── client.go          # RPC client calls
├── clustermanager/        # Cluster management
│   ├── manager.go         # State tracking
│   ├── events.go          # Event logging and filtering
│   └── logs.go            # Manager logs
├── api/                   # HTTP handlers
│   ├── manager_api/       # Cluster monitoring endpoints
│   └── node_api/          # Key-value operation endpoints
├── cli/                   # CLI implementations
│   ├── managercli/        # Manager commands
│   └── nodecli/           # Node commands
├── storage/               # Key-value store
├── types/                 # Shared type definitions
└── handlers/              # Command handlers
```

## Quick Start

### 1. Setup

Create `cluster.json` with your node configuration:

```json
{
  "nodes": [
    {"id": 1, "rpcAddr": "127.0.0.1:8001", "httpAddr": "127.0.0.1:9001", "pprofAddr": "127.0.0.1:6061"},
    {"id": 2, "rpcAddr": "127.0.0.1:8002", "httpAddr": "127.0.0.1:9002", "pprofAddr": "127.0.0.1:6062"},
    {"id": 3, "rpcAddr": "127.0.0.1:8003", "httpAddr": "127.0.0.1:9003", "pprofAddr": "127.0.0.1:6063"}
  ],
  "manager": {
    "rpcAddr": "127.0.0.1:8080",
    "httpAddr": "127.0.0.1:7090",
    "pprofAddr": "127.0.0.1:6060",
    "dbPath": "events.db",
    "outputDir": "events_output"
  }
}
```

### 2. Start the Manager (in one terminal)

```bash
go run ./app/main.go manager -config cluster.json
```

### 3. Start Nodes (in separate terminals)

```bash
# Node 1
go run ./app/main.go node -id 1 -config cluster.json

# Node 2
go run ./app/main.go node -id 2 -config cluster.json

# Node 3
go run ./app/main.go node -id 3 -config cluster.json
```

## Using the Node CLI

Once a node starts, you'll see a prompt. Available commands:

### Key-Value Operations (Leader Only)
```
set <key> <value>     # Set a key-value pair
delete <key>          # Delete a key
get <key>             # Get value for a key
all                   # Show all keys and values
```

### Diagnostics
```
status                # Show node role, term, log length
log [limit]           # Show replication log
help                  # Show all commands
```

## Using the Manager CLI

Connect to the manager to monitor the entire cluster:

### Cluster Status
```
cluster status        # Show leader, term, replication progress
```

### Event Monitoring
Events are automatically filtered to exclude heartbeats. Only meaningful RPCs are shown:

```
events list [limit]           # Show recent replication/vote events
events filter <type>          # Filter by RPC type (AppendEntries, RequestVote)
events stats                  # Show latency stats, error counts
events export <file.csv>      # Export to CSV
```

### Node Management
```
nodes list                                          # Show all nodes
node status <nodeID>                                # Detailed node info
node register <id> <rpcAddr> <httpAddr>           # Add node dynamically
node unregister <nodeID>                           # Remove node
```

### Diagnostics
```
health check                  # Check node connectivity
replication status            # Show match indices per follower
latency stats                 # Detailed RPC latency analysis
log [limit]                   # Show manager logs
```

## Event Tracking

The system tracks all RPC events with automatic filtering:

### Event Types
- **AppendEntries**: Log replication (data) or heartbeats
- **RequestVote**: Leader election votes

### Heartbeat Filtering

Heartbeats (empty AppendEntries) are automatically filtered out at multiple levels:

1. **Identified at source**: `IsHeartbeat` flag set in `EmitAppendEntriesEvent()`
2. **Filtered from files**: Events.log only contains meaningful RPCs
3. **Filtered from user APIs**: CLI defaults exclude heartbeats
4. **Raw database**: All events stored in SQLite for deep debugging

```
// A heartbeat event has IsHeartbeat=true
// len(args.Entries) == 0 indicates heartbeat
{
  "Timestamp": "2026-02-13T01:55:11.769...",
  "From": 1,
  "To": 2,
  "Type": "AppendEntries",
  "IsHeartbeat": true,  // ← Marked automatically
  "Details": "{\"args\":{\"Term\":4,...,\"Entries\":[]},\"reply\":{...}}",
  "Duration": 3642900,
  "Error": ""
}
```

### Event Statistics

Statistics automatically exclude heartbeats to show real cluster activity:

```
events stats

--- Event Statistics ---
avg_duration_ms: 6
total_events: 3
event_types: map[AppendEntries:2 RequestVote:1]
error_count: 1
```

## Replication Progress

Track how log entries are being replicated across the cluster:

```
cluster status

Replication Progress:
  Node 1:
    Follower 2: Match Index = 12
    Follower 3: Match Index = 10
```

- **Match Index**: Highest log entry known to be replicated to that follower
- Used to determine when entries are "committed" (replicated to majority)
- Follower with lower match index indicates replication lag

## HTTP API Endpoints

### Node API
```
GET  /kv/<key>              # Get value
POST /kv/<key>              # Set value (body: {"value": "..."})
DELETE /kv/<key>            # Delete value
GET  /health                # Node health and role info
```

### Manager API
```
GET /status                 # Cluster status (leader, term, nodes)
GET /events?limit=20        # Recent events
GET /nodes                  # All nodes details
GET /nodes/<id>             # Specific node details
```

## Configuration

Edit `cluster.json` to configure:
- Node addresses and ports
- Manager location
- Event storage directory
- Database path

```json
{
  "nodes": [
    {
      "id": 1,
      "rpcAddr": "127.0.0.1:8001",      // RPC server for Raft
      "httpAddr": "127.0.0.1:9001",     // HTTP server for client requests
      "pprofAddr": "127.0.0.1:6061"     // Profiling endpoint
    }
  ],
  "manager": {
    "rpcAddr": "127.0.0.1:8080",        // Manager RPC (for node heartbeats)
    "httpAddr": "127.0.0.1:7090",       // Manager HTTP API
    "dbPath": "events.db",               // SQLite database for events
    "outputDir": "events_output"         // Directory for event logs
  }
}
```

## Raft Implementation Details

### Elections
- Randomized timeout (150-300ms) prevents split votes
- Candidates request votes from all peers
- Majority wins leadership

### Log Replication
- Leader sends AppendEntries RPC to all followers
- Entries replicated only to followers behind leader
- Match index tracks replication progress
- Commit index advances when entry on majority of servers

### Safety
- Term numbers prevent stale data
- Leaders never overwrite committed entries
- Followers reject entries with mismatched terms
- Only leader can propose new entries

## Development Notes

### Testing

Run all tests:
```bash
go test ./...
```

Run specific package tests:
```bash
go test ./consensus -v      # Raft election and replication
go test ./rpc -v            # RPC communication
go test ./clustermanager -v # Event tracking
go test ./cli/managercli -v # CLI commands
```

### Debugging

Check node state:
```bash
# Terminal with node running
Node 1: status
Node 1: log
```

View all events in database:
```bash
sqlite3 events.db "SELECT * FROM rpc_events LIMIT 10;"
```

View raw event logs (formatted JSON):
```bash
cat events_output/events_*.log | jq .
```

## Implementation Status

✅ **Completed**
- Raft consensus algorithm (elections, replication, commits)
- Multi-node cluster support
- Event tracking with filtering
- CLI for nodes and manager
- HTTP APIs
- Persistent storage

🚀 **Potential Enhancements**
- Snapshot and log compaction
- Configuration changes (dynamic cluster membership)
- Client request pipelining
- Metrics and observability (Prometheus)
- Persistence optimizations

## Architecture Decisions

1. **Heartbeat Filtering**: Heartbeats stored in database but filtered from user-facing APIs to reduce noise
2. **IsHeartbeat Flag**: Semantic marker at RPC layer avoids JSON parsing for filtering
3. **PeerIDs Tracking**: Node struct tracks peer IDs for proper event logging
4. **Dual Storage**: In-memory buffer for recent events + database for history + files for offline analysis

## Contributors

@mokhatiri

## References

- [Raft Consensus Algorithm](https://raft.github.io/)
- [Extended Raft Paper](https://github.com/opennars/raft/blob/master/docs/raft_extended.pdf)