const assert = require('node:assert/strict');
const test = require('node:test');
const { collectMissingPages, mergeLatestPage } = require('../public/history-sync.js');

function historyEndpoint(maxSeq, pageSize) {
    const calls = [];
    return {
        calls,
        fetch: async (beforeSeq) => {
            calls.push(beforeSeq);
            const all = Array.from({ length: maxSeq }, (_, i) => ({ seq: i + 1 }));
            const matches = all.filter((message) => !beforeSeq || message.seq < beforeSeq);
            const newest = matches.slice(-pageSize);
            return {
                data: newest,
                has_more: matches.length > pageSize,
                next_before_seq: newest[0]?.seq || 0
            };
        }
    };
}

test('new local store establishes a latest-page baseline', async () => {
    const endpoint = historyEndpoint(250, 100);
    const result = await collectMissingPages(endpoint.fetch, 0);
    assert.deepEqual(endpoint.calls, [0]);
    assert.equal(result.messages.length, 100);
    assert.equal(result.newestSeq, 250);
});

test('existing cursor fetches every page until it overlaps the saved range', async () => {
    const endpoint = historyEndpoint(250, 100);
    const result = await collectMissingPages(endpoint.fetch, 80);
    assert.deepEqual(endpoint.calls, [0, 151]);
    assert.equal(result.newestSeq, 250);
    assert.equal(result.added, 170);
    assert.equal(result.pages, 2);
});

test('invalid cursor rejects instead of advancing a partial history range', async () => {
    await assert.rejects(
        collectMissingPages(async () => ({ data: [{ seq: 90 }], has_more: true, next_before_seq: 0 }), 80),
        /游标未前进/
    );
});

test('a stale HTTP history page keeps a newer WebSocket message in seq order', () => {
    const server = [
        { message_id: 'c2c:1:2-41', seq: 41 },
        { message_id: 'c2c:1:2-42', seq: 42 }
    ];
    const local = [
        { message_id: 'c2c:1:2-42', seq: 42 },
        { message_id: 'c2c:1:2-43', seq: 43 }
    ];
    assert.deepEqual(
        mergeLatestPage(server, local).map((message) => message.seq),
        [41, 42, 43]
    );
});
