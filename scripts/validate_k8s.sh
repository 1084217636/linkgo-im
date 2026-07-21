#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT

cd "$ROOT_DIR"
kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone >"$rendered"

require_count() {
  local pattern="$1" minimum="$2" label="$3" count
  count="$(grep -cE "$pattern" "$rendered" || true)"
  if ((count < minimum)); then
    echo "Kubernetes validation failed: $label count=$count, want >=$minimum" >&2
    exit 1
  fi
}

require_count '^kind: Deployment$' 3 "deployments"
require_count 'readinessProbe:' 3 "readiness probes"
require_count 'livenessProbe:' 3 "liveness probes"
require_count 'maxUnavailable: 0' 2 "zero-downtime rolling strategies"
require_count 'secretRef:' 3 "application secret injection"

if grep -q 'DB_DSN:' deploy/k8s/configmap.yaml; then
  echo "Kubernetes validation failed: DB_DSN must not be stored in ConfigMap" >&2
  exit 1
fi
grep -q 'DB_DSN:' deploy/k8s/secret.yaml
bash -n scripts/k8s_release.sh
if bash scripts/k8s_release.sh example/linkgo-im:latest >/dev/null 2>&1; then
  echo "Kubernetes validation failed: release script accepted :latest" >&2
  exit 1
else
  test "$?" -eq 2
fi

echo "PASS Kubernetes render, probes, secret boundary, and immutable release guard"
