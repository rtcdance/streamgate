const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');
const root = path.resolve(__dirname, '../..');
const A = '0x1111111111111111111111111111111111111111';
const C = '0x2222222222222222222222222222222222222222';
const now = 1900000000000;
function env() {
    const storage = () => { const m = new Map(); return { getItem: (k) => m.get(k) || null, setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) }; };
    const timers = new Map(); let timerID = 0;
    const window = new EventTarget();
    window.location = new URL('http://localhost:18000/');
    window.setTimeout = (f, ms) => { timers.set(++timerID, { f, ms }); return timerID; };
    window.clearTimeout = (id) => timers.delete(id);
    class CustomEvent extends Event { constructor(name, options = {}) { super(name); this.detail = options.detail; } }
    class Clock extends Date { static now() { return now; } }
    const context = vm.createContext({ window, CustomEvent, Date: Clock, console: { log() {}, warn() {}, error() {} }, URL, URLSearchParams, Blob, AbortSignal, atob, sessionStorage: storage(), localStorage: storage(), demoWallet: { isDemoMode: false }, document: { getElementById: () => null }, fetch: (...args) => context.fetchImpl(...args) });
    for (const f of ['auth.js', 'player.js', 'api.js']) vm.runInContext(fs.readFileSync(path.join(root, 'h5-demo/js', f), 'utf8'), context);
    const classes = vm.runInContext('({ AuthService, HLSPlayer, APIService })', context);
    const wallet = { address: A, chain: 11155111, getAddress() { return this.address; }, getChainId() { return this.chain; }, async ensureProvider() { return { request: async ({ method }) => method === 'eth_accounts' ? [this.address] : '0x' + this.chain.toString(16) }; }, async signMessage(message) { this.signed = message; return '0x1234'; } };
    const jwt = (subject = C, extra = {}) => 'header.' + Buffer.from(JSON.stringify({ sub: subject, wallet_address: subject, chain_id: wallet.chain, account_kind: subject === C ? 'erc1271' : 'eoa', exp: now / 1000 + 60, wallet_session_exp: now / 1000 + 7200, ...extra })).toString('base64url') + '.signature';
    const challenge = (subject = C) => ({ challenge_id: 'one', wallet: subject, chain_id: wallet.chain, nonce: '12345678', signing_type: 'siwe', message: `localhost:18000 wants you to sign in with your Ethereum account:\n${subject}\n\nURI: http://localhost:18000/login\nVersion: 1\nChain ID: ${wallet.chain}\nNonce: 12345678` });
    const api = { token: null, getAuthToken() { return this.token; }, setAuthToken(t) { this.token = t; }, clearAuthToken() { this.token = null; }, async getChallenge(subject, chain, signType) { this.challengeRequest = { subject, chain, signType }; return challenge(subject); }, async loginWithChallenge(subject, id, signature, chain) { this.loginRequest = { subject, id, signature, chain }; return { token: jwt(subject) }; }, async verifyNFT(subject) { this.nftSubject = subject; return { has_nft: true }; }, async refreshToken() { return { token: jwt(C, { exp: now / 1000 + 90 }) }; } };
    const auth = new classes.AuthService(api, wallet);
    return { ...classes, context, window, wallet, api, auth, jwt, challenge, timers, CustomEvent };
}
async function login(e) { e.auth.setLoginSubject(C); await e.auth.requestChallenge(); await e.auth.login(); }

test('CW UI keeps signer A separate from subject C and uses the server profile', async () => {
    const e = env(); await login(e);
    assert.deepEqual(e.api.challengeRequest, { subject: C, chain: 0, signType: 'siwe' });
    assert.equal(e.api.loginRequest.subject, C); assert.equal(e.auth.identity.controller, A);
    assert.equal(e.auth.identity.subject, C); assert.equal(e.auth.identity.kind, 'erc1271');
    assert.match(e.wallet.signed, new RegExp(C));
    await e.auth.verifyNFT('fixture', 11155111); assert.equal(e.api.nftSubject, C);
    assert.equal([...e.timers.values()][0].ms, 30000);
});

test('wrong chain, foreign SIWE origin and wrong admitted subject fail before use', async () => {
    const e = env(); e.auth.setLoginSubject(C);
    e.api.getChallenge = async () => ({ ...e.challenge(), chain_id: 31337 });
    await assert.rejects(e.auth.requestChallenge(), /Wrong network/); assert.equal(e.wallet.signed, undefined);
    e.api.getChallenge = async () => ({ ...e.challenge(), message: e.challenge().message.replaceAll('localhost:18000', 'evil.example') });
    await assert.rejects(e.auth.requestChallenge(), /origin/); assert.equal(e.wallet.signed, undefined);
    e.api.getChallenge = async () => e.challenge(); e.api.loginWithChallenge = async () => ({ token: e.jwt(A) });
    await e.auth.requestChallenge(); await assert.rejects(e.auth.login(), /identity mismatch/); assert.equal(e.api.token, null);
});

test('parallel renewals use one original token; account switch discards late success', async () => {
    const e = env(); await login(e);
    let release, entered; const reached = new Promise((r) => { entered = r; }); let calls = 0;
    e.api.refreshToken = async () => { calls++; entered(); return new Promise((r) => { release = r; }); };
    const renewals = Array.from({ length: 8 }, () => e.auth.refresh()); await reached;
    e.wallet.address = C; e.window.dispatchEvent(new e.CustomEvent('wallet:accountChanged', { detail: { address: C } }));
    release({ token: e.jwt() }); const results = await Promise.allSettled(renewals);
    assert.equal(calls, 1); assert(results.every((r) => r.status === 'rejected'));
    assert.equal(e.api.token, null); assert.equal(e.auth.identity, null); assert.equal(e.timers.size, 0);
});

test('dependency failure preserves only existing lease; definitive rejection clears it', async () => {
    const e = env(); await login(e); const original = e.api.token;
    const states = []; e.window.addEventListener('auth:state', (event) => states.push(event.detail.state));
    e.api.refreshToken = async () => { throw Object.assign(new Error('RPC down'), { status: 503 }); };
    await assert.rejects(e.auth.refresh()); assert.equal(e.api.token, original);
    assert.equal(e.auth.identity.expiresAt, now + 60000); assert.equal(states.at(-1), 'dependency-unavailable');
    assert.equal([...e.timers.values()][0].ms, 5000);
    e.api.refreshToken = async () => { throw Object.assign(new Error('owner changed'), { status: 401 }); };
    await assert.rejects(e.auth.refresh()); assert.equal(e.api.token, null); assert.equal(states.at(-1), 'authorization-ended');
});

test('expired credentials are not authenticated; wallet-chain switch clears challenges', async () => {
    const e = env(); await login(e); e.auth.identity.expiresAt = now - 1;
    assert.equal(e.auth.isLoggedIn(), false);
    e.window.dispatchEvent(new e.CustomEvent('wallet:chainChanged', { detail: { chainId: 1 } }));
    assert.equal(e.auth.getToken(), null); assert.equal(e.auth.currentChallenge, null);
});

test('media loader rotates capability only within the current origin and content', () => {
    const e = env(); const player = new e.HLSPlayer(null);
    player.manifestUrl = 'http://localhost:18000/api/v1/streaming/demo/manifest.m3u8';
    player.captureCapability('#EXTM3U\n/api/v1/streaming/demo/manifest.m3u8?quality=360&playback_token=NEW\n', e.window.location.origin);
    assert.match(player.authorizedURL('http://localhost:18000/api/v1/streaming/demo/segment/0.ts?playback_token=OLD'), /playback_token=NEW/);
    for (const raw of ['http://evil.example/api/v1/streaming/demo/segment/0.ts?playback_token=OLD', 'http://localhost:18000/api/v1/streaming/other/segment/0.ts?playback_token=OLD']) assert.equal(player.authorizedURL(raw), raw);
    assert.throws(() => player.captureCapability('#EXTM3U\nhttps://evil.example/a?playback_token=X\n', e.window.location.origin), /origin/);
});

test('late media renewal cannot restore a stopped session', async () => {
    const e = env(); const player = new e.HLSPlayer(null);
    player.currentVideoId = 'demo'; player.manifestUrl = 'http://localhost:18000/api/v1/streaming/demo/manifest.m3u8';
    let release; e.context.fetchImpl = async () => new Promise((r) => { release = r; });
    const renewal = player.refreshAuthorization('new-access'); player.endPlayback('wallet-changed');
    release({ ok: true, text: async () => '#EXTM3U\n/api/v1/streaming/demo/segment/0.ts?playback_token=NEW\n' });
    await renewal; assert.equal(player.playbackToken, null); assert.equal(player.currentVideoId, null);
});

test('a late 401 never clears the replacement access token', async () => {
    const e = env(); const api = new e.APIService('http://localhost:18000'); api.setAuthToken('old');
    let release; e.context.fetchImpl = async () => new Promise((r) => { release = r; });
    const pending = api.get('/protected'); api.setAuthToken('new');
    release({ ok: false, status: 401, json: async () => ({ error: 'expired' }) });
    await assert.rejects(pending); assert.equal(api.getAuthToken(), 'new');
});

test('late initial media load cannot resume playback after identity change', async () => {
    const e = env(); const video = { src: '', pause() {}, load() {}, removeAttribute() {}, addEventListener() {} };
    const player = new e.HLSPlayer(video);
    let release; e.context.fetchImpl = async () => new Promise((r) => { release = r; });
    const loading = player.loadVideo('demo', { token: 'old-access', baseUrl: e.window.location.origin });
    player.endPlayback('wallet-changed');
    release({ ok: true, text: async () => '#EXTM3U\n/api/v1/streaming/demo/segment/0.ts?playback_token=OLD\n' });
    await assert.rejects(loading, /cancelled/);
    assert.equal(player.playbackToken, null); assert.equal(player.currentVideoId, null); assert.equal(video.src, '');
});
