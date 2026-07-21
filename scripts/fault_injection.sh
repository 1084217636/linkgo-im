#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts}"
REPORT="${REPORT:-$ARTIFACT_DIR/fault_injection_report.md}"
GATEWAY_READY="${GATEWAY_READY:-http://127.0.0.1:8090/readyz}"
TRANSFER_READY="${TRANSFER_READY:-http://127.0.0.1:9102/readyz}"
WAIT_SECONDS="${WAIT_SECONDS:-45}"
services=(redis logic kafka transfer gateway-a)
stopped=()
rows=()

if [[ "${FAULT_INJECTION_CONFIRM:-}" != "1" ]]; then
  echo "refusing to stop services; rerun with FAULT_INJECTION_CONFIRM=1" >&2
  exit 2
fi

cd "$ROOT_DIR"
mkdir -p "$ARTIFACT_DIR"

cleanup() {
	local active=() service
	for service in "${stopped[@]}"; do [[ -n "$service" ]] && active+=("$service"); done
	if ((${#active[@]})); then
		docker compose start "${active[@]}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

for service in "${services[@]}"; do
  if [[ "$(docker compose ps --status running --services "$service")" != "$service" ]]; then
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

inject() {
  local service="$1" probe_name="$2" url="$3"
  docker compose stop -t 5 "$service" >/dev/null
  stopped+=("$service")
  if wait_http down "$url"; then
    rows+=("| $probe_name unavailable | PASS | $service stopped and readiness rejected traffic |")
  else
    rows+=("| $probe_name unavailable | FAIL | readiness stayed healthy |")
    return 1
  fi
	docker compose start "$service" >/dev/null
	local remaining=() item
	for item in "${stopped[@]}"; do [[ "$item" != "$service" ]] && remaining+=("$item"); done
	stopped=("${remaining[@]}")
  if wait_http up "$url"; then
    rows+=("| $probe_name recovery | PASS | readiness recovered after restart |")
  else
    rows+=("| $probe_name recovery | FAIL | readiness did not recover |")
    return 1
  fi
}

inject redis "Gateway Redis dependency" "$GATEWAY_READY"
inject logic "Gateway Logic dependency" "$GATEWAY_READY"
inject kafka "Transfer Kafka dependency" "$TRANSFER_READY"
inject transfer "Transfer process" "$TRANSFER_READY"

{
  echo "# LinkGo Fault Injection Report"
  echo
  echo "- Generated at: $(date -Is)"
  echo "- Compose project: $(basename "$ROOT_DIR")"
  echo
  echo "| Scenario | Result | Evidence |"
  echo "|---|---|---|"
  printf '%s\n' "${rows[@]}"
  echo
  echo "All stopped services are restored by an EXIT trap, including failed or interrupted runs."
} >"$REPORT"

echo "fault injection passed: $REPORT"
