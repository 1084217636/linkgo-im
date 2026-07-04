#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts/ai_summary_demo}"
GATEWAY_BASE="${GATEWAY_BASE:-http://127.0.0.1:8090}"
COMPOSE_FILE_PATH="${COMPOSE_FILE_PATH:-docker-compose.light.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-}"
GROUP_ID="${GROUP_ID:-G_AI_DEMO}"

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

echo "AI group summary demo"
echo "- gateway: $GATEWAY_BASE"
echo "- compose: $COMPOSE_FILE_PATH"
echo "- envfile: ${COMPOSE_ENV_FILE:-auto}"
echo "- group:   $GROUP_ID"
echo "- report:  $ARTIFACT_DIR/ai_summary_response.json"

if [[ "${APPLY_MIGRATION:-1}" == "1" ]]; then
  (cd "$ROOT_DIR" && compose exec -T mysql mysql -uroot -proot linkgo_im < sql/20260705_ai_summary.sql)
fi

login_json="$(curl -fsS "$GATEWAY_BASE/api/v1/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"userA","password":"123456"}')"
token="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])' <<<"$login_json")"

curl -fsS "$GATEWAY_BASE/api/v1/group/create" \
  -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/json' \
  -d "{\"group_id\":\"$GROUP_ID\",\"name\":\"AI Summary Demo\",\"members\":[\"1002\",\"1003\"]}" \
  > "$ARTIFACT_DIR/group_create_response.json"

now="$(date +%s)000"
conversation_id="group:$GROUP_ID"

(cd "$ROOT_DIR" && compose exec -T mysql mysql -uroot -proot linkgo_im <<SQL
INSERT INTO conversations (id, type, created_at, updated_at, last_seq)
VALUES ('$conversation_id', 'group', $now, $now, 3)
ON DUPLICATE KEY UPDATE updated_at = VALUES(updated_at), last_seq = GREATEST(last_seq, VALUES(last_seq));

INSERT INTO conversation_members (conversation_id, user_id, read_seq, joined_at)
VALUES
('$conversation_id', '1001', 3, $now),
('$conversation_id', '1002', 0, $now),
('$conversation_id', '1003', 0, $now)
ON DUPLICATE KEY UPDATE read_seq = GREATEST(read_seq, VALUES(read_seq));

INSERT INTO messages
  (message_id, client_msg_id, conversation_id, session_id, seq, from_uid, to_id, to_type, content, create_time)
VALUES
  ('ai-demo-$GROUP_ID-1', 'ai-demo-$GROUP_ID-c1', '$conversation_id', '$conversation_id', 1, '1001', '$GROUP_ID', 'group', '今天完成登录和 WebSocket 联调，准备补齐群聊总结演示。', $now),
  ('ai-demo-$GROUP_ID-2', 'ai-demo-$GROUP_ID-c2', '$conversation_id', '$conversation_id', 2, '1002', '$GROUP_ID', 'group', '请 1001 明天补充接口测试和 README 截图。', $now + 1),
  ('ai-demo-$GROUP_ID-3', 'ai-demo-$GROUP_ID-c3', '$conversation_id', '$conversation_id', 3, '1003', '$GROUP_ID', 'group', '风险：轻量环境没有 Kafka，群聊实时扩散需要完整 compose 验证。', $now + 2)
ON DUPLICATE KEY UPDATE
  content = VALUES(content),
  create_time = VALUES(create_time);
SQL
)

curl -fsS "$GATEWAY_BASE/api/v1/ai/group-summary" \
  -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/json' \
  -d "{\"group_id\":\"$GROUP_ID\",\"message_limit\":20,\"include_todos\":true,\"include_risks\":true}" \
  | tee "$ARTIFACT_DIR/ai_summary_response.json"

echo
echo "AI summary demo completed."
