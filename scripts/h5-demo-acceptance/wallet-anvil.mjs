import { chromium } from 'playwright';
import { assertLaunchable } from './lib/browser_preflight.mjs';
import { readFileSync, mkdirSync, writeFileSync, mkdtempSync, rmSync, appendFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { createRequire } from 'node:module';
import { createServer, connect } from 'node:net';
import { createHash } from 'node:crypto';
const require = createRequire(import.meta.url);
const ethers = require('../../h5-demo/vendor/ethers.umd.min.js');
const { localProvider } = require('../cd/prepare_wallet_anvil.cjs');
const fixture = JSON.parse(readFileSync(process.env.W3_FIXTURE_PATH, 'utf8'));
if (fixture.network !== 'ANVIL_LOCAL_ONLY' || fixture.chainId !== 31337) throw new Error('Local fixture required; never use this adapter on a public chain');
const mode = process.env.W3_MODE;
const candidateSHA = process.env.W3_CANDIDATE_SHA || null;
const evidenceContext = process.env.W3_EVIDENCE_CONTEXT || 'DEVELOPMENT';
if (evidenceContext === 'CANDIDATE' && (!/^[0-9a-f]{40}$/.test(candidateSHA || '') || fixture.candidateSHA !== candidateSHA || fixture.evidenceContext !== evidenceContext || process.env.W3_CONTENT_ID)) throw new Error('Candidate browser evidence requires a matching fresh fixture and upload');
if (!['monolith', 'api-gateway'].includes(mode)) throw new Error('W3_MODE required');
const base = process.env.W3_H5_BASE;
const output = process.env.W3_REPORT_DIR;
const bridge = process.env.W3_GRPC_BRIDGE;
if (evidenceContext === 'CANDIDATE' && (!bridge || !process.env.W3_GRPC_TARGET)) throw new Error('Candidate matrix requires the real gRPC bridge and target');
if (!base || !output || !process.env.W3_MEDIA_FILE) throw new Error('H5, media and report paths required');
mkdirSync(output, { recursive: true });
const checks = [], samples = [], errors = [], proxies = [];
const grpcSamples = [], grpcExits = [], grpcNFTSamples = [], nftProbes = [], nftManifestAttempts = [];
let mediaPhase = 'lease';
const freshUpload = !process.env.W3_CONTENT_ID;
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
let grpcPrivate, grpcSequence = 0, grpcToken, grpcMedia;
const execute = promisify(execFile);
const mediaResponses = [], signedMessages = [];
let lastSegmentURL;
async function rpc(method, args = {}) {
    if (!grpcPrivate) {
        const parent = process.env.W3_PRIVATE_DIR || tmpdir(); mkdirSync(parent, { recursive: true, mode: 0o700 });
        grpcPrivate = mkdtempSync(join(parent, 'cw-rpc-'));
    }
    const request = join(grpcPrivate, `${++grpcSequence}.request.json`), response = join(grpcPrivate, `${grpcSequence}.response.json`);
    writeFileSync(request, JSON.stringify({ method, ...args }), { mode: 0o600 });
    try {
        const result = await execute(bridge, ['--target', process.env.W3_GRPC_TARGET, '--input', request, '--output', response], { timeout: 15000 });
        appendFileSync(`${output}/grpc.log`, result.stdout + result.stderr);
        const value = JSON.parse(readFileSync(response, 'utf8'));
        grpcExits.push({ method, at: Date.now(), exit: 0, code: value.code }); return value;
    } catch (error) {
        grpcExits.push({ method, at: Date.now(), exit: error.code || 1 });
        throw new Error(`gRPC bridge ${method} failed; original exit ${error.code || 1}`);
    }
}
async function grpcLogin() {
    const nonce = await rpc('challenge', { wallet: fixture.contractWallet });
    if (nonce.code !== 'OK' || !nonce.data?.message?.includes(`\n${fixture.contractWallet}\n`) || !nonce.data.message.includes('Chain ID: 31337')) throw new Error('gRPC nonce did not bind C and the local chain');
    const signature = await owner.signMessage(nonce.data.message);
    return rpc('admit', { wallet: fixture.contractWallet, nonce: nonce.data.nonce, message: nonce.data.message, signature });
}
function acceptGRPC(response, previous, measurements = grpcSamples) {
    if (response.code !== 'OK' || !(response.data?.valid || response.data?.success) || !response.data.access_token) throw new Error('Real gRPC did not admit/renew C');
    const c = claim(response.data.access_token);
    if (!same(c.sub, fixture.contractWallet) || !same(c.wallet_address, fixture.contractWallet) || c.chain_id !== 31337 || c.account_kind !== 'erc1271' ||
        !Number.isInteger(c.iat) || !Number.isInteger(c.exp) || !Number.isInteger(c.wallet_session_exp) || c.exp - c.iat > 60 || c.exp <= c.iat || c.wallet_session_exp < c.exp || c.wallet_session_exp > c.iat + 7200 ||
        !c.wallet_proof_id || !c.session_id?.startsWith('cw:')) throw new Error('gRPC changed C identity or its lease policy');
    if (previous) {
        const old = claim(previous);
        if (old.session_id !== c.session_id || old.wallet_proof_id !== c.wallet_proof_id || old.wallet_session_exp !== c.wallet_session_exp || previous === response.data.access_token) throw new Error('gRPC refresh changed admission or reused a JTI');
    }
    grpcToken = response.data.access_token;
    measurements.push({ at: Date.now(), tokenHash: hash(grpcToken), subject: c.sub, chainId: c.chain_id, kind: c.account_kind, iat: c.iat, exp: c.exp, absolute: c.wallet_session_exp, sessionHash: hash(c.session_id), proofHash: hash(c.wallet_proof_id) });
}
async function grpcMediaBaseline(content) {
    const manifest = await rpc('manifest', { token: grpcToken, content });
    const segment = manifest.data?.segments?.[0];
    if (manifest.code !== 'OK' || !segment?.url || !segment?.segment_id) throw new Error('gRPC manifest has no real segment');
    const url = new URL(segment.url, base); const capability = url.searchParams.get('playback_token');
    const c = claim(capability), access = claim(grpcToken);
    if (!same(c.wallet_address, fixture.contractWallet) || c.chain_id !== 31337 || c.account_kind !== 'erc1271' || c.exp > access.exp || c.wallet_session_exp !== access.wallet_session_exp || c.session_id !== access.session_id || c.wallet_proof_id !== access.wallet_proof_id) throw new Error('gRPC capability is not clipped to C admission');
    const media = await rpc('segment', { token: grpcToken, content, segment: segment.segment_id, capability });
    const http = await fetch(url, { signal: AbortSignal.timeout(5000) }); const bytes = Buffer.from(await http.arrayBuffer());
    if (media.code !== 'OK' || http.status !== 200 || media.data.size !== bytes.length || media.data.sha256 !== hash(bytes)) throw new Error('gRPC and protected HTTP media bytes differ');
    return { content, segment: segment.segment_id, capability, capabilityExp: c.exp, accessExp: access.exp, capabilityHash: hash(capability), sha256: hash(bytes), size: bytes.length, observedAt: Date.now() };
}
async function nftManifest(content, current) {
    const started = Date.now();
    const request = async (identity) => {
        const response = await api('GET', `/api/v1/streaming/${content}/manifest.m3u8`, undefined, identity.token);
        nftManifestAttempts.push({ at: Date.now(), status: response.status, tokenHash: hash(identity.token) });
        return response;
    };
    let response = await request(current);
    if (response.status !== 401) return { response, current };
    // A real H5 refresh invalidates its previous JTI atomically. Only an
    // observed replacement of this same admission permits one five-second retry.
    const replacement = await state(), previous = claim(current.token), next = claim(replacement.token);
    const fixed = ['sub', 'wallet_address', 'chain_id', 'account_kind', 'session_id', 'wallet_proof_id', 'wallet_session_exp', 'auth_epoch', 'restore_epoch'];
    if (Date.now() - started > 5000 || replacement.token === current.token ||
        !fixed.every(key => previous[key] != null && previous[key] === next[key]) ||
        next.chain_id !== 31337 || next.account_kind !== 'erc1271' ||
        !next.jti || next.jti === previous.jti || previous.exp * 1000 <= started + 5000 ||
        !Number.isInteger(next.iat) || !Number.isInteger(next.exp) || next.exp <= next.iat || next.exp - next.iat > 60 ||
        next.exp * 1000 <= Date.now() + 5000 || next.exp > next.wallet_session_exp) throw new Error('No bounded same-admission manifest rotation observed');
    response = await request(replacement);
    if (response.status === 401 || Date.now() - started > 5000) throw new Error('Manifest repeated 401 or exceeded the original observation budget');
    return { response, current: replacement };
}
async function nftTransferCase(content, nft, wallet, recipient) {
    acceptGRPC(await grpcLogin(), undefined, grpcNFTSamples);
    const grpcBaseline = await grpcMediaBaseline(content);
    mediaPhase = 'nft-transfer';
    await page.click('#nav-playback'); await page.fill('#video-id', content); await page.click('#play-video');
    await page.waitForFunction(() => { const v = document.getElementById('video-player'); return v.currentTime >= 1 && v.currentTime < 10 && !v.paused; }, null, { timeout: 20000 });
    const original = await state();
    const manifest = await api('GET', `/api/v1/streaming/${content}/manifest.m3u8`);
    const cap = String(manifest.data).match(/playback_token=([^&\s]+)/)?.[1];
    if (!cap) throw new Error('NFT transfer requires a real HTTP capability');
    const capClaims = claim(cap), mediaURL = new URL(lastSegmentURL); mediaURL.searchParams.set('playback_token', cap);
    const before = await fetch(mediaURL, { signal: AbortSignal.timeout(5000) }); await before.arrayBuffer();
    const observedAt = Date.now();
    if (before.status !== 200 || Math.min(original.claims.exp, capClaims.exp, grpcBaseline.accessExp, grpcBaseline.capabilityExp) * 1000 - observedAt < 20000) throw new Error('NFT transfer started without live credentials and accepted capabilities');
    let transferred = false;
    try {
        const sent = await (await wallet.execute(fixture.nft, 0, nft.interface.encodeFunctionData('transferFrom', [fixture.contractWallet, recipient, fixture.tokenId]))).wait();
        if (sent.status !== 1) throw new Error('C.execute NFT transfer failed');
        transferred = true; await provider.send('anvil_mine', ['0x6']);
        const anchor = await provider.send('eth_getBlockByNumber', ['safe', false]), tag = Number(anchor.number);
        const balances = await Promise.all([nft.balanceOf(owner.address, { blockTag: tag }), nft.balanceOf(fixture.contractWallet, { blockTag: tag }), nft.balanceOf(recipient, { blockTag: tag })]);
        check('CW16_NFT_TRANSFER_SAFE', tag >= sent.blockNumber && balances[0].isZero() && balances[1].isZero() && balances[2].eq(1) &&
            same(await nft.ownerOf(fixture.tokenId, { blockTag: tag }), recipient) && same(await wallet.owner({ blockTag: tag }), owner.address),
            { transactionHash: sent.transactionHash, block: anchor.number, blockHash: anchor.hash, tokenId: fixture.tokenId, from: fixture.contractWallet, to: recipient, A: '0', C: '0', B: '1', walletOwner: owner.address,
                baselineAt: observedAt, httpAbsolute: original.claims.wallet_session_exp, httpAccessExp: original.claims.exp, httpCapabilityExp: capClaims.exp, grpcAccessExp: grpcBaseline.accessExp, grpcCapabilityExp: grpcBaseline.capabilityExp });
        const safeObservedAt = Date.now(); let httpDeniedAt, grpcDeniedAt, finished;
        while (Date.now() - safeObservedAt <= 70000) {
            if (Date.now() - grpcNFTSamples.at(-1).at >= 20000) acceptGRPC(await rpc('refresh', { token: grpcToken }), grpcToken, grpcNFTSamples);
            let current = await state();
            if (!current.claims || !same(current.claims.sub, fixture.contractWallet) || current.claims.session_id !== original.claims.session_id || current.claims.wallet_session_exp !== original.claims.wallet_session_exp || current.claims.exp * 1000 <= Date.now()) throw new Error('NFT transfer terminated the identity instead of its asset entitlement');
            const observedManifest = await nftManifest(content, current);
            const httpManifest = observedManifest.response; current = observedManifest.current;
            const grpcManifest = await rpc('manifest', { token: grpcToken, content });
            const identityBefore = await rpc('verify', { token: grpcToken });
            const media = await rpc('segment', { token: grpcToken, content, segment: grpcBaseline.segment, capability: grpcBaseline.capability });
            const identityAfter = await rpc('verify', { token: grpcToken });
            const http = await fetch(mediaURL, { signal: AbortSignal.timeout(5000) }); await http.arrayBuffer();
            if (![200, 403].includes(httpManifest.status) || !['OK', 'PermissionDenied'].includes(grpcManifest.code) || ![200, 401].includes(http.status) || !['OK', 'Unauthenticated', 'PermissionDenied'].includes(media.code)) throw new Error(`NFT transfer has no authorization evidence: HTTP manifest=${httpManifest.status}, media=${http.status}; gRPC manifest=${grpcManifest.code}, media=${media.code}`);
            if (identityBefore.code !== 'OK' || !identityBefore.data?.valid || identityAfter.code !== 'OK' || !identityAfter.data?.valid) throw new Error('gRPC identity is invalid; its media denial cannot prove an NFT transfer');
            const at = Date.now();
            if (http.status === 401 && !httpDeniedAt) httpDeniedAt = at;
            if (['Unauthenticated', 'PermissionDenied'].includes(media.code) && !grpcDeniedAt) grpcDeniedAt = at;
            nftProbes.push({ at, httpManifestStatus: httpManifest.status, grpcManifestCode: grpcManifest.code, httpMediaStatus: http.status, grpcMediaCode: media.code, grpcIdentityBefore: true, grpcIdentityAfter: true, httpIdentityExp: current.claims.exp, httpTokenHash: hash(current.token), grpcTokenHash: hash(grpcToken) });
            if (httpManifest.status === 403 && grpcManifest.code === 'PermissionDenied' && httpDeniedAt && grpcDeniedAt) {
                const profile = await api('GET', '/api/v1/auth/profile');
                if (profile.status === 200 && same(profile.data.wallet_address, fixture.contractWallet)) { finished = Date.now(); break; }
            }
            await new Promise((resolve) => setTimeout(resolve, 1000));
        }
        check('CW14_NFT_MANIFEST_DENIED_HTTP_GRPC', Boolean(finished) && finished - safeObservedAt <= 70000, { safeObservedAt, rejectedAt: finished || null, httpStatus: nftProbes.at(-1)?.httpManifestStatus, grpcCode: nftProbes.at(-1)?.grpcManifestCode });
        check('CW14_NFT_CAPABILITIES_REVOKED_WITH_LIVE_IDENTITY', Boolean(httpDeniedAt && grpcDeniedAt) && Math.max(httpDeniedAt, grpcDeniedAt) - safeObservedAt <= 70000 && new Set(grpcNFTSamples.map((s) => s.absolute)).size === 1,
            { safeObservedAt, httpDeniedAt, grpcDeniedAt, httpElapsedMs: httpDeniedAt - safeObservedAt, grpcElapsedMs: grpcDeniedAt - safeObservedAt, identityValid: true, nft: fixture.nft, tokenId: fixture.tokenId });
    } finally {
        if (transferred) {
            const restored = await (await nft.connect(provider.getSigner(recipient)).transferFrom(recipient, fixture.contractWallet, fixture.tokenId)).wait();
            if (restored.status !== 1) throw new Error('Local fixture NFT restoration failed');
            await provider.send('anvil_mine', ['0x6']);
        }
    }
}
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
    page.on('response', (response) => { const url = new URL(response.url()); if (url.pathname.includes('/segment/') && url.searchParams.has('playback_token')) { lastSegmentURL = url.href; mediaResponses.push({ at: Date.now(), phase: mediaPhase, status: response.status(), tokenHash: hash(url.searchParams.get('playback_token')), claims: claim(url.searchParams.get('playback_token')) }); } });
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
    await page.fill('#nft-contract', fixture.nft);
    await page.fill('#login-subject', fixture.contractWallet); await page.click('#login-btn');
    await page.waitForFunction(() => document.getElementById('auth-status').textContent === 'Authenticated');
    let s = await state();
    check('CW01_H5_SIGNER_AND_SUBJECT', same(s.identity.controller, owner.address) && same(s.identity.subject, fixture.contractWallet) && s.claims.account_kind === 'erc1271' && same(s.claims.sub, fixture.contractWallet) && s.claims.chain_id === 31337, { signer: owner.address, subject: s.claims.sub });
    check('CW09_INITIAL_LEASE', s.claims.exp - s.claims.iat <= 60 && s.claims.wallet_session_exp <= s.claims.iat + 7200);
    const profile = await api('GET', '/api/v1/auth/profile'); check('CW01_PROFILE_SUBJECT', profile.status === 200 && same(profile.data.wallet_address, fixture.contractWallet));
    const nft = new ethers.Contract(fixture.nft, ['function balanceOf(address) view returns (uint256)', 'function ownerOf(uint256) view returns (address)', 'function transferFrom(address,address,uint256)'], provider);
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
    writeFileSync(`${output}/content.json`, JSON.stringify({ contentId: content, mode, subject: fixture.contractWallet, freshUpload, candidateSHA, evidenceContext }, null, 2));
    let ready;
    const deadline = Date.now() + 180000;
    do {
        ready = await api('GET', `/api/v1/streaming/${content}/manifest.m3u8`);
        if (ready.status === 200) break;
        if (![404, 409, 503].includes(ready.status)) throw new Error(`Manifest readiness returned ${ready.status}`);
        await new Promise((resolve) => setTimeout(resolve, 1000));
    } while (Date.now() < deadline);
    check('CW01_REAL_WORKER_MEDIA', ready.status === 200 && String(ready.data).includes('playback_token='));
    if (bridge) {
        const admission = await grpcLogin(); acceptGRPC(admission);
        check('CW14_GRPC_LOGIN_SUBJECT', same(admission.data.user_info?.wallet_address, fixture.contractWallet), { subject: fixture.contractWallet, chainId: 31337, kind: 'erc1271' });
        grpcMedia = await grpcMediaBaseline(content);
        check('CW14_GRPC_REAL_MEDIA', grpcMedia.size > 0, { sha256: grpcMedia.sha256, size: grpcMedia.size, capabilityExp: grpcMedia.capabilityExp, accessExp: grpcMedia.accessExp, observedAt: grpcMedia.observedAt });
    }
    const challenge = await api('POST', '/api/v1/auth/challenge', { wallet: owner.address, chain_id: 31337, sign_type: 'siwe' });
    const eoa = await api('POST', '/api/v1/auth/login', { wallet: owner.address, chain_id: 31337, challenge_id: challenge.data.challenge_id, signature: await owner.signMessage(challenge.data.message) });
    const denied = await api('GET', `/api/v1/streaming/${content}/manifest.m3u8`, undefined, eoa.data.token);
    check('CW01_CONTROLLER_CANNOT_BORROW_NFT', eoa.status === 200 && denied.status === 403);
    await page.evaluate(() => { document.getElementById('video-player').muted = true; });
    await page.click('#nav-playback');
    await page.fill('#video-id', content); await page.click('#play-video');
    await page.waitForFunction(() => document.getElementById('video-player').currentTime >= 1 && !document.getElementById('video-player').paused, null, { timeout: 20000 });
    const initial = await state(); const started = Date.now(); const tokenHashes = new Set();
    const recordSample = (value) => samples.push({ at: Date.now(), currentTime: value.time, tokenHash: value.tokenHash, exp: value.claims.exp, iat: value.claims.iat, absolute: value.claims.wallet_session_exp, subject: value.claims.sub, kind: value.claims.account_kind, chainId: value.claims.chain_id });
    recordSample(initial);
    while (Date.now() - started < 71000) {
        if (bridge && Date.now() - grpcSamples.at(-1).at >= 20000) acceptGRPC(await rpc('refresh', { token: grpcToken }), grpcToken);
        s = await state(); tokenHashes.add(s.tokenHash);
        if (!s.claims || !same(s.claims.sub, fixture.contractWallet) || s.error || s.playbackState === 'authorization-ended' || s.paused) throw new Error('Playback or identity ended during lease renewal');
        if (Date.now() - samples.at(-1).at >= 10000) recordSample(s);
        await new Promise((resolve) => setTimeout(resolve, 1000));
    }
    s = await state();
    recordSample(s);
    check('CW09_CONTINUOUS_PLAYBACK_ACROSS_LEASES', s.time - initial.time >= 65 && tokenHashes.size >= 3 && s.claims.wallet_session_exp === initial.claims.wallet_session_exp, { elapsedSeconds: (Date.now() - started) / 1000, mediaAdvance: s.time - initial.time, accessTokensObserved: tokenHashes.size });
    check('CW09_MEDIA_REQUESTS_USE_NEW_CAPABILITY', new Set(mediaResponses.filter((r) => r.status === 200).map((r) => r.tokenHash)).size >= 2 && mediaResponses.every((r) => r.claims.exp <= r.claims.wallet_session_exp));
    const oldAccess = await api('GET', '/api/v1/auth/profile', undefined, initial.token);
    check('CW09_OLD_ACCESS_REJECTED', oldAccess.status === 401);
    // Start the policy transition just after a real H5 renewal. A mid-lease
    // baseline can expire at the next renewal and falsely stand in for revocation.
    const policyLeaseDeadline = Date.now() + 35000;
    let current = await state();
    while (current.claims && current.claims.exp * 1000 - Date.now() < 55000 && Date.now() < policyLeaseDeadline) {
        await new Promise((resolve) => setTimeout(resolve, 250));
        current = await state();
    }
    if (!current.claims || current.claims.session_id !== initial.claims.session_id ||
        current.claims.wallet_proof_id !== initial.claims.wallet_proof_id ||
        current.claims.wallet_session_exp !== initial.claims.wallet_session_exp) throw new Error('Policy baseline lost the original H5 admission');
    if (bridge) {
        acceptGRPC(await rpc('refresh', { token: grpcToken }), grpcToken);
        grpcMedia = await grpcMediaBaseline(content);
        check('CW14_GRPC_REFRESH_SUBJECT', new Set(grpcSamples.map((s) => s.tokenHash)).size >= 3 && new Set(grpcSamples.map((s) => s.absolute)).size === 1,
            { tokens: grpcSamples.length, sha256: grpcMedia.sha256, capabilityHash: grpcMedia.capabilityHash, capabilityExp: grpcMedia.capabilityExp, accessExp: grpcMedia.accessExp, observedAt: grpcMedia.observedAt });
    }
    // CW-10 changes only signature authority. Keep the NFT, live session and
    // issued capability intact; expiry/logout cannot substitute for revocation.
    current = await state();
    const freshManifest = await api('GET', `/api/v1/streaming/${content}/manifest.m3u8`);
    const capability = String(freshManifest.data).match(/playback_token=([^&\s]+)/)?.[1];
    if (!capability || !lastSegmentURL) throw new Error('Policy revocation needs a real issued media capability');
    const capabilityClaims = claim(capability);
    const protectedSegment = new URL(lastSegmentURL); protectedSegment.searchParams.set('playback_token', capability);
    const baseline = await fetch(protectedSegment, { signal: AbortSignal.timeout(5000) });
    await baseline.arrayBuffer();
    check('CW10_LIVE_POLICY_BASELINE', baseline.status === 200 &&
        Math.min(current.claims.exp, capabilityClaims.exp) * 1000 - Date.now() >= 50000,
        { accessExp: current.claims.exp, capabilityExp: capabilityClaims.exp, observedAt: Date.now(), tokenHash: hash(current.token), capabilityHash: hash(capability) });
    const walletContract = new ethers.Contract(fixture.contractWallet, ['function owner() view returns (address)', 'function setOwner(address)', 'function execute(address,uint256,bytes) returns (bytes)'], owner);
    const replacement = (await provider.send('eth_accounts', []))[1];
    if (!replacement || same(replacement, owner.address)) throw new Error('Independent local replacement owner required');
    let policyChanged = false;
    try {
        const rotation = await (await walletContract.setOwner(replacement)).wait(); policyChanged = true;
        await provider.send('anvil_mine', ['0x6']);
        const anchor = await provider.send('eth_getBlockByNumber', ['safe', false]);
        const tag = Number(anchor.number);
        const balanceC = await nft.balanceOf(fixture.contractWallet, { blockTag: tag });
        const balanceA = await nft.balanceOf(owner.address, { blockTag: tag });
        check('CW10_SAFE_POLICY_ONLY_CHANGE', tag >= rotation.blockNumber &&
            same(await walletContract.owner({ blockTag: tag }), replacement) && balanceA.isZero() && balanceC.eq(1),
            { block: anchor.number, blockHash: anchor.hash, transactionHash: rotation.transactionHash, ownerBefore: owner.address, ownerAfter: replacement, A: balanceA.toString(), C: balanceC.toString() });
        const safeObservedAt = Date.now(); let rejectedAt = null;
        if (bridge) {
            let refresh;
            do {
                refresh = await rpc('refresh', { token: grpcToken });
                if (refresh.code === 'Unauthenticated') break;
                if (refresh.code !== 'OK') throw new Error(`Policy revalidation returned ${refresh.code}, not a signature-policy decision`);
                // A still-age-valid positive proof may survive briefly. Follow
                // the current JTI so its normal rotation cannot fake revocation.
                acceptGRPC(refresh, grpcToken);
                await new Promise((resolve) => setTimeout(resolve, 1000));
            } while (Date.now() - safeObservedAt < 70000 && Date.now() < (grpcMedia.capabilityExp - 5) * 1000);
            const verified = await rpc('verify', { token: grpcToken });
            const manifest = await rpc('manifest', { token: grpcToken, content });
            const segment = await rpc('segment', { token: grpcToken, content, segment: grpcMedia.segment, capability: grpcMedia.capability });
            const login = await grpcLogin();
            const observedAt = Date.now();
            const verifyDenied = verified.code === 'Unauthenticated' || (verified.code === 'OK' && verified.data && !verified.data.valid);
            check('CW10_GRPC_POLICY_REVOKES_WITH_UNCHANGED_NFT', refresh.code === 'Unauthenticated' && verifyDenied && manifest.code === 'Unauthenticated' && segment.code === 'Unauthenticated' &&
                login.code === 'OK' && !login.data?.valid && observedAt - safeObservedAt <= 70000 && observedAt < (Math.min(grpcMedia.accessExp, grpcMedia.capabilityExp) - 5) * 1000,
                { safeObservedAt, rejectedAt: observedAt, elapsedMs: observedAt - safeObservedAt, accessExp: grpcMedia.accessExp, capabilityExp: grpcMedia.capabilityExp, refreshCode: refresh.code, verifyCode: verified.code, verifyValid: verified.data?.valid || false,
                    manifestCode: manifest.code, segmentCode: segment.code, newLoginValid: login.data?.valid || false, A: '0', C: '1', nft: fixture.nft });
        }
        while (Date.now() - safeObservedAt <= 70000) {
            const response = await fetch(protectedSegment, { signal: AbortSignal.timeout(5000) });
            await response.arrayBuffer();
            if (response.status === 401) { rejectedAt = Date.now(); break; }
            if (response.status !== 200) throw new Error(`Policy revocation returned unexpected media status ${response.status}`);
            await new Promise((resolve) => setTimeout(resolve, 1000));
        }
        const revoked = await api('GET', '/api/v1/auth/profile', undefined, current.token);
        const challenge = await api('POST', '/api/v1/auth/challenge', { wallet: fixture.contractWallet, chain_id: 31337, sign_type: 'siwe' });
        const relogin = await api('POST', '/api/v1/auth/login', { wallet: fixture.contractWallet, chain_id: 31337, challenge_id: challenge.data.challenge_id, signature: await owner.signMessage(challenge.data.message) });
        check('CW10_POLICY_REVOKES_WITH_UNCHANGED_NFT', rejectedAt !== null && rejectedAt - safeObservedAt <= 70000 &&
            rejectedAt < (Math.min(current.claims.exp, capabilityClaims.exp) - 5) * 1000 && revoked.status === 401 && relogin.status === 401 &&
            (await nft.balanceOf(fixture.contractWallet, { blockTag: tag })).eq(1) && (await nft.balanceOf(owner.address, { blockTag: tag })).isZero(),
            { safeObservedAt, rejectedAt, elapsedMs: rejectedAt === null ? null : rejectedAt - safeObservedAt, accessExp: current.claims.exp, capabilityExp: capabilityClaims.exp,
                oldAccessStatus: revoked.status, oldSignerLoginStatus: relogin.status, A: '0', C: '1', nft: fixture.nft, tokenId: fixture.tokenId });
    } finally {
        if (policyChanged) {
            const restored = await (await walletContract.connect(provider.getSigner(replacement)).setOwner(owner.address)).wait();
            if (restored.status !== 1) throw new Error('Local fixture owner restoration failed');
            await provider.send('anvil_mine', ['0x6']);
        }
    }
    await page.click('#nav-workflow'); await page.click('#login-btn');
    await page.waitForFunction(() => document.getElementById('auth-status').textContent === 'Authenticated');
    if (bridge) await nftTransferCase(content, nft, walletContract, replacement);
    await page.evaluate(() => window.__cwEmit('chainChanged', '0x1'));
    const cleared = await state(); check('CW11_CHAIN_SWITCH_CLEARS_STATE', !cleared.token && cleared.paused && !cleared.identity);
    await page.evaluate(() => { window.__cwEmit('chainChanged', '0x7a69'); window.__cwReject = true; });
    await page.click('#nav-workflow');
    await page.click('#login-btn'); await page.waitForFunction(() => document.getElementById('login-result').dataset.authError === 'signature-rejected');
    check('CW06_CANCEL_HAS_NO_HALF_LOGIN', !(await state()).token);
    check('CW_H5_NO_PAGE_ERRORS', errors.length === 0, { errors });
    await page.screenshot({ path: `${output}/wallet-state.png`, fullPage: true });
} catch (error) {
    console.error(`FAIL ${mode} LOCAL_WALLET_BROWSER: ${error.message}`);
    errors.push(error.message); if (page) await page.screenshot({ path: `${output}/failure.png`, fullPage: true }).catch(() => {});
    process.exitCode = 1;
} finally {
    const required = ['CW01_H5_SIGNER_AND_SUBJECT', 'CW09_INITIAL_LEASE', 'CW01_PROFILE_SUBJECT', 'CW01_INDEPENDENT_SAFE_OWNERSHIP', 'CW01_PERSISTED_CONTENT_OWNER', 'CW01_REAL_WORKER_MEDIA', 'CW01_CONTROLLER_CANNOT_BORROW_NFT', 'CW09_CONTINUOUS_PLAYBACK_ACROSS_LEASES', 'CW09_MEDIA_REQUESTS_USE_NEW_CAPABILITY', 'CW09_OLD_ACCESS_REJECTED', 'CW11_CHAIN_SWITCH_CLEARS_STATE', 'CW06_CANCEL_HAS_NO_HALF_LOGIN', 'CW_H5_NO_PAGE_ERRORS'];
    required.push('CW10_LIVE_POLICY_BASELINE', 'CW10_SAFE_POLICY_ONLY_CHANGE', 'CW10_POLICY_REVOKES_WITH_UNCHANGED_NFT');
    if (bridge) required.push('CW14_GRPC_LOGIN_SUBJECT', 'CW14_GRPC_REAL_MEDIA', 'CW14_GRPC_REFRESH_SUBJECT', 'CW10_GRPC_POLICY_REVOKES_WITH_UNCHANGED_NFT');
    if (bridge) required.push('CW16_NFT_TRANSFER_SAFE', 'CW14_NFT_MANIFEST_DENIED_HTTP_GRPC', 'CW14_NFT_CAPABILITIES_REVOKED_WITH_LIVE_IDENTITY');
    if (freshUpload) required.push('CW01_REAL_UPLOAD', 'CW01_CONTENT_CREATED', 'CW01_SERVER_POLICY_CREATED');
    const passed = !errors.length && required.every((id) => checks.some((c) => c.id === id && c.ok));
    if (!passed) process.exitCode = 1;
    writeFileSync(`${output}/result.json`, JSON.stringify({ component: 'LOCAL_WALLET_BROWSER', candidateSHA, evidenceContext, mode, testAdapter: 'EIP1193_LOCAL_ANVIL_REAL_SIGNATURE', publicWalletSignoff: false, checks, samples, grpcSamples, grpcNFTSamples, nftProbes, nftManifestAttempts, grpcExits, grpcBridgeSHA256: bridge ? hash(readFileSync(bridge)) : null, signedMessages, mediaResponses, errors, required, passed }, null, 2));
    await browser?.close(); for (const proxy of proxies) proxy.close();
    if (grpcPrivate) rmSync(grpcPrivate, { recursive: true, force: true });
}
