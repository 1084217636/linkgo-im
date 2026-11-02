#!/usr/bin/env python3
"""Exercise the IM-P1 API against a running local Docker stack.

Creates an isolated group and task; does not delete existing data.
"""

import argparse
import concurrent.futures
import json
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


def request(base, path, token=None, method="GET", body=None):
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    data = None
    if body is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(body, ensure_ascii=False).encode()
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=15) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as exc:
        return exc.code, json.load(exc)


def check(base, path, token=None, method="GET", body=None, status=200):
    code, result = request(base, path, token, method, body)
    assert code == status, f"{method} {path}: expected {status}, got {code}: {result}"
    return result


def rid():
    return "im-p1-" + uuid.uuid4().hex


def fixture_sql(statement):
    subprocess.run(
        ["docker", "exec", "linkgo-next-mysql", "mariadb", "-uroot", "-proot", "-e", statement],
        check=True, capture_output=True, text=True,
    )


def wait_history(base, token, target, marker, seconds=20):
    path = "/api/v1/history?" + urllib.parse.urlencode({"target_id": target, "limit": 50})
    until = time.monotonic() + seconds
    while time.monotonic() < until:
        result = check(base, path, token)
        messages = result.get("data") or []
        found = [msg for msg in messages if marker in (msg.get("body") or "")]
        if found:
            return found
        time.sleep(0.5)
    raise AssertionError(f"notification {marker} missing from {target} history")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default="http://127.0.0.1:8090")
    args = parser.parse_args()
    base = args.base.rstrip("/")
    tokens = {}
    for name in ("userA", "userB", "userC"):
        tokens[name] = check(base, "/api/v1/login", method="POST", body={"username": name, "password": "123456"})["token"]
    a, b, c = (tokens[name] for name in ("userA", "userB", "userC"))

    group_id = "GIMP1" + str(int(time.time())) + uuid.uuid4().hex[:5]
    check(base, "/api/v1/group/create", a, "POST", {"group_id": group_id, "name": "IM-P1 验收学习群", "members": ["1001", "1002"]})
    group_path = f"/api/v1/groups/{group_id}/tasks"
    create_id = rid()
    create = {"request_id": create_id, "title": "阅读并总结", "description": "提交三点学习笔记", "due_at": 0}
    task = check(base, group_path, a, "POST", create, 201)
    task_id = task["task_id"]
    path = f"/api/v1/tasks/{task_id}"
    assert check(base, group_path, a, "POST", create, 201)["task_id"] == task_id
    check(base, group_path, a, "POST", {**create, "title": "不同内容"}, 409)
    assert check(base, group_path, b)["tasks"] == []
    for token in (b, c):
        check(base, path, token, status=404)
    check(base, group_path, c, status=404)
    check(base, group_path, b, "POST", {**create, "request_id": rid()}, 404)

    publish = {"request_id": rid(), "expected_version": task["version"]}
    published = check(base, path + "/publish", a, "POST", publish)
    assert published["status"] == "published"
    assert check(base, path + "/publish", a, "POST", publish)["version"] == published["version"]
    assert any(t["task_id"] == task_id for t in check(base, group_path, b)["tasks"])
    wait_history(base, b, group_id, "task://" + task_id)
    print("PASS publish: durable group notification and task visibility")

    submission_id = rid()
    submit = {"request_id": submission_id, "text": "第一版总结", "link": "", "expected_version": 0}
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        duplicate_submits = list(pool.map(lambda _: request(base, path + "/submission", b, "PUT", submit), range(2)))
    assert [code for code, _ in duplicate_submits] == [200, 200], duplicate_submits
    assert duplicate_submits[0][1]["submission_id"] == duplicate_submits[1][1]["submission_id"]
    submitted = duplicate_submits[0][1]
    assert submitted["status"] == "submitted" and submitted["attempt_no"] == 1
    assert check(base, path + "/submission", b, "PUT", submit)["submission_id"] == submitted["submission_id"]
    check(base, path + "/submission", b, "PUT", {**submit, "request_id": rid()}, 409)
    check(base, path + "/submission", c, "PUT", {**submit, "request_id": rid()}, 404)
    check(base, path + "/submission", a, "PUT", {**submit, "request_id": rid()}, 404)
    check(base, path + "/submissions", b, status=404)
    assert check(base, path + "/progress", a)["submitted"] == 1
    check(base, path + "/close", a, "POST", {"request_id": rid(), "expected_version": published["version"]}, 409)
    assert check(base, path + "/submissions", a)["submissions"][0]["submission_id"] == submitted["submission_id"]
    wait_history(base, a, "9002", "task://" + task_id)
    print("PASS submit: idempotency, permissions, manager visibility")

    review_path = f"/api/v1/submissions/{submitted['submission_id']}/review"
    return_body = {"request_id": rid(), "expected_version": submitted["version"], "decision": "return", "reason": "请补充例子"}
    returned = check(base, review_path, a, "POST", return_body)
    assert returned["status"] == "returned"
    assert check(base, review_path, a, "POST", return_body)["version"] == returned["version"]
    returned_notice = wait_history(base, b, "9002", "审核退回：阅读并总结 task://" + task_id)
    assert "请补充例子" not in returned_notice[0]["body"]
    retry = {"request_id": rid(), "expected_version": returned["version"], "text": "补充例子的第二版总结", "link": ""}
    revised = check(base, path + "/submission", b, "PUT", retry)
    assert revised["attempt_no"] == 2 and revised["status"] == "submitted"
    stale_code, stale_body = request(base, path + "/submission", b, "PUT", {**retry, "request_id": rid()})
    assert stale_code == 409 and stale_body.get("current_version") == revised["version"], stale_body
    def race_review(_):
        return request(base, review_path, a, "POST", {"request_id": rid(), "expected_version": revised["version"], "decision": "approve"})
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        race = list(pool.map(race_review, range(2)))
    assert sorted(code for code, _ in race) == [200, 409], race
    winner = next(body for code, body in race if code == 200)
    assert winner["status"] == "approved"
    wait_history(base, b, "9002", "审核通过：阅读并总结 task://" + task_id)
    check(base, review_path, a, "POST", {"request_id": rid(), "expected_version": revised["version"], "decision": "approve"}, 409)
    assert check(base, path + "/progress", a)["approved"] == 1
    check(base, path + "/close", a, "POST", {"request_id": rid(), "expected_version": published["version"]})
    print("PASS concurrent review, return, resubmit, approve and close")

    assert check(base, path, b)["task"]["status"] == "closed"
    check(base, path + "/submission", b, "PUT", {"request_id": rid(), "text": "late"}, 409)
    # The history API still retrieves ordinary and task notification messages.
    assert wait_history(base, b, group_id, "task://" + task_id)
    # A successful request must remain replayable after its due_at passes.
    due = int(time.time() * 1000) + 1600
    timed = {"request_id": rid(), "title": "短截止草稿", "description": "验证跨时间幂等", "due_at": due}
    timed_task = check(base, group_path, a, "POST", timed, 201)
    time.sleep(1.8)
    assert check(base, group_path, a, "POST", timed, 201)["task_id"] == timed_task["task_id"]
    check(base, f"/api/v1/tasks/{timed_task['task_id']}/publish", a, "POST", {"request_id": rid(), "expected_version": 1}, 409)
    large = check(base, group_path, a, "POST", {"request_id": rid(), "title": "多字节正文", "description": "📝" * 5000, "due_at": 0}, 201)
    assert large["status"] == "draft"
    # Mutate only the isolated group created by this script.
    fixture_sql(f"UPDATE linkgo_im.group_members SET role='admin' WHERE group_id='{group_id}' AND user_id='1002'")
    admin_task = check(base, group_path, b, "POST", {"request_id": rid(), "title": "管理员权限", "description": "", "due_at": 0}, 201)
    assert admin_task["status"] == "draft"
    fixture_sql(f"UPDATE linkgo_im.group_members SET status='removed' WHERE group_id='{group_id}' AND user_id='1002'")
    check(base, group_path, b, status=404)
    check(base, path, b, status=404)
    check(base, path + "/progress", b, status=404)
    assert check(base, path + "/progress", a)["total"] == 0
    print("PASS expiry replay, admin role, removed-member access and progress denominator")
    print(f"PASS final: group={group_id} task={task_id}; history endpoint intact")


if __name__ == "__main__":
    main()
