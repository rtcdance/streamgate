const test = require('node:test');
const assert = require('node:assert/strict');
const { pathToFileURL } = require('node:url');
const path = require('node:path');
const ethers = require('../../h5-demo/vendor/ethers.umd.min.js');
const subject = '0x' + '2'.repeat(40);
const jwt = claims => `${Buffer.from('{}').toString('base64url')}.${Buffer.from(JSON.stringify(claims)).toString('base64url')}.fixture`;

async function fixture() {
    const { WalletPhaseNetwork } = await import(pathToFileURL(path.join(__dirname, '../h5-demo-acceptance/lib/wallet_phase_network.mjs')));
    const controller = ethers.Wallet.createRandom();
    const config = { origin: 'http://localhost:18500', content: '12345678-1234-1234-1234-123456789abc',
        subject, controller: controller.address.toLowerCase(), nft: '0x' + '3'.repeat(40), phase: 'contract' };
    const report = { challenges: [], admissions: [], refreshes: [], issues: [], media: [] };
    const observer = new WalletPhaseNetwork({ config, report, ethers });
    const challenge = { challenge_id: 'test-challenge', wallet: subject, chain_id: 11155111, signing_type: 'siwe', nonce: '12345678',
        message: `localhost:18500 wants you to sign in with your Ethereum account:\n${subject}\n\nURI: http://localhost:18500/login\nVersion: 1\nChain ID: 11155111\nNonce: 12345678` };
    const signature = await controller.signMessage(challenge.message);
    const record = (url, body, extra = {}) => ({ url: config.origin + url, status: 200, at: 1000, body: Buffer.from(JSON.stringify(body)), requestBody: '', headers: {}, ...extra });
    return { observer, report, config, challenge, signature, record };
}

test('independent network records require the fresh challenge and real controller signature', async () => {
    const e = await fixture();
    const login = e.record('/api/v1/auth/login', { token: 'test-token' }, { requestBody: JSON.stringify({ challenge_id: e.challenge.challenge_id, wallet: subject, chain_id: 11155111, signature: e.signature }) });
    await e.observer.observe(login); assert.equal(e.report.admissions.length, 0);
    await e.observer.observe(e.record('/api/v1/auth/challenge', e.challenge));
    await e.observer.observe(login); assert.equal(e.report.admissions.length, 1);
    assert.equal(e.observer.privateProofs[0].signature, e.signature);
    assert.equal(e.report.admissions[0].controller, e.config.controller);
});

test('a different controller, invalid origin or altered challenge cannot establish admission', async () => {
    const e = await fixture(); await e.observer.observe(e.record('/api/v1/auth/challenge', e.challenge));
    const wrong = await ethers.Wallet.createRandom().signMessage(e.challenge.message);
    const login = e.record('/api/v1/auth/login', { token: 'test-token' }, { requestBody: JSON.stringify({ challenge_id: e.challenge.challenge_id, wallet: subject, chain_id: 11155111, signature: wrong }) });
    await assert.rejects(e.observer.observe(login), /controller/);
    const foreign = { ...login, url: 'https://outside.invalid/api/v1/auth/login' };
    await e.observer.observe(foreign); assert.equal(e.report.admissions.length, 0);
    const bad = await fixture(); bad.challenge.message = bad.challenge.message.replaceAll('localhost:18500', 'elsewhere.invalid');
    await bad.observer.observe(bad.record('/api/v1/auth/challenge', bad.challenge));
    const requestBody = JSON.stringify({ challenge_id: bad.challenge.challenge_id, wallet: subject, chain_id: 11155111, signature: bad.signature });
    await assert.rejects(bad.observer.observe(bad.record('/api/v1/auth/login', { token: 'test-token' }, { requestBody })), /origin|SIWE/);
});

test('only the observed admission can issue or renew a media capability', async () => {
    const e = await fixture();
    const access = jwt({ sub: subject, iat: 1000, exp: 1060 });
    const next = jwt({ sub: subject, iat: 1030, exp: 1090 });
    const capability = jwt({ wallet_address: subject, chain_id: 11155111, contract: e.config.nft, content_id: e.config.content,
        token_type: 'playback', account_kind: 'erc1271', iat: 1000, exp: 1060, wallet_session_exp: 8200 });
    await e.observer.observe(e.record('/api/v1/auth/challenge', e.challenge));
    await e.observer.observe(e.record('/api/v1/auth/login', { token: access }, { requestBody: JSON.stringify({ challenge_id: e.challenge.challenge_id, wallet: subject, chain_id: 11155111, signature: e.signature }) }));
    const prefix = `/api/v1/streaming/${e.config.content}`;
    const body = Buffer.from(`#EXTM3U\n${prefix}/manifest.m3u8?playback_token=${capability}\n`);
    await e.observer.observe({ url: e.config.origin + prefix + '/manifest.m3u8', status: 200, at: 1000, body, headers: { authorization: 'Bearer foreign' } });
    assert.equal(e.report.issues.length, 0);
    await e.observer.observe({ url: e.config.origin + prefix + '/manifest.m3u8', status: 200, at: 1001, body, headers: { authorization: 'Bearer ' + access } });
    assert.equal(e.report.issues.length, 1);
    await e.observer.observe({ url: e.config.origin + prefix + '/segment/0.ts?playback_token=' + capability, status: 200, at: 1002, body: Buffer.from('media'), headers: {} });
    assert.equal(e.report.media.length, 1); assert.equal(e.report.media[0].size, 5);
    await e.observer.observe(e.record('/api/v1/auth/refresh', { token: next }, { requestBody: JSON.stringify({ token: access }) }));
    assert.equal(e.report.refreshes.length, 1); assert.equal(e.observer.privateProofs[0].credentials.length, 2);
    await e.observer.observe(e.record('/api/v1/auth/refresh', { token: access }, { requestBody: JSON.stringify({ token: 'foreign' }) }));
    assert.equal(e.report.refreshes.length, 1);
});

test('page network records flow into the same signature verification rules', async () => {
    const e = await fixture();
    const { CuaNetworkCapture } = await import('../h5-demo-acceptance/lib/wallet_cua_network.mjs');
    const bodies = { challenge: e.challenge, login: { token: 'test-token' } };
    const capture = new CuaNetworkCapture({ origin: e.config.origin, content: e.config.content,
        getBody: async id => ({ body: JSON.stringify(bodies[id]), base64Encoded: false }) });
    const postData = JSON.stringify({ challenge_id: e.challenge.challenge_id, wallet: subject, chain_id: 11155111, signature: e.signature });
    const events = [];
    for (const [id, pathname, requestBody] of [['challenge', '/api/v1/auth/challenge', '{}'], ['login', '/api/v1/auth/login', postData]]) {
        events.push({ sequence: events.length + 1, method: 'Network.requestWillBeSent', params: { requestId: id, request: { url: e.config.origin + pathname, method: 'POST', postData: requestBody } } });
        events.push({ sequence: events.length + 1, method: 'Network.responseReceived', params: { requestId: id, timestamp: 1, response: { status: 200 } } });
        events.push({ sequence: events.length + 1, method: 'Network.loadingFinished', params: { requestId: id, timestamp: 2 } });
    }
    const rows = await capture.consume({ truncated: false, events }); capture.finish();
    for (const row of rows) await e.observer.observe({ ...row, at: 1000 }); // Fixed test clock; not public evidence.
    assert.equal(e.report.admissions.length, 1); assert.equal(e.observer.privateProofs[0].signature, e.signature);
});
