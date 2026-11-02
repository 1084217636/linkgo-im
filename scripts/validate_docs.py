#!/usr/bin/env python3
"""Validate the authoritative LinkGo documentation layout and local links."""

from __future__ import annotations

import re
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
HANDBOOK = DOCS / "handbook"
MOBILE = DOCS / "mobile_go_interview"

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
    "14_SECURITY_OBSERVABILITY.md",
    "15_DOCKER_AND_CI.md",
    "16_KUBERNETES_DEPLOYMENT.md",
    "17_COMPLETE_CODE_WALK.md",
    "18_INTERVIEW_PREP.md",
    "19_CHECKLIST.md",
    "20_AI_ASSISTED_BACKEND_DEVELOPMENT.md",
    "APPENDIX_A_WALLET_RED_PACKET.md",
    "APPENDIX_B_AI_BOT.md",
]

EXPECTED_MOBILE = [
    "README.md",
    "01_GO_AND_CONCURRENCY.md",
    "02_CS_NETWORK.md",
    "03_LINKGO_ARCHITECTURE.md",
    "04_CHAT_AND_RELIABILITY.md",
    "05_DATA_AND_MIDDLEWARE.md",
    "06_FAILURE_AND_SCALING.md",
    "07_ENGINEERING_AND_TESTING.md",
    "08_PROJECT_STORY.md",
    "09_LAST_MINUTE_CARDS.md",
]

MOBILE_ANSWER_SECTIONS = (
    "### 面试官问",
    "### 30 秒回答",
    "### 详细回答",
    "### 结合 LinkGo",
    "### 为什么这样选，替代方案是什么",
    "### 边界与追问",
)

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
    "多网关扩展可线性提高吞吐",
    "当前 V2 使用 mock provider",
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

    actual_mobile = sorted(path.name for path in MOBILE.glob("*.md"))
    expected_mobile = sorted(EXPECTED_MOBILE)
    if actual_mobile != expected_mobile:
        missing = sorted(set(expected_mobile) - set(actual_mobile))
        extra = sorted(set(actual_mobile) - set(expected_mobile))
        fail(f"mobile handbook mismatch, missing={missing}, extra={extra}")


def validate_links() -> int:
    files = sorted(
        path
        for path in ROOT.rglob("*.md")
        if not {".git", "artifacts", "node_modules", ".venv"}.intersection(path.parts)
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
        if not {".git", "artifacts", "node_modules", ".venv"}.intersection(path.parts)
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
        # /api/v1/ws is obsolete, but /api/v1/ws-ticket is a valid endpoint.
        if re.search(r"/api/v1/ws(?=[^A-Za-z0-9_-]|$)", content):
            fail(f"forbidden stale route '/api/v1/ws' found in {source.relative_to(ROOT)}")


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


def validate_learning_contract() -> None:
    """Keep every numbered chapter usable without a separate answer document."""

    for name in EXPECTED_HANDBOOK[1:]:
        path = HANDBOOK / name
        content = path.read_text(encoding="utf-8")
        reading_heading = "## 本章代码阅读任务"
        if reading_heading not in content:
            fail(f"{name} is missing {reading_heading}")
        answer = re.search(r"^## .*参考答案\s*$", content, re.MULTILINE)
        if answer is None:
            fail(f"{name} is missing an in-file reference-answer section")
        reading_at = content.index(reading_heading)
        if answer.start() <= reading_at:
            fail(f"{name} puts reference answers before its reading task")
        reading_block = content[reading_at : answer.start()]
        if reading_block.count("`") < 4:
            fail(
                f"{name} reading task must name concrete files and symbols, "
                "not only another document"
            )
        if name.startswith(tuple(f"{number:02d}_" for number in range(9, 21))) or name.startswith("APPENDIX_"):
            if "> 我" not in reading_block or "答案" not in reading_block:
                fail(
                    f"{name} reading task must contain a same-file, copyable "
                    "one-problem AI prompt with answered self-checks"
                )


def validate_mobile_contract() -> None:
    """Keep each commuting chapter complete enough to answer without another prompt."""

    for name in EXPECTED_MOBILE[1:]:
        path = MOBILE / name
        content = path.read_text(encoding="utf-8")
        question_count = len(re.findall(r"^## \d+\.", content, re.MULTILINE))
        if question_count < 3:
            fail(f"{name} must contain at least 3 complete interview questions")
        for heading in MOBILE_ANSWER_SECTIONS:
            actual = len(
                re.findall(rf"^{re.escape(heading)}$", content, re.MULTILINE)
            )
            if actual != question_count:
                fail(
                    f"{name} has {actual} {heading!r} sections for "
                    f"{question_count} questions"
                )
        for unfinished in ("待补充", "再去问 AI", "继续向 AI 提问"):
            if unfinished in content:
                fail(f"{name} contains unfinished learning instruction {unfinished!r}")


def main() -> None:
    validate_layout()
    checked_links = validate_links()
    validate_no_obsolete_paths()
    validate_knowledge_paths()
    validate_learning_contract()
    validate_mobile_contract()
    print(
        "PASS documentation layout, "
        "21 ordered chapters plus 2 optional appendices, "
        "9 complete mobile interview chapters, "
        f"{checked_links} repository links, {len(KNOWLEDGE_PATHS)} knowledge sources, "
        "in-file chapter answers, and no stale claims"
    )


if __name__ == "__main__":
    main()
