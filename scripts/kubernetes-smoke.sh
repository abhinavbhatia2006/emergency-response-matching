#!/usr/bin/env bash
# Run in an isolated kind cluster using the application images built by Compose.
set -euo pipefail

cluster=response-matching-ci
kubeconfig=$(mktemp)
api_port=8082
api="http://127.0.0.1:$api_port"
kube=(kubectl --kubeconfig "$kubeconfig" --context "kind-$cluster")
project=("${kube[@]}" -n response-matching)
created=false
forward=

cleanup() {
  status=$?
  if [[ -n "$forward" ]]; then
    kill "$forward" 2>/dev/null || true
  fi
  if [[ "$created" == true ]]; then
    if (( status != 0 )); then
      "${project[@]}" get pods,pvc,deploy,statefulset -o wide || true
      "${project[@]}" get events --sort-by=.lastTimestamp || true
    fi
    kind delete cluster --name "$cluster" --kubeconfig "$kubeconfig"
  fi
  rm -f -- "$kubeconfig"
}
trap cleanup EXIT

kind create cluster --name "$cluster" --kubeconfig "$kubeconfig" --wait 120s
created=true
images=()
for target in coordinator storage-node request-api matching-worker; do
  images+=("emergency-response-matching/$target:local")
done
kind load docker-image --name "$cluster" "${images[@]}"
"${kube[@]}" apply -f deploy/kubernetes/namespace.yaml
"${project[@]}" create secret generic response-secrets \
  --from-literal="POSTGRES_PASSWORD=$POSTGRES_PASSWORD" \
  --from-literal="RABBITMQ_PASSWORD=$RABBITMQ_PASSWORD" \
  --from-literal="INTERNAL_TOKEN=$INTERNAL_TOKEN"
"${kube[@]}" apply -k deploy/kubernetes
for workload in $("${project[@]}" get deployment,statefulset -o name); do
  "${project[@]}" rollout status "$workload" --timeout=240s
done

"${project[@]}" port-forward service/request-api "${api_port}:8080" &
forward=$!
curl --fail --silent --show-error --retry 20 --retry-connrefused --retry-delay 1 "$api/readyz"

"${project[@]}" scale deployment/request-api deployment/matching-worker --replicas=2
curl --fail --silent --show-error "$api/readyz"
for workload in request-api matching-worker; do
  "${project[@]}" rollout status "deployment/$workload" --timeout=120s
  test "$("${project[@]}" get deployment "$workload" -o jsonpath='{.status.readyReplicas}')" = "2"
done
python3 scripts/smoke.py --url "$api" --count 12
echo "PASS: two API and two worker replicas ready; 12 concurrent assignments succeeded"

read -r -a workers <<< "$("${project[@]}" get pods -l app=matching-worker -o jsonpath='{.items[*].metadata.name}')"
test "${#workers[@]}" = "2"
before=$("${project[@]}" get pods -l app=matching-worker -o jsonpath='{.items[*].metadata.uid}')
"${project[@]}" delete pod "${workers[@]}" --timeout=90s
"${project[@]}" rollout status deployment/matching-worker --timeout=120s
for ((attempt = 0; attempt < 90; attempt++)); do
  ready=$("${project[@]}" get pods -l app=matching-worker -o json | python3 -c '
import json, sys
pods = json.load(sys.stdin)["items"]
for pod in pods:
    conditions = pod["status"].get("conditions", [])
    if any(c["type"] == "Ready" and c["status"] == "True" for c in conditions):
        print(pod["metadata"]["uid"], end=" ")
')
  read -r -a after <<< "$ready"
  if [[ "${#after[@]}" = "2" ]]; then
    break
  fi
  sleep 1
done
test "${#after[@]}" = "2"
for uid in $before; do
  case " ${after[*]} " in
    *" $uid "*) echo "Deleted worker UID still present: $uid" >&2; exit 1 ;;
  esac
done
python3 scripts/geographic-smoke.py --url "$api"
echo "PASS: fresh request matched after workers $before were replaced by ${after[*]}"
