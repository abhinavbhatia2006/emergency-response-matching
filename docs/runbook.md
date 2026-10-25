# Operations runbook

## Local stack

Copy `.env.example` to `.env` and replace all placeholder values. The `.env` file is ignored by Git.

```powershell
docker compose --env-file .env -f infra/compose/compose.yaml up -d --build
docker compose --env-file .env -f infra/compose/compose.yaml ps
node scripts/smoke.mjs
node scripts/load-test.mjs
```

Open http://localhost:8088. RabbitMQ management is on localhost:15672 with the credentials in `.env`. PostgreSQL is bound only to localhost on ports 5433, 5434, 5435. The coordinator and application ports remain inside the Compose network.

For a memory-constrained development machine, build Java once on the host to reduce peak memory, then package the verified JARs:

```powershell
./scripts/build.ps1
docker compose --env-file .env -f infra/compose/compose.yaml -f infra/compose/local-build.yaml -f infra/compose/testing.yaml up -d --build
node scripts/wait-ready.mjs
node scripts/smoke.mjs
node scripts/integration.mjs
node scripts/load-test.mjs
node scripts/broker-recovery.mjs
node scripts/backup-restore.mjs
```

The testing override exposes the token-protected coordinator on localhost:18080. Integration tests stop/restart only the project's matching consumer; the broker drill stops/restarts only its RabbitMQ. Backup checks use fresh disposable databases and preserve dumps in ignored `.local/backups`. Run these scripts sequentially against synthetic data.

Use `docker compose --env-file .env -f infra/compose/compose.yaml logs --tail 100 SERVICE` for diagnostics. Stop containers with `down` without `-v` to preserve database volumes.

## Kubernetes HA deployment

Build the images using Compose first. Install kind (a project-local executable may be available under `.tools`) and kubectl.

```powershell
kind create cluster --config infra/kubernetes/kind.yaml
$env:PATH = "$PWD\.tools;$env:PATH"
./scripts/deploy-kubernetes.ps1
kubectl --context kind-emergency -n emergency port-forward svc/frontend 8088:8080
```

Stop the Compose frontend before forwarding the same host port, or choose port 8089 and set `BASE_URL=http://localhost:8089/api` for tests. The final topology must show three CNPG clusters with two instances each. Manifest presence alone does not prove replication or recovery.

On a memory-constrained laptop, stop the entire Compose stack before Kubernetes (`docker compose --env-file .env -f infra/compose/compose.yaml stop`). This preserves its data. Run `node scripts/kubernetes-drills.mjs` only against the local demo cluster; it deliberately deletes selected database/application pods and leaves Matching scaled to three replicas.

## Failure drills

The automated Kubernetes checks, after forwarding the frontend, are:

```powershell
$env:BASE_URL = 'http://localhost:8088/api'
$env:EVIDENCE_NAME = 'smoke-kubernetes'
node scripts/smoke.mjs
$env:EVIDENCE_NAME = 'load-kubernetes'
node scripts/load-test.mjs
node scripts/kubernetes-drills.mjs
node scripts/kubernetes-read-fallback.mjs
```

The fallback script temporarily removes only shard-0's primary service endpoints, closes its application connections, verifies a labeled replica read, then restores the selector in a `finally` block. Do not interrupt this drill. The scripts default to the IPv6 loopback URL where specified; setting BASE_URL overrides that consistently.

For `node scripts/kubernetes-observability.mjs`, also run these temporary forwards in separate terminals:

```powershell
kubectl --context kind-emergency -n emergency port-forward svc/storage-coordinator 18081:8080
kubectl --context kind-emergency -n emergency port-forward svc/rabbitmq 15673:15672
```

This verifies token enforcement, metrics, heartbeat expiry, malformed-event dead-lettering and duplicate delivery. It leaves one clearly named synthetic expired heartbeat and one malformed fixture in the DLQ as evidence. Close the two diagnostic forwards when finished.

Local database manifests bound smart shutdown to 10 seconds and total shutdown/switchover to 60 seconds. These are demo settings, not production sizing. Service-link injection is disabled on application pods to prevent Kubernetes' RABBITMQ_PORT URL from overriding the numeric application port.

Run against synthetic data in the local `kind-emergency` context only. Capture before/after pod listings and logs in `docs/evidence/`.

1. Pause Matching with `kubectl --context kind-emergency -n emergency scale deployment/matching-service --replicas=0`. Submit requests, observe pending states and RabbitMQ backlog. Restore to three replicas and verify eventual outcomes.
2. Delete one Matching pod selected with `-l app=matching-service`. Verify Kubernetes replaces it and requests settle without duplicate allocation.
3. Identify a shard primary using `kubectl --context kind-emergency -n emergency get cluster shard-0 -o jsonpath='{.status.currentPrimary}'`. Delete only that pod. Observe standby promotion, the `shard-0-rw` endpoint, and restored writes. Record any transient errors and data loss.
4. Delete a standby pod. Write new records while it recovers; query the recovered member to confirm catch-up. Read `pg_is_in_recovery()` to verify its role.
5. Stop the coordinator temporarily and confirm API errors are visible. Restore it, then verify persisted records and interrupted matches.

Never perform a factory reset or delete database volumes as a recovery shortcut. Asynchronous replication may lose recent transactions; one broker and one coordinator remain availability limitations.

## Backup and restore

Replication is not a backup. For local development, use `pg_dump` inside each shard container and preserve output outside the database volume. Restore into a separate test database with `psql`, then compare schema and record counts. Do not overwrite a running shard as a restore test. Cross-shard dumps require a quiescent application or a coordinated snapshot strategy; independent dumps may capture inconsistent saga progress.
