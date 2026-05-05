#!/usr/bin/env python3
import argparse
import json
from pathlib import Path


def load_json(path):
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)


def add_issue(issues, severity, file_name, item_id, field, message, suggestion):
    issues.append(
        {
            "severity": severity,
            "file": file_name,
            "item_id": item_id,
            "field": field,
            "message": message,
            "suggestion": suggestion,
        }
    )


def check_required(issues, file_name, item, required):
    item_id = item.get("id")
    for field in required:
        if field not in item:
            add_issue(
                issues,
                "error",
                file_name,
                item_id,
                field,
                f"missing required field: {field}",
                f"add `{field}` to this config item",
            )


def check_number_range(issues, file_name, item, field, min_value, max_value):
    item_id = item.get("id")
    value = item.get(field)
    if value is None:
        return
    if not isinstance(value, (int, float)):
        add_issue(
            issues,
            "error",
            file_name,
            item_id,
            field,
            f"`{field}` should be a number",
            f"change `{field}` to a number in range [{min_value}, {max_value}]",
        )
        return
    if value < min_value or value > max_value:
        add_issue(
            issues,
            "warning",
            file_name,
            item_id,
            field,
            f"`{field}` is out of expected range: {value}",
            f"review balance value and keep `{field}` in [{min_value}, {max_value}]",
        )


def index_by_id(issues, file_name, items):
    index = {}
    for item in items:
        item_id = item.get("id")
        if item_id is None:
            add_issue(
                issues,
                "error",
                file_name,
                None,
                "id",
                "missing required field: id",
                "add a stable unique numeric id",
            )
            continue
        if item_id in index:
            add_issue(
                issues,
                "error",
                file_name,
                item_id,
                "id",
                f"duplicate id: {item_id}",
                "keep ids unique within the same config file",
            )
        index[item_id] = item
    return index


def check_config(config_dir):
    heroes_path = config_dir / "heroes.json"
    skills_path = config_dir / "skills.json"
    issues = []

    heroes = load_json(heroes_path)
    skills = load_json(skills_path)

    hero_index = index_by_id(issues, "heroes.json", heroes)
    skill_index = index_by_id(issues, "skills.json", skills)

    for hero in heroes:
        check_required(issues, "heroes.json", hero, ["id", "name", "hp", "attack", "skill_ids"])
        check_number_range(issues, "heroes.json", hero, "hp", 1, 100000)
        check_number_range(issues, "heroes.json", hero, "attack", 1, 10000)
        skill_ids = hero.get("skill_ids", [])
        if not isinstance(skill_ids, list):
            add_issue(
                issues,
                "error",
                "heroes.json",
                hero.get("id"),
                "skill_ids",
                "`skill_ids` should be a list",
                "change `skill_ids` to an array of skill ids",
            )
            continue
        for skill_id in skill_ids:
            if skill_id not in skill_index:
                add_issue(
                    issues,
                    "error",
                    "heroes.json",
                    hero.get("id"),
                    "skill_ids",
                    f"skill reference does not exist: {skill_id}",
                    "add the skill to skills.json or remove the reference",
                )

    for skill in skills:
        check_required(issues, "skills.json", skill, ["id", "name", "cooldown", "power"])
        check_number_range(issues, "skills.json", skill, "cooldown", 0, 300)
        check_number_range(issues, "skills.json", skill, "power", 1, 10000)

    errors = sum(1 for issue in issues if issue["severity"] == "error")
    warnings = sum(1 for issue in issues if issue["severity"] == "warning")
    return {
        "task_type": "game_config_check",
        "checked_files": [str(heroes_path), str(skills_path)],
        "checked_items": {
            "heroes": len(hero_index),
            "skills": len(skill_index),
        },
        "passed": errors == 0,
        "summary": {
            "total_issues": len(issues),
            "errors": errors,
            "warnings": warnings,
        },
        "issues": issues,
    }


def main():
    parser = argparse.ArgumentParser(description="Check game-like JSON config files.")
    parser.add_argument("--config-dir", default="examples/game_config")
    parser.add_argument("--output", default="artifacts/config_check_report.json")
    args = parser.parse_args()

    report = check_config(Path(args.config_dir))
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"config check report written to {output}")
    if not report["passed"]:
        print(f"config check found {report['summary']['errors']} errors and {report['summary']['warnings']} warnings")


if __name__ == "__main__":
    main()
