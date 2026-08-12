#!/usr/bin/env bash
set -Eeuo pipefail

# Destructive, opt-in local exercise. It deliberately stops Compose services,
# proves that readiness rejects traffic, restarts the service, and then runs a
# real IM black-box flow. It is not run in CI because it needs Docker services.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts}"
REPORT="${REPORT:-$ARTIFACT_DIR/fault_injection_report.md}"
GATEWAY_BASE="${GATEWAY_BASE:-http://127.0.0.1:8090}"
GATEWAY_READY="${GATEWAY_READY:-http://127.0.0.1:8090/readyz}"
GATEWAY_REPLACEMENT="${GATEWAY_REPLACEMENT:-http://127.0.0.1:8091}"
LOGIC_READY="${LOGIC_READY:-http://127.0.0.1:9002/readyz}"
TRANSFER_READY="${TRANSFER_READY:-http://127.0.0.1:9102/readyz}"
WAIT_SECONDS="${WAIT_SECONDS:-45}"
SMOKE_TIMEOUT="${SMOKE_TIMEOUT:-60}"
COMPOSE_FILE_PATH="${COMPOSE_FILE_PATH:-docker-compose.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-}"
stopped=()
rows=()
failures=0

if [[ "${FAULT_INJECTION_CONFIRM:-}" != "1" ]]; then
  echo "refusing to stop services; rerun with FAULT_INJECTION_CONFIRM=1" >&2
  exit 2
fi

cd "$ROOT_DIR"
mkdir -p "$ARTIFACT_DIR"
compose=(docker compose -f "$COMPOSE_FILE_PATH")
if [[ -n "$COMPOSE_ENV_FILE" ]]; then
  compose+=(--env-file "$COMPOSE_ENV_FILE")
fi

cleanup() {
  local active=() service
  for service in "${stopped[@]}"; do
    [[ -n "$service" ]] && active+=("$service")
  done
  if ((${#active[@]})); then
    "${compose[@]}" start "${active[@]}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

required_services=(redis mysql logic kafka transfer gateway-a gateway-b)
for service in "${required_services[@]}"; do
  if [[ "$("${compose[@]}" ps --status running --services "$service")" != "$service" ]]; then
    echo "required service is not running: $service" >&2
    exit 3
  fi
done

wait_http() {
  local expectation="$1" url="$2" deadline=$((SECONDS + WAIT_SECONDS))
  while ((SECONDS < deadline)); do
    if curl --fail --silent --max-time 2 "$url" >/dev/null 2>&1; then
      [[ "$expectation" == "up" ]] && return 0
    else
      [[ "$expectation" == "down" ]] && return 0
    fi
    sleep 1
  done
  return 1
}

run_business_smoke() {
  local label="$1" base="$2" require_transfer="${3:-0}"
  local report_dir="$ARTIFACT_DIR/business-$label"
  mkdir -p "$report_dir"
  if GATEWAY_BASE="$base" TRANSFER_BASE="http://127.0.0.1:9102" REQUIRE_TRANSFER="$require_transfer" TIMEOUT="$SMOKE_TIMEOUT" \
    ARTIFACT_DIR="$report_dir" START_STACK=0 bash scripts/demo_core_im.sh >"$report_dir.log" 2>&1; then
    rows+=("| $label business smoke | PASS | core IM flow completed through $base |")
    return 0
  fi
  rows+=("| $label business smoke | FAIL | see $report_dir.log |")
  failures=$((failures + 1))
  return 0
}

restart_service() {
  local service="$1"
  "${compose[@]}" start "$service" >/dev/null
  local remaining=() item
  for item in "${stopped[@]}"; do
    [[ "$item" != "$service" ]] && remaining+=("$item")
  done
  stopped=("${remaining[@]}")
}

stop_service() {
  local service="$1"
  "${compose[@]}" stop -t 5 "$service" >/dev/null
  stopped+=("$service")
}

expect_down_then_up() {
  local label="$1" url="$2" service="$3"
  stop_service "$service"
  if wait_http down "$url"; then
    rows+=("| $label unavailable | PASS | $service stopped and readiness rejected traffic |")
  else
    rows+=("| $label unavailable | FAIL | readiness stayed healthy |")
    return 1
  fi
  restart_service "$service"
  if wait_http up "$url"; then
    rows+=("| $label recovery | PASS | readiness recovered after replacement |")
  else
    rows+=("| $label recovery | FAIL | readiness did not recover |")
    return 1
  fi
}

gateway_failover() {
  stop_service gateway-a
  if wait_http down "$GATEWAY_READY" && wait_http up "$GATEWAY_REPLACEMENT/readyz"; then
    rows+=("| gateway replacement | PASS | gateway-a removed; gateway-b accepted readiness |")
  else
    rows+=("| gateway replacement | FAIL | replacement gateway was not ready |")
    return 1
  fi
  run_business_smoke gateway-replacement "$GATEWAY_REPLACEMENT" 0
  restart_service gateway-a
}

expect_down_then_up "Redis dependency" "$GATEWAY_READY" redis
wait_http up "$LOGIC_READY"
run_business_smoke redis-recovery "$GATEWAY_BASE" 0

expect_down_then_up "Logic dependency" "$GATEWAY_READY" logic
run_business_smoke logic-recovery "$GATEWAY_BASE" 0

stop_service kafka
if wait_http down "$LOGIC_READY" && wait_http down "$TRANSFER_READY"; then
  rows+=("| Kafka dependency unavailable | PASS | Logic and Transfer readiness rejected traffic |")
else
  rows+=("| Kafka dependency unavailable | FAIL | dependency-aware readiness stayed healthy |")
  exit 1
fi
restart_service kafka
if wait_http up "$LOGIC_READY" && wait_http up "$TRANSFER_READY"; then
  rows+=("| Kafka dependency recovery | PASS | Logic and Transfer recovered after broker replacement |")
else
  rows+=("| Kafka dependency recovery | FAIL | broker recovery was not observed |")
  exit 1
fi
run_business_smoke kafka-recovery "$GATEWAY_BASE" 1

expect_down_then_up "Transfer process" "$TRANSFER_READY" transfer
run_business_smoke transfer-recovery "$GATEWAY_BASE" 1

expect_down_then_up "MySQL dependency" "$GATEWAY_READY" mysql
if wait_http up "$LOGIC_READY"; then
  rows+=("| MySQL recovery dependency check | PASS | Logic readiness recovered with database |")
else
  rows+=("| MySQL recovery dependency check | FAIL | Logic readiness stayed unavailable |")
  exit 1
fi
run_business_smoke mysql-recovery "$GATEWAY_BASE" 0

gateway_failover

{
  echo "# LinkGo Fault Injection Report"
  echo
  echo "- Generated at: $(date -Is)"
  echo "- Compose file: \`$COMPOSE_FILE_PATH\`"
  echo "- This report proves local failure detection, replacement, recovery, and a post-recovery business flow."
  echo
  echo "| Scenario | Result | Evidence |"
  echo "|---|---|---|"
  printf '%s\n' "${rows[@]}"
  echo
  echo "All stopped services are restored by an EXIT trap, including failed or interrupted runs."
} >"$REPORT"

if ((failures > 0)); then
  echo "fault injection completed with $failures business smoke failure(s): $REPORT" >&2
  exit 1
fi
echo "fault injection passed: $REPORT"
