#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts/cluster_topology}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-}"

mkdir -p "$ARTIFACT_DIR"

compose() {
  local args=()
  if [[ -n "$COMPOSE_ENV_FILE" ]]; then
    args+=(--env-file "$COMPOSE_ENV_FILE")
  elif [[ "${USE_DOCKER_CN:-1}" == "1" && -f "$ROOT_DIR/.env.docker-cn" ]]; then
    args+=(--env-file .env.docker-cn)
  fi
  docker compose "${args[@]}" "$@"
}

if [[ "${START_STACK:-0}" == "1" ]]; then
  (cd "$ROOT_DIR" && compose up -d --build)
fi

(cd "$ROOT_DIR" && compose ps) > "$ARTIFACT_DIR/compose-ps.txt"

for container in linkgo-logic linkgo-logic-b; do
  docker exec "$container" wget -qO- http://127.0.0.1:9002/readyz \
    > "$ARTIFACT_DIR/${container}-ready.json"
done

gateway_ports=(
  "${LINKGO_GATEWAY_A_PORT:-8090}"
  "${LINKGO_GATEWAY_B_PORT:-8091}"
  "${LINKGO_GATEWAY_C_PORT:-8092}"
)
for port in "${gateway_ports[@]}"; do
  curl -fsS "http://127.0.0.1:${port}/readyz" \
    > "$ARTIFACT_DIR/gateway-${port}-ready.json"
done

docker exec -e ETCDCTL_API=3 linkgo-etcd \
  etcdctl --endpoints=http://127.0.0.1:2379 get services/logic --prefix --keys-only \
  > "$ARTIFACT_DIR/logic-etcd-keys.txt"

logic_nodes="$(grep -c '^services/logic/' "$ARTIFACT_DIR/logic-etcd-keys.txt" || true)"
if [[ "$logic_nodes" -lt 2 ]]; then
  echo "expected at least 2 Logic registrations, got $logic_nodes" >&2
  cat "$ARTIFACT_DIR/logic-etcd-keys.txt" >&2
  exit 1
fi

cat > "$ARTIFACT_DIR/report.md" <<REPORT
# LinkGo Cluster Topology Verification

- Logic registrations in Etcd: ${logic_nodes}
- Logic readiness: linkgo-logic PASS, linkgo-logic-b PASS
- Gateway readiness: ${gateway_ports[*]} PASS
- Gateway routing mode: Etcd service discovery (LOGIC_ADDR unset)

Raw evidence is stored beside this report.
REPORT

echo "PASS: ${logic_nodes} Logic nodes and 3 Gateway nodes are ready"
echo "report: $ARTIFACT_DIR/report.md"
