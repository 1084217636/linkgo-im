#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts/group_transfer_demo}"
COMPOSE_FILE_PATH="${COMPOSE_FILE_PATH:-docker-compose.yml}"
TIMEOUT="${TIMEOUT:-35}"

echo "group Transfer demo"
echo "- compose: $COMPOSE_FILE_PATH"
echo "- report:  $ARTIFACT_DIR/core_im_demo_report.md"
echo "- note:    if ports are occupied by the light stack, run make docker-light-down first."

START_STACK="${START_STACK:-1}" \
REQUIRE_TRANSFER=1 \
ARTIFACT_DIR="$ARTIFACT_DIR" \
COMPOSE_FILE_PATH="$COMPOSE_FILE_PATH" \
TIMEOUT="$TIMEOUT" \
bash "$ROOT_DIR/scripts/demo_core_im.sh"
