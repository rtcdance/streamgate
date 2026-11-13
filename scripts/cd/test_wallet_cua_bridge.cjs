const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');

const load = () => import('../h5-demo-acceptance/lib/wallet_cua_bridge.mjs');
function send(origin, method, path, token, data, overrides = {}, agent) {
    const body = data === undefined ? null : JSON.stringify(data);
    return new Promise((resolve, reject) => {
        const request = http.request(origin + path, { method, agent, headers: { Host: new URL(origin).host, Origin: origin,
            'Sec-Fetch-Site': 'same-origin', ...(token ? { Authorization: `Bearer ${token}` } : {}),
            ...(body ? { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) } : {}), ...overrides } }, response => {
            const chunks = []; response.on('data', chunk => chunks.push(chunk));
            response.on('end', () => resolve({ status: response.statusCode, headers: response.headers, text: Buffer.concat(chunks).toString() }));
        });
        request.on('error', reject); if (body) request.write(body); request.end();
    });
}

async function fixture(t) {
    const { CuaPageBridge } = await load();
    const bridge = new CuaPageBridge({ targetOrigin: 'http://localhost:18500', content: '12345678-1234-1234-1234-123456789abc', timeoutMs: 1000 });
    await bridge.start(); t.after(() => bridge.close());
    const page = await send(bridge.origin, 'GET', '/', null);
    const token = fs.readFileSync(bridge.sessionFile, 'utf8');
    assert.equal(page.status, 200); assert.match(token, /^[a-f0-9]{64}$/);
    assert.equal(page.text.includes(token), false);
    assert.equal(fs.statSync(bridge.sessionFile).mode & 0o077, 0);
    assert.equal(fs.statSync(path.dirname(bridge.sessionFile)).mode & 0o077, 0);
    return { bridge, token };
}

test('relays only a bounded approved operation and its matching response', async t => {
    const { bridge, token } = await fixture(t);
    const result = bridge.request('network-body', { requestId: '123.4' });
    const next = await send(bridge.origin, 'GET', '/next', token);
    assert.equal(next.status, 200); const task = JSON.parse(next.text);
    assert.equal(next.headers.connection, 'close');
    assert.deepEqual(task, { id: 1, op: 'network-body', args: { requestId: '123.4' } });
    assert.equal((await send(bridge.origin, 'POST', '/reply', token, { id: task.id, targetOrigin: 'http://localhost:18500', ok: true, value: { body: 'fixture' } })).status, 204);
    assert.deepEqual(await result, { body: 'fixture' });
    assert.equal((await send(bridge.origin, 'GET', '/next', token)).status, 204);
});

test('rejects foreign origins, wrong capability, arbitrary commands and concurrent requests', async t => {
    const { bridge, token } = await fixture(t);
    assert.equal((await send(bridge.origin, 'GET', '/next', 'wrong')).status, 403);
    assert.equal((await send(bridge.origin, 'GET', '/next', token, undefined, { Origin: 'https://outside.invalid' })).status, 403);
    assert.equal((await send(bridge.origin, 'GET', '/next', token, undefined, { Host: 'attacker.invalid' })).status, 403);
    await assert.rejects(bridge.request('Runtime.evaluate', { expression: 'arbitrary' }), /unsupported/);
    await assert.rejects(bridge.request('network-body', { requestId: '../escape' }), /invalid/);
    const pending = bridge.request('state', {});
    await assert.rejects(bridge.request('profile', {}), /pending/);
    const task = JSON.parse((await send(bridge.origin, 'GET', '/next', token)).text);
    await send(bridge.origin, 'POST', '/reply', token, { id: task.id, targetOrigin: 'http://localhost:18500', ok: true, value: { origin: 'http://localhost:18500' } });
    assert.equal((await pending).origin, 'http://localhost:18500');
});

test('permits Chrome top-level navigation but refuses cross-site embedding and API calls', async t => {
    const { bridge, token } = await fixture(t);
    const navigation = { Origin: undefined, 'Sec-Fetch-Site': 'cross-site', 'Sec-Fetch-Mode': 'navigate', 'Sec-Fetch-Dest': 'document' };
    delete navigation.Origin;
    // An omitted Origin matches browser top-level GET; do not use the helper's default.
    const top = await new Promise((resolve, reject) => {
        http.get(bridge.origin + '/', { headers: { Host: new URL(bridge.origin).host, ...navigation } }, r => { r.resume(); r.on('end', () => resolve(r.statusCode)); }).on('error', reject);
    });
    assert.equal(top, 200);
    assert.equal((await send(bridge.origin, 'GET', '/', null, undefined, { 'Sec-Fetch-Site': 'cross-site', 'Sec-Fetch-Mode': 'navigate', 'Sec-Fetch-Dest': 'iframe' })).status, 403);
    assert.equal((await send(bridge.origin, 'GET', '/next', token, undefined, { 'Sec-Fetch-Site': 'cross-site' })).status, 403);
});

test('wrong sequence or timeout fails closed and cannot be silently restarted', async t => {
    const { bridge, token } = await fixture(t);
    const pending = bridge.request('state', {}); const rejected = assert.rejects(pending, /sequence/);
    const task = JSON.parse((await send(bridge.origin, 'GET', '/next', token)).text);
    assert.equal((await send(bridge.origin, 'POST', '/reply', token, { id: task.id + 1, ok: true, value: {} })).status, 409);
    await rejected; await assert.rejects(bridge.request('state', {}), /failed/);
    const { CuaPageBridge } = await load(); const timed = new CuaPageBridge({ targetOrigin: 'http://localhost:18500', timeoutMs: 25 });
    await timed.start(); t.after(() => timed.close());
    await assert.rejects(timed.request('state', {}), /timeout/);
    await assert.rejects(timed.request('state', {}), /failed/);
});

test('a reply from a navigated target fails the entire capture', async t => {
    const { bridge, token } = await fixture(t);
    const pending = bridge.request('state', {}); const rejected = assert.rejects(pending, /origin/);
    const task = JSON.parse((await send(bridge.origin, 'GET', '/next', token)).text);
    assert.equal((await send(bridge.origin, 'POST', '/reply', token, { id: task.id, targetOrigin: 'http://localhost:18501', ok: true, value: {} })).status, 409);
    await rejected; await assert.rejects(bridge.request('state', {}), /failed/);
});

test('closing a bridge does not wait for a browser keep-alive socket', async () => {
    const { CuaPageBridge } = await load();
    const bridge = new CuaPageBridge({ targetOrigin: 'http://localhost:18500', timeoutMs: 1000 });
    await bridge.start();
    const agent = new http.Agent({ keepAlive: true });
    try {
        await new Promise((resolve, reject) => {
            http.get(bridge.origin + '/', { agent, headers: { Host: new URL(bridge.origin).host, 'Sec-Fetch-Site': 'none' } }, r => { r.resume(); r.on('end', resolve); }).on('error', reject);
        });
        let timer;
        await Promise.race([bridge.close(), new Promise((_, reject) => { timer = setTimeout(() => reject(Error('close hung on idle connection')), 250); })]).finally(() => clearTimeout(timer));
    } finally { agent.destroy(); await bridge.close(); }
});

test('immediate close after a reply delivers that reply and closes the live socket', async t => {
    const { bridge, token } = await fixture(t); const agent = new http.Agent({ keepAlive: true });
    try {
        const pending = bridge.request('state', {});
        const closing = pending.then(() => bridge.close());
        const task = JSON.parse((await send(bridge.origin, 'GET', '/next', token, undefined, {}, agent)).text);
        const response = await send(bridge.origin, 'POST', '/reply', token,
            { id: task.id, targetOrigin: 'http://localhost:18500', ok: true, value: { origin: 'http://localhost:18500' } }, {}, agent);
        assert.equal(response.status, 204);
        let timer;
        await Promise.race([closing, new Promise((_, reject) => { timer = setTimeout(() => reject(Error('close hung after reply')), 250); })]).finally(() => clearTimeout(timer));
    } finally { agent.destroy(); }
});

test('preserves UTF-8 when a reply splits a multibyte character across writes', async t => {
    const { bridge, token } = await fixture(t);
    const pending = bridge.request('state', {});
    const task = JSON.parse((await send(bridge.origin, 'GET', '/next', token)).text);
    const data = Buffer.from(JSON.stringify({ id: task.id, targetOrigin: 'http://localhost:18500', ok: true,
        value: { origin: 'http://localhost:18500', status: '测试' } }));
    const split = data.indexOf(Buffer.from('测试')) + 1;
    const status = await new Promise((resolve, reject) => {
        const req = http.request(bridge.origin + '/reply', { method: 'POST', headers: { Host: new URL(bridge.origin).host, Origin: bridge.origin,
            'Sec-Fetch-Site': 'same-origin', Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', 'Content-Length': data.length } },
        res => { res.resume(); res.on('end', () => resolve(res.statusCode)); });
        req.on('error', reject); req.write(data.subarray(0, split)); setImmediate(() => req.end(data.subarray(split)));
    });
    assert.equal(status, 204); assert.equal((await pending).status, '测试');
});

test('removes its private session capability when closed', async t => {
    const { bridge } = await fixture(t); const file = bridge.sessionFile, directory = path.dirname(file);
    assert.equal(fs.existsSync(file), true); await bridge.close();
    assert.equal(fs.existsSync(file), false); assert.equal(fs.existsSync(directory), false);
});

test('control requests cannot turn the relay into an arbitrary authenticated client', async t => {
    const { bridge, token } = await fixture(t);
    const content = '12345678-1234-1234-1234-123456789abc';
    await assert.rejects(bridge.request('control-http', { url: 'https://outside.invalid/', options: {} }), /invalid/);
    await assert.rejects(bridge.request('control-http', { url: 'http://localhost:18500/api/v1/auth/logout', options: { method: 'POST' } }), /invalid/);
    const pending = bridge.request('control-http', { url: `http://localhost:18500/api/v1/streaming/${content}/manifest.m3u8`, options: {} });
    const task = JSON.parse((await send(bridge.origin, 'GET', '/next', token)).text);
    assert.equal(task.op, 'control-http');
    await send(bridge.origin, 'POST', '/reply', token, { id: task.id, targetOrigin: 'http://localhost:18500', ok: true, value: { status: 401 } });
    assert.equal((await pending).status, 401);
});
