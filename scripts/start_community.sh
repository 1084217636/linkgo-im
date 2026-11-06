#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
ENV_FILE="${COMPOSE_ENV_FILE:-.env.docker-cn}"
if [[ ! -f "$ENV_FILE" ]]; then
 printf 'Missing Compose environment file: %s\n' "$ENV_FILE" >&2
 exit 1
fi
if ! docker info >/dev/null 2>&1; then
 printf 'Docker daemon is unavailable. Start Docker Desktop/Engine and retry.\n' >&2
 exit 1
fi
COMPOSE=(docker compose --env-file "$ENV_FILE" -f docker-compose.yml -f docker-compose.community.yml -f docker-compose.observability.yml)
mkdir -p .local-tls
if [[ -f .local-tls/windows-host-proxy.enabled ]]; then
 export LINKGO_WEB_BIND=127.0.0.1
 export LINKGO_WEB_PORT=8444
fi
if [[ ! -s .local-tls/cert.pem || ! -s .local-tls/key.pem ]]; then
 openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 30 \
  -keyout .local-tls/key.pem -out .local-tls/cert.pem \
  -subj '/CN=localhost' -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1' >/dev/null 2>&1
 chmod 600 .local-tls/key.pem
fi
"${COMPOSE[@]}" up -d mysql
for i in {1..60}; do
 if "${COMPOSE[@]}" exec -T mysql sh -c 'if command -v mariadb >/dev/null; then mariadb-admin -uroot -proot ping; else mysqladmin -uroot -proot ping; fi' >/dev/null 2>&1; then break; fi
 sleep 1
done
for migration in sql/20260903_delivery_outbox.sql sql/20261102_delivery_trace.sql sql/20261028_learning_tasks.sql sql/20261030_attachments.sql sql/20261030_task_attachments.sql sql/20261102_communities.sql; do
 "${COMPOSE[@]}" exec -T mysql sh -c 'if command -v mariadb >/dev/null; then exec mariadb -uroot -proot; else exec mysql -uroot -proot; fi' < "$migration"
done
"${COMPOSE[@]}" up -d --build
printf 'Waiting for Kafka and application readiness...\n'
ready=0
for i in {1..60}; do
 if docker exec linkgo-next-kafka kafka-broker-api-versions --bootstrap-server kafka:9092 >/dev/null 2>&1 \
  && curl -fsS --max-time 2 http://127.0.0.1:8090/readyz >/dev/null 2>&1 \
  && curl -fsS --max-time 2 http://127.0.0.1:9002/readyz >/dev/null 2>&1 \
  && curl -fsS --max-time 2 http://127.0.0.1:9102/readyz >/dev/null 2>&1; then
  ready=1
  break
 fi
 sleep 2
done
if [[ "$ready" != 1 ]]; then
 printf 'Services did not become ready within 120 seconds. Current status:\n' >&2
 "${COMPOSE[@]}" ps >&2
 exit 1
fi
if [[ -f .local-tls/windows-host-proxy.enabled ]]; then
 bash scripts/start_windows_host_proxy.sh
fi
printf '%s\n' 'Community: https://localhost:8443/community/' 'Grafana: http://localhost:3000 (admin / linkgo)' 'Local self-signed TLS certificate requires browser trust for this demo.'
