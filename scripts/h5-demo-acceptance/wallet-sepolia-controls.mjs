// Observe existing user admissions and transfers. No wallet signing, provider
// injection, transaction submission, approval clicks or browser creation.
import { readFile, writeFile, lstat } from 'node:fs/promises';
import { join } from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { PrivateFileRPC } from './lib/wallet_file_rpc.mjs';
import { settings, validIdentity } from './lib/wallet_sepolia_contract.mjs';
import { PublicControlJournal, claims, digest, chooseBaseline, playbackForWitness, readCompletedOracle, sameSession, sleep } from './lib/wallet_public_controls.mjs';
import { CuaPageBridge } from './lib/wallet_cua_bridge.mjs';
import { CuaControlSession } from './lib/wallet_cua_control_session.mjs';

const execute = promisify(execFile);
let context, config, browser, page, rpc, journal, cuaBridge, cuaControl, cuaConnected = false, disconnected = false;
const result = { schemaVersion: 1, component: 'SEPOLIA_PUBLIC_CONTROL_PHASE', completeAcceptance: false, publicWalletSignoff: false, status: 'BLOCKED' };
const privateWrite = (name, value) => writeFile(join(config.output, name), JSON.stringify(value, null, 2) + '\n', { flag: 'wx', mode: 0o600 });
async function bounded(operation, ms) {
    let timer;
    try { return await Promise.race([operation, new Promise((_, reject) => { timer = setTimeout(() => reject(Error('Observation exceeded its bounded transport time')), ms); })]); }
    finally { clearTimeout(timer); }
}

async function state() {
    if (!cuaControl && new URL(page.url()).origin !== config.origin) throw Error('Selected H5 navigated away');
    const value = cuaControl ? await bounded(cuaControl.state(), 12000) : await bounded(page.evaluate(async () => {
        const video = document.getElementById('video-player');
        const p = window.ethereum?.providers?.find(item => item.isMetaMask) || (window.ethereum?.isMetaMask ? window.ethereum : null);
        const token = sessionStorage.getItem('streamgate_token'), identity = sessionStorage.getItem('streamgate_identity');
        return { token, identity: identity ? JSON.parse(identity) : null, wallet: p ? { metaMask: true,
            accounts: await p.request({ method: 'eth_accounts' }), chainId: Number(await p.request({ method: 'eth_chainId' })) } : null,
            uiState: document.getElementById('login-session-state')?.textContent || '', navigationType: performance.getEntriesByType('navigation')[0]?.type,
            playback: { at: Date.now(), time: video?.currentTime || 0, duration: video?.duration, frames: video?.getVideoPlaybackQuality?.().totalVideoFrames || 0,
                paused: video?.paused ?? true, ended: video?.ended ?? true, loop: video?.loop === true, rate: video?.playbackRate,
                error: video?.error?.code || null, state: document.getElementById('player-status')?.dataset.sgPlaybackState || 'unknown' } };
    }), 12000);
    value.claims = value.token ? claims(value.token) : null;
    return value;
}

async function token() {
    const current = await state();
    if (!validIdentity(current, config) || !sameSession(current.claims, context.sessionBinding)) throw Error('The original independently verified admission was lost or replaced');
    return current.token;
}

async function http(url, options) {
    // Requests use the actual existing page origin. The browser is never given
    // a synthetic provider, token, response, fixture credential or signature.
    if (cuaControl) return bounded(cuaControl.http(url, options), 15000);
    const observed = await bounded(page.evaluate(async ({ url, options }) => {
        const response = await fetch(url, { ...options, redirect: 'error', signal: AbortSignal.timeout(12000) });
        const bytes = new Uint8Array(await response.arrayBuffer());
        if (bytes.length > 2 * 1024 * 1024) throw Error('Observed body exceeds media budget');
        let encoded = ''; for (let i = 0; i < bytes.length; i += 8192) encoded += String.fromCharCode(...bytes.subarray(i, i + 8192));
        return { status: response.status, base64: btoa(encoded), at: Date.now() };
    }, { url, options }), 15000);
    return { status: observed.status, bytes: Buffer.from(observed.base64, 'base64'), at: observed.at };
}

async function oracle() {
    const raw = await readCompletedOracle(context.oracle, context.oracleExit);
    if (!raw) return null;
    const report = JSON.parse(raw);
    if (report.status !== 'PHASE_CHAIN_VERIFIED' || report.phase !== config.phase + '-transferred' || report.candidateSHA !== config.candidateSHA || report.recipient !== context.recipient ||
        report.controller !== config.controller || report.wallet !== context.wallet || report.nft !== config.nft || report.chainId !== 11155111 ||
        !Number.isFinite(report.safeObservedAtMs)) throw Error('Original transfer oracle belongs to another phase, chain or fixture');
    return { report, sha256: digest(raw) };
}

async function transfer() {
    console.log(`USER ACTION ${config.mode} ${config.phase}: keep this content playing and enable Loop in the native video menu before starting the transfer`);
    const waitStarted = Date.now(); let initialPlayback;
    while (true) {
        const current = await state();
        if (validIdentity(current, config) && current.playback.loop && !current.playback.paused && !current.playback.ended && current.playback.frames > 0 && current.playback.rate === 1) {
            initialPlayback = current.playback; break;
        }
        if (Date.now() - waitStarted >= config.waitMs) { const error = Error('Normal repeated playback was not observed'); error.blocked = true; throw error; }
        await sleep(1000);
    }
    const started = Date.now(), baselines = [], identities = [], refusals = [], playback = [initialPlayback];
    let frozen = false, chain;
    console.log(`READY ${config.mode} ${config.phase}: observe the original user transfer and its concurrently running oracle`);
    await privateWrite('ready.json', { candidateSHA: config.candidateSHA, mode: config.mode, phase: config.phase, startedAt: started, completeAcceptance: false });
    while (Date.now() - started < config.waitMs) {
        const tick = Date.now(), identity = await journal.live(); identities.push(identity); playback.push((await state()).playback);
        if (!frozen) {
            const baseline = await journal.mediaBaseline(identity);
            if (baseline) { baselines.push(baseline); if (baselines.length > 6) baselines.shift(); }
            else frozen = true;
        }
        if (frozen) {
            const access = await token();
            for (const baseline of baselines) {
                const refusal = await journal.refusal(baseline.reference, access);
                if (refusal.httpManifest.code === 403 && refusal.grpcManifest.code === 'PermissionDenied' &&
                    refusal.httpMedia.code === 401 && refusal.grpcMedia.code === 'Unauthenticated') {
                    const witness = await journal.live(); identities.push(witness);
                    playback.push((await state()).playback);
                    refusals.push({ capability: baseline.media.credentialSHA256, refusal, witness });
                }
            }
        }
        chain ||= await oracle();
        if (chain) {
            const safe = chain.report.safeObservedAtMs;
            if (safe < started) throw Error('Collector started after the original safe observation');
            const baseline = chooseBaseline(baselines, safe);
            if (!baseline && frozen) throw Error('No original live HTTP/gRPC baseline remains within five seconds before safe');
            if (baseline) {
                const last = refusals.find(item => item.capability === baseline.media.credentialSHA256 &&
                    Object.values(item.refusal).every(event => safe <= event.at && event.at <= safe + 70000) &&
                    Object.values(item.refusal).every(event => event.at <= Math.max(item.witness.http.at, item.witness.grpc.at) &&
                        Math.max(item.witness.http.at, item.witness.grpc.at) - event.at <= 5000) &&
                    playbackForWitness(playback, item.witness, safe));
                if (last) {
                    const before = Math.max(baseline.identity.http.at, baseline.identity.grpc.at), after = Math.max(last.witness.http.at, last.witness.grpc.at);
                    const timeline = identities.filter(item => Math.min(item.http.at, item.grpc.at) >= before && Math.max(item.http.at, item.grpc.at) <= after);
                    const { reference, ...publicBaseline } = baseline;
                    return { oracleSHA256: chain.sha256, baseline: publicBaseline, identities: timeline, refusal: last.refusal,
                        playback: playbackForWitness(playback, last.witness, safe) };
                }
            }
            if (Date.now() > safe + 75000) throw Error('Original safe-time refusal window was missed; do not reset the clock or rebroadcast');
        }
        await sleep(Math.max(0, 1800 - (Date.now() - tick)));
    }
    const error = Error('Original transfer/oracle was not observed within the bounded user wait'); error.blocked = true; throw error;
}

async function network(action) {
    const { stdout } = await execute('docker', ['inspect', '--format', '{{json .}}', context.container], { maxBuffer: 2 * 1024 * 1024, timeout: 15000 });
    const live = JSON.parse(stdout), labels = live.Config.Labels;
    if (live.Id !== context.container || live.Image !== context.image || !live.State.Running ||
        labels['com.streamgate.w3.candidate'] !== config.candidateSHA || labels['com.streamgate.w3.testnet'] !== context.project ||
        !live.NetworkSettings.Networks[context.dataNetwork]) throw Error('Fault scope changed; preserve the original runtime');
    if (action === 'disconnect') {
        if (!live.NetworkSettings.Networks[context.egressNetwork]) throw Error('RPC egress was already absent before the controlled fault');
        // Mark before dispatch: an interrupted CLI may have completed the
        // disconnect. Both child and parent independently inspect on recovery.
        disconnected = true;
        await execute('docker', ['network', 'disconnect', context.egressNetwork, context.container], { timeout: 15000 });
    } else if (!live.NetworkSettings.Networks[context.egressNetwork]) {
        const aliases = context.egressAliases.flatMap(alias => ['--alias', alias]);
        await execute('docker', ['network', 'connect', ...aliases, context.egressNetwork, context.container], { timeout: 15000 }); disconnected = false;
    } else disconnected = false;
}

async function outage() {
    // Wait for a naturally renewed H5 lease. A second successful refresh client
    // would consume the JTI and race the application, so it is never used here.
    const deadline = Date.now() + 65000;
    while (claims(await token()).exp * 1000 < Date.now() + 42000) {
        if (Date.now() > deadline) throw Error('No fresh natural H5 lease was observed before the dependency test');
        await sleep(1000);
    }
    const before = await journal.live(), original = await token();
    let unavailable, grpc, uiState, anonymous;
    try {
        await network('disconnect');
        [unavailable, grpc] = await Promise.all([
            journal.httpEvent('refresh', '/api/v1/auth/refresh', original, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token: original }) }),
            journal.grpcEvent('RefreshToken', 'refresh', original),
        ]);
        if (unavailable.code !== 503 || grpc.code !== 'Unavailable') throw Error('RPC outage did not retain dependency-unavailable semantics');
        anonymous = await journal.anonymous();
        if (anonymous.http.code !== 401 || anonymous.grpc.code !== 'Unauthenticated') throw Error('Anonymous manifest access was granted during the dependency fault');
        const end = Math.min(claims(original).exp * 1000 - 7000, Date.now() + 40000);
        while (Date.now() < end) {
            const current = await state();
            if (current.uiState.startsWith('dependency-unavailable')) { uiState = 'unavailable'; break; }
            await sleep(500);
        }
        if (uiState !== 'unavailable') throw Error('Actual H5 dependency-unavailable state was not observed');
    } finally { if (disconnected) await network('restore'); }
    const after = await journal.live();
    return { before, http: unavailable, grpc, after, uiState, anonymous, anonymousAccessGranted: false };
}

try {
    const path = process.argv[2], info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || info.mode & 0o077 || info.size > 65536) throw Error('Verified private observation context required');
    context = JSON.parse(await readFile(path, 'utf8')); config = settings(context.environment);
    Object.assign(result, { candidateSHA: config.candidateSHA, mode: config.mode, phase: config.phase, origin: config.origin,
        controller: config.controller, wallet: context.wallet, nft: config.nft, content: config.content, chainId: 11155111 });
    if (config.transport === 'cua') {
        cuaBridge = new CuaPageBridge({ targetOrigin: config.origin, content: config.content });
        await cuaBridge.start();
        cuaControl = new CuaControlSession({ bridge: cuaBridge, origin: config.origin, content: config.content });
        console.log('CUA_RELAY ' + JSON.stringify({ origin: cuaBridge.origin, sessionFile: cuaBridge.sessionFile, targetOrigin: config.origin }));
        await cuaControl.state(); cuaConnected = true;
    } else {
        const { chromium } = await import('playwright');
        browser = await chromium.connectOverCDP(config.cdp, { timeout: 10000, noDefaults: true });
        const matches = [];
        for (const p of browser.contexts().flatMap(c => c.pages())) {
            if (new URL(p.url()).origin === config.origin && await p.locator('#video-player').count() === 1) matches.push(p);
        }
        if (matches.length !== 1) throw Error('Exactly one existing MetaMask H5 player is required');
        [page] = matches;
    }
    rpc = await new PrivateFileRPC(context.command, context.requestDirectory).start();
    journal = new PublicControlJournal(config, rpc, token, http);
    await token();
    if (config.phase === 'contract') result.dependencyOutage = await outage();
    result.transfer = await transfer();
    if (config.phase === 'contract') {
        const startedAt = Date.now();
        if (cuaControl) await cuaControl.reload(); else await page.reload({ waitUntil: 'domcontentloaded' });
        let observed;
        for (let i = 0; i < 100; i++) {
            observed = await state();
            if (validIdentity(observed, config) || !observed.token && !observed.identity) break;
            await sleep(100);
        }
        const finishedAt = Date.now();
        if (observed.navigationType !== 'reload' || !observed.wallet?.metaMask || observed.wallet.chainId !== 11155111 ||
            String(observed.wallet.accounts?.[0]).toLowerCase() !== config.controller) throw Error('Actual reload/wallet state is unavailable');
        result.reload = { navigationType: observed.navigationType, startedAt, finishedAt, provider: 'MetaMask', chainId: observed.wallet.chainId,
            account: observed.wallet.accounts[0].toLowerCase(), authenticated: validIdentity(observed, config), hasAccessToken: !!observed.token };
        if (result.reload.authenticated) result.reload.identity = await journal.live();
        else if (observed.token || observed.identity) throw Error('Reload left a half-authenticated state');
        else result.reload.uiState = 'unknown';
    }
    result.status = 'CONTROL_PHASE_OBSERVED';
} catch (error) {
    result.status = error.blocked || !(browser || cuaConnected) ? 'BLOCKED' : 'FAIL';
    // Browser/network errors may contain a URL or credential. Original private
    // files remain available; public errors contain only a bounded category.
    result.reason = result.status === 'BLOCKED' ? 'Verified inputs, existing wallet browser or user transfer unavailable' : 'Original control observation failed';
} finally {
    if (disconnected) { try { await network('restore'); } catch { result.status = 'FAIL'; result.reason = 'Owned RPC egress restoration failed'; } }
    if (rpc) {
        result.clientExit = await rpc.close();
        if (result.clientExit?.code !== 0 || result.clientExit?.signal || rpc.failure) result.status = 'FAIL';
        result.clientEvents = rpc.events;
    }
    if (browser) await browser.close().catch(() => { result.status = 'FAIL'; });
    if (cuaControl) {
        try { await cuaControl.close(); }
        catch { if (cuaConnected) result.status = 'FAIL'; }
    }
    if (cuaBridge) {
        try { await cuaBridge.close(); }
        catch { result.status = 'FAIL'; }
    }
    if (config) {
        result.credentialRotations = journal?.credentialRotations || [];
        const raw = JSON.stringify(journal?.private || { credentials: {}, responses: {} }, null, 2) + '\n';
        await writeFile(join(config.output, 'controls.private.json'), raw, { flag: 'wx', mode: 0o600 });
        result.privateSHA256 = digest(raw); await privateWrite('result.json', result);
    }
    console.log(`${result.status}: public control phase only; full acceptance=false`);
    process.exitCode = result.status === 'CONTROL_PHASE_OBSERVED' ? 0 : result.status === 'BLOCKED' ? 2 : 1;
}
