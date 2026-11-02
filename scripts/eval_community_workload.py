#!/usr/bin/env python3
"""Small repeatable business workload; reports observed local results only.

Creates fresh users/community and 100 posts, then checks feed pagination,
comment persistence and task state. Never deletes an existing community.
"""
import concurrent.futures
import importlib.util
import json
import pathlib
import statistics
import time
import uuid

spec = importlib.util.spec_from_file_location(
    "checks", pathlib.Path(__file__).with_name("verify_learning_tasks.py")
)
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)
BASE = "http://127.0.0.1:8090"


def call(path, token=None, method="GET", body=None, status=200):
    for attempt in range(30):
        code, result = checks.request(BASE, "/api/v1" + path, token, method, body)
        if code == 429:
            time.sleep(min(.05 * (attempt + 1), .5))
            continue
        assert code == status, f"{method} {path}: expected {status}, got {code}: {result}"
        return result
    raise AssertionError(f"{method} {path}: rate limit did not clear")


def write(path, token, body):
    return call(path, token, "POST", {"request_id": uuid.uuid4().hex, **body})


def percentile(values, fraction):
    ordered = sorted(values)
    return round(ordered[min(len(ordered) - 1, int((len(ordered) - 1) * fraction))], 2)


def main():
    suffix = uuid.uuid4().hex[:10]
    tokens = []
    for label in ("admin", "member"):
        username = f"load{label}{suffix}"
        call("/register", method="POST", body={"username": username, "password": "Test-Pass-2026!"})
        tokens.append(call("/login", method="POST", body={"username": username, "password": "Test-Pass-2026!"})["token"])
    admin, member = tokens
    group = write("/communities", admin, {"name": "本地业务工作量 " + suffix})["group_id"]
    write(f"/communities/{group}/join", member, {})

    def create_post(index):
        start = time.perf_counter()
        body = {"request_id": uuid.uuid4().hex, "kind": "note",
                "title": f"并发笔记 {index}", "body": f"业务工作量样本 {index}"}
        rejected = 0
        for attempt in range(30):
            code, response = checks.request(
                BASE, f"/api/v1/communities/{group}/posts", member, "POST", body
            )
            if code == 429:
                rejected += 1
                time.sleep(min(.05 * (attempt + 1), .5))
                continue
            assert code == 200, (code, response)
            return response["post_id"], (time.perf_counter() - start) * 1000, rejected
        raise AssertionError("post remained rate limited after backoff")

    started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        results = list(pool.map(create_post, range(100)))
    elapsed = time.perf_counter() - started
    ids = [item[0] for item in results]
    assert len(ids) == len(set(ids)) == 100

    seen = set()
    cursor = None
    pages = 0
    while True:
        path = f"/communities/{group}/posts"
        if cursor:
            path += "?cursor=" + cursor
        page = call(path, admin)
        page_ids = [item["post_id"] for item in page["posts"]]
        assert not seen.intersection(page_ids)
        seen.update(page_ids)
        pages += 1
        cursor = page.get("next_cursor")
        if not cursor:
            break
    assert set(ids).issubset(seen), f"only {len(set(ids) & seen)}/100 posts in feed"

    post = ids[0]
    comments = [write(f"/posts/{post}/comments", member, {"body": f"评论 {i}"})["comment_id"] for i in range(20)]
    visible = {item["comment_id"] for item in call(f"/posts/{post}/comments", admin)["comments"]}
    assert set(comments).issubset(visible)
    task = call(f"/groups/{group}/tasks", admin, "POST", {
        "request_id": uuid.uuid4().hex, "title": "100 帖工作量验收", "description": "确认流程", "due_at": 0
    }, 201)
    task_id = task["task_id"]
    write(f"/tasks/{task_id}/publish", admin, {"expected_version": 1})
    submitted = call(f"/tasks/{task_id}/submission", member, "PUT", {
        "request_id": uuid.uuid4().hex, "text": "提交完成", "expected_version": 0
    })
    reviewed = write(f"/submissions/{submitted['submission_id']}/review", admin, {
        "expected_version": submitted["version"], "decision": "approve"
    })
    assert reviewed["status"] == "approved"

    latencies = [item[1] for item in results]
    report = {
        "scope": "local Docker, two users, eight concurrent HTTP writers; not a production capacity result",
        "community_id": group,
        "post_requests": 100,
        "rate_limit_rejections_retried": sum(item[2] for item in results),
        "unique_persisted_posts": len(ids),
        "feed_pages": pages,
        "feed_contains_all_posts": True,
        "comments_persisted": len(comments),
        "task_approved": True,
        "post_wall_seconds": round(elapsed, 3),
        "post_requests_per_second": round(100 / elapsed, 2),
        "post_latency_ms": {
            "median": round(statistics.median(latencies), 2),
            "p95": percentile(latencies, .95),
            "max": round(max(latencies), 2),
        },
    }
    target = pathlib.Path("artifacts/community-workload.json")
    target.parent.mkdir(exist_ok=True)
    target.write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
