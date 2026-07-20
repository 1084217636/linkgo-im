#!/usr/bin/env bash
set -Eeuo pipefail

image="${1:-}"
namespace="${NAMESPACE:-linkgo-im}"
timeout="${ROLLOUT_TIMEOUT:-180s}"
deployments=(gateway logic transfer)
updated=()
port_forward_pid=""

if [[ -z "$image" || "$image" == *":latest" ]]; then
  echo "usage: $0 <registry/image:immutable-version>" >&2
  exit 2
fi

cleanup() {
  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

rollback() {
  echo "release failed; rolling back updated workloads" >&2
  for deployment in "${updated[@]}"; do
    kubectl -n "$namespace" rollout undo "deployment/$deployment" || true
  done
  for deployment in "${updated[@]}"; do
    kubectl -n "$namespace" rollout status "deployment/$deployment" --timeout="$timeout" || true
  done
}
trap rollback ERR

kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone | kubectl apply -f -
for deployment in "${deployments[@]}"; do
  kubectl -n "$namespace" set image "deployment/$deployment" "$deployment=$image"
  updated+=("$deployment")
  kubectl -n "$namespace" annotate "deployment/$deployment" \
    "linkgo.io/release-image=$image" "linkgo.io/released-at=$(date -u +%Y-%m-%dT%H:%M:%SZ)" --overwrite
done

for deployment in "${deployments[@]}"; do
  kubectl -n "$namespace" rollout status "deployment/$deployment" --timeout="$timeout"
done

kubectl -n "$namespace" port-forward service/gateway 18090:80 >/tmp/linkgo-k8s-port-forward.log 2>&1 &
port_forward_pid=$!
for _ in {1..20}; do
  if curl --fail --silent --show-error http://127.0.0.1:18090/readyz >/dev/null; then
    trap - ERR
    echo "release succeeded: $image"
    exit 0
  fi
  sleep 1
done
echo "post-release smoke test failed" >&2
exit 1
