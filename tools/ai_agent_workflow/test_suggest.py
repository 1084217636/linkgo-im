#!/usr/bin/env python3
import argparse
import json
import re
from pathlib import Path


FUNC_PATTERN = re.compile(
    r"^func\s+(?:\([^)]*\)\s*)?(?P<name>[A-Za-z_][A-Za-z0-9_]*)\s*\(",
    re.MULTILINE,
)
TEST_PATTERN = re.compile(r"^func\s+(Test[A-Za-z0-9_]+)\s*\(", re.MULTILINE)
SKIP_DIRS = {".git", "vendor", "artifacts", "mysql_data"}
SKIP_FILES = {"api/protocol.pb.go", "api/protocol_grpc.pb.go"}


def should_skip(path, root):
    rel = path.relative_to(root).as_posix()
    if rel in SKIP_FILES:
        return True
    return any(part in SKIP_DIRS for part in path.parts)


def line_number(content, index):
    return content.count("\n", 0, index) + 1


def collect_existing_tests(root):
    tests = set()
    for path in root.rglob("*_test.go"):
        if should_skip(path, root):
            continue
        content = path.read_text(encoding="utf-8", errors="ignore")
        tests.update(TEST_PATTERN.findall(content))
    return tests


def classify_reason(name, path):
    lowered = name.lower()
    path_text = path.as_posix()
    if any(token in lowered for token in ["ack", "route", "deliver", "sync", "parse", "build"]):
        return "core message reliability or routing function"
    if any(token in path_text for token in ["middleware", "handler", "logic"]):
        return "request path function that affects API behavior"
    if name[0].isupper():
        return "exported function without direct test"
    return "helper function worth covering if it owns branch logic"


def suggest_tests(root):
    existing_tests = collect_existing_tests(root)
    suggestions = []
    for path in root.rglob("*.go"):
        if path.name.endswith("_test.go") or should_skip(path, root):
            continue
        content = path.read_text(encoding="utf-8", errors="ignore")
        for match in FUNC_PATTERN.finditer(content):
            name = match.group("name")
            if name in {"main", "init"}:
                continue
            suggested_name = f"Test{name}"
            if suggested_name in existing_tests:
                continue
            suggestions.append(
                {
                    "file": path.relative_to(root).as_posix(),
                    "line": line_number(content, match.start()),
                    "function": name,
                    "suggested_test": suggested_name,
                    "reason": classify_reason(name, path),
                }
            )
    suggestions.sort(key=lambda item: (item["file"], item["line"]))
    return {
        "task_type": "go_test_suggestion",
        "total_suggestions": len(suggestions),
        "manual_review_required": len(suggestions) > 0,
        "suggestions": suggestions[:80],
    }


def main():
    parser = argparse.ArgumentParser(description="Suggest missing Go tests from function signatures.")
    parser.add_argument("--root", default=".")
    parser.add_argument("--output", default="artifacts/test_suggestions.json")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    report = suggest_tests(root)
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"test suggestion report written to {output}")
    print(f"total suggestions: {report['total_suggestions']}")


if __name__ == "__main__":
    main()
