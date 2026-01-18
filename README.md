# Distributed Key-Value Store with Raft Consensus

A simplified implementation of the Raft consensus algorithm to ensure data consistency across multiple nodes in a distributed key-value store.

## Getting Started

### Run a Single Node

```bash
go run ./app/node/main.go -id node1 -address localhost:5000
```

Options:
- `-id`: Node ID (default: "node1")
- `-address`: RPC server bind address (default: "localhost:5000")
- `-data-dir`: Directory for persistent state (default: "./data")

### Run a Three-Node Cluster

(Coming in Phase 1)

## Project Layout

```
.
├── api/             RPC and HTTP API handlers
├── app/node/        Entry point and main server orchestration
├── config/          Configuration loading and defaults
├── consensus/       Raft consensus logic (elections, log replication)
├── rpc/             RPC server implementation
├── storage/         Persistent state and key-value store
└── types/           Shared type definitions (no logic)
```

## Phase 0: Scaffolding ✓

- Shared types defined (no circular imports)
- Configuration structure (node ID, address, peers, timeouts)
- Minimal main.go (flag parsing, config creation, ready for Phase 1)
- README documenting how to run

## Phase 1: Storage (Coming Next)

State machine implementation and persistent log storage.

## Contributors

@mokhatiri