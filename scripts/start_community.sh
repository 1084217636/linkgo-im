#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
ENV_FILE="${COMPOSE_ENV_FILE:-.env.docker-cn}"
COMPOSE=(docker compose --env-file "$ENV_FILE" -f docker-compose.yml -f docker-compose.community.yml -f docker-compose.observability.yml)
mkdir -p .local-tls
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
printf '%s\n' 'Community: https://localhost:8443/community/' 'Grafana: http://localhost:3000 (admin / linkgo)' 'Local self-signed TLS certificate requires browser trust for this demo.'
