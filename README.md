# Emergency Response Matching

A Go system connecting emergency requests with available resources by type and location.

## Development status

Domain models, validation, request lifecycle rules and deterministic resource selection are implemented. The storage cluster now provides a coordinator, three replicated shards, heartbeat leases, failover and restart synchronization. The public application API, RabbitMQ workflow, browser frontend and container deployment are under development.

## Architecture

The runtime design has a request API with an embedded browser frontend, a matching worker, RabbitMQ, a storage coordinator, and three storage shards with one replica each. Clients use REST/JSON without knowing which shard owns their data. Docker Compose and Kubernetes deployment configurations will follow.

Supported assistance types are food, water, medicine, transportation and shelter. Locations are named districts, trimmed and converted to lowercase. Matching requires the same type and district, and selects the available resource with the lexicographically smallest ID. This gives deterministic results; it does not calculate geographic distance or emergency priority.

Requests progress from `pending` to `matched` to `completed`. Pending or matched requests may be cancelled. A matching operation must assign a resource before entering `matched`. Selection alone does not reserve a resource: the service layer must atomically coordinate assignment and availability.

## Checks

Install Go 1.25 or newer, then run:

```sh
go test ./...
go test -race ./...
go vet ./...
```

The race detector requires a supported C compiler. CI runs formatting, vet and race-enabled tests on Linux.

## Layout

- `internal/domain`: request/resource models and matching rules.
- `internal/storage`: durable versioned JSON snapshots and shard hashing.
- `internal/coordinator`: routing, recovery journal, replication and node leases.
- `internal/node`: authenticated storage-node HTTP and heartbeat client.
- `internal/integration`: six-node failure and recovery tests.
- `cmd/coordinator`, `cmd/storage-node`: runnable storage services.
- `config/nodes.local.json`: six-node local topology.
- `.github/workflows`: continuous integration.

## Storage cluster

Build the services:

```sh
go build -o bin/coordinator ./cmd/coordinator
go build -o bin/storage-node ./cmd/storage-node
```

Set the same nonempty `INTERNAL_TOKEN` for every process. Start the coordinator with `-nodes config/nodes.local.json`, then start six nodes with the IDs, ports and shard indices in that file. Each process must have its own `-data` path. See [coordinator protocol and failure semantics](docs/coordinator.md) and [node startup instructions](docs/node.md).

Existing-record reads and writes can continue with one synchronized copy per shard. A coordinator outage pauses service until its journal is reopened and replicas register again. This is a single-coordinator demonstration, not a consensus database.

This project is a demonstration system, not an operational emergency dispatch service.
