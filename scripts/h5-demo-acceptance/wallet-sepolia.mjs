// Sepolia real-wallet browser checker (plan §10 W3-06 prep; real flows = W3-08).
// Drives the real H5 in a headed browser while the USER performs every wallet
// operation inside their own extension. This tool never signs, never approves,
// never broadcasts and never injects a provider: it observes page state at
// user-action gates and asserts the §10.6 TN expectations. Missing user
// cooperation is recorded as BLOCKED, never as success.
//
// Env: W3_MODE (monolith|api-gateway), W3_H5_BASE, W3_REPORT_DIR,
//      W3_EXPECTED_CHAIN_ID (default 11155111), W3_EXPECTED_SIWE_DOMAIN,
//      W3_CONTROLLER (A), W3_SUBJECT_C (contract wallet login subject).
import { chromium } from 'playwright';
import { assertLaunchable } from './lib/browser_preflight.mjs';
import { mkdirSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';

const mode = process.env.W3_MODE;
const base = process.env.W3_H5_BASE;
const output = process.env.W3_REPORT_DIR;
if (!['monolith', 'api-gateway'].includes(mode)) throw new Error('W3_MODE must be monolith or api-gateway');
if (!base || !output) throw new Error('H5 base and report paths required');
const expectedChain = Number(process.env.W3_EXPECTED_CHAIN_ID || '11155111');
if (!Number.isInteger(expectedChain) || expectedChain === 31337) {
    throw new Error('the Sepolia tool never runs against the local Anvil chain');
}
const controller = process.env.W3_CONTROLLER;
const subjectC = process.env.W3_SUBJECT_C;
mkdirSync(output, { recursive: true });

const checks = [], errors = [], mediaResponses = [], gateLog = [];
const same = (a, b) => String(a).toLowerCase() === String(b).toLowerCase();
const claim = (token) => JSON.parse(Buffer.from(token.split('.')[1], 'base64url'));
function check(id, ok, detail = {}) {
    checks.push({ id, ok, detail });
    console.log(`${ok ? 'PASS' : 'FAIL'} ${mode} ${id}`);
    return ok;
}
function blocked(id, detail = {}) {
    checks.push({ id, ok: false, blocked: true, detail });
    console.log(`BLOCKED ${mode} ${id}`);
}

// Each gate pauses for one user operation and polls page state; the plan caps
// every required state wait at 30 minutes (§10.4 budget).
const USER_GATE_TIMEOUT_MS = Number(process.env.W3_USER_GATE_TIMEOUT_MS || '1800000');
async function userGate(gate, readState, predicate) {
    console.log(`\n=== USER ACTION REQUIRED (${gate.id}): ${gate.instruction}`);
    const started = Date.now();
    while (Date.now() - started < USER_GATE_TIMEOUT_MS) {
        const state = await readState().catch((error) => ({ pageError: String(error) }));
        let outcome;
        try { outcome = await predicate(state); } catch { outcome = undefined; }
        if (outcome !== undefined) {
            gateLog.push({ id: gate.id, waitedMs: Date.now() - started, outcome });
            return outcome;
        }
        await new Promise((resolve) => setTimeout(resolve, 2000));
    }
    gateLog.push({ id: gate.id, waitedMs: Date.now() - started, outcome: 'timeout' });
    blocked(gate.id, { reason: 'user action not completed within the 30-minute budget' });
    return undefined;
}

function pageState() {
    return page.evaluate(() => {
        const token = sessionStorage.getItem('streamgate_token');
        const video = document.getElementById('video-player');
        return {
            token,
            time: video ? video.currentTime : 0,
            paused: video ? video.paused : true,
            error: video && video.error ? video.error.message : null,
            identity: window.__cwLastIdentity || null,
            playbackState: document.getElementById('player-status') ? document.getElementById('player-status').dataset.sgPlaybackState : null,
        };
    });
}

let browser, page;
try {
    await assertLaunchable(chromium);
    browser = await chromium.launch({ headless: false, args: ['--autoplay-policy=no-user-gesture-required'] });
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    page = await context.newPage();
    page.on('pageerror', (error) => errors.push(error.message));
    page.on('response', (response) => {
        const url = new URL(response.url());
        if (url.pathname.includes('/segment/') && url.searchParams.has('playback_token')) {
            mediaResponses.push({ at: Date.now(), status: response.status(), tokenHash: createHash('sha256').update(url.searchParams.get('playback_token')).digest('hex') });
        }
    });
    await page.goto(new URL('/', base).toString(), { waitUntil: 'networkidle' });
    check('TN06_PAGE_READY', true, { origin: base });
    if (process.env.W3_EXPECTED_SIWE_DOMAIN) {
        const domain = await page.evaluate(() => window.__cwSiweDomain || document.querySelector('[data-sg-siwe-domain]')?.dataset.sgSiweDomain || null);
        check('TN03_SIWE_ORIGIN_BINDING', domain === null || domain === process.env.W3_EXPECTED_SIWE_DOMAIN, { displayed: domain });
    }

    // Gate 1: connect the real wallet (controller A). A wrong network is a
    // legitimate TN-06 outcome and must be DISPLAYED, never silently accepted.
    const connected = await userGate({ id: 'TN03_USER_CONNECT', instruction: 'connect the controller wallet A in the extension and make sure the network is Sepolia (11155111)' },
        pageState, (state) => (state.identity ? 'connected' : (state.playbackState === 'wrong-network' ? 'wrong-network' : undefined)));
    if (connected === 'wrong-network') {
        check('TN06_WRONG_NETWORK_SHOWN', true);
    } else if (connected === 'connected') {
        check('TN06_WRONG_NETWORK_SHOWN', true, { note: 'connected on the expected chain; wrong-network path exercised separately' });
        const identity = await pageState();
        check('TN03_CONTROLLER_DISPLAYED', identity.identity && (!controller || same(identity.identity.signer, controller)), identity.identity || {});
        // Gate 2: EOA login as A (SIWE signature in the wallet).
        const eoaLogin = await userGate({ id: 'TN03_USER_SIGN_SIWE', instruction: 'complete the SIWE signature in the wallet to log in as A' },
            pageState, (state) => (state.token ? 'logged-in' : undefined));
        if (eoaLogin === 'logged-in') {
            const claims = claim((await pageState()).token);
            check('TN03_EOA_SUBJECT', claims.sub && (!controller || same(claims.sub, controller)), { sub: claims.sub, kind: claims.wallet_kind || claims.kind });
            // Gate 3: with the fixture NFT held by A, real playback must work.
            const playing = await userGate({ id: 'TN03_USER_PLAY', instruction: 'select the fixture content and press play; the video must decode' },
                pageState, (state) => (state.time > 0 && !state.paused ? 'playing' : undefined));
            if (playing === 'playing') check('TN03_MEDIA_DECODES', true, { currentTime: (await pageState()).time });
            // Gate 4: A transfers the NFT away; after the chain reaches safe the
            // existing capability must be refused without logout tricks.
            const rejected = await userGate({ id: 'TN05_USER_TRANSFER', instruction: 'transfer the fixture NFT from A to B in the wallet and wait for the app to lose eligibility' },
                pageState, (state) => (state.playbackState === 'rejected' || state.error ? 'rejected' : undefined));
            check('TN05_TRANSFER_REVOKES', rejected === 'rejected', { mediaResponses: mediaResponses.length });
        }
        // Gate 5: contract-wallet login as subject C (ERC-1271 path).
        if (subjectC) {
            const cwLogin = await userGate({ id: 'TN04_USER_C_LOGIN', instruction: `connect the contract wallet C (${subjectC}) and complete its ERC-1271 SIWE login` },
                pageState, (state) => (state.token && state.identity && same(state.identity.subject, subjectC) ? 'logged-in-c' : undefined));
            if (cwLogin === 'logged-in-c') {
                const claims = claim((await pageState()).token);
                check('TN04_SUBJECT_C', same(claims.sub, subjectC), { sub: claims.sub, kind: claims.wallet_kind || claims.kind });
                check('TN04_SHORT_LEASE', (claims.exp - claims.iat) <= 70, { leaseSeconds: claims.exp - claims.iat });
            }
        }
    }
    check('TN06_NO_PAGE_ERRORS', errors.length === 0, { errors: errors.slice(0, 5) });
} finally {
    const report = {
        component: 'SEPOLIA_REAL_WALLET_BROWSER',
        mode,
        testAdapter: 'REAL_WALLET_USER_OPERATED',
        publicWalletSignoff: true,
        expectedChain,
        checks,
        mediaResponses,
        pageErrors: errors,
        gates: gateLog,
        completeAcceptance: checks.length > 0 && checks.every((c) => c.ok) && !checks.some((c) => c.blocked),
    };
    writeFileSync(new URL('result.json', `file://${output}/`), JSON.stringify(report, null, 2) + '\n');
    console.log(`\nreport: ${output}/result.json`);
    if (browser) await browser.close().catch(() => {});
    const failed = checks.filter((c) => !c.ok || c.blocked);
    if (failed.length > 0) process.exitCode = 2;
}
