const test = require('node:test');
const assert = require('node:assert/strict');
const load = () => import('../h5-demo-acceptance/lib/wallet_cua_control_contract.mjs');
const origin = 'http://localhost:18500', content = '12345678-1234-1234-1234-123456789abc', token = 'aaa.bbb.ccc';

test('allows only the original profile, manifest, media and bounded refresh operations', async () => {
    const { validateControlRequest } = await load();
    for (const [url, options] of [
        ['/api/v1/auth/profile', { headers: { Authorization: 'Bearer ' + token } }],
        [`/api/v1/streaming/${content}/manifest.m3u8`, {}],
        [`/api/v1/streaming/${content}/segment/360_001.ts?quality=360p&playback_token=${token}`, {}],
        ['/api/v1/auth/refresh', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) }],
    ]) {
        const result = validateControlRequest({ origin, content, url: origin + url, options });
        assert.equal(new URL(result.url).origin, origin);
    }
});

test('rejects cross-origin, path escape, injected headers and arbitrary writes', async () => {
    const { validateControlRequest } = await load();
    const cases = [
        ['https://outside.invalid/api/v1/auth/profile', {}],
        ['http://user:secret@localhost:18500/api/v1/auth/profile', {}],
        [origin + '/api/v1/auth/logout', { method: 'POST' }],
        [origin + '/api/v1/auth/profile', { headers: { Cookie: 'secret' } }],
        [origin + '/api/v1/auth/profile', { method: 'DELETE' }],
        [origin + '/api/v1/auth/refresh', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify({ token: 'different' }) }],
        [origin + `/api/v1/streaming/${content}/segment/../private?playback_token=${token}`, {}],
        [origin + `/api/v1/streaming/${content}/segment/a.ts?playback_token=${token}&next=https://outside.invalid`, {}],
        [origin + '/api/v1/auth/profile', { redirect: 'follow' }],
    ];
    for (const [url, options] of cases) assert.throws(() => validateControlRequest({ origin, content, url, options }), /invalid|unsupported|origin/i);
});
