# Emergency Response Matching

A Go system connecting emergency requests with available resources by type and location.

## Development status

Domain models, validation, request lifecycle rules and deterministic resource selection are implemented. The storage cluster provides a coordinator, three replicated shards, heartbeat leases, failover and restart synchronization. The public request/resource API, atomic matching, RabbitMQ outbox workflow and React dashboard run in Docker Compose and Kubernetes. Full deployment failure and scaling demonstrations are the next stage.

## Architecture

The runtime has a request API with a React browser frontend, a matching worker, RabbitMQ, a storage coordinator, and three storage shards with one replica each. Clients use REST/JSON without knowing which shard owns their data. Each storage process has its own persistent PostgreSQL database. Complete deployment configurations include seven database instances and a durable message broker.

## Run the complete application

Follow the [Docker Compose guide](docs/docker.md) to build and start the application locally, or the [Kubernetes guide](docs/kubernetes.md) to deploy it to a cluster. Both guides include private credential setup, persistent storage, health checks and an end-to-end smoke command. Only the API/frontend needs to be reachable by clients.

Supported assistance types are food, water, medicine, transportation and shelter. New requests and resources require latitude and longitude in both the dashboard and the API. Matching selects the nearest available resource of the same type within a configurable straight-line geographic radius (50 km by default), with resource ID as a deterministic tie-breaker. Distance is calculated locally without an external service. The optional location name is a display label. See the [geographic matching guide](docs/geographic-matching.md) for configuration and accuracy details.

Requests progress from `pending` to `matched` to `completed`. Pending or matched requests may be cancelled. A matching operation must assign a resource before entering `matched`. Selection alone does not reserve a resource: the service layer must atomically coordinate assignment and availability.

## Checks

Install Go 1.25 or newer, PostgreSQL 17, RabbitMQ 4 and Node.js 22.12+. Set `TEST_RABBITMQ_URL` for real broker tests. Set `TEST_DATABASE_URL` to a disposable test database whose user can create schemas, then run:

```sh
go test ./...
go test -race ./...
go vet ./...
```

The race detector requires a supported C compiler. CI provisions PostgreSQL 17 and RabbitMQ 4 and runs formatting, vet and race-enabled tests on Linux. Database tests explicitly skip without `TEST_DATABASE_URL`; a full verification requires it. Tests isolate each store in a fresh schema, then drop it during cleanup. Sharing one test server does not provide independent failure domains.

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

## Application

The dashboard includes a read-only **Shard details** development tab showing live coordinator-reported node readiness and heartbeats. Follow the [demonstration guide](docs/demonstration.md) to verify shard distribution, storage failover, recovery, concurrent matching and Kubernetes scaling.

See the [API and React dashboard guide](docs/application.md) and [matching worker guide](docs/messaging.md) for setup, endpoints, delivery guarantees and capacity limits. The API serves compiled frontend assets from `web/dist`; build them with `npm ci && npm run build` inside `web`. The worker requires `RABBITMQ_URL`.
