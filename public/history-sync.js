(() => {
    "use strict";

    // The history API pages backward. Keep fetching until a page overlaps the
    // last completed local range; only then may its new high-water mark commit.
    async function collectMissingPages(fetchPage, previousSeq) {
        const baseline = Number(previousSeq || 0);
        const messages = [];
        let beforeSeq = 0;
        let newestSeq = baseline;
        let pages = 0;
        for (;;) {
            const payload = await fetchPage(beforeSeq);
            const page = Array.isArray(payload.data) ? payload.data : [];
            if (!page.length) break;
            pages++;
            messages.push(...page);
            const seqs = page.map((message) => Number(message.seq || 0));
            newestSeq = Math.max(newestSeq, ...seqs);
            const oldestSeq = Math.min(...seqs);
            // A new local store establishes a recent-history baseline. Older
            // messages remain available through the explicit history button.
            if (!baseline || oldestSeq <= baseline || !payload.has_more) break;
            const next = Number(payload.next_before_seq || 0);
            if (!Number.isSafeInteger(next) || next <= 0 || (beforeSeq && next >= beforeSeq) || pages >= 10000) {
                throw new Error("历史分页游标未前进");
            }
            beforeSeq = next;
        }
        return {
            messages,
            newestSeq,
            added: messages.filter((message) => Number(message.seq || 0) > baseline).length,
            pages
        };
    }

    // An HTTP page can race with WebSocket replay/live delivery. Keep locally
    // received messages in or newer than the page range when repainting it.
    function mergeLatestPage(serverMessages, localMessages) {
        const server = Array.isArray(serverMessages) ? serverMessages : [];
        const local = Array.isArray(localMessages) ? localMessages : [];
        const serverMinSeq = server.length
            ? Math.min(...server.map((message) => Number(message.seq || 0)))
            : Number.NEGATIVE_INFINITY;
        const byID = new Map(server.filter((message) => message.message_id)
            .map((message) => [message.message_id, message]));
        for (const message of local) {
            if (message.message_id
                && Number(message.seq || 0) >= serverMinSeq
                && !byID.has(message.message_id)) {
                byID.set(message.message_id, message);
            }
        }
        return [...byID.values()].sort((a, b) =>
            Number(a.seq || 0) - Number(b.seq || 0)
            || String(a.message_id).localeCompare(String(b.message_id))
        );
    }

    const api = { collectMissingPages, mergeLatestPage };
    if (typeof window !== "undefined") window.LinkGoHistorySync = api;
    if (typeof module !== "undefined" && module.exports) module.exports = api;
})();
