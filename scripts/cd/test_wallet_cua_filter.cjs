const test = require('node:test');
const assert = require('node:assert/strict');
const load = () => import('../h5-demo-acceptance/lib/wallet_cua_filter.mjs');
const origin = 'http://localhost:18500', content = '12345678-1234-1234-1234-123456789abc';

function request(sequence, id, url) {
    return { sequence, method: 'Network.requestWillBeSent', params: { requestId: id, timestamp: 100, wallTime: 1700000000,
        request: { url, method: 'POST', postData: '{}', headers: { Cookie: 'unrelated-cookie' } } } };
}
function extra(sequence, id, token) {
    return { sequence, method: 'Network.requestWillBeSentExtraInfo', params: { requestId: id,
        headers: { Authorization: token, Cookie: 'unrelated-cookie' } } };
}

test('holds early headers until their origin is known without exporting unrelated credentials', async () => {
    const { CuaEventFilter } = await load(); const filter = new CuaEventFilter({ origin, content, initialCursor: 0 });
    const first = filter.consume({ cursor: 1, truncated: false, hasMore: false, events: [extra(1, 'foreign', 'Bearer foreign-secret')] });
    assert.equal(first.cursor, 0); assert.equal(first.events.length, 0); assert.equal(first.unresolved, true);
    const second = filter.consume({ cursor: 2, truncated: false, hasMore: false, events: [request(2, 'foreign', 'https://outside.invalid/path')] });
    assert.equal(second.cursor, 2); assert.equal(second.events.length, 0); assert.equal(second.unresolved, false);
    assert.equal(JSON.stringify(second).includes('foreign-secret'), false); filter.finish();
});

test('preserves early target headers and event sequence across batches', async () => {
    const { CuaEventFilter } = await load(); const filter = new CuaEventFilter({ origin, content, initialCursor: 0 });
    const first = filter.consume({ cursor: 1, truncated: false, hasMore: false, events: [extra(1, 'r1', 'Bearer fixture')] });
    assert.equal(first.cursor, 0);
    const second = filter.consume({ cursor: 2, truncated: false, hasMore: false, events: [request(2, 'r1', origin + '/api/v1/auth/login')] });
    assert.equal(second.cursor, 2); assert.deepEqual(second.events.map(e => e.sequence), [1, 2]);
    assert.equal(second.events[0].params.headers.authorization, 'Bearer fixture');
    assert.equal(JSON.stringify(second).includes('unrelated-cookie'), false); filter.finish();
});

test('fails closed on truncation, unresolved origins and selected redirects', async () => {
    const { CuaEventFilter } = await load();
    const truncated = new CuaEventFilter({ origin, content, initialCursor: 0 });
    assert.throws(() => truncated.consume({ cursor: 1, truncated: true, hasMore: false, events: [] }), /truncated/);
    assert.throws(() => truncated.consume({ cursor: 1, truncated: false, hasMore: false, events: [] }), /failed/);
    const unresolved = new CuaEventFilter({ origin, content, initialCursor: 0 });
    unresolved.consume({ cursor: 1, truncated: false, hasMore: false, events: [extra(1, 'unknown', 'Bearer private')] });
    assert.throws(() => unresolved.finish(), /unresolved/);
    const redirected = new CuaEventFilter({ origin, content, initialCursor: 0 });
    redirected.consume({ cursor: 1, truncated: false, hasMore: false, events: [request(1, 'r1', origin + '/api/v1/auth/login')] });
    const next = request(2, 'r1', 'https://outside.invalid/'); next.params.redirectResponse = { status: 302 };
    assert.throws(() => redirected.consume({ cursor: 2, truncated: false, hasMore: false, events: [next] }), /redirect/);
});

test('does not export same-host URLs that carry embedded credentials', async () => {
    const { CuaEventFilter } = await load(); const filter = new CuaEventFilter({ origin, content, initialCursor: 0 });
    const batch = filter.consume({ cursor: 1, truncated: false, hasMore: false,
        events: [request(1, 'foreign', 'http://user:password@localhost:18500/api/v1/auth/login')] });
    assert.equal(batch.events.length, 0); assert.equal(JSON.stringify(batch).includes('password'), false); filter.finish();
});
