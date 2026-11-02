#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${COMPOSE_ENV_FILE:-.env.docker-cn}"
REQUIRED_PORTS=(2379 3306 6379 8090 8091 8092 9002 9092 9102)

cd "$ROOT_DIR"

echo "[LinkGo] clean previous LinkGo containers"
docker compose --env-file "$ENV_FILE" down --remove-orphans

for port in "${REQUIRED_PORTS[@]}"; do
  mapfile -t containers < <(docker ps --filter "publish=$port" --format '{{.Names}}')
  for container in "${containers[@]}"; do
    if [[ "$container" == comet-* ]]; then
      echo "[LinkGo] stop known conflicting container $container on port $port"
      docker stop "$container" >/dev/null
      continue
    fi
    echo "[LinkGo] port $port is occupied by Docker container $container" >&2
    echo "[LinkGo] stop that service explicitly, then run this command again" >&2
    exit 1
  done

  if [[ -n "$(ss -H -ltn "sport = :$port" 2>/dev/null)" ]]; then
    echo "[LinkGo] port $port is still occupied by a non-LinkGo listener:" >&2
    ss -ltnp "sport = :$port" >&2 || true
    echo "[LinkGo] refusing to kill an unknown process or switch to a random port" >&2
    exit 1
  fi
done

echo "[LinkGo] start all services on repository default ports"
docker compose --env-file "$ENV_FILE" up -d --build

for _ in $(seq 1 60); do
  if curl -fsS --max-time 2 http://127.0.0.1:8090/readyz >/dev/null 2>&1; then
    echo "[LinkGo] ready: http://127.0.0.1:8090"
    exit 0
  fi
  sleep 1
done

echo "[LinkGo] Gateway did not become ready within 60 seconds" >&2
docker compose --env-file "$ENV_FILE" ps >&2
exit 1
