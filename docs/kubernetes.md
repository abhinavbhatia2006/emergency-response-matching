# Kubernetes deployment (Day 4)

This local deployment uses namespace `response-matching` on context `kind-emergency`. The unrelated namespace `emergency` is outside this deployment. Run commands from the repository root. Prerequisites: Docker, kubectl, Python 3, a running kind cluster, and a default dynamic StorageClass (`kubectl --context kind-emergency get storageclass`). The eight claims request 8 GiB of persistent storage in total.

## Build and load images

Build the four application images as described in [Docker setup](docker.md). Each Kubernetes image uses the tag `emergency-response-matching/<target>:local` and `imagePullPolicy: IfNotPresent`. Images must be loaded on every schedulable kind node before starting the workloads.

PowerShell, with kind installed:

```powershell
$targets = 'coordinator', 'storage-node', 'request-api', 'matching-worker'
foreach ($target in $targets) {
  docker build --target $target -t "emergency-response-matching/${target}:local" .
  kind load docker-image "emergency-response-matching/${target}:local" --name emergency
}
```

Bash, with kind installed:

```bash
for target in coordinator storage-node request-api matching-worker; do
  docker build --target "$target" -t "emergency-response-matching/$target:local" .
  kind load docker-image "emergency-response-matching/$target:local" --name emergency
done
```

If the kind CLI is absent, import into the existing node's containerd. First inspect `docker ps` and `kubectl --context kind-emergency get nodes` and use the actual node container name. For the single-node cluster named `emergency`, this is normally `emergency-control-plane`. These commands do not create a cluster:

```powershell
$nodeContainer = 'emergency-control-plane'
$archive = Join-Path $env:TEMP 'response-matching-images.tar'
docker save -o $archive emergency-response-matching/coordinator:local emergency-response-matching/storage-node:local emergency-response-matching/request-api:local emergency-response-matching/matching-worker:local
docker cp $archive "${nodeContainer}:/tmp/response-matching-images.tar"
docker exec $nodeContainer ctr --namespace k8s.io images import /tmp/response-matching-images.tar
```

Unix equivalent:

```bash
node_container=emergency-control-plane
docker save -o /tmp/response-matching-images.tar emergency-response-matching/coordinator:local emergency-response-matching/storage-node:local emergency-response-matching/request-api:local emergency-response-matching/matching-worker:local
docker cp /tmp/response-matching-images.tar "$node_container:/tmp/response-matching-images.tar"
docker exec "$node_container" ctr --namespace k8s.io images import /tmp/response-matching-images.tar
```

Repeat the import for each node in a multi-node cluster. `postgres:17-alpine` and `rabbitmq:4-alpine` are pulled from their official images; an offline cluster also needs those images loaded.

## Credentials and rollout

Use the ignored repository `.env` created for Docker, containing `POSTGRES_PASSWORD`, `RABBITMQ_PASSWORD`, and `INTERNAL_TOKEN`. Values must be unquoted, nonempty, URL-safe hex/alphanumeric strings: Kubernetes expands passwords into connection URLs without URL encoding. Never commit this file or a rendered Secret. To create fresh credentials only when `.env` does not exist, this Python command works in both shells (exclusive creation prevents overwriting existing credentials):

```text
python -c "import secrets; from pathlib import Path; p=Path('.env'); f=p.open('x'); f.write(''.join(k+'='+secrets.token_hex(32)+'\n' for k in ['POSTGRES_PASSWORD','RABBITMQ_PASSWORD','INTERNAL_TOKEN'])); f.close()"
```

The following commands work in PowerShell and Bash:

```text
kubectl --context kind-emergency apply -f deploy/kubernetes/namespace.yaml
kubectl --context kind-emergency -n response-matching create secret generic response-secrets --from-env-file=.env
kubectl kustomize deploy/kubernetes
kubectl --context kind-emergency apply --dry-run=server -k deploy/kubernetes
kubectl --context kind-emergency apply -k deploy/kubernetes
kubectl --context kind-emergency -n response-matching get pods,pvc,services
```

The Secret creation command is for the first installation. For subsequent deployments, reuse the existing Secret and PVCs. Changing initialization credentials in the Secret does not change credentials stored in existing PostgreSQL or RabbitMQ data directories; credential rotation requires coordinated database/broker administration and application restarts.

Wait for every workload. PowerShell:

```powershell
kubectl --context kind-emergency -n response-matching get statefulset,deployment -o name | ForEach-Object {
  kubectl --context kind-emergency -n response-matching rollout status $_ --timeout=300s
}
```

Bash:

```bash
for workload in $(kubectl --context kind-emergency -n response-matching get statefulset,deployment -o name); do
  kubectl --context kind-emergency -n response-matching rollout status "$workload" --timeout=300s
done
```

Startup can temporarily restart application containers while databases initialize. If rollout fails, inspect `kubectl --context kind-emergency -n response-matching get events --sort-by=.lastTimestamp`, `describe pod <pod>`, and `logs <pod> --previous`. A Pending PVC usually indicates missing default storage provisioning; ImagePullBackOff for a local application image usually means it was not loaded into that node.

## Open and verify

All Services are internal ClusterIP services (database and broker Services are headless). In a dedicated terminal:

```text
kubectl --context kind-emergency -n response-matching port-forward --address 127.0.0.1 service/request-api 18080:8080
```

Open [the application](http://127.0.0.1:18080). In another terminal run:

```text
python scripts/smoke.py --url http://127.0.0.1:18080 --timeout 180
```

This checks the compiled UI, public create/read/status flow, and asynchronous matching, leaving uniquely named demonstration records for inspection. A Running worker alone does not prove useful work.

## Workload design and lifecycle

- Seven PostgreSQL StatefulSets each use a dedicated 1 GiB ReadWriteOnce PVC: one coordinator journal plus six shard stores. RabbitMQ has its own 1 GiB PVC and stable `rabbitmq-0` hostname/node name. No shared database or queue data volume is used.
- The coordinator and six logical storage nodes each run as a single-replica StatefulSet with a stable ordinal-zero Pod identity. Unlike Deployment Recreate (which governs upgrades only), StatefulSet replacement waits for ordinary Pod termination before reusing the identity. Existing ClusterIP Services provide application discovery; these processes do not depend on individual Pod DNS records. There are 15 StatefulSets in total (eight persistent infrastructure stores and seven application identities) and two Deployments for the stateless API and matching worker. Never scale any coordinator, storage-node, database, or broker StatefulSet above one. Only API/worker Deployments are independently scalable; scaling/failure demonstrations belong to Day 5.
- Do not force-delete singleton Pods, strip their finalizers, force-replace their controllers, or create another controller for the same identity. During an uncertain node failure, keep the replacement blocked until the old process is confirmed stopped. This application has no distributed fencing mechanism: StatefulSet lifecycle ordering protects normal replacement, not an operator bypass or concurrent externally started process. See Kubernetes [force-deletion semantics](https://kubernetes.io/docs/tasks/run-application/force-delete-stateful-set-pod/).
- The topology is a generated ConfigMap. Kustomize gives it a content hash and updates the coordinator volume reference on changes. Every storage identity has a stable Service DNS name and listens on port 9100.
- Application containers run as UID/GID 10001 with a read-only root filesystem, no Linux capabilities, and no mounted service-account token. Official database/broker images retain their entrypoint initialization behavior.
- API readiness uses `/readyz`, while liveness uses `/healthz`. Coordinator and storage-node probes use process `/healthz`, allowing heartbeat/bootstrap traffic before shard readiness; dependency readiness here would create a startup cycle. Startup probes allow up to five minutes. Worker has no HTTP health endpoint; Kubernetes restarts a terminated process, and the asynchronous smoke check verifies queue processing.
- Resource requests are 25m CPU per container, 32 MiB for each Go process, 64 MiB per database, and 128 MiB for RabbitMQ. Limits are 128 MiB per Go process, 256 MiB per database, and 512 MiB for RabbitMQ, with 500m CPU per container except RabbitMQ at one CPU. These are local-demo settings, not production sizing or a high-availability database deployment.
- Ordered environment entries resolve Secret passwords before the `DATABASE_URL`/`RABBITMQ_URL` that reference them. This follows Kubernetes [dependent environment variable semantics](https://kubernetes.io/docs/tasks/inject-data-application/define-interdependent-environment-variables/). Persistent identities and volumes follow [StatefulSet semantics](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/).

After rebuilding an image under the same `:local` tag, load it again, then restart the corresponding Deployment or StatefulSet; Kubernetes does not detect a change to an image with an unchanged tag. Ordinary Pod recreation preserves data. StatefulSet deletion normally retains its claims, but **deleting namespace `response-matching` (including `kubectl delete -k deploy/kubernetes`, because this bundle includes the namespace) deletes its PVCs and may permanently delete their backing data under a Delete reclaim policy**. Do not use namespace/bundle deletion to restart this stack. Back up any data you need before explicitly deleting claims or the kind cluster.

## Geographic matching radius

The API and worker share a default 50 km straight-line geographic limit. No external distance API or credential is required. See [geographic matching](geographic-matching.md) to adjust the radius and run `scripts/geographic-smoke.py`. Keep API and worker configuration consistent.
