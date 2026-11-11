# Emergency Response Matching

A Go system connecting emergency requests with available resources by type and location.

## Development status

The first increment provides domain models, input validation, request lifecycle rules and deterministic resource selection. The distributed services and deployment runtime are under development.

## Architecture

The planned runtime has a request API, a matching worker, RabbitMQ, a storage coordinator, and three storage shards with one replica each. Clients use REST/JSON without knowing which shard owns their data. Docker Compose and Kubernetes deployment configurations will follow.

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
- `.github/workflows`: continuous integration.

This project is a demonstration system, not an operational emergency dispatch service.
