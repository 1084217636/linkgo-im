#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts/ai_ask_demo}"
GATEWAY_BASE="${GATEWAY_BASE:-http://127.0.0.1:8090}"
COMPOSE_FILE_PATH="${COMPOSE_FILE_PATH:-docker-compose.light.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-}"
QUESTION="${QUESTION:-群聊为什么用 Kafka？}"

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

echo "AI knowledge ask demo"
echo "- gateway: $GATEWAY_BASE"
echo "- compose: $COMPOSE_FILE_PATH"
echo "- envfile: ${COMPOSE_ENV_FILE:-auto}"
echo "- question: $QUESTION"
echo "- report: $ARTIFACT_DIR/ai_ask_response.json"

if [[ "${APPLY_MIGRATION:-1}" == "1" ]]; then
  (cd "$ROOT_DIR" && compose exec -T mysql mysql -uroot -proot linkgo_im < sql/20260707_ai_provider_attempt_logs.sql)
  (cd "$ROOT_DIR" && compose exec -T mysql mysql -uroot -proot linkgo_im < sql/20260707_ai_qa_records.sql)
fi

login_json="$(curl -fsS "$GATEWAY_BASE/api/v1/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"userA","password":"123456"}')"
token="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])' <<<"$login_json")"

curl -fsS "$GATEWAY_BASE/api/v1/ai/ask" \
  -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/json' \
  -d "$(python3 -c 'import json,sys; print(json.dumps({"question": sys.argv[1], "top_k": 3}, ensure_ascii=False))' "$QUESTION")" \
  | tee "$ARTIFACT_DIR/ai_ask_response.json"

echo
echo "AI ask demo completed."
