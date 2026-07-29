#!/usr/bin/env python3
"""Validate the authoritative LinkGo documentation layout and local links."""

from __future__ import annotations

import re
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
HANDBOOK = DOCS / "handbook"

EXPECTED_HANDBOOK = [
    "README.md",
    "00_START_HERE.md",
    "01_WHAT_IS_IM.md",
    "02_SERVER_AND_NETWORK.md",
    "03_GO_PROJECT_MAP.md",
    "04_HTTP_FIRST_REQUEST.md",
    "05_MYSQL_AND_DATA.md",
    "06_LOGIN_AND_JWT.md",
    "07_WEBSOCKET_CONNECTION.md",
    "08_SINGLE_GATEWAY_CHAT.md",
    "09_REDIS_BASICS.md",
    "10_MULTI_GATEWAY_CHAT.md",
    "11_RELIABILITY_AND_OFFLINE.md",
    "12_GROUP_CHAT_AND_KAFKA.md",
    "13_RELATIONSHIPS_AND_CONVERSATIONS.md",
    "14_RED_PACKET.md",
    "15_AI_BOT.md",
    "16_SECURITY_OBSERVABILITY.md",
    "17_DOCKER_AND_CI.md",
    "18_KUBERNETES_DEPLOYMENT.md",
    "19_COMPLETE_CODE_WALK.md",
    "20_INTERVIEW_PREP.md",
    "21_CHECKLIST.md",
]

KNOWLEDGE_PATHS = [
    "docs/knowledge/IM_FAQ.md",
    "docs/knowledge/ARCHITECTURE.md",
    "docs/knowledge/MESSAGE_RELIABILITY.md",
]

OBSOLETE_DOC_PATHS = [
    "docs/AI_FAQ.md",
    "docs/CODE_MAP.md",
    "docs/CORE_LINKS.md",
    "docs/MODULE_CARDS.md",
    "docs/INTERVIEW_QA.md",
    "docs/FINAL_PROJECT_LEARNING_PACKAGE.md",
    "docs/FINAL_RESUME_AND_INTERVIEW_PACK.md",
    "docs/FINAL_DEMO_RUNBOOK.md",
]

FORBIDDEN_DOC_CLAIMS = [
    "基于用户维度做一致性路由",
    "异步持久化到 MySQL",
    "异步落库到 MySQL",
    "/api/v1/ws",
    "多网关扩展可线性提高吞吐",
    "当前 V2 使用 mock provider",
    "游戏运营",
    "游戏策划",
    "活动缓存",
    "道具发放",
    "GameOps",
]

LINK_RE = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
SCHEME_RE = re.compile(r"^[a-z][a-z0-9+.-]*:", re.IGNORECASE)


def fail(message: str) -> None:
    raise SystemExit(f"documentation validation failed: {message}")


def validate_layout() -> None:
    root_files = sorted(path.name for path in DOCS.iterdir() if path.is_file())
    if root_files != ["README.md"]:
        fail(f"docs root must contain only README.md, got {root_files}")

    actual = sorted(path.name for path in HANDBOOK.glob("*.md"))
    expected = sorted(EXPECTED_HANDBOOK)
    if actual != expected:
        missing = sorted(set(expected) - set(actual))
        extra = sorted(set(actual) - set(expected))
        fail(f"handbook chapter mismatch, missing={missing}, extra={extra}")


def validate_links() -> int:
    files = sorted(
        path
        for path in ROOT.rglob("*.md")
        if ".git" not in path.parts and "artifacts" not in path.parts
    )

    checked = 0
    for source in files:
        content = source.read_text(encoding="utf-8")
        for raw_target in LINK_RE.findall(content):
            target = raw_target.strip().strip("<>").split(maxsplit=1)[0]
            if not target or target.startswith("#") or SCHEME_RE.match(target):
                continue
            relative = target.split("#", 1)[0]
            resolved = (source.parent / relative).resolve()
            if not resolved.exists():
                fail(f"broken link in {source.relative_to(ROOT)}: {target}")
            checked += 1
    return checked


def validate_no_obsolete_paths() -> None:
    files = sorted(
        path
        for path in ROOT.rglob("*.md")
        if ".git" not in path.parts and "artifacts" not in path.parts
    )
    for source in files:
        content = source.read_text(encoding="utf-8")
        for obsolete in OBSOLETE_DOC_PATHS:
            if obsolete in content:
                fail(
                    f"obsolete path {obsolete} referenced by "
                    f"{source.relative_to(ROOT)}"
                )
        for forbidden in FORBIDDEN_DOC_CLAIMS:
            if forbidden in content:
                fail(
                    f"forbidden stale claim {forbidden!r} found in "
                    f"{source.relative_to(ROOT)}"
                )


def validate_knowledge_paths() -> None:
    for relative in KNOWLEDGE_PATHS:
        path = ROOT / relative
        if not path.is_file() or not path.read_text(encoding="utf-8").strip():
            fail(f"knowledge source missing or empty: {relative}")

    source = (ROOT / "internal/ai/knowledge_base.go").read_text(encoding="utf-8")
    gateway_config = (ROOT / "cmd/gateway/etc/gateway-api.yaml").read_text(encoding="utf-8")
    logic_config = (ROOT / "cmd/logic/etc/logic.yaml").read_text(encoding="utf-8")
    for relative in KNOWLEDGE_PATHS:
        for label, content in (
            ("defaultKnowledgePaths", source),
            ("gateway config", gateway_config),
            ("logic config", logic_config),
        ):
            if relative not in content:
                fail(f"{relative} missing from {label}")

    for removed in (
        "docs/AI_FAQ.md",
        "docs/CODE_MAP.md",
        "docs/CORE_LINKS.md",
        "docs/INTERVIEW_QA.md",
    ):
        if removed in source or removed in gateway_config or removed in logic_config:
            fail(f"obsolete knowledge path still configured: {removed}")


def main() -> None:
    validate_layout()
    checked_links = validate_links()
    validate_no_obsolete_paths()
    validate_knowledge_paths()
    print(
        "PASS documentation layout, "
        f"{len(EXPECTED_HANDBOOK) - 1} ordered chapters, "
        f"{checked_links} repository links, {len(KNOWLEDGE_PATHS)} knowledge sources, "
        "and no stale claims"
    )


if __name__ == "__main__":
    main()
