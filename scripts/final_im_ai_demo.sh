#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts/final_im_ai_demo}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-}"
LIGHT_COMPOSE_FILE="${LIGHT_COMPOSE_FILE:-docker-compose.light.yml}"
FULL_COMPOSE_FILE="${FULL_COMPOSE_FILE:-docker-compose.yml}"
INCLUDE_TRANSFER="${INCLUDE_TRANSFER:-0}"
TIMEOUT="${TIMEOUT:-20}"
DEMO_START_STACK="${START_STACK:-1}"

mkdir -p "$ARTIFACT_DIR"

echo "final IM + AI demo"
echo "- artifact root: $ARTIFACT_DIR"
echo "- light compose: $LIGHT_COMPOSE_FILE"
echo "- include transfer: $INCLUDE_TRANSFER"

START_STACK="$DEMO_START_STACK" \
ARTIFACT_DIR="$ARTIFACT_DIR/core_im" \
COMPOSE_FILE_PATH="$LIGHT_COMPOSE_FILE" \
TIMEOUT="$TIMEOUT" \
bash "$ROOT_DIR/scripts/demo_core_im.sh"

START_STACK=0 \
ARTIFACT_DIR="$ARTIFACT_DIR/ai_summary" \
COMPOSE_FILE_PATH="$LIGHT_COMPOSE_FILE" \
COMPOSE_ENV_FILE="$COMPOSE_ENV_FILE" \
bash "$ROOT_DIR/scripts/ai_demo.sh"

START_STACK=0 \
ARTIFACT_DIR="$ARTIFACT_DIR/ai_ask" \
COMPOSE_FILE_PATH="$LIGHT_COMPOSE_FILE" \
COMPOSE_ENV_FILE="$COMPOSE_ENV_FILE" \
bash "$ROOT_DIR/scripts/ai_ask_demo.sh"

if [[ "$INCLUDE_TRANSFER" == "1" ]]; then
  if [[ "$DEMO_START_STACK" == "1" ]]; then
    docker compose -f "$LIGHT_COMPOSE_FILE" down >/dev/null 2>&1 || true
  fi
  START_STACK=1 \
  ARTIFACT_DIR="$ARTIFACT_DIR/group_transfer" \
  COMPOSE_FILE_PATH="$FULL_COMPOSE_FILE" \
  TIMEOUT="${TRANSFER_TIMEOUT:-35}" \
  bash "$ROOT_DIR/scripts/demo_group_transfer.sh"
fi

echo
echo "final IM + AI demo completed."
