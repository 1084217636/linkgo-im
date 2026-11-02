#!/usr/bin/env python3
"""Verify task state commits while Logic is stopped, then notifications recover.

Run only against the local Docker demo stack. Both Logic containers are
restarted in a finally block even when an assertion fails.
"""

import argparse
import json
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

COMPOSE = ["docker", "compose", "--env-file", ".env.docker-cn"]
BASE = "http://127.0.0.1:8090"


def api(path, token=None, method="GET", body=None, expected=200):
    headers = {}
    if token:
        headers["Authorization"] = "Bearer " + token
    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            status, result = response.status, json.load(response)
    except urllib.error.HTTPError as exc:
        if exc.code >= 500:
            raise urllib.error.URLError(f"temporary upstream status {exc.code}") from exc
        status, result = exc.code, json.load(exc)
    assert status == expected, f"{path}: {status}, {result}"
    return result


def sql(query):
    run = subprocess.run(
        ["docker", "exec", "linkgo-next-mysql", "mariadb", "-uroot", "-proot", "-N", "-e", query],
        check=True, capture_output=True, text=True,
    )
    return run.stdout.strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--stop-service", choices=("logic", "redis"), default="logic")
    args = parser.parse_args()
    stopped = ["logic", "logic-b"] if args.stop_service == "logic" else ["redis"]
    a = api("/api/v1/login", method="POST", body={"username": "userA", "password": "123456"})["token"]
    b = api("/api/v1/login", method="POST", body={"username": "userB", "password": "123456"})["token"]
    group = "GIMP1R" + str(int(time.time())) + uuid.uuid4().hex[:5]
    api("/api/v1/group/create", a, "POST", {"group_id": group, "name": "通知恢复验收群", "members": ["1001", "1002"]})
    path = f"/api/v1/groups/{group}/tasks"
    task = api(path, a, "POST", {"request_id": uuid.uuid4().hex, "title": "断通知仍可发布", "description": "恢复后通知", "due_at": 0}, 201)
    task_path = f"/api/v1/tasks/{task['task_id']}"
    subprocess.run(COMPOSE + ["stop", *stopped], check=True)
    try:
        published = api(task_path + "/publish", a, "POST", {"request_id": uuid.uuid4().hex, "expected_version": 1})
        assert published["status"] == "published"
        assert api(task_path, b)["task"]["status"] == "published"
        if args.stop_service == "redis":
            time.sleep(12)  # let the worker attempt Redis and persist its failure
        pending = sql("SELECT COUNT(*) FROM linkgo_im.task_notification_outbox WHERE event_id IN (SELECT event_id FROM linkgo_im.task_events WHERE task_id='%s' AND action='publish') AND status IN ('pending','processing')" % task["task_id"])
        assert pending == "1", f"expected one unfinished notification, got {pending}"
        if args.stop_service == "redis":
            failed = sql("SELECT COUNT(*) FROM linkgo_im.task_notification_outbox WHERE event_id IN (SELECT event_id FROM linkgo_im.task_events WHERE task_id='%s' AND action='publish') AND attempts>=1 AND last_error<>''" % task["task_id"])
            assert failed == "1", "notification worker did not record an external failure"
        print(f"PASS while {args.stop_service} stopped: published state committed, one notification unfinished")
    finally:
        subprocess.run(COMPOSE + ["start", *stopped], check=True)
    history_path = "/api/v1/history?" + urllib.parse.urlencode({"target_id": group, "limit": 50})
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        try:
            messages = api(history_path, b).get("data") or []
            matching = [m for m in messages if "task://" + task["task_id"] in (m.get("body") or "")]
            if matching:
                assert len(matching) == 1, f"duplicate persistent IM messages: {matching}"
                done = sql("SELECT COUNT(*) FROM linkgo_im.task_notification_outbox WHERE event_id IN (SELECT event_id FROM linkgo_im.task_events WHERE task_id='%s' AND action='publish') AND status='done'" % task["task_id"])
                if done == "1":
                    print(f"PASS recovery: exactly one durable group notification; group={group} task={task['task_id']}")
                    return
        except urllib.error.URLError:
            pass
        time.sleep(0.5)
    raise AssertionError("notification did not recover within 35 seconds")


if __name__ == "__main__":
    main()
