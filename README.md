# KVStore-raft-consensus

A distributed key-value store implementation using the **Raft consensus algorithm** to ensure strong consistency and fault tolerance across a cluster of nodes.

## Overview

This project implements a production-ready distributed KV store where:

- **Strong Consistency**: All writes go through the Raft consensus protocol
- **Fault Tolerance**: Cluster survives up to (N-1)/2 node failures
- **Dynamic Membership**: Add/remove nodes from the cluster without downtime
- **Durability**: State persisted to disk (JSON or SQLite)
- **Snapshots**: Log compaction and quick recovery via snapshots

## Architecture

```draft
  ┌───────────────────────────────────────────┐
  │             Client Applications           │
  └───────────────────────────────────────────┘
                        │
        ┌───────────────┼───────────────┐
        │               │               │
    ┌───▼───┐       ┌───▼───┐       ┌───▼───┐
    │ Node1 │       │ Node2 │       │ Node3 │
    │       │       │       │       │       │
    │┌─────┐│       │┌─────┐│       │┌─────┐│
    ││ RPC ││◄──────┤│ RPC │├──────►││ RPC ││
    │└─────┘│       │└─────┘│       │└─────┘│
    │┌─────┐│       │┌─────┐│       │┌─────┐│
    ││ HTTP││       ││ HTTP││       ││ HTTP││
    │└─────┘│       │└─────┘│       │└─────┘│
    │┌─────┐│       │┌─────┐│       │┌─────┐│
    ││Raft ││       ││Raft ││       ││Raft ││
    │└─────┘│       │└─────┘│       │└─────┘│
    │┌─────┐│       │┌─────┐│       │┌─────┐│
    ││ KV  ││       ││ KV  ││       ││ KV  ││
    │└─────┘│       │└─────┘│       │└─────┘│
    └───────┘       └───────┘       └───────┘
```

### Components

- **Consensus Module** (`consensus/`): Raft protocol implementation
- **Storage** (`storage/`): Persistent state management (JSON/SQLite)
- **RPC** (`rpc/`): Inter-node communication
- **HTTP API** (`api/`): Client and manager interfaces
- **Handlers** (`handlers/`): Command processing
- **CLI** (`cli/`): Command-line interface

## Features

### Implemented

- **Core Raft**
  - Leader election with randomized timeouts
  - Log replication with term tracking
  - Commit index management
  - State machine snapshots for log compaction

- **Cluster Management**
  - Joint consensus for safe cluster membership changes
  - Add/remove nodes dynamically
  - Configuration propagation

- **Storage**
  - JSON-based persistence (default)
  - SQLite backend for production deployments
  - State restoration on startup

- **Client API**
  - HTTP endpoints for KV operations
  - Health checks
  - Cluster status monitoring

### In Progress / Planned

- **Security**
  - TLS encryption for RPC and HTTP
  - Authentication and authorization
  - Rate limiting

- **Monitoring**
  - Prometheus metrics
  - Detailed replication lag tracking
  - Cluster health visualization

- **Operational Tools**
  - Force snapshot trigger
  - Leadership transfer
  - Node rebuild operations

## Installation

### Prerequisites

- Go 1.25+
- SQLite3 (optional, for production)

### Setup

```bash
# Clone the repository
git clone https://github.com/mokhatiri/KVStore-raft-consensus.git
cd KVStore-raft-consensus

# Install dependencies
go mod download

# Build
go build -o kvstore ./app

# Or use the CLI directly
go run ./app/main.go
```

## Usage

### Starting a Cluster

#### 1. Create cluster configuration

Create `cluster.json`:

```json
{
  "nodes": {
    "1": {
      "id": 1,
      "http_addr": "localhost:8001",
      "rpc_addr": "localhost:9001"
    },
    "2": {
      "id": 2,
      "http_addr": "localhost:8002",
      "rpc_addr": "localhost:9002"
    },
    "3": {
      "id": 3,
      "http_addr": "localhost:8003",
      "rpc_addr": "localhost:9003"
    }
  }
}
```

#### 2. Start nodes

```bash
# Terminal 1 - Start Node 1
go run ./app/main.go --node-id 1

# Terminal 2 - Start Node 2
go run ./app/main.go --node-id 2

# Terminal 3 - Start Node 3
go run ./app/main.go --node-id 3
```

The cluster will elect a leader automatically within ~1.5 seconds.

### Client Operations

#### Set a key-value pair

```bash
curl -X POST http://localhost:8001/kv/mykey \
  -H "Content-Type: application/json" \
  -d '{"value": "myvalue"}'
```

#### Get a key

```bash
curl http://localhost:8001/kv/mykey
```

#### Delete a key

```bash
curl -X DELETE http://localhost:8001/kv/mykey
```

#### Check node health

```bash
curl http://localhost:8001/health
```

### Admin Operations (Manager API)

Get cluster status:

```bash
curl http://localhost:8000/cluster/status
```

Response:

```json
{
  "leader": 1,
  "currentTerm": 5,
  "liveNodes": 3,
  "totalNodes": 3,
  "electedAt": "2026-02-28T10:30:00Z",
  "replicationProgress": {
    "2": {
      "matchIndex": 42,
      "nextIndex": 43
    },
    "3": {
      "matchIndex": 42,
      "nextIndex": 43
    }
  }
}
```

Get cluster nodes:

```bash
curl http://localhost:8000/cluster/nodes
```

### Using the CLI

```bash
# Start a node with CLI
go run ./app/main.go --node-id 1

# Inside CLI, available commands:
set <key> <value>     # Set a key-value pair
get <key>             # Get a value by key
delete <key>          # Delete a key
clean                 # Clear all entries
add-server <id> <rpc> <http>  # Add a node to cluster
remove-server <id>    # Remove a node from cluster
status                # Show cluster status
help                  # Show available commands
```

## API Reference

### Node API (Client Operations)

| Method | Endpoint    | Description              |
|--------|-------------|--------------------------|
| GET    | `/kv/{key}` | Retrieve value for a key |
| POST   | `/kv/{key}` | Set value for a key      |
| DELETE | `/kv/{key}` | Delete a key             |
| GET    | `/health`   | Health check endpoint    |

### Manager API (Cluster Operations)

| Method | Endpoint          | Description                                 |
|--------|-------------------|---------------------------------------------|
| GET    | `/cluster/status` | Get cluster status and replication progress |
| GET    | `/cluster/nodes`  | List all nodes with their status            |

### Request/Response Examples

**Set Key:**

```terminal
POST /kv/username
Content-Type: application/json

{"value": "john_doe"}

Response: 200 OK
✓ Proposed SET username = john_doe (index: 42, term: 5) - waiting for commitment...
```

**Get Key:**

```terminal
GET /kv/username

Response: 200 OK
john_doe
```

**Delete Key:**

```terminal
DELETE /kv/username

Response: 200 OK
✓ Key deleted
```

## Storage Backends

### JSON (Default)

Simple file-based storage. Good for development and testing.

```files
state/
  node_1
  store_1
  node_2
  store_2
  ...
```

### SQLite (Production)

Use SQLite for better durability and query capabilities:

```go
persister, err := storage.NewDatabasePersister(nodeID, "./data")
```

Database structure:

- `raft_state`: Current term, voted for, log entries
- `log_entries`: Raft log with index, term, type, data

## Consensus Algorithm Details

### Leader Election

1. Follower timeout triggers → becomes Candidate
2. Candidate requests votes from all peers
3. If majority votes received → becomes Leader
4. Heartbeats reset follower timeouts

### Log Replication

1. Client sends command to Leader
2. Leader appends to its log → sends to Followers
3. Followers append and acknowledge
4. Leader commits when majority acknowledged
5. All nodes apply committed entries to state machine

### Snapshots

- Triggered when log size exceeds threshold
- Contains state machine data + last included index/term
- Followers can install snapshots instead of replaying entire log

### Membership Changes

Uses joint consensus for safe cluster transitions:

1. Leader proposes new config (old + new members)
2. Both configs must have majority to commit entry
3. New config entry committed
4. Transition complete

## Testing

Run the test suite:

```bash
go test ./...

# With verbose output
go test -v ./...

# Test specific package
go test -v ./consensus
go test -v ./storage
go test -v ./api/node_api
```

Test coverage:

```bash
go test -cover ./...
```

## Performance Considerations

### Current Limitations

- **HTTP Polling**: Manager API uses HTTP polling (1-2s latency)
- **Single Leader**: All writes serialized through leader
- **Network Timeouts**: 150ms election timeout (configurable)

### Optimization Tips

- Increase batch sizes for high-throughput scenarios
- Use SQLite backend instead of JSON for large logs
- Adjust election timeout based on network latency in /consensus/config.go
- Enable snapshots for faster recovery

## Troubleshooting

### Cluster won't elect a leader

- Check all nodes are running
- Verify network connectivity between nodes
- Check logs for election errors
- Ensure node IDs match configuration

### Writes timeout

- Not connected to leader - try another node
- Leader crashed - wait for re-election (~1.5s)
- Check network latency between nodes

### High replication lag

- Indicates slow follower or network issues
- Check CPU/memory on slow node
- Monitor `/cluster/status` endpoint

## Configuration

Key configurable parameters in `consensus/config.go`:

```go
// Election timeout range
ElectionTimeoutMin: 150ms
ElectionTimeoutMax: 300ms

// Heartbeat interval
HeartbeatInterval: 50ms

// Snapshot settings
SnapshotThreshold: 1000 entries
```

## Development

### Adding a new command

1. Add handler in `handlers/handlers.go`:

    ```go
    func MyCommandHandler(consensus types.ConsensusModule, args... string) (string, error) {
        command := fmt.Sprintf("MYCOMMAND:%s", args)
        index, term, _ := consensus.Propose(command)
        return fmt.Sprintf("✓ Proposed...", index, term), nil
    }
    ```

2. Register in CLI `cli/nodecli/node_handling.go`

3. Handle in state machine `app/state/`

4. Add tests in `*_test.go` files

### Running in Debug Mode

Enable debug logging:

```go
logBuffer.AddLog("DEBUG", "detailed message")
```

View logs:

```bash
tail -f logbuffer.log
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## Authors

- @mokhatiri

## Support

For issues, questions, or suggestions, open a GitHub issue or contact the maintainers.

---

**Status**: Alpha. Suitable for learning and small-scale deployments. Production use requires TLS, monitoring, and operational tooling .
