(() => {
    "use strict";

    const DB_NAME = "linkgo-client";
    const DB_VERSION = 2;
    const STORES = {
        messages: "messages",
        conversations: "conversations",
        cursors: "cursors",
        syncProgress: "sync_progress"
    };

    let dbPromise;

    function open() {
        if (!window.indexedDB) return Promise.reject(new Error("浏览器不支持 IndexedDB"));
        if (dbPromise) return dbPromise;
        dbPromise = new Promise((resolve, reject) => {
            const request = window.indexedDB.open(DB_NAME, DB_VERSION);
            request.onerror = () => reject(request.error || new Error("IndexedDB 打开失败"));
            request.onupgradeneeded = () => {
                const db = request.result;
                if (!db.objectStoreNames.contains(STORES.messages)) {
                    const messages = db.createObjectStore(STORES.messages, { keyPath: "key" });
                    messages.createIndex("user_session", ["user_id", "session_id"], { unique: false });
                    messages.createIndex("user_client", ["user_id", "client_msg_id"], { unique: false });
                }
                if (!db.objectStoreNames.contains(STORES.conversations)) {
                    const conversations = db.createObjectStore(STORES.conversations, { keyPath: "key" });
                    conversations.createIndex("user", "user_id", { unique: false });
                }
                if (!db.objectStoreNames.contains(STORES.cursors)) {
                    const cursors = db.createObjectStore(STORES.cursors, { keyPath: "key" });
                    cursors.createIndex("user", "user_id", { unique: false });
                }
                if (!db.objectStoreNames.contains(STORES.syncProgress)) {
                    const progress = db.createObjectStore(STORES.syncProgress, { keyPath: "key" });
                    progress.createIndex("user", "user_id", { unique: false });
                }
            };
            request.onsuccess = () => resolve(request.result);
        });
        return dbPromise;
    }

    function requestResult(request) {
        return new Promise((resolve, reject) => {
            request.onsuccess = () => resolve(request.result);
            request.onerror = () => reject(request.error || new Error("IndexedDB 请求失败"));
        });
    }

    async function transaction(storeNames, mode, work) {
        const db = await open();
        return new Promise((resolve, reject) => {
            const tx = db.transaction(storeNames, mode);
            let result;
            tx.oncomplete = () => resolve(result);
            tx.onerror = () => reject(tx.error || new Error("IndexedDB 事务失败"));
            tx.onabort = () => reject(tx.error || new Error("IndexedDB 事务已取消"));
            try {
                result = work(tx);
            } catch (error) {
                tx.abort();
                reject(error);
            }
        });
    }

    function messageKey(userId, message) {
        const identity = message.message_id
            ? `mid:${message.message_id}`
            : `cid:${message.client_msg_id || `${Date.now()}-${Math.random()}`}`;
        return `${userId}:${identity}`;
    }

    async function putMessage(userId, sessionId, message, status) {
        if (!userId || !sessionId || !message) return;
        const row = {
            ...message,
            key: messageKey(userId, message),
            user_id: userId,
            session_id: sessionId,
            status: status || "received",
            stored_at: Date.now()
        };
        await transaction([STORES.messages], "readwrite", (tx) => {
            const store = tx.objectStore(STORES.messages);
            if (message.message_id && message.client_msg_id) {
                store.delete(`${userId}:cid:${message.client_msg_id}`);
            }
            store.put(row);
        });
    }

    async function updateOutgoingStatus(userId, clientMsgId, status) {
        if (!userId || !clientMsgId) return;
        const db = await open();
        const tx = db.transaction([STORES.messages], "readwrite");
        const done = new Promise((resolve, reject) => {
            tx.oncomplete = resolve;
            tx.onerror = () => reject(tx.error || new Error("消息状态更新失败"));
            tx.onabort = () => reject(tx.error || new Error("消息状态更新已取消"));
        });
        const index = tx.objectStore(STORES.messages).index("user_client");
        const keys = await requestResult(index.getAllKeys(IDBKeyRange.only([userId, clientMsgId])));
        for (const key of keys) {
            const store = tx.objectStore(STORES.messages);
            const row = await requestResult(store.get(key));
            if (row) store.put({ ...row, status, stored_at: Date.now() });
        }
        await done;
    }

    async function listMessages(userId, sessionId) {
        const db = await open();
        const tx = db.transaction([STORES.messages], "readonly");
        const index = tx.objectStore(STORES.messages).index("user_session");
        const rows = await requestResult(index.getAll(IDBKeyRange.only([userId, sessionId])));
        return rows.sort((a, b) => {
            const aSeq = Number(a.seq || 0);
            const bSeq = Number(b.seq || 0);
            if (aSeq && bSeq && aSeq !== bSeq) return aSeq - bSeq;
            return Number(a.sent_at || a.stored_at || 0) - Number(b.sent_at || b.stored_at || 0);
        });
    }

    async function putConversation(userId, conversation) {
        if (!userId || !conversation || !conversation.target) return;
        const type = conversation.type || "user";
        await transaction([STORES.conversations], "readwrite", (tx) => {
            tx.objectStore(STORES.conversations).put({
                ...conversation,
                key: `${userId}:${type}:${conversation.target}`,
                user_id: userId,
                updated_at: conversation.updated_at || Date.now()
            });
        });
    }

    async function listConversations(userId) {
        const db = await open();
        const tx = db.transaction([STORES.conversations], "readonly");
        return requestResult(tx.objectStore(STORES.conversations).index("user").getAll(userId));
    }

    async function putCursor(userId, sessionId, cursor) {
        if (!userId || !sessionId) return;
        await transaction([STORES.cursors], "readwrite", (tx) => {
            tx.objectStore(STORES.cursors).put({
                ...cursor,
                key: `${userId}:${sessionId}`,
                user_id: userId,
                session_id: sessionId,
                updated_at: Date.now()
            });
        });
    }

    async function listCursors(userId) {
        const db = await open();
        const tx = db.transaction([STORES.cursors], "readonly");
        return requestResult(tx.objectStore(STORES.cursors).index("user").getAll(userId));
    }

    async function listSyncProgress(userId) {
        const db = await open();
        const tx = db.transaction([STORES.syncProgress], "readonly");
        return requestResult(tx.objectStore(STORES.syncProgress).index("user").getAll(userId));
    }

    // A completed server history range, rather than a single live ACK, advances
    // this cursor. Messages and progress commit in the same IndexedDB transaction.
    async function saveHistoryRange(userId, sessionId, messages, syncedSeq) {
        if (!userId || !sessionId || !Array.isArray(messages) || syncedSeq <= 0) return;
        await transaction([STORES.messages, STORES.syncProgress], "readwrite", (tx) => {
            const messageStore = tx.objectStore(STORES.messages);
            for (const message of messages) {
                if (!message || !message.message_id) continue;
                messageStore.put({
                    ...message,
                    key: messageKey(userId, message),
                    user_id: userId,
                    session_id: sessionId,
                    status: message.from === userId ? "accepted" : "received",
                    stored_at: Date.now()
                });
            }
            tx.objectStore(STORES.syncProgress).put({
                key: `${userId}:${sessionId}`,
                user_id: userId,
                session_id: sessionId,
                synced_seq: syncedSeq,
                updated_at: Date.now()
            });
        });
    }

    async function stats(userId) {
        const [conversations, cursors, progress] = await Promise.all([
            listConversations(userId),
            listCursors(userId),
            listSyncProgress(userId)
        ]);
        const db = await open();
        const tx = db.transaction([STORES.messages], "readonly");
        const allMessages = await requestResult(tx.objectStore(STORES.messages).getAll());
        const messages = allMessages.filter((row) => row.user_id === userId);
        return {
            conversations: conversations.length,
            messages: messages.length,
            pending: messages.filter((row) => row.status === "pending").length,
            cursors: cursors.length,
            synced_conversations: progress.length
        };
    }

    async function clearUser(userId) {
        const db = await open();
        for (const name of Object.values(STORES)) {
            const tx = db.transaction([name], "readwrite");
            const done = new Promise((resolve, reject) => {
                tx.oncomplete = resolve;
                tx.onerror = () => reject(tx.error || new Error("本地数据清理失败"));
                tx.onabort = () => reject(tx.error || new Error("本地数据清理已取消"));
            });
            const store = tx.objectStore(name);
            const rows = await requestResult(store.getAll());
            for (const row of rows) {
                if (row.user_id === userId) store.delete(row.key);
            }
            await done;
        }
    }

    window.LinkGoStore = {
        open,
        putMessage,
        updateOutgoingStatus,
        listMessages,
        putConversation,
        listConversations,
        putCursor,
        listCursors,
        listSyncProgress,
        saveHistoryRange,
        stats,
        clearUser
    };
})();
