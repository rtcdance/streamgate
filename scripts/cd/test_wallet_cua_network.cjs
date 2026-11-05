const test = require('node:test');
const assert = require('node:assert/strict');
const { pathToFileURL } = require('node:url');
const path = require('node:path');

async function subject(options = {}) {
    const { CuaNetworkCapture } = await import(pathToFileURL(path.join(__dirname, '../h5-demo-acceptance/lib/wallet_cua_network.mjs')));
    return new CuaNetworkCapture({ origin: 'http://localhost:18500', content: '12345678-1234-1234-1234-123456789abc',
        getBody: async () => ({ body: '{"token":"fixture"}', base64Encoded: false }), ...options });
}

function event(sequence, method, params) { return { sequence, method, params }; }
function request(sequence, id, url, extra = {}) {
    return event(sequence, 'Network.requestWillBeSent', { requestId: id, request: { url, method: 'POST', postData: '{}', ...extra } });
}

test('captures an original same-origin login with request and response bytes', async () => {
    const c = await subject();
    const rows = await c.consume({ truncated: false, events: [
        request(1, 'r1', 'http://localhost:18500/api/v1/auth/login'),
        event(2, 'Network.responseReceived', { requestId: 'r1', response: { status: 200 } }),
        event(3, 'Network.loadingFinished', { requestId: 'r1' }),
    ] });
    assert.equal(rows.length, 1); assert.equal(rows[0].status, 200);
    assert.equal(rows[0].requestBody, '{}'); assert.equal(rows[0].body.toString(), '{"token":"fixture"}');
    assert.equal(rows[0].url, 'http://localhost:18500/api/v1/auth/login');
});

test('ignores foreign traffic and preserves encoded media response bytes', async () => {
    const c = await subject({ getBody: async () => ({ body: 'AAEC/w==', base64Encoded: true }) });
    const rows = await c.consume({ truncated: false, events: [
        request(1, 'foreign', 'https://example.invalid/api/v1/auth/login'),
        request(2, 'media', 'http://localhost:18500/api/v1/streaming/12345678-1234-1234-1234-123456789abc/segment/0.ts'),
        event(3, 'Network.responseReceived', { requestId: 'media', response: { status: 200 } }),
        event(4, 'Network.loadingFinished', { requestId: 'media' }),
    ] });
    assert.equal(rows.length, 1); assert.deepEqual(rows[0].body, Buffer.from([0, 1, 2, 255]));
});

test('rejects truncated, reordered, incomplete and redirected evidence', async () => {
    await assert.rejects((await subject()).consume({ truncated: true, events: [] }), /truncated/);
    await assert.rejects((await subject()).consume({ truncated: false, events: [event(2, 'Network.loadingFinished', { requestId: 'x' }), event(1, 'Network.loadingFinished', { requestId: 'y' })] }), /sequence/);
    const c = await subject();
    await assert.rejects(c.consume({ truncated: false, events: [request(1, 'r1', 'http://localhost:18500/api/v1/auth/login'), event(2, 'Network.loadingFinished', { requestId: 'r1' })] }), /response/);
    const d = await subject();
    await assert.rejects(d.consume({ truncated: false, events: [request(1, 'r1', 'http://localhost:18500/api/v1/auth/login'), event(2, 'Network.requestWillBeSent', { requestId: 'r1', request: { url: 'http://localhost:18500/api/v1/auth/login' }, redirectResponse: { status: 302 } })] }), /redirect/);
});

test('rejects malformed body and stays failed after losing evidence', async () => {
    const c = await subject({ getBody: async () => ({ body: 'not valid base64', base64Encoded: true }) });
    await assert.rejects(c.consume({ truncated: false, events: [
        request(1, 'r1', 'http://localhost:18500/api/v1/auth/login'),
        event(2, 'Network.responseReceived', { requestId: 'r1', response: { status: 200 } }),
        event(3, 'Network.loadingFinished', { requestId: 'r1' }),
    ] }), /base64/);
    await assert.rejects(c.consume({ truncated: false, events: [] }), /already failed/);
});

test('captures extra authorization headers and preserves browser timestamps', async () => {
    const c = await subject();
    const rows = await c.consume({ truncated: false, events: [
        request(1, 'r1', 'http://localhost:18500/api/v1/streaming/12345678-1234-1234-1234-123456789abc/manifest.m3u8'),
        event(2, 'Network.requestWillBeSentExtraInfo', { requestId: 'r1', headers: { Authorization: 'Bearer test-only' } }),
        event(3, 'Network.responseReceived', { requestId: 'r1', timestamp: 10.2, response: { status: 200 } }),
        event(4, 'Network.loadingFinished', { requestId: 'r1', timestamp: 10.4 }),
    ] });
    assert.equal(rows[0].headers.authorization, 'Bearer test-only');
    assert.equal(rows[0].responseTimestamp, 10.2); assert.equal(rows[0].finishTimestamp, 10.4);
});

test('correlates extra authorization headers delivered before the request', async () => {
    const c = await subject();
    const rows = await c.consume({ truncated: false, events: [
        event(1, 'Network.requestWillBeSentExtraInfo', { requestId: 'r1', headers: { Authorization: 'Bearer early', Cookie: 'must-not-retain' } }),
        request(2, 'r1', 'http://localhost:18500/api/v1/streaming/12345678-1234-1234-1234-123456789abc/manifest.m3u8'),
        event(3, 'Network.responseReceived', { requestId: 'r1', hasExtraInfo: true, response: { status: 200 } }),
        event(4, 'Network.loadingFinished', { requestId: 'r1' }),
    ] });
    assert.equal(rows.length, 1); assert.equal(rows[0].headers.authorization, 'Bearer early');
    assert.equal(Object.keys(rows[0].headers).some(k => k.toLowerCase() === 'cookie'), false);
});

test('does not emit evidence until a promised late extra-info event arrives', async () => {
    const c = await subject();
    const first = await c.consume({ truncated: false, events: [
        request(1, 'r1', 'http://localhost:18500/api/v1/streaming/12345678-1234-1234-1234-123456789abc/manifest.m3u8'),
        event(2, 'Network.responseReceived', { requestId: 'r1', hasExtraInfo: true, response: { status: 200 } }),
        event(3, 'Network.loadingFinished', { requestId: 'r1' }),
    ] });
    assert.equal(first.length, 0);
    const second = await c.consume({ truncated: false, events: [event(4, 'Network.requestWillBeSentExtraInfo', { requestId: 'r1', headers: { Authorization: 'Bearer late' } })] });
    assert.equal(second.length, 1); assert.equal(second[0].headers.authorization, 'Bearer late');
    assert.equal(second[0].finishedSequence, 3);
    c.finish();
});

test('refuses to finish if a promised extra-info event was lost', async () => {
    const c = await subject();
    await c.consume({ truncated: false, events: [
        request(1, 'r1', 'http://localhost:18500/api/v1/auth/login'),
        event(2, 'Network.responseReceived', { requestId: 'r1', hasExtraInfo: true, response: { status: 200 } }),
        event(3, 'Network.loadingFinished', { requestId: 'r1' }),
    ] });
    assert.throws(() => c.finish(), /incomplete/);
});

test('a finished capture cannot be reused for a later wallet phase', async () => {
    const c = await subject(); c.finish();
    await assert.rejects(c.consume({ truncated: false, events: [] }), /finished/);
    assert.throws(() => c.finish(), /finished/);
});

test('browser response epoch derives from the original request clock and rejects stale or impossible timing', async () => {
    const { browserResponseEpoch } = await import('../h5-demo-acceptance/lib/wallet_cua_network.mjs');
    const original = { requestTimestamp: 1000, requestWallTime: 1700000000, responseTimestamp: 1000.125 };
    assert.equal(browserResponseEpoch(original, 1700000000200), 1700000000125);
    for (const value of [{ ...original, responseTimestamp: 999.9 }, { ...original, responseTimestamp: 1121 },
        { ...original, requestWallTime: NaN }, { ...original, requestTimestamp: undefined }]) {
        assert.throws(() => browserResponseEpoch(value, 1700000000200), /clock|timing/);
    }
    assert.throws(() => browserResponseEpoch(original, 1700000006000), /stale|timing/);
    assert.throws(() => browserResponseEpoch(original, 1699999998000), /future|timing/);
});

test('negative wallet phases can observe authentication without a content fixture', async () => {
    const c = await subject({ content: null });
    const rows = await c.consume({ truncated: false, events: [
        request(1, 'r1', 'http://localhost:18500/api/v1/auth/challenge'),
        event(2, 'Network.responseReceived', { requestId: 'r1', response: { status: 200 } }),
        event(3, 'Network.loadingFinished', { requestId: 'r1' }),
        request(4, 'other', 'http://localhost:18500/api/v1/streaming/null/manifest.m3u8'),
    ] });
    assert.equal(rows.length, 1); assert.equal(c.pending.size, 0); c.finish();
});
