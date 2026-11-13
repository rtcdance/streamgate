const test = require('node:test');
const assert = require('node:assert/strict');
const load = () => import('../h5-demo-acceptance/lib/wallet_cua_session.mjs');
const origin = 'http://localhost:18500', content = '12345678-1234-1234-1234-123456789abc';

function fixture() {
    const at = Date.now(), events = [
        { sequence: 11, method: 'Network.requestWillBeSent', params: { requestId: 'r1', timestamp: 100, wallTime: at / 1000,
            request: { url: origin + '/api/v1/auth/challenge', method: 'POST', postData: '{}' } } },
        { sequence: 12, method: 'Network.responseReceived', params: { requestId: 'r1', timestamp: 100.1, response: { status: 200 } } },
        { sequence: 13, method: 'Network.loadingFinished', params: { requestId: 'r1', timestamp: 100.2 } },
    ];
    const calls = [], responses = [];
    const bridge = { async request(op, args) {
        calls.push({ op, args });
        if (op === 'network-enable') return { cursor: 10 };
        if (op === 'network-events') return responses.shift() || { cursor: args.afterSequence, events: [], truncated: false, hasMore: false, unresolved: false };
        if (op === 'network-body') return { body: '{"fixture":true}', base64Encoded: false };
        if (op === 'state') return { origin, token: null, identity: null };
        if (op === 'profile') return { status: 200, data: { wallet_address: 'fixture' } };
        throw Error('unexpected operation');
    } };
    return { bridge, events, responses, calls, at };
}

test('drains scoped events and uses browser time before returning state', async () => {
    const { CuaPageSession } = await load(); const f = fixture();
    f.responses.push({ cursor: 13, events: f.events, truncated: false, hasMore: false, unresolved: false });
    const observed = []; const session = new CuaPageSession({ bridge: f.bridge, origin, content, onResponse: row => observed.push(row), onPageError: () => {} });
    await session.start(); const state = await session.state();
    assert.equal(state.origin, origin); assert.equal(observed.length, 1);
    assert.equal(observed[0].body.toString(), '{"fixture":true}'); assert.equal(observed[0].at, f.at + 100);
    assert.equal(f.calls.filter(c => c.op === 'network-body').length, 1);
    await session.finish();
});

test('rejects truncated or non-advancing batches and foreign state', async () => {
    const { CuaPageSession } = await load();
    for (const bad of [{ cursor: 13, events: [], truncated: true, hasMore: false, unresolved: false }, { cursor: 10, events: [], truncated: false, hasMore: true, unresolved: false }]) {
        const f = fixture(); f.responses.push(bad); const session = new CuaPageSession({ bridge: f.bridge, origin, content, onResponse: () => {}, onPageError: () => {} });
        await session.start(); await assert.rejects(session.state(), /truncated|progress/);
        await assert.rejects(session.state(), /failed/);
    }
    const f = fixture(); const original = f.bridge.request;
    f.bridge.request = async (op, args) => op === 'state' ? { origin: 'http://localhost:18501' } : original.call(f.bridge, op, args);
    const session = new CuaPageSession({ bridge: f.bridge, origin, content, onResponse: () => {}, onPageError: () => {} });
    await session.start(); await assert.rejects(session.state(), /origin/);
});

test('continues paginated batches without losing the original response', async () => {
    const { CuaPageSession } = await load(); const f = fixture(), rows = [];
    f.responses.push({ cursor: 12, events: f.events.slice(0, 2), truncated: false, hasMore: true, unresolved: false },
        { cursor: 13, events: f.events.slice(2), truncated: false, hasMore: false, unresolved: false });
    const session = new CuaPageSession({ bridge: f.bridge, origin, content, onResponse: row => rows.push(row), onPageError: () => {} });
    await session.start(); await session.state();
    assert.equal(rows.length, 1); assert.equal(rows[0].at, f.at + 100);
    assert.deepEqual(f.calls.filter(c => c.op === 'network-events').slice(0, 2).map(c => c.args.afterSequence), [10, 12]);
    await session.finish();
});

test('stale browser events and page exceptions are never silently accepted', async () => {
    const { CuaPageSession } = await load(); const f = fixture(); f.events[0].params.wallTime -= 6;
    f.responses.push({ cursor: 13, events: f.events, truncated: false, hasMore: false, unresolved: false });
    const rows = []; const session = new CuaPageSession({ bridge: f.bridge, origin, content, onResponse: row => rows.push(row), onPageError: () => {} });
    await session.start(); await assert.rejects(session.state(), /stale/); assert.equal(rows.length, 0);
    const g = fixture(), errors = [];
    g.responses.push({ cursor: 11, events: [{ sequence: 11, method: 'Runtime.exceptionThrown', params: {} }], truncated: false, hasMore: false, unresolved: false });
    const other = new CuaPageSession({ bridge: g.bridge, origin, content, onResponse: () => {}, onPageError: e => errors.push(e) });
    await other.start(); await other.state(); assert.deepEqual(errors, ['H5 page error observed']);
});

test('cannot finish while the peer still holds an unclassified request', async () => {
    const { CuaPageSession } = await load(); const f = fixture();
    f.responses.push({ cursor: 10, events: [], truncated: false, hasMore: false, unresolved: true });
    const session = new CuaPageSession({ bridge: f.bridge, origin, content, onResponse: () => {}, onPageError: () => {} });
    await session.start(); await assert.rejects(session.finish(), /unresolved/);
});

test('private trace records original bounded requests and complete call ranges', async () => {
    const { CuaPageSession } = await load();
    const { CuaTrace } = await import('../h5-demo-acceptance/lib/wallet_cua_trace.mjs');
    const f = fixture(); f.responses.push({ cursor: 13, events: f.events, truncated: false, hasMore: false, unresolved: false });
    const trace = new CuaTrace({ bridge: f.bridge, candidateSHA: 'a'.repeat(40), mode: 'monolith', phase: 'eoa', origin, content });
    const session = new CuaPageSession({ bridge: trace, origin, content, onResponse: () => {}, onPageError: () => {} });
    await assert.rejects(trace.request('state', {}), /outside trace call/);
    await trace.run('start', () => session.start());
    await trace.run('state', () => session.state());
    await assert.rejects(async () => trace.serialize(), /incomplete/);
    await trace.run('finish', () => session.finish());
    const saved = JSON.parse(trace.serialize());
    assert.deepEqual(saved.calls.map(call => call.kind), ['start', 'state', 'finish']);
    assert.deepEqual(saved.requests.map(call => call.op), f.calls.map(call => call.op));
    assert.deepEqual(saved.requests.find(call => call.op === 'network-events').value.events, f.events);
    assert.equal(saved.calls[0].first, 0);
    assert.equal(saved.calls.at(-1).end, saved.requests.length);
    f.events[0].params.request.url = 'http://evil.example/';
    assert.notEqual(saved.requests.find(call => call.op === 'network-events').value.events[0].params.request.url, f.events[0].params.request.url);
});

test('trace budget or failed relay prevents a successful trace', async () => {
    const { CuaTrace } = await import('../h5-demo-acceptance/lib/wallet_cua_trace.mjs');
    const trace = new CuaTrace({ bridge: { request: async () => ({ body: 'x'.repeat(2048) }) }, candidateSHA: 'a'.repeat(40),
        mode: 'monolith', phase: 'eoa', origin, content, limit: 1024 });
    await assert.rejects(trace.run('start', () => trace.request('network-enable', {})), /budget/);
    assert.throws(() => trace.serialize(), /incomplete/);
    await assert.rejects(trace.run('finish', async () => {}), /invalid/);
});
