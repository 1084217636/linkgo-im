#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_PORT="${FRONTEND_PORT:-8088}"
GATEWAY_BASE="${GATEWAY_BASE:-http://127.0.0.1:8090}"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts/frontend_smoke}"
COMPOSE_FILE_PATH="${COMPOSE_FILE_PATH:-docker-compose.light.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-}"

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

python3 -m http.server "$FRONTEND_PORT" --directory "$ROOT_DIR/public" > "$ARTIFACT_DIR/frontend_http.log" 2>&1 &
SERVER_PID=$!
trap 'kill "$SERVER_PID" >/dev/null 2>&1 || true' EXIT

sleep 1
FRONTEND_URL="http://127.0.0.1:$FRONTEND_PORT/"

curl -fsS "$FRONTEND_URL" > "$ARTIFACT_DIR/index.html"
curl -fsS "${FRONTEND_URL}admin.html" > "$ARTIFACT_DIR/admin.html"
python3 "$ROOT_DIR/scripts/validate_frontend.py"
curl -fsS "$GATEWAY_BASE/healthz" > "$ARTIFACT_DIR/gateway_health.json"

(cd "$ROOT_DIR" && START_STACK=0 ARTIFACT_DIR="$ARTIFACT_DIR/core_im_demo" GATEWAY_BASE="$GATEWAY_BASE" COMPOSE_FILE_PATH="$COMPOSE_FILE_PATH" bash scripts/demo_core_im.sh)

cat > "$ARTIFACT_DIR/frontend_smoke_report.md" <<REPORT
# Frontend Smoke Report

- Frontend URL: $FRONTEND_URL
- Gateway: $GATEWAY_BASE
- Checked page load: PASS
- Checked operations console load and required controls: PASS
- Checked Gateway healthz: PASS
- Checked userA/userB WebSocket chat: PASS
- Checked userA -> AI assistant private reply: PASS

Open two browser tabs at $FRONTEND_URL, login userA and userB separately, then use "打开对聊" and "AI 助手" for manual verification. Open ${FRONTEND_URL}admin.html to exercise operator/reviewer/admin separation.
REPORT

echo "frontend: $FRONTEND_URL"
echo "report: $ARTIFACT_DIR/frontend_smoke_report.md"
