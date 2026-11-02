#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p .local-tls
if [[ ! -s .local-tls/cert.pem || ! -s .local-tls/key.pem ]]; then
  openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 30 \
    -keyout .local-tls/key.pem -out .local-tls/cert.pem \
    -subj '/CN=localhost' \
    -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1' >/dev/null 2>&1
  chmod 600 .local-tls/key.pem
fi

exec go run ./cmd/secure-demo -listen 127.0.0.1:8443 \
  -public public -gateway http://127.0.0.1:8090 \
  -cert .local-tls/cert.pem -key .local-tls/key.pem
