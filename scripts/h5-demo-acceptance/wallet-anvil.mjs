import { chromium } from 'playwright';
import { assertLaunchable } from './lib/browser_preflight.mjs';
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { createServer, connect } from 'node:net';
import { createHash } from 'node:crypto';
const require = createRequire(import.meta.url);
const ethers = require('../../h5-demo/vendor/ethers.umd.min.js');
const { localProvider } = require('../cd/prepare_wallet_anvil.cjs');
const fixture = JSON.parse(readFileSync(process.env.W3_FIXTURE_PATH, 'utf8'));
if (fixture.network !== 'ANVIL_LOCAL_ONLY' || fixture.chainId !== 31337) throw new Error('Local fixture required; never use this adapter on a public chain');
const mode = process.env.W3_MODE;
if (!['monolith', 'api-gateway'].includes(mode)) throw new Error('W3_MODE required');
const base = process.env.W3_H5_BASE;
const output = process.env.W3_REPORT_DIR;
if (!base || !output || !process.env.W3_MEDIA_FILE) throw new Error('H5, media and report paths required');
mkdirSync(output, { recursive: true });
const checks = [], samples = [], errors = [], proxies = [];
const hash = (v) => createHash('sha256').update(v).digest('hex');
const same = (a, b) => String(a).toLowerCase() === String(b).toLowerCase();
const claim = (token) => JSON.parse(Buffer.from(token.split('.')[1], 'base64url'));
function check(id, ok, detail = {}) { checks.push({ id, ok, detail }); if (!ok) throw new Error(`Required check failed: ${id}`); console.log(`PASS ${mode} ${id}`); }
const provider = localProvider(process.env.W3_ANVIL_RPC);
if (Number(await provider.send('eth_chainId', [])) !== 31337) throw new Error('Refusing Anvil key on another chain');
const demo = readFileSync(new URL('../../h5-demo/js/demo.js', import.meta.url), 'utf8');
const owner = new ethers.Wallet(demo.match(/const DEMO_ANVIL_KEY\s*=\s*['"](0x[0-9a-fA-F]{64})['"]/)[1], provider);
if (!same(owner.address, fixture.controller)) throw new Error('Fixture controller mismatch');
for (const item of JSON.parse(process.env.W3_PROXY_TARGETS || '[]')) {
    const proxy = createServer((client) => {
        const upstream = connect(item.targetPort, item.targetHost);
        client.on('error', () => upstream.destroy()); upstream.on('error', () => client.destroy());
        client.pipe(upstream).pipe(client);
    });
    await new Promise((resolve, reject) => { proxy.once('error', reject); proxy.listen(item.listenPort, '127.0.0.1', resolve); }); proxies.push(proxy);
}
let browser, page;
const mediaResponses = [], signedMessages = [];
async function api(method, endpoint, body, token) {
    const credential = token || await page.evaluate(() => sessionStorage.getItem('streamgate_token'));
    const response = await fetch(new URL(endpoint, base), { method, headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${credential}` }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(10000) });
    const text = await response.text(); let data; try { data = JSON.parse(text); } catch { data = text; }
    return { status: response.status, data };
}
async function state() {
    const s = await page.evaluate(() => {
        const token = sessionStorage.getItem('streamgate_token'); const v = document.getElementById('video-player');
        return { token, time: v.currentTime, paused: v.paused, error: v.error?.message, identity: window.__cwLastIdentity, playbackState: document.getElementById('player-status').dataset.sgPlaybackState };
    });
    const c = s.token ? claim(s.token) : null;
    return { ...s, claims: c, tokenHash: s.token ? hash(s.token) : null };
}
try {
    await assertLaunchable(chromium);
    browser = await chromium.launch({ headless: true, args: ['--autoplay-policy=no-user-gesture-required'] });
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    page = await context.newPage(); page.on('pageerror', (error) => errors.push(error.message));
    page.on('response', (response) => { const url = new URL(response.url()); if (url.pathname.includes('/segment/') && url.searchParams.has('playback_token')) mediaResponses.push({ at: Date.now(), status: response.status(), tokenHash: hash(url.searchParams.get('playback_token')), claims: claim(url.searchParams.get('playback_token')) }); });
    await page.exposeFunction('__cwWalletRequest', async ({ method, params }) => {
        switch (method) {
            case 'eth_requestAccounts': case 'eth_accounts': return [owner.address];
            case 'eth_chainId': return provider.send(method, []);
            case 'personal_sign':
                if (!same(params[1], owner.address)) throw new Error('Wrong signer');
                signedMessages.push({ digest: ethers.utils.hashMessage(ethers.utils.arrayify(params[0])), signer: owner.address });
                return owner.signMessage(ethers.utils.arrayify(params[0]));
            default: throw new Error('Unsupported local wallet method');
        }
    });
    await page.addInitScript(() => {
        const listeners = new Map();
        window.__cwEmit = (name, value) => { for (const listener of listeners.get(name) || []) listener(value); };
        window.addEventListener('auth:state', (event) => { window.__cwLastIdentity = event.detail.identity; });
        window.ethereum = { isMetaMask: true, on(name, callback) { if (!listeners.has(name)) listeners.set(name, []); listeners.get(name).push(callback); }, request(request) { if (request.method === 'personal_sign' && window.__cwReject) return Promise.reject(Object.assign(new Error('Signature rejected by user'), { code: 4001 })); return window.__cwWalletRequest(request); } };
    });
    await page.goto(base, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await page.waitForFunction(() => document.getElementById('backend-status').textContent === 'Online');
    await page.click('#nav-workflow'); await page.click('#connect-wallet');
    await page.locator('#login-section').waitFor({ state: 'visible' });
    await page.fill('#login-subject', fixture.contractWallet); await page.click('#login-btn');
    await page.waitForFunction(() => document.getElementById('auth-status').textContent === 'Authenticated');
    let s = await state();
    check('CW01_H5_SIGNER_AND_SUBJECT', same(s.identity.controller, owner.address) && same(s.identity.subject, fixture.contractWallet) && s.claims.account_kind === 'erc1271' && same(s.claims.sub, fixture.contractWallet) && s.claims.chain_id === 31337, { signer: owner.address, subject: s.claims.sub });
    check('CW09_INITIAL_LEASE', s.claims.exp - s.claims.iat <= 60 && s.claims.wallet_session_exp <= s.claims.iat + 7200);
    const profile = await api('GET', '/api/v1/auth/profile'); check('CW01_PROFILE_SUBJECT', profile.status === 200 && same(profile.data.wallet_address, fixture.contractWallet));
    const nft = new ethers.Contract(fixture.nft, ['function balanceOf(address) view returns (uint256)'], provider);
    const safe = await provider.send('eth_getBlockByNumber', ['safe', false]);
    const aBalance = await nft.balanceOf(owner.address, { blockTag: Number(safe.number) });
    const cBalance = await nft.balanceOf(fixture.contractWallet, { blockTag: Number(safe.number) });
    check('CW01_INDEPENDENT_SAFE_OWNERSHIP', aBalance.isZero() && cBalance.eq(1), { A: aBalance.toString(), C: cBalance.toString(), block: safe.number, blockHash: safe.hash });
    let content = process.env.W3_CONTENT_ID;
    if (!content) {
        const form = new FormData(); form.append('file', new Blob([readFileSync(process.env.W3_MEDIA_FILE)], { type: 'video/mp4' }), 'wallet-continuity.mp4');
        const uploaded = await fetch(new URL('/api/v1/upload', base), { method: 'POST', headers: { Authorization: `Bearer ${s.token}` }, body: form });
        const upload = await uploaded.json(); check('CW01_REAL_UPLOAD', uploaded.ok && upload.upload_id && upload.status === 'completed');
        const completed = await api('POST', `/api/v1/upload/${upload.upload_id}/complete-upload`);
        content = completed.data.content_id; check('CW01_CONTENT_CREATED', completed.status === 200 && Boolean(content));
        const rule = await api('POST', `/api/v1/content/${content}/gating-rules`, { contract_address: fixture.nft, chain_id: 31337, token_id: '', standard: 'ERC721', min_balance: 1 });
        check('CW01_SERVER_POLICY_CREATED', rule.status === 201 || rule.status === 200);
    }
    const record = await api('GET', `/api/v1/content/${content}`);
    check('CW01_PERSISTED_CONTENT_OWNER', record.status === 200 && same((record.data.content || record.data).owner_id, fixture.contractWallet));
    writeFileSync(`${output}/content.json`, JSON.stringify({ contentId: content, mode, subject: fixture.contractWallet }, null, 2));
    let ready;
    const deadline = Date.now() + 180000;
    do {
        ready = await api('GET', `/api/v1/streaming/${content}/manifest.m3u8`);
        if (ready.status === 200) break;
        if (![404, 409, 503].includes(ready.status)) throw new Error(`Manifest readiness returned ${ready.status}`);
        await new Promise((resolve) => setTimeout(resolve, 1000));
    } while (Date.now() < deadline);
    check('CW01_REAL_WORKER_MEDIA', ready.status === 200 && String(ready.data).includes('playback_token='));
    const challenge = await api('POST', '/api/v1/auth/challenge', { wallet: owner.address, chain_id: 31337, sign_type: 'siwe' });
    const eoa = await api('POST', '/api/v1/auth/login', { wallet: owner.address, chain_id: 31337, challenge_id: challenge.data.challenge_id, signature: await owner.signMessage(challenge.data.message) });
    const denied = await api('GET', `/api/v1/streaming/${content}/manifest.m3u8`, undefined, eoa.data.token);
    check('CW01_CONTROLLER_CANNOT_BORROW_NFT', eoa.status === 200 && denied.status === 403);
    await page.evaluate(() => { document.getElementById('video-player').muted = true; });
    await page.click('#nav-playback');
    await page.fill('#video-id', content); await page.click('#play-video');
    await page.waitForFunction(() => document.getElementById('video-player').currentTime >= 1 && !document.getElementById('video-player').paused, null, { timeout: 20000 });
    const initial = await state(); const started = Date.now(); const tokenHashes = new Set();
    while (Date.now() - started < 70000) {
        s = await state(); tokenHashes.add(s.tokenHash);
        if (!s.claims || !same(s.claims.sub, fixture.contractWallet) || s.error || s.playbackState === 'authorization-ended' || s.paused) throw new Error('Playback or identity ended during lease renewal');
        if (samples.length === 0 || Date.now() - samples.at(-1).at >= 10000) samples.push({ at: Date.now(), currentTime: s.time, tokenHash: s.tokenHash, exp: s.claims.exp, iat: s.claims.iat, absolute: s.claims.wallet_session_exp });
        await new Promise((resolve) => setTimeout(resolve, 1000));
    }
    s = await state();
    check('CW09_CONTINUOUS_PLAYBACK_ACROSS_LEASES', s.time - initial.time >= 65 && tokenHashes.size >= 3 && s.claims.wallet_session_exp === initial.claims.wallet_session_exp, { elapsedSeconds: (Date.now() - started) / 1000, mediaAdvance: s.time - initial.time, accessTokensObserved: tokenHashes.size });
    check('CW09_MEDIA_REQUESTS_USE_NEW_CAPABILITY', new Set(mediaResponses.filter((r) => r.status === 200).map((r) => r.tokenHash)).size >= 2 && mediaResponses.every((r) => r.claims.exp <= r.claims.wallet_session_exp));
    const oldAccess = await api('GET', '/api/v1/auth/profile', undefined, initial.token);
    check('CW09_OLD_ACCESS_REJECTED', oldAccess.status === 401);
    await page.evaluate(() => window.__cwEmit('chainChanged', '0x1'));
    const cleared = await state(); check('CW11_CHAIN_SWITCH_CLEARS_STATE', !cleared.token && cleared.paused && !cleared.identity);
    await page.evaluate(() => { window.__cwEmit('chainChanged', '0x7a69'); window.__cwReject = true; });
    await page.click('#nav-workflow');
    await page.click('#login-btn'); await page.waitForFunction(() => document.getElementById('login-result').dataset.authError === 'signature-rejected');
    check('CW06_CANCEL_HAS_NO_HALF_LOGIN', !(await state()).token);
    check('CW_H5_NO_PAGE_ERRORS', errors.length === 0, { errors });
    await page.screenshot({ path: `${output}/wallet-state.png`, fullPage: true });
} catch (error) {
    errors.push(error.message); if (page) await page.screenshot({ path: `${output}/failure.png`, fullPage: true }).catch(() => {});
    process.exitCode = 1;
} finally {
    const required = ['CW01_H5_SIGNER_AND_SUBJECT', 'CW09_INITIAL_LEASE', 'CW01_PROFILE_SUBJECT', 'CW01_INDEPENDENT_SAFE_OWNERSHIP', 'CW01_PERSISTED_CONTENT_OWNER', 'CW01_REAL_WORKER_MEDIA', 'CW01_CONTROLLER_CANNOT_BORROW_NFT', 'CW09_CONTINUOUS_PLAYBACK_ACROSS_LEASES', 'CW09_MEDIA_REQUESTS_USE_NEW_CAPABILITY', 'CW09_OLD_ACCESS_REJECTED', 'CW11_CHAIN_SWITCH_CLEARS_STATE', 'CW06_CANCEL_HAS_NO_HALF_LOGIN', 'CW_H5_NO_PAGE_ERRORS'];
    const passed = !errors.length && required.every((id) => checks.some((c) => c.id === id && c.ok));
    if (!passed) process.exitCode = 1;
    writeFileSync(`${output}/result.json`, JSON.stringify({ component: 'LOCAL_WALLET_BROWSER', mode, testAdapter: 'EIP1193_LOCAL_ANVIL_REAL_SIGNATURE', publicWalletSignoff: false, checks, samples, signedMessages, mediaResponses, errors, required, passed }, null, 2));
    await browser?.close(); for (const proxy of proxies) proxy.close();
}
