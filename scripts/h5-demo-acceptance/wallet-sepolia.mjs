// Observe a user-operated MetaMask phase in an existing H5 tab. Never sign,
// approve, inject a provider, create a wallet context or broadcast a transaction.
// Phase evidence does not replace canonical transfers, safe-time revocation,
// real gRPC, RPC faults or the final TN-01..08 verifier.
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { settings, evmAddress, validIdentity, phaseVerified } from './lib/wallet_sepolia_contract.mjs';
import { WalletPhaseNetwork } from './lib/wallet_phase_network.mjs';
import { CuaPageBridge } from './lib/wallet_cua_bridge.mjs';
import { CuaPageSession } from './lib/wallet_cua_session.mjs';
import { CuaTrace } from './lib/wallet_cua_trace.mjs';
const require = createRequire(import.meta.url);
const ethers = require('../../h5-demo/vendor/ethers.umd.min.js');
const hash = (value) => createHash('sha256').update(value).digest('hex');
const report = { component: 'SEPOLIA_REAL_WALLET_BROWSER_PHASE', phase: process.env.W3_PHASE || 'eoa',
    candidateSHA: process.env.W3_CANDIDATE_SHA || null, mode: process.env.W3_MODE || null,
    testAdapter: process.env.W3_BROWSER_TRANSPORT === 'cua' ? 'CUA_SCOPED_READ_ONLY' : 'EXISTING_METAMASK_CDP_READ_ONLY', publicWalletSignoff: false,
    completeAcceptance: false, checks: [], errors: [], blockers: [], gates: [], admissions: [], media: [], samples: [], challenges: [], issues: [], refreshes: [] };
let config, browser, page, onResponse, onError, cuaBridge, cuaSession, cuaTrace, cuaStarted = false;
let phaseNetwork, admissions = new Map(), privateProofs = [];
const pending = new Set();
const issuanceJobs = new Set();
let challengeSequence = 0;
let preserveExistingOutput = false;
class Blocked extends Error {}
const check = (id, ok, detail = {}) => {
    report.checks.push({ id, ok: ok === true, detail });
    if (!ok) throw new Error(`Required component check failed: ${id}`);
    console.log(`PASS ${id}`);
};
const summarize = (s) => ({ at: Date.now(), tokenHash: s.token ? hash(s.token) : null, claims: s.claims,
    identity: s.identity, wallet: s.wallet, time: s.time, frames: s.frames, paused: s.paused, playbackState: s.playbackState, videoError: s.videoError, authError: s.authError });

async function state() {
    if (cuaSession) {
        const value = await cuaTrace.run('state', () => cuaSession.state());
        value.claims = value.token ? JSON.parse(Buffer.from(value.token.split('.')[1], 'base64url')) : null;
        return value;
    }
    if (new URL(page.url()).origin !== config.origin) throw new Error('The selected H5 page navigated away');
    const value = await page.evaluate(async () => {
        const video = document.getElementById('video-player');
        const provider = window.ethereum?.providers?.find((item) => item.isMetaMask) || (window.ethereum?.isMetaMask ? window.ethereum : null);
        let wallet = null;
        if (provider) wallet = { metaMask: true, accounts: await provider.request({ method: 'eth_accounts' }), chainId: Number(await provider.request({ method: 'eth_chainId' })) };
        const stored = sessionStorage.getItem('streamgate_identity');
        return { token: sessionStorage.getItem('streamgate_token'), identity: stored ? JSON.parse(stored) : null, wallet,
            time: video?.currentTime || 0, frames: video?.getVideoPlaybackQuality?.().totalVideoFrames || 0, paused: video?.paused ?? true,
            playbackState: document.getElementById('player-status')?.dataset.sgPlaybackState,
            videoError: video?.error?.code || null, authError: document.getElementById('login-result')?.dataset.authError || null };
    });
    value.claims = value.token ? JSON.parse(Buffer.from(value.token.split('.')[1], 'base64url')) : null;
    return value;
}

async function userGate(id, instruction, predicate) {
    console.log(`USER ACTION REQUIRED (${id}): ${instruction}`);
    const started = Date.now(), before = challengeSequence;
    while (Date.now() - started < config.waitMs) {
        const current = await state();
        if (await predicate(current)) {
            const finished = Date.now();
            report.gates.push({ id, startedAt: started, finishedAt: finished, elapsedMs: finished - started,
                challengeSequenceBefore: before, challengeSequenceAfter: challengeSequence, state: summarize(current) });
            return current;
        }
        await new Promise((resolve) => setTimeout(resolve, 2000));
    }
    throw new Blocked(`User action ${id} was not observed within the configured wait`);
}

async function observeResponse(response) {
    const url = new URL(response.url());
    if (url.origin !== config.origin) return;
    const auth = ['/api/v1/auth/challenge', '/api/v1/auth/login', '/api/v1/auth/refresh'].includes(url.pathname);
    const manifest = url.pathname === `/api/v1/streaming/${config.content}/manifest.m3u8`;
    const segment = url.pathname.startsWith(`/api/v1/streaming/${config.content}/segment/`);
    if (!auth && !manifest && !segment) return;
    const at = Date.now(), status = response.status(), token = url.searchParams.get('playback_token');
    let body = Buffer.alloc(0), headers = {}, requestBody = '';
    if (status === 200 && (auth || (manifest && !token) || (segment && token))) body = await response.body();
    if (status === 200 && ['/api/v1/auth/login', '/api/v1/auth/refresh'].includes(url.pathname)) requestBody = response.request().postData() || '';
    if (status === 200 && manifest && !token) headers = await response.request().allHeaders();
    // An observed media response cannot outrun the corresponding issuance body.
    if (token) await Promise.all([...issuanceJobs]);
    await phaseNetwork.observe({ url: url.href, status, at, body, headers, requestBody });
    challengeSequence = phaseNetwork.challengeSequence;
}

async function profile(current, prefix) {
    const result = cuaSession ? await cuaTrace.run('profile', () => cuaSession.profile()) : await page.evaluate(async () => {
        const response = await fetch('/api/v1/auth/profile', { headers: { Authorization: `Bearer ${sessionStorage.getItem('streamgate_token')}` } });
        return { status: response.status, data: await response.json() };
    });
    check(`${prefix}_PROFILE_SUBJECT`, result.status === 200 && evmAddress(result.data.wallet_address) === config.subject,
        { status: result.status, subject: result.data.wallet_address, accessTokenHash: hash(current.token) });
}

try {
    config = settings(process.env);
    phaseNetwork = new WalletPhaseNetwork({ config, report, ethers });
    admissions = phaseNetwork.admissions; privateProofs = phaseNetwork.privateProofs;
    if (existsSync(join(config.output, 'result.json')) || existsSync(join(config.output, 'siwe-proofs.private.json')) || existsSync(join(config.output, 'cua-trace.private.json'))) {
        preserveExistingOutput = true;
        throw new Blocked('The report directory already contains original evidence; choose a new phase directory');
    }
    if (config.transport === 'cua') {
        cuaBridge = new CuaPageBridge({ targetOrigin: config.origin });
        await cuaBridge.start();
        cuaTrace = new CuaTrace({ bridge: cuaBridge, candidateSHA: config.candidateSHA, mode: config.mode,
            phase: config.phase, origin: config.origin, content: config.content });
        cuaSession = new CuaPageSession({ bridge: cuaTrace, origin: config.origin, content: config.content,
            onResponse: async row => { await phaseNetwork.observe(row); challengeSequence = phaseNetwork.challengeSequence; },
            onPageError: reason => report.errors.push(reason) });
        console.log('CUA_RELAY ' + JSON.stringify({ origin: cuaBridge.origin, sessionFile: cuaBridge.sessionFile, targetOrigin: config.origin }));
        try { await cuaTrace.run('start', () => cuaSession.start()); cuaStarted = true; }
        catch { throw new Blocked('The origin-scoped CUA observer did not connect'); }
    } else {
        const { chromium } = await import('playwright');
        browser = await chromium.connectOverCDP(config.cdp, { timeout: 10000, noDefaults: true }).catch(() => { throw new Blocked('The existing MetaMask browser CDP endpoint is unavailable'); });
        const candidates = browser.contexts().flatMap((context) => context.pages()).filter((candidate) => {
            try { return new URL(candidate.url()).origin === config.origin; } catch { return false; }
        });
        const pages = [];
        // The user's transaction panel shares this origin. Select the actual H5
        // player without closing, navigating or claiming that other wallet tab.
        for (const candidate of candidates) if (await candidate.locator('#video-player').count() === 1) pages.push(candidate);
        if (pages.length !== 1) throw new Blocked('Exactly one existing H5 tab on the configured origin is required');
        [page] = pages;
        onError = (error) => report.errors.push(error.message);
        onResponse = (response) => {
            const url = new URL(response.url());
            const issuance = url.origin === config.origin && url.pathname === `/api/v1/streaming/${config.content}/manifest.m3u8` && !url.searchParams.has('playback_token');
            const operation = observeResponse(response).catch((error) => report.errors.push(error instanceof SyntaxError ? 'Malformed original HTTP/JWT evidence' : error.message))
                .finally(() => { pending.delete(operation); issuanceJobs.delete(operation); });
            if (issuance) issuanceJobs.add(operation);
            pending.add(operation);
        };
        page.on('pageerror', onError); page.on('response', onResponse);
    }
    const current = await state();
    if (!current.wallet?.metaMask) throw new Blocked('MetaMask was not found on the existing H5 page');
    if (config.phase === 'negative') {
        const beforeWrong = challengeSequence;
        await userGate('TN06_WRONG_NETWORK', 'select a non-Sepolia network in MetaMask, then attempt H5 login',
            (s) => s.wallet?.chainId !== 11155111 && !s.token && !s.identity && s.authError === 'wrong-network' && challengeSequence > beforeWrong);
        check('TN06_WRONG_NETWORK_SHOWN', true);
        const beforeReject = challengeSequence;
        await userGate('TN06_USER_CANCEL', 'switch MetaMask to Sepolia, start H5 login and reject its signature request',
            (s) => s.wallet?.chainId === 11155111 && !s.token && !s.identity && s.authError === 'signature-rejected' && challengeSequence > beforeReject);
        check('TN06_SIGNATURE_REJECTED', true);
    } else {
        const prefix = config.phase === 'contract' ? 'TN04' : 'TN03';
        const admitted = await userGate(`${prefix}_SIGN_SIWE`, `log in to H5 as ${config.subject}, approving its SIWE request in MetaMask account ${config.controller}`,
            (s) => validIdentity(s, config) && admissions.has(hash(s.token)));
        check(`${prefix}_SIWE_ORIGIN_BINDING`, admissions.get(hash(admitted.token))?.originBound === true);
        check(config.phase === 'contract' ? 'TN04_SUBJECT_C' : 'TN03_EOA_SUBJECT', validIdentity(admitted, config));
        await profile(admitted, prefix);
        if (config.phase === 'contract') check('TN04_SHORT_LEASE', admitted.claims.exp - admitted.claims.iat <= 60);
        const playing = await userGate(`${prefix}_PLAY`, `select fixture content ${config.content} and press play; keep the page playing`,
            (s) => validIdentity(s, config) && s.time >= 1 && s.frames > 0 && !s.paused && !s.videoError &&
                report.media.some((r) => r.status === 200 && r.kind === 'segment' && evmAddress(r.claims.wallet_address) === config.subject && r.claims.content_id === config.content && r.claims.chain_id === 11155111));
        const started = Date.now(), tokens = new Set(), absolute = playing.claims.wallet_session_exp;
        const duration = config.phase === 'contract' ? 71000 : 4000;
        let latest = playing;
        while (Date.now() - started < duration) {
            latest = await state();
            if (!validIdentity(latest, config) || latest.paused || latest.videoError) throw new Error('Identity or decoded playback ended during observation');
            if (config.phase === 'contract' && latest.claims.wallet_session_exp !== absolute) throw new Error('Absolute contract session deadline moved');
            tokens.add(hash(latest.token)); report.samples.push(summarize(latest));
            await new Promise((resolve) => setTimeout(resolve, 1000));
        }
        latest = await state(); report.samples.push(summarize(latest));
        const decoded = validIdentity(latest, config) && !latest.paused && !latest.videoError && latest.frames > playing.frames && latest.time - playing.time >= (config.phase === 'contract' ? 65 : 2);
        const capabilities = new Set(report.media.filter((r) => r.status === 200 && r.kind === 'segment' && r.at >= started).map((r) => r.tokenHash));
        check(config.phase === 'contract' ? 'TN04_MEDIA_RENEWS' : 'TN03_MEDIA_DECODES', decoded &&
            (config.phase !== 'contract' || (tokens.size >= 3 && capabilities.size >= 2)),
            { elapsedMs: Date.now() - started, mediaAdvance: latest.time - playing.time, frames: latest.frames - playing.frames, accessTokens: tokens.size, capabilities: capabilities.size });
    }
    await Promise.all([...pending]);
    check('TN06_NO_PAGE_ERRORS', report.errors.length === 0);
} catch (error) {
    if (error instanceof Blocked || !config) report.blockers.push(error.message);
    else report.errors.push(error.message);
} finally {
    if (page && onResponse) page.off('response', onResponse);
    if (page && onError) page.off('pageerror', onError);
    await Promise.all([...pending]);
    if (cuaSession && cuaStarted) {
        try { await cuaTrace.run('finish', () => cuaSession.finish()); }
        catch { report.errors.push('CUA observer ended with incomplete original events'); }
    }
    if (cuaBridge) {
        try { await cuaBridge.close(); }
        catch { report.errors.push('CUA observer cleanup failed'); }
    }
    // No contexts were created. This disconnects the CDP client and preserves
    // the user's pre-existing browser context, wallet and tabs.
    if (browser) await browser.close().catch((error) => report.errors.push(error.message));
    report.phaseVerified = phaseVerified(report);
    report.status = report.errors.length ? 'FAIL' : report.phaseVerified ? 'WALLET_PHASE_OBSERVED' : 'BLOCKED';
    report.remaining = ['canonical deployment/CI identity', 'safe holdings/transfer oracles', 'both-mode HTTP/gRPC revocation', 'RPC fault evidence', 'final TN-01..08 verifier'];
    const output = config?.output || process.env.W3_REPORT_DIR;
    if (output && !preserveExistingOutput) {
        mkdirSync(output, { recursive: true, mode: 0o700 });
        if (privateProofs.length) {
            const raw = JSON.stringify(privateProofs, null, 2) + '\n';
            writeFileSync(join(output, 'siwe-proofs.private.json'), raw, { mode: 0o600, flag: 'wx' });
            report.privateProofSHA256 = hash(raw);
        }
        if (cuaTrace && report.phaseVerified) {
            try {
                const raw = cuaTrace.serialize();
                writeFileSync(join(output, 'cua-trace.private.json'), raw, { mode: 0o600, flag: 'wx' });
                report.cuaTraceSHA256 = hash(raw);
            } catch { report.errors.push('CUA private trace was incomplete or could not be saved'); report.phaseVerified = false; report.status = 'FAIL'; }
        }
        writeFileSync(join(output, 'result.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
    }
    console.log(`${report.status}: wallet phase only; full testnet acceptance=false`);
    process.exitCode = report.status === 'FAIL' ? 1 : report.phaseVerified ? 0 : 2;
}
