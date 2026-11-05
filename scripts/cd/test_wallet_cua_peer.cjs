const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const load = () => import('../h5-demo-acceptance/lib/wallet_cua_peer.mjs');
const origin = 'http://localhost:18500', content = '12345678-1234-1234-1234-123456789abc';

function fixture() {
    let url = origin + '/'; const calls = [], events = [];
    const page = { async url() { return url; } };
    const cap = { async send(method, args) {
        calls.push({ method, args });
        if (method === 'Runtime.evaluate') return { result: { value: { origin, token: null, identity: null } } };
        if (method === 'Network.getResponseBody') return { body: 'fixture', base64Encoded: false };
        return {};
    }, async readEvents() { return events.shift() || { cursor: 0, events: [], truncated: false, hasMore: false }; } };
    return { page, cap, calls, events, navigate(value) { url = value; } };
}

test('runs only approved page operations and refuses a changed target origin', async () => {
    const { CuaPeer } = await load(); const f = fixture();
    const invalid = new CuaPeer({ targetPage: f.page, targetCap: f.cap, origin, content });
    await assert.rejects(invalid.execute({ op: 'Runtime.evaluate', args: { expression: 'arbitrary' } }), /unsupported/);
    assert.equal(f.calls.length, 0);
    const peer = new CuaPeer({ targetPage: f.page, targetCap: f.cap, origin, content });
    assert.equal((await peer.execute({ op: 'state', args: {} })).origin, origin);
    f.navigate('https://outside.invalid/');
    await assert.rejects(peer.execute({ op: 'state', args: {} }), /origin/);
    assert.equal(f.calls.length, 1);
});

test('reads bodies only for classified target requests', async () => {
    const { CuaPeer } = await load(); const f = fixture();
    f.events.push({ cursor: 0, events: [], truncated: false, hasMore: false });
    f.events.push({ cursor: 1, truncated: false, hasMore: false, events: [{ sequence: 1, method: 'Network.requestWillBeSent',
        params: { requestId: 'r1', request: { url: origin + '/api/v1/auth/challenge', method: 'POST', postData: '{}' } } }] });
    const peer = new CuaPeer({ targetPage: f.page, targetCap: f.cap, origin, content });
    assert.equal((await peer.execute({ op: 'network-enable', args: {} })).cursor, 0);
    const batch = await peer.execute({ op: 'network-events', args: { afterSequence: 0 } });
    assert.equal(batch.events.length, 1);
    assert.equal((await peer.execute({ op: 'network-body', args: { requestId: 'r1' } })).body, 'fixture');
    await assert.rejects(peer.execute({ op: 'network-body', args: { requestId: 'foreign' } }), /classified/);
    assert.equal(f.calls.filter(c => c.method === 'Network.getResponseBody').length, 1);
});

test('a navigation racing page evaluation cannot read another origin', async () => {
    const { CuaPeer } = await load();
    for (const op of ['state', 'profile']) {
        let accesses = 0;
        const page = { async url() { return origin + '/'; } };
        const cap = { async readEvents() { throw Error('unexpected'); }, async send(method, args) {
            assert.equal(method, 'Runtime.evaluate');
            const context = { location: { origin: 'https://outside.invalid' }, document: { getElementById() { return null; } },
                window: { get ethereum() { accesses++; return null; } }, sessionStorage: { getItem() { accesses++; return null; } },
                fetch() { accesses++; throw Error('should not fetch'); } };
            try { return { result: { value: await vm.runInNewContext(args.expression, context) } }; }
            catch { return { exceptionDetails: { text: 'fixture exception' } }; }
        } };
        const peer = new CuaPeer({ targetPage: page, targetCap: cap, origin, content });
        await assert.rejects(peer.execute({ op, args: {} }), /page observation/);
        assert.equal(accesses, 0, op);
    }
});

test('a CUA relay step returns a scoped result without exposing the session key', async () => {
    const { CuaPeer } = await load(); const f = fixture(), token = 'a'.repeat(64), relayOrigin = 'http://127.0.0.1:18333';
    let reply;
    const relayPage = { async url() { return relayOrigin + '/'; } };
    const relayCap = { async send(method, args) {
        assert.equal(method, 'Runtime.evaluate');
        const context = { location: { origin: relayOrigin }, async fetch(url, options) {
            assert.equal(options.headers.Authorization, 'Bearer ' + token);
            if (url === '/next') return { status: 200, async json() { return { id: 1, op: 'state', args: {} }; } };
            assert.equal(url, '/reply'); reply = JSON.parse(options.body); return { status: 204 };
        } };
        return { result: { value: await vm.runInNewContext(args.expression, context) } };
    } };
    const peer = new CuaPeer({ targetPage: f.page, targetCap: f.cap, origin, content });
    assert.equal(await peer.relayOnce({ relayPage, relayCap, relayOrigin, sessionKey: token }), true);
    assert.equal(reply.id, 1); assert.equal(reply.ok, true); assert.equal(reply.targetOrigin, origin);
    assert.equal(reply.value.origin, origin); assert.equal(JSON.stringify(reply).includes(token), false);
});

test('a navigated relay cannot receive the session key', async () => {
    const { CuaPeer } = await load(); const f = fixture(); let sent = false;
    const peer = new CuaPeer({ targetPage: f.page, targetCap: f.cap, origin, content });
    await assert.rejects(peer.relayOnce({ relayPage: { async url() { return 'https://outside.invalid/'; } },
        relayCap: { async send() { sent = true; } }, relayOrigin: 'http://127.0.0.1:18333', sessionKey: 'a'.repeat(64) }), /relay origin/);
    assert.equal(sent, false);
});

test('control observations use fixed same-origin requests and reject arbitrary destinations', async () => {
    const { CuaPeer } = await load(); let requests = 0;
    const page = { async url() { return origin + '/'; }, async reload() {} };
    const cap = { async readEvents() { throw Error('unexpected'); }, async send(method, args) {
        assert.equal(method, 'Runtime.evaluate');
        const context = { location: { origin }, AbortSignal, Uint8Array, Date, btoa: value => Buffer.from(value, 'binary').toString('base64'),
            async fetch(url, options) { requests++; assert.equal(url, origin + '/api/v1/auth/profile'); assert.equal(options.redirect, 'error');
                return { status: 200, async arrayBuffer() { return Uint8Array.from([1, 2, 3]).buffer; } }; } };
        try { return { result: { value: await vm.runInNewContext(args.expression, context) } }; }
        catch { return { exceptionDetails: { text: 'fixture exception' } }; }
    } };
    const peer = new CuaPeer({ targetPage: page, targetCap: cap, origin, content });
    const observed = await peer.execute({ op: 'control-http', args: { url: origin + '/api/v1/auth/profile', options: {} } });
    assert.equal(observed.status, 200); assert.deepEqual(Buffer.from(observed.base64, 'base64'), Buffer.from([1, 2, 3]));
    assert.equal(requests, 1);
    const denied = new CuaPeer({ targetPage: page, targetCap: cap, origin, content });
    await assert.rejects(denied.execute({ op: 'control-http', args: { url: 'https://outside.invalid/', options: {} } }), /origin|invalid/);
    assert.equal(requests, 1);
});

test('reload checks the same target before and after the documented CUA action', async () => {
    const { CuaPeer } = await load(); const f = fixture(); let reloaded = 0;
    f.page.reload = async () => { reloaded++; };
    const peer = new CuaPeer({ targetPage: f.page, targetCap: f.cap, origin, content });
    await peer.execute({ op: 'reload', args: {} }); assert.equal(reloaded, 1);
});
