#!/usr/bin/env python3
import argparse
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path


def run_command(command, log_path):
    if not command:
        return None
    result = subprocess.run(command, shell=True, text=True, capture_output=True)
    log_path.parent.mkdir(parents=True, exist_ok=True)
    log_path.write_text((result.stdout or "") + (result.stderr or ""), encoding="utf-8")
    return result.returncode


def git_changed_files():
    diff = subprocess.run(["git", "diff", "--name-only"], text=True, capture_output=True)
    status = subprocess.run(["git", "status", "--short"], text=True, capture_output=True)
    files = set()
    if diff.returncode == 0:
        files.update(line.strip() for line in diff.stdout.splitlines() if line.strip())
    if status.returncode == 0:
        for line in status.stdout.splitlines():
            if len(line) >= 4:
                files.add(line[3:].strip())
    return sorted(files)


def build_summary(args):
    output = Path(args.output)
    validation_log = output.parent / "validation.log"
    exit_code = run_command(args.validation_command, validation_log)
    files_changed = git_changed_files()
    tests_passed = None if exit_code is None else exit_code == 0
    failure_reason = None
    if exit_code not in (None, 0):
        failure_reason = f"validation command exited with code {exit_code}"

    manual_review_required = bool(args.manual_review_required)
    if tests_passed is False or len(files_changed) > args.review_file_threshold:
        manual_review_required = True

    return {
        "task_type": args.task_type,
        "created_at": datetime.now(timezone.utc).isoformat(),
        "files_changed": len(files_changed),
        "changed_files": files_changed,
        "validation_command": args.validation_command,
        "tests_passed": tests_passed,
        "rollback_triggered": args.rollback_triggered,
        "manual_review_required": manual_review_required,
        "failure_reason": failure_reason,
        "artifacts": {
            "validation_log": str(validation_log) if exit_code is not None else None,
            "summary": str(output),
        },
    }


def main():
    parser = argparse.ArgumentParser(description="Write an AI task quality summary artifact.")
    parser.add_argument("--task-type", default="demo_task")
    parser.add_argument("--validation-command", default="")
    parser.add_argument("--output", default="artifacts/quality_summary.json")
    parser.add_argument("--rollback-triggered", action="store_true")
    parser.add_argument("--manual-review-required", action="store_true")
    parser.add_argument("--review-file-threshold", type=int, default=5)
    args = parser.parse_args()

    summary = build_summary(args)
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"quality summary written to {output}")
    if summary["failure_reason"]:
        print(summary["failure_reason"])


if __name__ == "__main__":
    main()
