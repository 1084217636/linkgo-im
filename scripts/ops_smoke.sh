#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$ROOT_DIR/artifacts}"
REPORT="${REPORT:-$ARTIFACT_DIR/ops_smoke_report.md}"

GATEWAY_BASE="${GATEWAY_BASE:-http://127.0.0.1:8090}"
TRANSFER_BASE="${TRANSFER_BASE:-http://127.0.0.1:9102}"
PROMETHEUS_BASE="${PROMETHEUS_BASE:-http://127.0.0.1:9090}"
GRAFANA_BASE="${GRAFANA_BASE:-http://127.0.0.1:3000}"

mkdir -p "$ARTIFACT_DIR"

failures=0
rows=()

record() {
  local name="$1"
  local url="$2"
  local result="$3"
  rows+=("| $name | \`$url\` | $result |")
}

check_http() {
  local name="$1"
  local url="$2"
  if curl -fsS --max-time 5 "$url" >/tmp/linkgo_ops_smoke_body 2>/tmp/linkgo_ops_smoke_err; then
    record "$name" "$url" "PASS"
    return 0
  fi
  record "$name" "$url" "FAIL: $(tr '\n' ' ' </tmp/linkgo_ops_smoke_err)"
  failures=$((failures + 1))
  return 1
}

check_metrics() {
  local name="$1"
  local url="$2"
  local pattern="$3"
  if curl -fsS --max-time 5 "$url" 2>/tmp/linkgo_ops_smoke_err | grep -q "$pattern"; then
    record "$name" "$url" "PASS"
    return 0
  fi
  record "$name" "$url" "FAIL: missing $pattern"
  failures=$((failures + 1))
  return 1
}

check_prom_query() {
  local query="$1"
  local url="$PROMETHEUS_BASE/api/v1/query"
  if curl -fsS --max-time 5 -G "$url" --data-urlencode "query=$query" >/tmp/linkgo_prom_query.json 2>/tmp/linkgo_ops_smoke_err; then
    record "prometheus query" "$url?query=$query" "PASS"
    return 0
  fi
  record "prometheus query" "$url?query=$query" "FAIL: $(tr '\n' ' ' </tmp/linkgo_ops_smoke_err)"
  failures=$((failures + 1))
  return 1
}

check_http "gateway healthz" "$GATEWAY_BASE/healthz"
check_http "gateway readyz" "$GATEWAY_BASE/readyz"
check_metrics "gateway metrics" "$GATEWAY_BASE/metrics" "linkgo_ws_connections"

check_http "transfer healthz" "$TRANSFER_BASE/healthz"
check_http "transfer readyz" "$TRANSFER_BASE/readyz"
check_metrics "transfer metrics" "$TRANSFER_BASE/metrics" "linkgo_ws_connections"

check_http "prometheus ready" "$PROMETHEUS_BASE/-/ready"
check_prom_query 'up{job=~"linkgo-.*"}'
check_http "grafana health" "$GRAFANA_BASE/api/health"

gateway_metric_sample="$(curl -fsS --max-time 5 "$GATEWAY_BASE/metrics" 2>/dev/null | grep -E '^(# HELP linkgo_|# TYPE linkgo_|linkgo_)' | head -n 40 || true)"
transfer_metric_sample="$(curl -fsS --max-time 5 "$TRANSFER_BASE/metrics" 2>/dev/null | grep -E '^(# HELP linkgo_|# TYPE linkgo_|linkgo_)' | head -n 40 || true)"

{
  echo "# LinkGo IM Ops Smoke Report"
  echo
  echo "- Generated at: $(date -Is)"
  echo "- Gateway: \`$GATEWAY_BASE\`"
  echo "- Transfer: \`$TRANSFER_BASE\`"
  echo "- Prometheus: \`$PROMETHEUS_BASE\`"
  echo "- Grafana: \`$GRAFANA_BASE\`"
  echo
  echo "| Check | URL | Result |"
  echo "|---|---|---|"
  for row in "${rows[@]}"; do
    echo "$row"
  done
  echo
  echo "## Gateway Metric Sample"
  echo
  echo '```text'
  echo "$gateway_metric_sample"
  echo '```'
  echo
  echo "## Transfer Metric Sample"
  echo
  echo '```text'
  echo "$transfer_metric_sample"
  echo '```'
} >"$REPORT"

rm -f /tmp/linkgo_ops_smoke_body /tmp/linkgo_ops_smoke_err /tmp/linkgo_prom_query.json

echo "ops smoke report: $REPORT"
if [ "$failures" -gt 0 ]; then
  echo "ops smoke failed: $failures checks failed"
  exit 1
fi

echo "ops smoke passed"
