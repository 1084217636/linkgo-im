import React, { useState, useEffect, useRef } from "react";
import { createRoot } from "react-dom/client";
import { encode, decode, Wire } from "./wire";
import "./style.css";
declare global {
  interface Window {
    LinkGoStore: any;
  }
}
type Group = {
  group_id: string;
  name: string;
  description: string;
  role: string;
  members: number;
  owner_id: string;
};
type Post = {
  post_id: string;
  group_id: string;
  author_id: string;
  title: string;
  body: string;
  kind: string;
  version: number;
  likes: number;
  liked: boolean;
  pinned: boolean;
  featured: boolean;
  status: string;
  attachments: any[];
  created_at: number;
  comments: number;
};
type Friend = { user_id: string; username?: string; status: string };
type FriendRequest = { from_user_id: string; message: string; status: string };
const id = () => crypto.randomUUID();
const date = (n: number) => new Date(n).toLocaleString();
const names = {
  question: "提问",
  note: "笔记",
  resource: "资料",
  announcement: "公告",
};
function App() {
  const [token, setToken] = useState(""),
    [user, setUser] = useState(""),
    [username, setUsername] = useState(""),
    [password, setPassword] = useState("");
  const [groups, setGroups] = useState<Group[]>([]),
    [mine, setMine] = useState(true),
    [group, setGroup] = useState<Group | null>(null),
    [tab, setTab] = useState("动态");
  const [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false),
    [posts, setPosts] = useState<Post[]>([]),
    [cursor, setCursor] = useState(""),
    [filter, setFilter] = useState("");
  const [post, setPost] = useState<Post | null>(null),
    [comments, setComments] = useState<any[]>([]),
    [reply, setReply] = useState(""),
    [parent, setParent] = useState("");
  const [title, setTitle] = useState(""),
    [body, setBody] = useState(""),
    [kind, setKind] = useState("note"),
    [files, setFiles] = useState<any[]>([]),
    [editing, setEditing] = useState(false);
  const [notifications, setNotifications] = useState<any[]>([]),
    [wsState, setWsState] = useState("未连接"),
    [messages, setMessages] = useState<Wire[]>([]),
    [chat, setChat] = useState(""),
    [older, setOlder] = useState<number | undefined>();
  const [friends, setFriends] = useState<Friend[]>([]),
    [friendRequests, setFriendRequests] = useState<FriendRequest[]>([]),
    [peer, setPeer] = useState<Friend | null>(null),
    [privateUnread, setPrivateUnread] = useState<Record<string, number>>({}),
    [memberNames, setMemberNames] = useState<Record<string, string>>({});
  const ws = useRef<WebSocket>(),
    current = useRef<Group | null>(group),
    currentPeer = useRef<Friend | null>(peer),
    currentTab = useRef(tab),
    generation = useRef(0),
    pending = useRef(new Map<string, string>()),
    historySeq = useRef(new Map<string, number>());
  current.current = group;
  currentPeer.current = peer;
  currentTab.current = tab;
  function privateSession(a: string, b: string) {
    return "c2c:" + [a, b].sort().join(":");
  }
  async function api(path: string, method = "GET", value?: any) {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 15000);
    try {
      const r = await fetch("/api/v1" + path, {
        method,
        headers: {
          ...(token ? { Authorization: "Bearer " + token } : {}),
          ...(value ? { "Content-Type": "application/json" } : {}),
        },
        body: value ? JSON.stringify(value) : undefined,
        signal: ctrl.signal,
      });
      const text = await r.text();
      let data: any;
      try {
        data = JSON.parse(text);
      } catch {
        data = { error: text };
      }
      if (!r.ok) throw Error(data.error || `请求失败 ${r.status}`);
      return data;
    } finally {
      clearTimeout(timer);
    }
  }
  async function write(path: string, value: any = {}, method = "POST") {
    const signature = JSON.stringify([path, method, value]);
    const key = pending.current.get(signature) || id();
    pending.current.set(signature, key);
    const result = await api(path, method, { ...value, request_id: key });
    pending.current.delete(signature);
    return result;
  }
  async function run(fn: () => Promise<any>) {
    if (busy) return;
    setBusy(true);
    setNotice("");
    try {
      await fn();
    } catch (e) {
      setNotice(String(e.message || e));
    } finally {
      setBusy(false);
    }
  }
  async function list() {
    const data = await api("/communities" + (mine ? "?mine=1" : ""));
    setGroups(data.communities);
  }
  async function loadFriends() {
    const [f, r] = await Promise.all([api("/friends"), api("/friend/requests")]);
    setFriends(f.friends || []);
    setFriendRequests(r.requests || []);
  }
  async function loadPrivateUnread() {
    const data = await api("/private/unread");
    setPrivateUnread(data.unread || {});
  }
  useEffect(() => {
    if (token) list().catch((e) => setNotice(e.message));
  }, [token, mine]);
  useEffect(() => {
    if (token) loadFriends().catch((e) => setNotice(e.message));
  }, [token]);
  useEffect(() => {
    if (!token) return;
    loadPrivateUnread().catch((e) => setNotice(e.message));
    const timer = setInterval(() => loadPrivateUnread().catch(() => {}), 8000);
    return () => clearInterval(timer);
  }, [token]);
  async function auth(register = false) {
    if (register) {
      await api("/register", "POST", { username, password });
      setNotice("注册成功，请登录");
      return;
    }
    const data = await api("/login", "POST", { username, password });
    setToken(data.token);
    setUser(data.user_id);
    setPassword("");
  }
  function logout() {
    setToken("");
    setUser("");
    setGroup(null);
    setPeer(null);
    setFriends([]);
    setFriendRequests([]);
    setPrivateUnread({});
    setMemberNames({});
    setNotifications([]);
    pending.current.clear();
    historySeq.current.clear();
  }
  async function select(g: Group) {
    generation.current++;
    setGroup(g);
    setPeer(null);
    setPost(null);
    setPosts([]);
    setFiles([]);
    setMessages([]);
    setTab("动态");
    setTitle("");
    setBody("");
    setEditing(false);
    setParent("");
  }
  async function feed(append = false) {
    if (!group) return;
    const gen = generation.current;
    const q = new URLSearchParams();
    if (append && cursor) q.set("cursor", cursor);
    if (filter === "resource") q.set("kind", "resource");
    else if (filter) q.set("filter", filter);
    const data = await api(`/communities/${group.group_id}/posts?${q}`);
    if (gen !== generation.current) return;
    setPosts((old) => (append ? [...old, ...data.posts] : data.posts));
    setCursor(data.next_cursor);
  }
  useEffect(() => {
    if (group && tab === "动态") feed().catch((e) => setNotice(e.message));
  }, [group, filter, tab]);
  async function openPost(pid: string) {
    const gen = generation.current;
    const p = await api("/posts/" + pid);
    const c = await api("/posts/" + pid + "/comments");
    if (gen !== generation.current) return;
    setPost(p);
    setComments(c.comments);
    setReply("");
    setParent("");
    setEditing(false);
    setTab("动态");
  }
  async function upload(file: File) {
    if (!group) throw Error("请先选择社区");
    const fd = new FormData();
    fd.append("file", file);
    const r = await fetch(`/api/v1/groups/${group.group_id}/attachments`, {
      method: "POST",
      headers: { Authorization: "Bearer " + token },
      body: fd,
    });
    const text = await r.text();
    if (!r.ok) throw Error(text);
    return JSON.parse(text);
  }
  async function download(a: any) {
    const r = await fetch(
      "/api/v1/attachments/" + a.attachment_id + "/download",
      { headers: { Authorization: "Bearer " + token } },
    );
    if (!r.ok) throw Error("文件不可访问");
    const url = URL.createObjectURL(await r.blob());
    const el = document.createElement("a");
    el.href = url;
    el.download = a.file_name;
    el.click();
    setTimeout(() => URL.revokeObjectURL(url), 10000);
  }
  async function loadNotices() {
    let after = 0,
      all: any[] = [];
    for (let i = 0; i < 20; i++) {
      const data = await api("/notifications?after=" + after);
      all.push(...data.notifications);
      after = data.next_cursor;
      if (data.notifications.length < 100) break;
    }
    setNotifications(all.slice(-200).reverse());
  }
  useEffect(() => {
    if (!token) return;
    loadNotices().catch(() => {});
    const timer = setInterval(() => loadNotices().catch(() => {}), 10000);
    return () => clearInterval(timer);
  }, [token]);
  // One authenticated connection per browser account; reconnect uses a fresh,
  // one-use ticket. Historical ranges are filled over HTTP, live messages merge
  // by message_id and sort by conversation seq.
  useEffect(() => {
    if (!token) return;
    let stopped = false,
      timer: any,
      heartbeat: any,
      attempt = 0;
    async function connect() {
      try {
        setWsState("连接中");
        const issued = await api("/ws-ticket", "POST");
        if (stopped) return;
        const socket = new WebSocket(
          `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/ws?ticket=${encodeURIComponent(issued.ticket)}`,
        );
        socket.binaryType = "arraybuffer";
        ws.current = socket;
        socket.onopen = () => {
          if (stopped) {
            socket.close();
            return;
          }
          attempt = 0;
          setWsState("已连接");
          heartbeat = setInterval(() => {
            if (socket.readyState === 1)
              socket.send(encode({ msg_type: 1, sent_at: Date.now() }));
          }, 20000);
          const g = current.current;
          if (g) loadHistory(g, false).catch((e) => setNotice(e.message));
          const p = currentPeer.current;
          if (p) loadDirectHistory(p, false).catch((e) => setNotice(e.message));
        };
        socket.onmessage = async (ev) => {
          try {
            if (typeof ev.data === "string") {
              if (JSON.parse(ev.data).type === "community_notice")
                await loadNotices();
              return;
            }
            const m = decode(ev.data);
            if (m.msg_type === 1) return;
            if (m.msg_type === 3) {
              try {
                const result = JSON.parse(m.body || "{}");
                if (result.code === "MESSAGE_ACCEPTED") {
                  await window.LinkGoStore.updateOutgoingStatus(
                    user,
                    m.client_msg_id,
                    "accepted",
                  );
                  if (currentPeer.current)
                    await loadDirectHistory(currentPeer.current, false);
                  else if (current.current)
                    await loadHistory(current.current, false);
                } else if (result.type === "error") {
                  await window.LinkGoStore.updateOutgoingStatus(
                    user,
                    m.client_msg_id,
                    "rejected",
                  );
                  setNotice("消息发送失败，请检查权限或稍后重试");
                }
              } catch {}
              return;
            }
            if (!m.message_id) return;
            const sid = m.session_id || "group:" + m.to;
            await window.LinkGoStore.putMessage(user, sid, m, "received");
            if (socket.readyState === 1)
              socket.send(
                encode({
                  msg_type: 2,
                  ack_message_id: m.message_id,
                  sent_at: Date.now(),
                }),
              );
            if (m.to_type === "group" && m.to === current.current?.group_id)
              mergeMessages([m]);
            if (m.to_type !== "group" && currentTab.current === "私聊" && currentPeer.current &&
                (m.from === currentPeer.current.user_id ||
                  (m.from === user && m.to === currentPeer.current.user_id)))
              mergeMessages([m]);
            if (m.to_type !== "group" && m.from !== user) {
              if (currentTab.current === "私聊" && currentPeer.current?.user_id === m.from && m.seq)
                await markDirectRead(currentPeer.current, m.seq);
              else
                await loadPrivateUnread();
            }
          } catch (e) {
            setNotice("接收消息失败：" + e.message);
          }
        };
        socket.onclose = () => {
          clearInterval(heartbeat);
          if (!stopped) {
            setWsState("已断开，等待重连");
            timer = setTimeout(connect, Math.min(1000 * 2 ** attempt++, 15000));
          }
        };
        socket.onerror = () => socket.close();
      } catch (e) {
        if (!stopped) {
          setWsState("连接失败，等待重连");
          timer = setTimeout(connect, Math.min(1000 * 2 ** attempt++, 15000));
        }
      }
    }
    connect();
    return () => {
      stopped = true;
      clearTimeout(timer);
      clearInterval(heartbeat);
      ws.current?.close();
    };
  }, [token]);
  function mergeMessages(items: Wire[]) {
    setMessages((old) => {
      const all = new Map(old.map((m) => [m.message_id || m.client_msg_id, m]));
      for (const m of items) {
        for (const [k, v] of all) {
          if (m.client_msg_id && v.client_msg_id === m.client_msg_id)
            all.delete(k);
        }
        all.set(m.message_id || m.client_msg_id, m);
      }
      return [...all.values()].sort(
        (a, b) =>
          (a.seq || Number.MAX_SAFE_INTEGER) -
          (b.seq || Number.MAX_SAFE_INTEGER),
      );
    });
  }
  async function loadHistory(g: Group, previous: boolean) {
    const gen = generation.current;
    const q = new URLSearchParams({
      target_id: "group:" + g.group_id,
      limit: "50",
    });
    if (previous && older) q.set("before_seq", String(older));
    const data = await api("/history?" + q);
    if (gen !== generation.current) return;
    const items = data.data || [];
    for (const m of items)
      await window.LinkGoStore.putMessage(
        user,
        "group:" + g.group_id,
        m,
        "received",
      );
    mergeMessages(items);
    setOlder(data.has_more ? data.next_before_seq : undefined);
    if (items.length)
      historySeq.current.set(
        g.group_id,
        Math.max(...items.map((m) => m.seq || 0)),
      );
  }
  async function loadDirectHistory(f: Friend, previous: boolean) {
    const gen = generation.current;
    const sid = privateSession(user, f.user_id);
    const q = new URLSearchParams({ target_id: f.user_id, limit: "50" });
    if (previous && older) q.set("before_seq", String(older));
    const data = await api("/history?" + q);
    if (gen !== generation.current || currentPeer.current?.user_id !== f.user_id) return;
    const items: Wire[] = data.data || [];
    for (const m of items)
      await window.LinkGoStore.putMessage(user, sid, m, "received");
    mergeMessages(items);
    setOlder(data.has_more ? data.next_before_seq : undefined);
    if (!previous && currentTab.current === "私聊" && items.length) {
      const latest = Math.max(...items.map((m) => m.seq || 0));
      if (latest > 0) await markDirectRead(f, latest);
    }
  }
  async function markDirectRead(f: Friend, throughSeq: number) {
    if (throughSeq <= 0) return;
    await api("/private/read", "POST", { peer_user_id: f.user_id, through_seq: throughSeq });
    await loadPrivateUnread();
  }
  useEffect(() => {
    if (!group || tab !== "聊天") return;
    let cancelled = false;
    setMemberNames({});
    api("/group/members?group_id=" + encodeURIComponent(group.group_id))
      .then((data) => {
        if (!cancelled)
          setMemberNames(Object.fromEntries((data.members || []).map((m: any) => [m.user_id, m.username])));
      })
      .catch((e) => setNotice(e.message));
    return () => { cancelled = true; };
  }, [group, tab]);
  useEffect(() => {
    if (!group || tab !== "聊天") return;
    const gen = generation.current;
    setMessages([]);
    setOlder(undefined);
    window.LinkGoStore.listMessages(user, "group:" + group.group_id)
      .then((items) => {
        if (gen === generation.current) mergeMessages(items);
      })
      .catch((e) => setNotice(e.message));
    loadHistory(group, false).catch((e) => setNotice(e.message));
  }, [group, tab]);
  useEffect(() => {
    if (!peer || tab !== "私聊") return;
    const gen = generation.current;
    setMessages([]);
    setOlder(undefined);
    window.LinkGoStore.listMessages(user, privateSession(user, peer.user_id))
      .then((items) => {
        if (gen === generation.current) mergeMessages(items);
      })
      .catch((e) => setNotice(e.message));
    loadDirectHistory(peer, false).catch((e) => setNotice(e.message));
  }, [peer, tab]);
  async function sendChat(text: string) {
    const direct = tab === "私聊" && peer;
    if ((!group && !direct) || ws.current?.readyState !== 1)
      throw Error("聊天连接尚未就绪");
    const m: Wire = {
      from: user,
      to: direct ? peer.user_id : group!.group_id,
      to_type: direct ? "user" : "group",
      session_id: direct ? privateSession(user, peer.user_id) : "group:" + group!.group_id,
      body: text,
      client_msg_id: id(),
      msg_type: 0,
      sent_at: Date.now(),
    };
    await window.LinkGoStore.putMessage(user, m.session_id, m, "pending");
    mergeMessages([{ ...m, status: "pending" }]);
    ws.current.send(encode(m));
    setChat("");
    await new Promise((r) => setTimeout(r, 300));
    if (direct) await loadDirectHistory(peer, false);
    else await loadHistory(group!, false);
  }
  const privateUnreadTotal = Object.values(privateUnread).reduce((sum, count) => sum + count, 0);
  const notificationUnreadTotal = notifications.filter((n) => !n.read_at).length;
  const isManager = group?.role === "owner" || group?.role === "admin";
  if (!token)
    return (
      <main className="auth">
        <h1>LinkGo 学习社区</h1>
        <p>在社区里沉淀笔记、讨论问题，一起完成学习任务。</p>
        <label>
          用户名
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
          />
        </label>
        <label>
          密码
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </label>
        <div className="row">
          <button disabled={busy} onClick={() => run(() => auth())}>
            登录
          </button>
          <button disabled={busy} onClick={() => run(() => auth(true))}>
            注册账号
          </button>
        </div>
        <p role="status">{notice}</p>
        <small>注册密码至少8字节。登录凭证只保存在当前页面内存。</small>
      </main>
    );
  return (
    <>
      <header>
        <div>
          <strong>LinkGo 学习社区</strong>
          <small>记录 · 讨论 · 协作</small>
        </div>
        <span>
          {username} {privateUnreadTotal > 0 && <span className="badge">私聊 {privateUnreadTotal}</span>} · {wsState}
        </span>
        <button
          onClick={() => {
            generation.current++;
            setTab("私聊");
            setMessages([]);
            setOlder(undefined);
            loadFriends().catch((e) => setNotice(e.message));
          }}
        >
          好友私聊 {privateUnreadTotal > 0 && <span className="badge">{privateUnreadTotal}</span>} {friendRequests.length ? `(${friendRequests.length} 条申请)` : ""}
        </button>
        <button
          onClick={() => {
            setTab("通知");
            loadNotices().catch((e) => setNotice(e.message));
          }}
        >
          通知 {notificationUnreadTotal > 0 && <span className="badge">{notificationUnreadTotal}</span>}
        </button>
        <button onClick={logout}>退出登录</button>
      </header>
      <div className="layout">
        <aside>
          <div className="row">
            <button
              onClick={() => setMine(true)}
              className={mine ? "selected" : ""}
            >
              我的社区
            </button>
            <button
              onClick={() => setMine(false)}
              className={!mine ? "selected" : ""}
            >
              发现社区
            </button>
          </div>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const data = new FormData(e.currentTarget);
              run(async () => {
                const g = await write("/communities", {
                  name: data.get("name"),
                  description: data.get("description"),
                });
                await list();
                await select(g);
              });
            }}
          >
            <h3>创建社区</h3>
            <input name="name" placeholder="社区名称" required maxLength={80} />
            <input name="description" placeholder="社区简介" maxLength={1000} />
            <button disabled={busy}>创建</button>
          </form>
          <div className="groups">
            {groups.map((g) => (
              <button
                className={
                  "group " + (g.group_id === group?.group_id ? "selected" : "")
                }
                key={g.group_id}
                onClick={() =>
                  g.role
                    ? run(() => select(g))
                    : run(async () => {
                        await write(`/communities/${g.group_id}/join`);
                        await list();
                        await select({ ...g, role: "member" });
                      })
                }
              >
                <strong>{g.name}</strong>
                <small>
                  {g.members} 位成员 · {g.role || "点击加入"}
                </small>
              </button>
            ))}
          </div>
        </aside>
        <main>
          <p role="status" className="status">
            {notice}
          </p>
          {tab === "私聊" ? (
            <section>
              <h2>好友私聊</h2>
              <p>从社区成员列表申请好友，对方接受后可在这里私聊。</p>
              <button onClick={() => run(loadFriends)}>刷新好友和申请</button>
              {friendRequests.map((request) => (
                <article key={request.from_user_id}>
                  <strong>好友申请：{request.from_user_id}</strong>
                  <p>{request.message}</p>
                  <button onClick={() => run(async () => {
                    await api("/friend/respond", "POST", { from_user_id: request.from_user_id, action: "accept" });
                    await loadFriends();
                  })}>接受</button>
                  <button onClick={() => run(async () => {
                    await api("/friend/respond", "POST", { from_user_id: request.from_user_id, action: "reject" });
                    await loadFriends();
                  })}>拒绝</button>
                </article>
              ))}
              <div className="row">
                {friends.map((f) => <button key={f.user_id}
                  className={peer?.user_id === f.user_id ? "selected" : ""}
                  onClick={() => {
                    generation.current++;
                    setPeer(f);
                    setMessages([]);
                    setOlder(undefined);
                  }}>{f.username || f.user_id} {(privateUnread[f.user_id] || 0) > 0 && <span className="badge">{privateUnread[f.user_id]}</span>}</button>)}
              </div>
              {peer ? <>
                <h3>与 {peer.username || peer.user_id} 对话</h3>
                <small>{wsState} · 收到消息先存本地再确认</small>
                {older && <button onClick={() => run(() => loadDirectHistory(peer, true))}>加载更早消息</button>}
                <div className="chat">
                  {messages.map((m, i) => <div className={"bubble " + (m.from === user ? "mine" : "")}
                    key={m.message_id || m.client_msg_id || i}>
                    <small>{m.from === user ? "我" : peer.username || peer.user_id} · #{m.seq}</small>
                    <p>{m.body}</p>
                  </div>)}
                </div>
                <form onSubmit={(e) => { e.preventDefault(); run(() => sendChat(chat)); }}>
                  <input aria-label="私聊内容" value={chat} onChange={(e) => setChat(e.target.value)}
                    required maxLength={4000}/>
                  <button disabled={busy || wsState !== "已连接"}>发送私聊</button>
                </form>
              </> : <p>选择好友开始对话</p>}
            </section>
          ) : tab === "通知" ? (
            <section>
              <h2>通知中心</h2>
              <button onClick={() => run(loadNotices)}>刷新通知</button>
              {notifications.map((n) => (
                <article key={n.id}>
                  <strong>
                    {n.kind.startsWith("task_")
                      ? "学习任务有更新"
                      : n.kind === "comment_created"
                        ? "收到新评论"
                        : "社区有新动态"}
                  </strong>
                  <small>
                    {date(n.created_at)} · {n.read_at ? "已读" : "未读"}
                  </small>
                  <button
                    onClick={() =>
                      run(async () => {
                        const data = await api("/communities?mine=1");
                        const g = data.communities.find(
                          (g) => g.group_id === n.group_id,
                        );
                        if (!g) throw Error("已离开该社区");
                        await select(g);
                        await api(`/notifications/${n.id}/read`, "POST");
                        if (n.kind.startsWith("task_")) {
                          setTab("任务");
                          setNotice("请打开对应任务查看最新状态");
                        } else await openPost(n.object_id);
                        await loadNotices();
                      })
                    }
                  >
                    查看详情
                  </button>
                </article>
              ))}
              {!notifications.length && <p>暂无通知</p>}
            </section>
          ) : !group ? (
            <section className="empty">
              <h1>选择一个学习社区</h1>
              <p>创建自己的社区，或在发现社区中加入一个。</p>
            </section>
          ) : (
            <>
              <section className="community-head">
                <h1>{group.name}</h1>
                <p>{group.description}</p>
                <small>{group.role} · 所有内容仅社区成员可见</small>
                <div className="row">
                  {["动态", "聊天", "任务", "成员"].map((t) => (
                    <button
                      key={t}
                      className={tab === t ? "selected" : ""}
                      onClick={() => {
                        setTab(t);
                        setPost(null);
                      }}
                    >
                      {t}
                    </button>
                  ))}
                </div>
              </section>
              {tab === "动态" && (
                <>
                  <div className="row">
                    {[
                      ["", "最新"],
                      ["resource", "学习资料"],
                      ["featured", "精华"],
                      ["owner", "只看创建者"],
                      ...(isManager ? [["hidden", "已隐藏"]] : []),
                    ].map(([f, label]) => (
                      <button
                        key={label}
                        className={filter === f ? "selected" : ""}
                        onClick={() => {
                          setFilter(f);
                          setPost(null);
                        }}
                      >
                        {label}
                      </button>
                    ))}
                    <button onClick={() => run(() => feed())}>刷新动态</button>
                  </div>
                  {!post ? (
                    <>
                      <form
                        onSubmit={(e) => {
                          e.preventDefault();
                          run(async () => {
                            await write(
                              `/communities/${group.group_id}/posts`,
                              {
                                title,
                                body,
                                kind,
                                attachment_ids: files.map(
                                  (f) => f.attachment_id,
                                ),
                              },
                            );
                            setTitle("");
                            setBody("");
                            setFiles([]);
                            await feed();
                          });
                        }}
                      >
                        <h3>发布动态</h3>
                        <select
                          value={kind}
                          onChange={(e) => setKind(e.target.value)}
                        >
                          {Object.entries(names)
                            .filter(([k]) => k !== "announcement" || isManager)
                            .map(([k, n]) => (
                              <option key={k} value={k}>
                                {n}
                              </option>
                            ))}
                        </select>
                        <input
                          aria-label="帖子标题"
                          placeholder="标题"
                          value={title}
                          onChange={(e) => setTitle(e.target.value)}
                          required
                          maxLength={160}
                        />
                        <textarea
                          aria-label="帖子正文"
                          placeholder="分享问题、笔记或资料"
                          value={body}
                          onChange={(e) => setBody(e.target.value)}
                          required
                          maxLength={10000}
                        />
                        <input
                          aria-label="帖子附件"
                          type="file"
                          disabled={busy || files.length >= 8}
                          onChange={(e) => {
                            const f = e.target.files?.[0];
                            if (f)
                              run(async () => {
                                const a = await upload(f);
                                setFiles((old) => [...old, a]);
                              });
                            e.target.value = "";
                          }}
                        />
                        {files.map((f) => (
                          <small key={f.attachment_id}>{f.file_name}</small>
                        ))}
                        <button disabled={busy}>发布动态</button>
                      </form>
                      {posts.map((p) => (
                        <article key={p.post_id}>
                          <small>
                            {p.pinned ? "置顶 · " : ""}
                            {p.featured ? "精华 · " : ""}
                            {names[p.kind]} · {date(p.created_at)}
                          </small>
                          <h2>
                            <button
                              className="link"
                              onClick={() => run(() => openPost(p.post_id))}
                            >
                              {p.title}
                            </button>
                          </h2>
                          <p className="preview">{p.body}</p>
                          <small>
                            评论 {p.comments} · 赞 {p.likes}
                          </small>
                        </article>
                      ))}
                      {cursor && (
                        <button onClick={() => run(() => feed(true))}>
                          加载更多动态
                        </button>
                      )}
                    </>
                  ) : (
                    <article>
                      <button
                        onClick={() => {
                          setPost(null);
                          setEditing(false);
                        }}
                      >
                        返回动态
                      </button>
                      <small>
                        {names[post.kind]} · 版本 {post.version}
                      </small>
                      {editing ? (
                        <form
                          onSubmit={(e) => {
                            e.preventDefault();
                            run(async () => {
                              await write(
                                "/posts/" + post.post_id,
                                {
                                  title,
                                  body,
                                  expected_version: post.version,
                                  pinned: post.pinned,
                                  featured: post.featured,
                                },
                                "PATCH",
                              );
                              await openPost(post.post_id);
                              await feed();
                            });
                          }}
                        >
                          <input
                            value={title}
                            onChange={(e) => setTitle(e.target.value)}
                          />
                          <textarea
                            value={body}
                            onChange={(e) => setBody(e.target.value)}
                          />
                          <button disabled={busy}>保存编辑</button>
                        </form>
                      ) : (
                        <>
                          <h2>{post.title}</h2>
                          <p className="prose">{post.body}</p>
                        </>
                      )}
                      {post.attachments.map((f) => (
                        <button
                          key={f.attachment_id}
                          onClick={() => run(() => download(f))}
                        >
                          下载 {f.file_name}
                        </button>
                      ))}
                      <div className="row">
                        <button
                          onClick={() =>
                            run(async () => {
                              await api(
                                `/posts/${post.post_id}/like`,
                                post.liked ? "DELETE" : "PUT",
                              );
                              await openPost(post.post_id);
                            })
                          }
                        >
                          {post.liked ? "取消点赞" : "点赞"} {post.likes}
                        </button>
                        {(post.author_id === user || isManager) && (
                          <>
                            <button
                              onClick={() => {
                                setEditing(true);
                                setTitle(post.title);
                                setBody(post.body);
                              }}
                            >
                              编辑
                            </button>
                            <button
                              onClick={() =>
                                run(async () => {
                                  await write(
                                    "/posts/" + post.post_id,
                                    {
                                      title: post.title,
                                      body: post.body,
                                      status: "deleted",
                                      expected_version: post.version,
                                    },
                                    "PATCH",
                                  );
                                  setPost(null);
                                  await feed();
                                })
                              }
                            >
                              删除
                            </button>
                          </>
                        )}
                        {isManager && (
                          <>
                            <button
                              onClick={() =>
                                run(async () => {
                                  await write(
                                    "/posts/" + post.post_id,
                                    {
                                      title: post.title,
                                      body: post.body,
                                      status:
                                        post.status === "hidden"
                                          ? "published"
                                          : "hidden",
                                      expected_version: post.version,
                                    },
                                    "PATCH",
                                  );
                                  setPost(null);
                                  await feed();
                                })
                              }
                            >
                              {post.status === "hidden"
                                ? "恢复显示"
                                : "隐藏帖子"}
                            </button>
                            <button
                              onClick={() =>
                                run(async () => {
                                  await write(
                                    "/posts/" + post.post_id,
                                    {
                                      title: post.title,
                                      body: post.body,
                                      pinned: !post.pinned,
                                      featured: post.featured,
                                      expected_version: post.version,
                                    },
                                    "PATCH",
                                  );
                                  await openPost(post.post_id);
                                })
                              }
                            >
                              {post.pinned ? "取消置顶" : "置顶"}
                            </button>
                            <button
                              onClick={() =>
                                run(async () => {
                                  await write(
                                    "/posts/" + post.post_id,
                                    {
                                      title: post.title,
                                      body: post.body,
                                      pinned: post.pinned,
                                      featured: !post.featured,
                                      expected_version: post.version,
                                    },
                                    "PATCH",
                                  );
                                  await openPost(post.post_id);
                                })
                              }
                            >
                              {post.featured ? "取消精选" : "精选"}
                            </button>
                          </>
                        )}
                      </div>
                      <h3>评论</h3>
                      {comments.map((c) => (
                        <div
                          className={"comment " + (c.parent_id ? "reply" : "")}
                          key={c.comment_id}
                        >
                          <small>
                            {c.author_id.slice(0, 8)} · {date(c.created_at)}
                          </small>
                          <p>{c.body}</p>
                          {!c.parent_id && (
                            <button onClick={() => setParent(c.comment_id)}>
                              回复
                            </button>
                          )}
                        </div>
                      ))}
                      <form
                        onSubmit={(e) => {
                          e.preventDefault();
                          run(async () => {
                            await write(`/posts/${post.post_id}/comments`, {
                              body: reply,
                              parent_id: parent,
                            });
                            await openPost(post.post_id);
                          });
                        }}
                      >
                        {parent && (
                          <small>
                            正在回复评论{" "}
                            <button type="button" onClick={() => setParent("")}>
                              取消回复
                            </button>
                          </small>
                        )}
                        <textarea
                          aria-label="评论内容"
                          placeholder="写下你的回复"
                          value={reply}
                          onChange={(e) => setReply(e.target.value)}
                          required
                          maxLength={2000}
                        />
                        <button disabled={busy}>提交评论</button>
                      </form>
                    </article>
                  )}
                </>
              )}
              {tab === "聊天" && (
                <section>
                  <h2>社区聊天室</h2>
                  <small>
                    {wsState} · 收到消息先存本地再确认，按会话序号合并
                  </small>
                  {older && (
                    <button onClick={() => run(() => loadHistory(group, true))}>
                      加载更早消息
                    </button>
                  )}
                  <div className="chat">
                    {messages.map((m, i) => (
                      <div
                        className={"bubble " + (m.from === user ? "mine" : "")}
                        key={m.message_id || i}
                      >
                        <small>
                          {m.from === user ? "我" : memberNames[m.from || ""] || "已离开成员"} · #
                          {m.seq}
                        </small>
                        <p>
                          {(() => {
                            try {
                              const a = JSON.parse(m.body || "");
                              if (a.kind === "attachment")
                                return (
                                  <button
                                    onClick={() => run(() => download(a))}
                                  >
                                    下载 {a.file_name}
                                  </button>
                                );
                            } catch {}
                            return m.body;
                          })()}
                        </p>
                      </div>
                    ))}
                  </div>
                  <input
                    aria-label="聊天附件"
                    type="file"
                    disabled={busy}
                    onChange={(e) => {
                      const f = e.target.files?.[0];
                      if (f)
                        run(async () => {
                          const a = await upload(f);
                          await sendChat(
                            JSON.stringify({
                              kind: "attachment",
                              attachment_id: a.attachment_id,
                            }),
                          );
                        });
                      e.target.value = "";
                    }}
                  />
                  <form
                    onSubmit={(e) => {
                      e.preventDefault();
                      run(() => sendChat(chat));
                    }}
                  >
                    <input
                      aria-label="聊天内容"
                      value={chat}
                      onChange={(e) => setChat(e.target.value)}
                      required
                      maxLength={4000}
                    />
                    <button disabled={busy || wsState !== "已连接"}>
                      发送消息
                    </button>
                  </form>
                </section>
              )}
              {tab === "任务" && (
                <Tasks
                  key={group.group_id}
                  group={group}
                  api={api}
                  write={write}
                  run={run}
                  busy={busy}
                  upload={upload}
                  download={download}
                />
              )}
              {tab === "成员" && (
                <Members
                  group={group}
                  api={api}
                  write={write}
                  run={run}
                  user={user}
                  refresh={list}
                  leave={() => {
                    generation.current++;
                    setGroup(null);
                  }}
                  onFriend={(uid: string) => run(async () => {
                    await api("/friend/apply", "POST", { target_user_id: uid, message: "想一起交流学习" });
                    setNotice("好友申请已发送");
                  })}
                />
              )}
            </>
          )}
        </main>
      </div>
    </>
  );
}
function Members({ group, api, write, run, user, refresh, leave, onFriend }: any) {
  const [items, setItems] = useState<any[]>([]);
  async function load() {
    const d = await api("/group/members?group_id=" + group.group_id);
    setItems(d.members || []);
  }
  useEffect(() => {
    load();
  }, [group.group_id]);
  return (
    <section>
      <h2>社区成员</h2>
      {items.map((m) => (
        <article key={m.user_id}>
          <strong>{m.user_id === user ? "我" : m.username || m.user_id}</strong> · {m.role}
          {m.user_id !== user && <button onClick={() => onFriend(m.user_id)}>加为好友</button>}
          {group.role === "owner" && m.user_id !== user && (
            <>
              <button
                onClick={() =>
                  run(async () => {
                    await write(`/communities/${group.group_id}/transfer`, {
                      user_id: m.user_id,
                    });
                    leave();
                    await refresh();
                  })
                }
              >
                转让创建者
              </button>
              <button
                onClick={() =>
                  run(async () => {
                    await write(`/communities/${group.group_id}/remove`, {
                      user_id: m.user_id,
                    });
                    await load();
                  })
                }
              >
                移除成员
              </button>
            </>
          )}
        </article>
      ))}
      <button
        onClick={() =>
          run(async () => {
            await write(`/communities/${group.group_id}/leave`);
            leave();
            await refresh();
          })
        }
      >
        退出社区
      </button>
    </section>
  );
}
function Tasks({ group, api, write, run, busy, upload, download }: any) {
  const [items, setItems] = useState<any[]>([]),
    [detail, setDetail] = useState<any>(null),
    [subs, setSubs] = useState<any[]>([]),
    [progress, setProgress] = useState<any>(null),
    [title, setTitle] = useState(""),
    [description, setDescription] = useState(""),
    [text, setText] = useState(""),
    [link, setLink] = useState(""),
    [files, setFiles] = useState<any[]>([]),
    [reasons, setReasons] = useState<Record<string, string>>({});
  const manager = group.role === "owner" || group.role === "admin";
  async function refresh() {
    const d = await api(`/groups/${group.group_id}/tasks`);
    setItems(d.tasks || []);
  }
  async function open(t: any) {
    const d = await api("/tasks/" + t.task_id);
    setDetail(d);
    setText(d.my_submission?.text || "");
    setLink(d.my_submission?.link || "");
    setFiles([]);
    if (manager) {
      setSubs((await api(`/tasks/${t.task_id}/submissions`)).submissions || []);
      setProgress(await api(`/tasks/${t.task_id}/progress`));
    }
  }
  useEffect(() => {
    refresh();
  }, [group.group_id]);
  async function action(name: string) {
    await write(`/tasks/${detail.task.task_id}/${name}`, {
      expected_version: detail.task.version,
    });
    await open(detail.task);
    await refresh();
  }
  return (
    <section>
      <h2>学习任务</h2>
      <button
        onClick={() =>
          run(async () => {
            await refresh();
            if (detail) await open(detail.task);
          })
        }
      >
        刷新任务
      </button>
      {manager && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            run(async () => {
              const t = await write(`/groups/${group.group_id}/tasks`, {
                title,
                description,
                due_at: 0,
              });
              setTitle("");
              setDescription("");
              await refresh();
              await open(t);
            });
          }}
        >
          <input
            aria-label="任务标题"
            placeholder="任务标题"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
          />
          <textarea
            aria-label="任务说明"
            placeholder="任务要求"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
          <button disabled={busy}>创建任务草稿</button>
        </form>
      )}
      {items.map((t) => (
        <button
          key={t.task_id}
          className="task"
          onClick={() => run(() => open(t))}
        >
          {t.title} · {t.status}
        </button>
      ))}
      {detail && (
        <article>
          <h2>{detail.task.title}</h2>
          <p>{detail.task.description}</p>
          <p>任务状态：{detail.task.status}</p>
          {manager ? (
            <>
              {detail.task.status === "draft" && (
                <button onClick={() => run(() => action("publish"))}>
                  发布任务
                </button>
              )}
              {detail.task.status === "published" && (
                <button onClick={() => run(() => action("close"))}>
                  关闭任务
                </button>
              )}
              {progress && (
                <p>
                  未交 {progress.not_submitted} · 待审 {progress.submitted} ·
                  退回 {progress.returned} · 通过 {progress.approved}
                </p>
              )}
              {subs.map((s) => (
                <div className="comment" key={s.submission_id}>
                  <small>
                    {s.user_id} · {s.status} · 第{s.attempt_no}次提交
                  </small>
                  <p>{s.text}</p>
                  <p>{s.link}</p>
                  <p>{s.review_note}</p>
                  {(s.attachment_ids || []).map((a) => (
                    <button
                      key={a}
                      onClick={() =>
                        run(() =>
                          download({ attachment_id: a, file_name: "任务附件" }),
                        )
                      }
                    >
                      下载附件
                    </button>
                  ))}
                  {s.status === "submitted" && (
                    <>
                      <input
                        placeholder="退回理由"
                        value={reasons[s.submission_id] || ""}
                        onChange={(e) =>
                          setReasons({
                            ...reasons,
                            [s.submission_id]: e.target.value,
                          })
                        }
                      />
                      {[
                        ["approve", "通过"],
                        ["return", "退回"],
                      ].map(([decision, label]) => (
                        <button
                          key={decision}
                          onClick={() =>
                            run(async () => {
                              await write(
                                `/submissions/${s.submission_id}/review`,
                                {
                                  decision,
                                  reason: reasons[s.submission_id] || "",
                                  expected_version: s.version,
                                },
                              );
                              await open(detail.task);
                            })
                          }
                        >
                          {label}
                        </button>
                      ))}
                    </>
                  )}
                </div>
              ))}
            </>
          ) : (
            <>
              <p>我的提交：{detail.my_submission?.status || "未提交"}</p>
              {detail.my_submission?.review_note && (
                <p>审核意见：{detail.my_submission.review_note}</p>
              )}
              {detail.task.status === "published" &&
                (!detail.my_submission ||
                  detail.my_submission.status === "returned") && (
                  <form
                    onSubmit={(e) => {
                      e.preventDefault();
                      run(async () => {
                        await write(
                          `/tasks/${detail.task.task_id}/submission`,
                          {
                            text,
                            link,
                            expected_version:
                              detail.my_submission?.version || 0,
                            attachment_ids: files.map((f) => f.attachment_id),
                          },
                          "PUT",
                        );
                        await open(detail.task);
                      });
                    }}
                  >
                    <textarea
                      aria-label="任务提交内容"
                      placeholder="学习成果"
                      value={text}
                      onChange={(e) => setText(e.target.value)}
                    />
                    <input
                      placeholder="成果链接（可选）"
                      value={link}
                      onChange={(e) => setLink(e.target.value)}
                    />
                    <input
                      aria-label="任务附件"
                      type="file"
                      disabled={busy}
                      onChange={(e) => {
                        const f = e.target.files?.[0];
                        if (f)
                          run(async () => {
                            const a = await upload(f);
                            setFiles((old) => [...old, a]);
                          });
                        e.target.value = "";
                      }}
                    />
                    {files.map((f) => (
                      <small key={f.attachment_id}>{f.file_name}</small>
                    ))}
                    <button disabled={busy}>
                      {detail.my_submission ? "修改后重交" : "提交任务"}
                    </button>
                  </form>
                )}
            </>
          )}
        </article>
      )}
    </section>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
