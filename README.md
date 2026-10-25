# Emergency Response Matching

A distributed academic application for matching requests for food, water, medicine, transport and shelter with available nearby resources.

## Architecture

- React and TypeScript web client.
- Three Java 21 / Spring Boot business services: Request, Resource and Matching.
- One storage coordinator with UUID-based routing across three PostgreSQL shards.
- RabbitMQ asynchronous matching with a transactional outbox, guarded allocations and recovery of interrupted work.
- Kubernetes deployment with one primary and one standby per shard, managed by CloudNativePG.

Matching selects an available resource of the same type within 50 km using geographic distance, with nearest first and UUID tie-breaking. Each resource has capacity one. Priority is stored as metadata and does not change matching order.

## Launch with Docker Compose

Install Docker Desktop with Linux containers, then run from the project directory:

```powershell
Copy-Item .env.example .env
```

Edit `.env` and replace all placeholder passwords and the internal token. Then run:

```powershell
docker compose --env-file .env -f infra/compose/compose.yaml up -d --build
```

Open [http://localhost:8088](http://localhost:8088). Initial image downloads and Java builds take several minutes. To inspect startup:

```powershell
docker compose --env-file .env -f infra/compose/compose.yaml ps
docker compose --env-file .env -f infra/compose/compose.yaml logs --tail 100
```

Compose provides three independent primary databases. Use the [Kubernetes runbook](docs/runbook.md) for the replicated deployment required for the full project.

## Use the website

1. Register an available resource with its type and coordinates.
2. Create a request of the same type nearby.
3. Matching runs asynchronously; the website refreshes every five seconds.
4. Start a matched response, then mark it completed.
5. For a request with no suitable resource, register supply and use Retry matching.

Completed resources become unavailable and may be explicitly replenished. Pending requests can be cancelled. Use synthetic data for demonstrations.

## Build and test

Java 21 is required for backend builds. Node.js 22 is used for frontend builds and verification scripts.

```powershell
./scripts/build.ps1
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run build
node scripts/smoke.mjs
node scripts/load-test.mjs
```

The last two commands require the running application and create synthetic records. Additional concurrency and failure tests are described in the runbook. GitHub Actions builds the backend/frontend and runs Compose integration tests.

## Documentation

- [Architecture and lifecycle](docs/architecture.md)
- [Public API](docs/api/contract.md) and [OpenAPI specification](docs/api/openapi.yaml)
- [Deployment and operations](docs/runbook.md)
- [Security and operational limits](docs/security-and-limits.md)

This is a localhost academic demonstration, not a production emergency service. Replication is asynchronous; failover can temporarily return errors. One coordinator, one broker and a single-node local cluster remain availability limitations. Public APIs have no user authentication.
