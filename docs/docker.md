# Full Docker Compose deployment

Run commands from the repository root with Docker Engine and Docker Compose v2 installed. Reserve about 8 GiB for Docker when also running the Kubernetes demonstration. The Compose project is `response-matching`; it is separate from the older database-only `emergency-postgres` project.

## Credentials and launch

Copy `.env.example` to the ignored `.env` and fill all three required values with independent random secrets. For example, `python -c "import secrets; print(secrets.token_hex(32))"` generates one value; repeat for each variable. Do not commit `.env`, paste rendered configuration into tickets, or reuse validation/CI placeholders.

`POSTGRES_PASSWORD` and `RABBITMQ_PASSWORD` must contain only ASCII letters and digits (hex is recommended). These deployment templates interpolate credentials into URL DSNs without percent encoding. Punctuation, whitespace and URL delimiters are therefore outside this template's supported password contract. Use a long random alphanumeric `INTERNAL_TOKEN` too. An empty required value makes Compose fail before deployment.

```sh
docker compose -f compose.yml config --quiet
docker compose -f compose.yml build
docker compose -f compose.yml up -d --wait --wait-timeout 300
docker compose -f compose.yml ps
python scripts/smoke.py --url http://127.0.0.1:8081
```

Open [the dashboard](http://127.0.0.1:8081). Set `APP_PORT` in `.env` to change the local port, then use that port for the browser and smoke check. Run `docker compose -f compose.yml port request-api 8080` to see the actual host port. Compose publishes the API on `127.0.0.1` and uses its internal network for PostgreSQL, RabbitMQ, the coordinator, and the storage nodes. Container listeners bind `0.0.0.0` so peer services can reach them.

## Images and topology

The multi-stage Dockerfile compiles Go 1.25 binaries with CGO disabled and builds the Vite dashboard with Node 22 using `npm ci`. The four targets are `coordinator`, `storage-node`, `request-api`, and `matching-worker`, tagged `emergency-response-matching/<target>:local`. Each Alpine runtime runs as UID/GID `10001:10001`. The API image includes `/app/web/dist`; no host frontend build or bind mount is required. `.dockerignore` excludes local credentials, dependencies, build output and Git metadata.

Seven independent PostgreSQL 17 instances hold the coordinator journal and six shard replicas, each with its own named volume. `config/nodes.container.json` lists the six service DNS names on port 9100. RabbitMQ 4 uses a persistent volume and stable `rabbitmq` hostname so broker identity survives recreation. Database and broker credentials initialize new volumes; changing `.env` alone does not rotate credentials already stored in existing volumes.

Compose waits for database health before starting each application. Coordinator and node health checks use `/healthz` (process availability), allowing nodes to register without a circular dependency on cluster readiness. The API uses `/readyz` after nodes start. The worker starts after API and broker health, reconnects through application logic, and has no HTTP health endpoint. A running worker or successful `up --wait` does not prove asynchronous matching: run the public smoke check. Docker documents the dependency behavior in [Control startup and shutdown order](https://docs.docker.com/compose/how-tos/startup-order/).

Each PostgreSQL container has a 256 MiB memory limit, application containers 128 MiB, and RabbitMQ 512 MiB. CPU limits and RabbitMQ scheduler caps keep the local demonstration modest. These are development demonstration limits, not production sizing results. Never scale the coordinator or a named logical shard identity above one instance; only the API and worker are designed for multiple instances. The fixed API host port needs a proxy or a different publishing arrangement before scaling it in Compose.

## Inspect, stop and preserve data

```sh
docker compose -f compose.yml logs --tail 100 coordinator request-api matching-worker
docker compose -f compose.yml stop
docker compose -f compose.yml start
docker compose -f compose.yml down
```

`stop`, container recreation, and ordinary `down` preserve all eight named volumes. `docker compose -f compose.yml down --volumes` permanently deletes this project's database and broker data; use it only for an intentional clean reset after saving anything needed. Do not remove unrelated containers or volumes. Secrets are environment variables visible to Docker administrators; use a managed secret system and authenticated TLS networking for deployment beyond this local demonstration.

## Geographic matching radius

The API and worker share a default 50 km straight-line geographic limit. No external distance API or credential is required. See [geographic matching](geographic-matching.md) to adjust the radius and run `scripts/geographic-smoke.py`. Keep API and worker configuration consistent.
