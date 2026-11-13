# Emergency Response Matching

A Go system connecting emergency requests with available resources by type and location.

## Development status

Domain models, validation, request lifecycle rules and deterministic resource selection are implemented. The storage cluster now provides a coordinator, three replicated shards, heartbeat leases, failover and restart synchronization. The public application API, RabbitMQ workflow, browser frontend and container deployment are under development.

## Architecture

The runtime design has a request API with a planned React browser frontend, a matching worker, RabbitMQ, a storage coordinator, and three storage shards with one replica each. Clients use REST/JSON without knowing which shard owns their data. React application work and Docker Compose/Kubernetes application deployment configurations will follow; the current database Compose configuration starts PostgreSQL only.

Supported assistance types are food, water, medicine, transportation and shelter. Locations are named districts, trimmed and converted to lowercase. Matching requires the same type and district, and selects the available resource with the lexicographically smallest ID. This gives deterministic results; it does not calculate geographic distance or emergency priority.

Requests progress from `pending` to `matched` to `completed`. Pending or matched requests may be cancelled. A matching operation must assign a resource before entering `matched`. Selection alone does not reserve a resource: the service layer must atomically coordinate assignment and availability.

## Checks

Install Go 1.25 or newer and PostgreSQL 17. Set `TEST_DATABASE_URL` to a disposable test database whose user can create schemas, then run:

```sh
go test ./...
go test -race ./...
go vet ./...
```

The race detector requires a supported C compiler. CI provisions PostgreSQL 17 and runs formatting, vet and race-enabled tests on Linux. Database tests explicitly skip without `TEST_DATABASE_URL`; a full verification requires it. Tests isolate each store in a fresh schema, then drop it during cleanup. Sharing one test server does not provide independent failure domains.

## Layout

- `internal/domain`: request/resource models and matching rules.
- `internal/storage`: PostgreSQL-backed versioned records, transactional replacement and shard hashing.
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

Set the same nonempty `INTERNAL_TOKEN` for every process. Start the coordinator with `-nodes config/nodes.local.json`, then start six nodes with the IDs, ports and shard indices in that file. Each process requires its own `DATABASE_URL`; the `-data` flag is removed. Start the seven PostgreSQL instances using the [database setup guide](docs/storage.md). See [coordinator protocol and failure semantics](docs/coordinator.md) and [node startup instructions](docs/node.md).

Existing-record reads and writes can continue with one synchronized copy per shard. A coordinator outage pauses service until its journal is reopened and replicas register again. This is a single-coordinator demonstration, not a consensus database.

This project is a demonstration system, not an operational emergency dispatch service.
