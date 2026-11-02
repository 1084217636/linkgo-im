#!/usr/bin/env bash
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$project_root"

compose_args=(-f docker-compose.10node.yml)
if [[ -f .env.docker-cn ]]; then
  compose_args=(--env-file .env.docker-cn "${compose_args[@]}")
fi

gateway_count=${GATEWAY_COUNT:-10}
connections_per_gateway=${CONNECTIONS_PER_GATEWAY:-1000}
friend_pairs=${FRIEND_PAIRS:-100}
messages=${MESSAGES:-2000}
send_rate=${SEND_RATE:-500}
heartbeat_duration=${HEARTBEAT_DURATION:-30s}
message_timeout=${MESSAGE_TIMEOUT:-45s}

docker compose "${compose_args[@]}" up --build -d

# The current single-chat protocol requires a normal friendship. These rows
# belong only to the local benchmark database and are idempotently refreshed.
friend_values=""
for ((i = 0; i < friend_pairs; i++)); do
  now_ms=$(date +%s%3N)
  friend_values+="('msg_sender_${i}','msg_receiver_${i}','normal',${now_ms},${now_ms}),"
  friend_values+="('msg_receiver_${i}','msg_sender_${i}','normal',${now_ms},${now_ms}),"
done
docker exec linkgo-next-mysql mariadb -uroot -proot linkgo_im -e \
  "INSERT INTO friend_relations (user_id, friend_id, status, created_at, updated_at) VALUES ${friend_values%,} ON DUPLICATE KEY UPDATE status='normal', updated_at=VALUES(updated_at)"

# Restarting clears process-local Prometheus counters, so the reported ACK
# total belongs to this run rather than earlier smoke tests.
gateway_services=()
for ((i = 0; i < gateway_count; i++)); do
  printf -v gateway_letter "\\$(printf '%03o' $((97 + i)))"
  gateway_services+=("gateway-${gateway_letter}")
done
docker compose "${compose_args[@]}" restart "${gateway_services[@]}"

go run benchmark/current_protocol_bench.go \
  -gateways="$gateway_count" \
  -per-gateway="$connections_per_gateway" \
  -heartbeat-duration="$heartbeat_duration" \
  -pairs="$friend_pairs" \
  -messages="$messages" \
  -send-rate="$send_rate" \
  -message-timeout="$message_timeout"
