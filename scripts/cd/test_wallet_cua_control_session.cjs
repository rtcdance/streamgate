const test = require('node:test');
const assert = require('node:assert/strict');
const load = () => import('../h5-demo-acceptance/lib/wallet_cua_control_session.mjs');
const origin = 'http://localhost:18500', content = '12345678-1234-1234-1234-123456789abc';

function fixture() {
    let active = 0, maximum = 0; const calls = [];
    const bridge = { async request(op, args) {
        active++; maximum = Math.max(maximum, active); calls.push({ op, args });
        await new Promise(resolve => setImmediate(resolve)); active--;
        if (op === 'control-http') return { status: 200, base64: 'AAEC', at: Date.now() };
        if (op === 'control-state') return { origin, token: null, identity: null, playback: { at: Date.now() } };
        if (op === 'reload') return { origin };
        throw Error('unexpected operation');
    } };
    return { bridge, calls, maximum: () => maximum };
}

test('serializes concurrent controls while retaining original response bytes', async () => {
    const { CuaControlSession } = await load(); const f = fixture();
    const session = new CuaControlSession({ bridge: f.bridge, origin, content });
    const url = origin + `/api/v1/streaming/${content}/manifest.m3u8`;
    const [a, b] = await Promise.all([session.http(url, {}), session.http(url, {})]);
    assert.equal(f.maximum(), 1); assert.deepEqual(a.bytes, Buffer.from([0, 1, 2])); assert.equal(b.status, 200);
    assert.equal((await session.state()).origin, origin); await session.reload(); await session.close();
    await assert.rejects(session.state(), /closed/);
});

test('refuses foreign requests, malformed bytes and impossible response times', async () => {
    const { CuaControlSession } = await load();
    let f = fixture(), session = new CuaControlSession({ bridge: f.bridge, origin, content });
    await assert.rejects(session.http('https://outside.invalid/', {}), /origin|invalid/); assert.equal(f.calls.length, 0);
    for (const result of [{ status: 200, base64: '!bad!', at: Date.now() }, { status: 200, base64: '', at: 1 }]) {
        f = fixture(); f.bridge.request = async () => result;
        session = new CuaControlSession({ bridge: f.bridge, origin, content });
        await assert.rejects(session.http(origin + '/api/v1/auth/profile', {}), /invalid|time/);
        await assert.rejects(session.state(), /failed/);
    }
});

test('closing drains already accepted work and refuses new observations', async () => {
    const { CuaControlSession } = await load(); let release, entered;
    const started = new Promise(resolve => { entered = resolve; });
    const bridge = { async request() { entered(); return new Promise(resolve => { release = resolve; }); } };
    const session = new CuaControlSession({ bridge, origin, content });
    const pending = session.http(origin + '/api/v1/auth/profile', {}); await started;
    const closing = session.close(); await assert.rejects(session.state(), /closed|closing/);
    release({ status: 200, base64: '', at: Date.now() }); await pending; await closing;
});
