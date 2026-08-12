#!/usr/bin/env python3
"""Static contract checks for the fault-first runbook and manifests."""

from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def require(text: str, needle: str, label: str) -> None:
    if needle not in text:
        raise SystemExit(f"fault-handling validation failed: {label}: missing {needle}")


def main() -> None:
    script = (ROOT / "scripts/fault_injection.sh").read_text()
    runbook = (ROOT / "docs/runbooks/FAULT_HANDLING.md").read_text()
    logic = (ROOT / "deploy/k8s/logic.yaml").read_text()
    logic_health = (ROOT / "cmd/logic/health.go").read_text()
    logic_main = (ROOT / "cmd/logic/main.go").read_text()
    logic_router = (ROOT / "cmd/gateway/internal/svc/logicrouter.go").read_text()
    gateway = (ROOT / "deploy/k8s/gateway.yaml").read_text()
    transfer = (ROOT / "deploy/k8s/transfer.yaml").read_text()
    prometheus = (ROOT / "deploy/observability/prometheus.yml").read_text()
    alerts = (ROOT / "deploy/observability/rules/linkgo-alerts.yml").read_text()

    for service in ("redis", "mysql", "logic", "kafka", "transfer", "gateway-a", "gateway-b"):
        require(script, service, f"fault scenario {service}")
    for token in (
        "FAULT_INJECTION_CONFIRM",
        "wait_http down",
        "wait_http up",
        "run_business_smoke",
        "gateway-replacement",
        "trap cleanup EXIT",
    ):
        require(script, token, f"fault script contract {token}")
    for token in (
        "healthz",
        "readyz",
        "client_msg_id",
        "retry/DLQ",
        "preStop",
        "不能迁移旧 WebSocket",
        "Redis Cluster",
    ):
        require(runbook, token, f"runbook boundary {token}")
    for token in ("9002", "/readyz", "/healthz", "targetPort: health", "preStop"):
        require(logic, token, f"Logic probe {token}")
    require(logic_main, "RegisterHealthServer", "Logic gRPC health registration")
    require(logic_health, "NOT_SERVING", "Logic dependency health failure status")
    require(logic_router, "NewHealthClient", "Gateway Logic dependency health check")
    for manifest, label in ((gateway, "Gateway"), (transfer, "Transfer")):
        require(manifest, "preStop", f"{label} drain hook")
        require(manifest, "maxUnavailable: 0", f"{label} rolling replacement")
    require(transfer, "kind: PodDisruptionBudget", "Transfer disruption budget")
    require(prometheus, "job_name: linkgo-logic", "Logic metrics target")
    require(alerts, "linkgo-logic", "Logic target-down alert")
    print("PASS fault-handling contract, probes, replacement hooks, observability, and runbook")


if __name__ == "__main__":
    main()
