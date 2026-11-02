// Learning-group task UI. MySQL-backed task APIs are authoritative; IM messages
// only prompt a refresh and never mutate task state in the browser.
(() => {
    const state = { request: null, upload: null, openAttachment: null, log: () => {}, getUserID: () => "", groupID: "", taskID: "", detail: null, items: [], nextCursor: "", groupGeneration: 0, pendingWrites: new Map(), reviewReasons: new Map(), submissionAttachments: [] };
    const $ = (id) => document.getElementById(id);
    const isManager = (role) => role === "owner" || role === "admin";
    const requestID = () => globalThis.crypto?.randomUUID?.() || `task-${Date.now()}-${Math.random().toString(16).slice(2)}`;

    function node(tag, text, className) {
        const el = document.createElement(tag);
        if (text !== undefined) el.textContent = text;
        if (className) el.className = className;
        return el;
    }
    function button(text, callback, className) {
        const el = node("button", text, className);
        el.type = "button";
        el.onclick = async () => {
            if (el.disabled) return;
            el.disabled = true;
            try { await run(callback); }
            finally { el.disabled = false; }
        };
        return el;
    }
    function info(message, error = false) {
        $("task-status").textContent = message;
        $("task-status").style.color = error ? "#b91c1c" : "";
        state.log(message);
    }
    async function run(fn) {
        try { await fn(); }
        catch (err) { info(err.message || String(err), true); }
    }
    async function write(key, path, method, body) {
        const signature = JSON.stringify([path, method, body]);
        const saved = state.pendingWrites.get(key);
        const pending = saved?.signature === signature ? saved : { signature, id: requestID() };
        state.pendingWrites.set(key, pending);
        const result = await state.request(path, {
            method, body: { request_id: pending.id, ...body }
        });
        state.pendingWrites.delete(key);
        return result;
    }
    function init({ request, upload, openAttachment, log, getUserID }) {
        state.request = request;
        state.upload = upload;
        state.openAttachment = openAttachment;
        state.log = log || state.log;
        state.getUserID = getUserID || state.getUserID;
        $("task-refresh-btn").onclick = () => run(refresh);
        $("task-new-btn").onclick = () => { $("task-create-form").hidden = !$("task-create-form").hidden; };
        $("task-create-btn").onclick = async () => {
            const el = $("task-create-btn");
            if (el.disabled) return;
            el.disabled = true;
            try { await run(create); }
            finally { el.disabled = false; }
        };
    }
    function reset() {
        state.groupGeneration++;
        state.pendingWrites.clear();
        state.reviewReasons.clear();
        state.submissionAttachments = [];
        state.groupID = ""; state.taskID = ""; state.detail = null; state.items = []; state.nextCursor = "";
        $("task-panel").hidden = true;
        $("task-list").replaceChildren();
        $("task-detail").replaceChildren();
    }
    async function setGroup(groupID) {
        const generation = ++state.groupGeneration;
        state.groupID = String(groupID || "").replace(/^group:/, "");
        state.taskID = ""; state.detail = null;
        $("task-detail").replaceChildren();
        $("task-panel").hidden = !state.groupID;
        if (!state.groupID) return;
        $("task-group-name").textContent = state.groupID;
        $("task-create-form").hidden = true;
        $("task-new-btn").hidden = true;
        await run(async () => {
            const data = await state.request(`/api/v1/group/members?group_id=${encodeURIComponent(state.groupID)}`);
            if (generation !== state.groupGeneration) return;
            const me = (data.members || []).find((row) => row.user_id === state.getUserID());
            $("task-new-btn").hidden = !isManager(me?.role);
            await refresh();
        });
    }
    async function refresh() {
        if (!state.groupID) return;
        const generation = state.groupGeneration;
        const groupID = state.groupID;
        const data = await state.request(`/api/v1/groups/${encodeURIComponent(groupID)}/tasks?limit=50`);
        if (generation !== state.groupGeneration) return;
        state.items = data.tasks || [];
        state.nextCursor = data.next_cursor || "";
        renderTaskList();
        info(`已加载 ${state.items.length} 个任务`);
        if (state.taskID) await open(state.taskID);
    }
    function renderTaskList() {
        const list = $("task-list"); list.replaceChildren();
        if (!state.items.length) list.append(node("div", "这个群还没有学习任务", "task-muted"));
        for (const task of state.items) {
            const row = node("div", undefined, "task-card");
            row.append(node("strong", task.title));
            row.append(node("div", `状态：${task.status} · 截止：${task.due_at ? new Date(task.due_at).toLocaleString() : "不限"}`, "task-muted"));
            row.append(button("打开任务", () => open(task.task_id)));
            list.append(row);
        }
        if (state.nextCursor) list.append(button("加载更多任务", async () => {
            const data = await state.request(`/api/v1/groups/${encodeURIComponent(state.groupID)}/tasks?limit=50&cursor=${encodeURIComponent(state.nextCursor)}`);
            state.items.push(...(data.tasks || []));
            state.nextCursor = data.next_cursor || "";
            renderTaskList();
        }));
    }
    async function create() {
        if (!state.groupID) throw new Error("请先打开群聊");
        const title = $("task-title").value.trim();
        const description = $("task-description").value;
        const dueValue = $("task-due-at").value;
        const due_at = dueValue ? new Date(dueValue).getTime() : 0;
        const task = await write(`create:${state.groupID}`, `/api/v1/groups/${encodeURIComponent(state.groupID)}/tasks`, "POST", { title, description, due_at });
        $("task-create-form").hidden = true;
        $("task-title").value = ""; $("task-description").value = ""; $("task-due-at").value = "";
        await refresh(); await open(task.task_id);
        info("草稿已保存；点击“发布任务”通知群成员");
    }
    async function writeTask(action) {
        const t = state.detail?.task;
        if (!t) return;
        await write(`${action}:${t.task_id}`, `/api/v1/tasks/${encodeURIComponent(t.task_id)}/${action}`, "POST", { expected_version: t.version });
        await refresh();
        info(action === "publish" ? "已发布；群消息通知会异步送达" : "任务已关闭");
    }
    async function editDraft(title, description, due) {
        const t = state.detail?.task;
        if (!t || t.status !== "draft") return;
        await write(`edit:${t.task_id}`, `/api/v1/tasks/${encodeURIComponent(t.task_id)}`, "PATCH", {
            expected_version: t.version,
            title: title.value.trim(), description: description.value,
            due_at: due.value ? new Date(due.value).getTime() : 0
        });
        await refresh();
        info("草稿已更新");
    }
    async function submit() {
        const t = state.detail?.task;
        if (!t) return;
        await write(`submit:${t.task_id}`, `/api/v1/tasks/${encodeURIComponent(t.task_id)}/submission`, "PUT", {
            expected_version: state.detail.my_submission?.version || 0,
            text: $("task-submit-text").value,
            link: $("task-submit-link").value.trim(),
            attachment_ids: state.submissionAttachments.map((item) => item.attachment_id)
        });
        await open(t.task_id);
        info("提交成功；管理员可审核");
    }
    async function review(submission, decision, reason) {
        await write(`review:${submission.submission_id}`, `/api/v1/submissions/${encodeURIComponent(submission.submission_id)}/review`, "POST", {
            expected_version: submission.version, decision, reason
        });
        state.reviewReasons.delete(submission.submission_id);
        await open(submission.task_id);
        info(decision === "approve" ? "已通过" : "已退回；成员会收到提醒");
    }
    async function open(taskID) {
        const detail = await state.request(`/api/v1/tasks/${encodeURIComponent(taskID)}`);
        state.groupID = detail.task.group_id;
        state.taskID = taskID; state.detail = detail;
        $("task-panel").hidden = false;
        $("task-panel").open = true;
        $("task-group-name").textContent = state.groupID;
        const box = $("task-detail"); box.replaceChildren();
        const t = detail.task;
        const card = node("div", undefined, "task-card");
        card.append(node("h3", t.title));
        card.append(node("div", t.description || "（无说明）"));
        card.append(node("div", `状态：${t.status} · 截止：${t.due_at ? new Date(t.due_at).toLocaleString() : "不限"}`, "task-muted"));
        if (isManager(detail.role)) {
            if (t.status === "draft") {
                const title = node("input"); title.value = t.title; title.placeholder = "任务标题";
                const description = node("textarea"); description.value = t.description; description.placeholder = "任务说明";
                const due = node("input"); due.type = "datetime-local";
                if (t.due_at) {
                    const local = new Date(t.due_at - new Date(t.due_at).getTimezoneOffset() * 60000);
                    due.value = local.toISOString().slice(0, 16);
                }
                card.append(title, description, due);
                card.append(button("保存修改", () => editDraft(title, description, due)));
                card.append(button("发布任务", () => writeTask("publish"), "primary"));
            }
            if (t.status === "published") card.append(button("关闭任务", () => writeTask("close")));
            if (t.status !== "draft") {
                const progress = await state.request(`/api/v1/tasks/${encodeURIComponent(taskID)}/progress`);
                card.append(node("div", `进度：总计 ${progress.total} · 未提交 ${progress.not_submitted} · 待审核 ${progress.submitted} · 退回 ${progress.returned} · 通过 ${progress.approved}`));
                const submissions = node("div"); card.append(submissions);
                const loadSubmissions = async (cursor = "") => {
                    const rows = await state.request(`/api/v1/tasks/${encodeURIComponent(taskID)}/submissions?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`);
                    for (const submission of rows.submissions || []) {
                    const sub = node("div", undefined, "task-card");
                    sub.append(node("strong", `${submission.user_id} · 第 ${submission.attempt_no} 次 · ${submission.status}`));
                    sub.append(node("div", submission.text || "（仅链接）"));
                    for (const attachmentID of submission.attachment_ids || []) {
                        sub.append(button(`查看附件 ${attachmentID.slice(0, 8)}`, () => state.openAttachment(attachmentID, attachmentID, "application/octet-stream")));
                    }
                    if (submission.link) {
                        const link = node("a", submission.link); link.href = submission.link; link.target = "_blank"; link.rel = "noopener noreferrer"; sub.append(link);
                    }
                    if (submission.status === "submitted" && t.status === "published") {
                        const reason = node("input"); reason.placeholder = "退回理由（退回时必填）";
                        reason.value = state.reviewReasons.get(submission.submission_id) || "";
                        reason.oninput = () => state.reviewReasons.set(submission.submission_id, reason.value);
                        sub.append(reason);
                        sub.append(button("通过", () => review(submission, "approve", ""), "primary"));
                        sub.append(button("退回", () => review(submission, "return", reason.value)));
                    }
                        submissions.append(sub);
                    }
                    if (rows.next_cursor) {
                        const more = button("加载更多提交", async () => { more.remove(); await loadSubmissions(rows.next_cursor); });
                        submissions.append(more);
                    }
                };
                await loadSubmissions();
            }
        } else {
            const own = detail.my_submission;
            state.submissionAttachments = (own?.attachment_ids || []).map((attachment_id) => ({ attachment_id }));
            card.append(node("div", own ? `我的提交：${own.status} · 第 ${own.attempt_no} 次${own.review_note ? ` · 退回理由：${own.review_note}` : ""}` : "尚未提交"));
            for (const attachment of state.submissionAttachments) {
                card.append(button(`查看附件 ${attachment.attachment_id.slice(0, 8)}`, () => state.openAttachment(attachment.attachment_id, attachment.file_name || attachment.attachment_id, attachment.mime_type || "application/octet-stream")));
            }
            if (t.status === "published" && (!t.due_at || Date.now() < t.due_at) && (!own || own.status === "returned")) {
                const text = node("textarea"); text.id = "task-submit-text"; text.placeholder = "完成内容"; text.value = own?.text || "";
                const link = node("input"); link.id = "task-submit-link"; link.placeholder = "相关链接（可选）"; link.value = own?.link || "";
                const file = node("input"); file.type = "file"; file.accept = "image/*,video/mp4,video/webm,.pdf,.txt,.md,.csv,.docx,.xlsx,.pptx";
                file.onchange = async () => {
                    const chosen = file.files?.[0];
                    if (!chosen) return;
                    try {
                        const uploaded = await state.upload(state.groupID, chosen);
                        state.submissionAttachments.push(uploaded);
                        card.append(button(`已添加：${uploaded.file_name}`, () => state.openAttachment(uploaded.attachment_id, uploaded.file_name, uploaded.mime_type)));
                        info(`附件已上传：${uploaded.file_name}`);
                    } catch (error) { info(error.message || String(error), true); }
                };
                card.append(text, link, node("label", "添加提交附件"), file, button(own ? "修改后重交" : "提交任务", submit, "primary"));
            }
        }
        box.append(card);
    }
    async function openFromMessage(body) {
        const match = String(body || "").match(/task:\/\/([a-zA-Z0-9_-]+)/);
        if (!match) return;
        await run(async () => { await open(match[1]); await refresh(); });
    }
    function onMessage(body) {
        if (!String(body || "").includes("task://")) return;
        if (state.groupID) void run(refresh);
    }
    window.LinkGoTasks = { init, reset, setGroup, openFromMessage, onMessage };
})();
