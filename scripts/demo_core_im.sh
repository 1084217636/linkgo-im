#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts/core_im_demo}"
GATEWAY_BASE="${GATEWAY_BASE:-http://127.0.0.1:8090}"
TRANSFER_BASE="${TRANSFER_BASE:-http://127.0.0.1:9102}"
COMPOSE_FILE_PATH="${COMPOSE_FILE_PATH:-docker-compose.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-}"
REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"
REDIS_PASSWORD="${REDIS_PASSWORD:-123456}"
MYSQL_DSN="${MYSQL_DSN:-root:root@tcp(127.0.0.1:3306)/linkgo_im?charset=utf8mb4&parseTime=True&loc=Local}"
TIMEOUT="${TIMEOUT:-20}"
REQUIRE_TRANSFER="${REQUIRE_TRANSFER:-0}"

mkdir -p "$ARTIFACT_DIR"

compose() {
  local args=()
  if [[ -n "$COMPOSE_ENV_FILE" ]]; then
    args+=(--env-file "$COMPOSE_ENV_FILE")
  elif [[ "${USE_DOCKER_CN:-1}" == "1" && -f "$ROOT_DIR/.env.docker-cn" ]]; then
    args+=(--env-file .env.docker-cn)
  fi
  docker compose "${args[@]}" -f "$COMPOSE_FILE_PATH" "$@"
}

if [[ "${START_STACK:-0}" == "1" ]]; then
  (cd "$ROOT_DIR" && compose up -d --build)
fi

echo "core IM demo"
echo "- gateway: $GATEWAY_BASE"
echo "- transfer:$TRANSFER_BASE"
echo "- compose: $COMPOSE_FILE_PATH"
echo "- envfile: ${COMPOSE_ENV_FILE:-auto}"
echo "- redis:   $REDIS_ADDR"
echo "- report:  $ARTIFACT_DIR/core_im_demo_report.md"

(cd "$ROOT_DIR" && go run ./tools/core_im_demo \
  -gateway-base "$GATEWAY_BASE" \
  -transfer-base "$TRANSFER_BASE" \
  -redis-addr "$REDIS_ADDR" \
  -redis-password "$REDIS_PASSWORD" \
  -mysql-dsn "$MYSQL_DSN" \
  -artifact-dir "$ARTIFACT_DIR" \
  -timeout "$TIMEOUT" \
  -require-transfer="$REQUIRE_TRANSFER")
