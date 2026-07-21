#!/usr/bin/env python3
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


class PageParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.ids: set[str] = set()

    def handle_starttag(self, _tag: str, attrs: list[tuple[str, str | None]]) -> None:
        element_id = dict(attrs).get("id")
        if element_id:
            self.ids.add(element_id)


def validate(path: str, required_ids: set[str], required_text: set[str]) -> None:
    content = (ROOT / path).read_text(encoding="utf-8")
    parser = PageParser()
    parser.feed(content)
    missing_ids = sorted(required_ids - parser.ids)
    missing_text = sorted(value for value in required_text if value not in content)
    if missing_ids or missing_text:
        raise SystemExit(f"{path}: missing ids={missing_ids}, text={missing_text}")
    print(f"PASS {path}: {len(parser.ids)} interactive ids")


validate(
    "public/index.html",
    {"login-btn", "connect-btn", "send-btn", "history-btn", "red-packet-create-btn", "open-ai-btn"},
    {"/api/v1/login", "/api/v1/history", "/api/v1/red-packets", "/ws?"},
)
validate(
    "public/admin.html",
    {"login", "create-draft", "submit-activity", "approve-activity", "publish-activity", "rollback-activity", "grant-items-btn"},
    {
        "/admin/activities/drafts",
        "/admin/activities/submit",
        "/admin/activities/approve",
        "/admin/activities/publish",
        "/admin/activities/rollback",
        "/admin/items/grant",
        "target_version",
    },
)
