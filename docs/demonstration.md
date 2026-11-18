# Demonstrating the system

Use a local demonstration deployment. The checks create uniquely labelled records and preserve them for inspection. They do not delete persistent volumes.

## Normal operation and concurrency

Start the stack using [Docker instructions](docker.md) or [Kubernetes instructions](kubernetes.md). Run:

```text
python scripts/smoke.py --url http://127.0.0.1:18081 --count 12
python scripts/geographic-smoke.py --url http://127.0.0.1:18081
```

The first check creates resources and simultaneous requests, waits for RabbitMQ-driven assignments, verifies every assigned resource is distinct, and exercises retrieval, completion, cancellation, availability and error responses. The second verifies coordinates, cross-district geographic selection, persisted distance and exclusion beyond the configured radius.

## Storage distribution, failure and recovery

From the repository root, with the Compose environment file available:

```text
python scripts/failure-smoke.py --env-file .env --url http://127.0.0.1:18081
```

The script creates requests until all three FNV-1a partitions are represented. It reads each node through its authenticated internal endpoint and checks that each request exists on exactly its shard's two copies. It then stops shard-0-primary, waits for the coordinator's heartbeat lease to expire, retrieves a request through the replica and updates it during the outage. A finally block restarts the primary. Recovery succeeds only when both copies agree on the newer version and the client reads the updated status. Internal credentials remain inside the container.

The **Shard details** activity tab is a read-only development diagnostic. It displays the coordinator's last observed heartbeat and readiness for each configured node. Each shard also shows one count per saved request and resource, using the coordinator's recovery journal. Counts exclude deleted records and outbox jobs, and include records in every request or resource status. Refresh the tab during the demonstration. Readiness is lease-based and may lag a failure until detection. Primary and replica names identify the configured copies. Use the failure smoke script to compare actual contents across copies. The localhost-oriented client API has no end-user authentication and needs access controls before wider exposure.

## Kubernetes scaling and automatic replacement

Only scale the stateless API and worker Deployments. Keep every storage, database, broker and coordinator StatefulSet at one replica.

```text
kubectl --context kind-emergency -n response-matching scale deployment/request-api deployment/matching-worker --replicas=2
kubectl --context kind-emergency -n response-matching rollout status deployment/request-api
kubectl --context kind-emergency -n response-matching rollout status deployment/matching-worker
kubectl --context kind-emergency -n response-matching get pods -l app=matching-worker
```

With an API port-forward active, run the concurrent smoke above against that forwarded URL. Record the worker Pod names and UIDs. Delete one worker using ordinary `kubectl delete pod <worker-pod-name>` in the same context and namespace, then wait for the worker Deployment rollout. The replacement must have a new UID and become ready. Run the geographic smoke again to prove useful processing after replacement. Do not use force deletion.

Restore the baseline after testing:

```text
kubectl --context kind-emergency -n response-matching scale deployment/request-api deployment/matching-worker --replicas=1
```

These demonstrations use separate persistent databases but share a single Docker host. They demonstrate application-level replication and normal Kubernetes replacement, not multi-host disaster tolerance or coordinator consensus. See the deployment documentation for singleton and volume lifecycle constraints.
