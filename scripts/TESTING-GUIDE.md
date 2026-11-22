# Sahara testing and viva walkthrough

Run the numbered scripts **one at a time** from the project repository. Steps 01–10 cover Docker Compose. Steps 11–15 deploy the same application to Kubernetes and cover Pod recovery and scaling. The test order follows sections 10 and 11 of the project brief and includes the Kubernetes deployment required by section 9.2.

## Before you start

1. Open Docker Desktop, use Linux containers, and wait for the engine to start.
2. Open **PowerShell** in the `emergency-response-matching` folder. From Command Prompt, you can enter `powershell` to switch shells while staying in the same folder.
3. Verify Git, Docker Compose v2, Python 3, Go 1.25 or newer, Node.js 22, npm, kubectl, and kind. Python launched as `py -3` also works with the scripts. Kubernetes runs through kind using Docker Desktop's engine. Use the project's local demo dataset for the smoke tests. The tests create and retain sample records.
4. Leave Compose on `http://127.0.0.1:18080` on this machine. The scripts read the running Compose port mapping, so they also work if the repository is configured for a different local port. Kubernetes uses `http://127.0.0.1:18082`.

```powershell
git status --short --branch
docker info --format '{{.OSType}}'
docker compose version
python --version
go version
node --version
npm.cmd --version
kind version
kubectl version --client
```

## 01–03. Setup and code checks

```powershell
.\scripts\01-setup-docker.ps1
.\scripts\02-test-go.ps1
.\scripts\03-test-frontend.ps1
```

| Step | What happens | What to show |
| --- | --- | --- |
| 01 | Generates local credentials if `.env` is missing, builds the four application images, starts the Compose stack, waits for health checks, and calls `/readyz` on the published port. | Compose service status and the printed dashboard URL. PostgreSQL, the coordinator, the shard nodes, RabbitMQ, and the API and worker run in separate containers. |
| 02 | Runs `go vet` and all Go test packages with verbose output and caching disabled. | The package results. A service-dependent SKIP line means that test did not run in this invocation. |
| 03 | Installs frontend dependencies if needed, runs Vitest, and builds the production frontend. Add `-Install` to reinstall the locked dependencies. | Vitest's passed count plus a successful TypeScript and Vite build. |

### What the Go tests do

Go runs the `Test…` functions in files ending in `_test.go` and reports results per package. `go vet` is a separate static check for suspicious Go code. The command in step 02 uses `./...` to run the test packages throughout the repository. `-count=1` disables reuse of cached test results. `-v` makes individual tests and skips visible.

| Go code area | Examples of what the tests assert |
| --- | --- |
| `internal/domain` and `internal/config` | Coordinate and model validation, allowed request transitions, and matching radius configuration. |
| `internal/application` | API response and error behavior, nearest eligible resource selection, stable tie-breaking, radius boundaries, concurrent reservations, and shard diagnostics. |
| `internal/coordinator`, `internal/node`, `internal/storage` and `internal/integration` | Deterministic shard selection, batched writes, heartbeat and recovery behavior, version handling, and storage replication across components. |
| `internal/messaging` | Outbox publication, broker acknowledgments and retry behavior, and worker handling of successfully processed and invalid jobs. |

Some PostgreSQL and RabbitMQ integration tests require `TEST_DATABASE_URL` or `TEST_RABBITMQ_URL`. The repository's CI workflow supplies dedicated test services. The Compose and Kubernetes tests below exercise the actual deployed services as well.

## 04–06. Requests, geographic matching and RabbitMQ

```powershell
.\scripts\04-test-request-lifecycle.ps1
.\scripts\05-test-geographic-matching.ps1
.\scripts\06-test-rabbitmq-concurrency.ps1
```

| Step | What to demonstrate and explain |
| --- | --- |
| 04 | The deployed dashboard assets load. The API registers a resource, creates and retrieves a request, verifies the assignment, completes the request and verifies the resource's new status. It also exercises cancellation, explicit availability changes, API errors and idempotent matching. Show the IDs and final status printed by the test. |
| 05 | Two locations with different labels still match when their coordinates are close. The test checks the assigned resource ID and saved straight-line distance. An out-of-range resource is rejected. Show the distance and configured radius. |
| 06 | RabbitMQ lists an active consumer on `matching.requests`. The test submits 12 requests using up to six concurrent threads and waits for asynchronous matches before calling any manual matching endpoint. Every request must get a different resource. Show the queue row and the two sets of IDs. |

For a short visual demonstration before step 04, open **New request** in the Compose dashboard and create a test Shelter request with a public-location pin. Retrieve it from the request list. Register an available Shelter resource nearby with **Add resource**, then show the matched request and distance. Complete it and show the resource status. Use fresh demo labels and avoid a spot with other available Shelter resources.

### What a smoke test does

A smoke test runs a small, representative workflow against the **running application**. Here, `scripts/smoke.py` checks `/readyz`, downloads the compiled dashboard and assets, then uses the real HTTP API to exercise the request, resource and worker paths. A green Go package result establishes that its assertions passed. A green smoke test establishes that the deployed components work together for that workflow. The numbered PowerShell scripts call these Python checks and report a failed command if an assertion fails.

## 07–10. Sharding, storage failure and recovery

Open **Shard details** in the Compose dashboard. Run these scripts sequentially. A switch named `-Pause` is available on steps 08–10. The script waits for Enter while the outage is visible so you can refresh Shard details, then it restores the stopped services. Steps 08 and 09 each create their own fixture and can be demonstrated independently.

```powershell
.\scripts\07-test-shard-distribution.ps1
.\scripts\08-test-storage-failover.ps1 -Pause
.\scripts\09-test-storage-recovery.ps1 -Pause
.\scripts\10-test-shard-outage.ps1 -Pause
```

| Step | Operation | What proves it passed |
| --- | --- | --- |
| 07 | Generate requests until all three shards are represented. Calculate FNV-1a over each request key modulo three and inspect all six storage processes. | Each tested record appears on the primary and replica of exactly its assigned shard. The script prints the request-to-shard mapping. |
| 08 | Stop the shard 0 primary, wait for its heartbeat lease to expire, then read and update a request on that shard. Restore the primary. | The API stays usable for the test request and the replica holds a newer version from the write made during the outage. |
| 09 | Make a fresh write while the primary is down. Restart the primary and compare the full stored record on both copies with the version written during the outage. | Primary and replica agree on the newer version. The API still returns the updated status. |
| 10 | Optional extended failure check. Stop both copies of shard 0, then bring both back. | A known shard 0 record returns HTTP 503 while known shard 1 and 2 records return HTTP 200. Both shard 0 copies retain the original committed record after recovery. |

If a terminal was closed mid-test, restore the Compose services and wait for health before continuing.

```powershell
docker compose up -d --no-build --wait --wait-timeout 300
```

## 11–13. Deploy to Kubernetes and exercise the baseline

Run setup in your main window. It creates or reuses the dedicated `sahara-demo` kind cluster, loads the images built in step 01, applies the manifests in `response-matching`, and waits for the controllers and eight Bound claims. Run this before the presentation so the first image pulls and Pod start-up can finish.

```powershell
.\scripts\11-setup-kubernetes.ps1
```

In a **second PowerShell window** in the same repository, start the port-forward and leave the window open.

```powershell
.\scripts\12-forward-kubernetes.ps1
```

Open [the Kubernetes dashboard](http://127.0.0.1:18082). Back in your main window, verify the Deployment, Pods and Services listings and run the deployed smoke test.

```powershell
.\scripts\13-test-kubernetes-baseline.ps1
```

Show the two Deployments, the coordinator and storage StatefulSets, the internal Services, and the completed matching workflow. The browser reaches the API through the port-forward. The request, storage, and broker work still takes place inside the Kubernetes deployment.

If Compose and kind together run short of memory, use `docker compose stop` after finishing step 10. Use `docker compose start --wait --wait-timeout 180` when you next need Compose. Stopping the stack preserves its volumes.

## 14–15. Kubernetes Pod replacement and scaling

Keep the port-forward running and use the main window.

```powershell
.\scripts\14-test-kubernetes-pod-recovery.ps1
.\scripts\15-test-kubernetes-scaling.ps1
```

- **14 checks automatic recovery.** The script deletes the single `matching-worker` Pod and waits for a new one. Show the old and new Pod UIDs, Ready status and successful fresh geographic match. The new match proves that the replacement processes work.
- **15 checks scaling.** The script grows the API and worker Deployments from one to two replicas each. Show the readiness samples during rollout, two Ready API endpoints, both Deployments at 2/2, and the asynchronous matching test afterward. The port-forward uses one API Pod, while the EndpointSlice shows that the Service can route to both. The script restores both Deployments to one replica afterward.

If the port-forward closes when its selected Pod is removed, rerun step 12. At the end, press Ctrl+C in the forwarding window. Keep the kind cluster and database volumes for the next rehearsal.

## Troubleshooting during rehearsal

- If Docker reports the API healthy but Windows cannot open the Compose URL, check `docker compose port request-api 8080`. If the published port is correct and the Windows connection still times out, save WSL work, quit Docker Desktop, run `wsl --shutdown`, reopen Docker Desktop, and rerun step 01. This stops all running WSL distributions.
- For Kubernetes startup issues, check `kubectl --context kind-sahara-demo -n response-matching get pods,pvc` and `kubectl --context kind-sahara-demo -n response-matching get events --sort-by=.lastTimestamp`. An `ImagePullBackOff` on a project image usually means the image tag or the kind load step needs checking. Pending claims point to storage provisioning.
- Smoke fixtures use sample geographic locations and assert the expected resource IDs. If other available resources previously created at those locations interfere with the test, run it against a dedicated rehearsal dataset or resolve those old demo fixtures.

The numbered scripts are the Windows entry points. `test-common.ps1` checks native exit codes and finds deployment addresses. `smoke.py`, `geographic-smoke.py`, and `failure-smoke.py` hold the deployed test assertions. The repository's existing `kubernetes-smoke.sh` continues to run the Kubernetes CI checks.
