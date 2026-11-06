// Synthetic controls with a test-only signer. They prove the gate can fail,
// never that a user has signed, controlled A or played on public Sepolia.
const assert = require('node:assert/strict');
const { createHash, createHmac } = require('node:crypto');
const test = require('node:test');
const { execFileSync, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const ethers = require('../../h5-demo/vendor/ethers.umd.min.js');
const hash = value => createHash('sha256').update(value).digest('hex');
const load = () => import('../h5-demo-acceptance/lib/wallet_sepolia_evidence.mjs');
const signer = new ethers.Wallet('0x' + '11'.repeat(32));
const C = '0x' + '2'.repeat(40), N = '0x' + '3'.repeat(40), now = 1700000000000;
const token = claims => {
    const body = Buffer.from(JSON.stringify({ alg: 'HS256', typ: 'JWT' })).toString('base64url') + '.' + Buffer.from(JSON.stringify(claims)).toString('base64url');
    return body + '.' + createHmac('sha256', 'UNIT_ONLY_NEVER_APPLICATION_SECRET').update(body).digest('base64url');
};

async function fixture(phase = 'contract') {
    const { PHASE_CHECKS } = await import('../h5-demo-acceptance/lib/wallet_sepolia_contract.mjs');
    const config = { candidateSHA: 'a'.repeat(40), mode: 'monolith', phase, controller: signer.address.toLowerCase(), subject: phase === 'contract' ? C : signer.address.toLowerCase(),
        nft: N, content: '12345678-1234-1234-1234-123456789abc', origin: 'http://localhost:18500' };
    const report = { candidateSHA: config.candidateSHA, mode: config.mode, phase, component: 'SEPOLIA_REAL_WALLET_BROWSER_PHASE',
        testAdapter: 'EXISTING_METAMASK_CDP_READ_ONLY', status: 'WALLET_PHASE_OBSERVED', phaseVerified: true,
        publicWalletSignoff: false, completeAcceptance: false, errors: [], blockers: [], gates: [], admissions: [], samples: [], media: [], challenges: [],
        issues: [], refreshes: [], checks: PHASE_CHECKS[phase].map(id => ({ id, ok: true, detail: {} })) };
    const wallet = { metaMask: true, accounts: [config.controller], chainId: 11155111 };
    const gate = (id, at, state, before, after) => ({ id, startedAt: at - 100, finishedAt: at, elapsedMs: 100,
        challengeSequenceBefore: before, challengeSequenceAfter: after, state: { ...state, at } });
    if (phase === 'negative') {
        report.challenges = [1, 2].map(sequence => ({ sequence, at: now + sequence * 1000 - 50, origin: config.origin, status: 200, chainId: 11155111,
            subject: config.subject, signingType: 'siwe', messageSHA256: hash('negative-test-' + sequence) }));
        report.gates = [gate('TN06_WRONG_NETWORK', now + 1000, { tokenHash: null, claims: null, identity: null, wallet: { ...wallet, chainId: 1 }, authError: 'wrong-network' }, 0, 1),
            gate('TN06_USER_CANCEL', now + 2000, { tokenHash: null, claims: null, identity: null, wallet, authError: 'signature-rejected' }, 1, 2)];
        return { config, report, proofsRaw: null };
    }
    const prefix = phase === 'contract' ? 'TN04' : 'TN03';
    const issued = new Date(now - 1000).toISOString().replace('.000Z', 'Z'), expires = new Date(now + 299000).toISOString().replace('.000Z', 'Z');
    const challenge = { challenge_id: 'unit-challenge', wallet: config.subject, chain_id: 11155111, nonce: '12345678', signing_type: 'siwe', issued_at: issued, expires_at: expires,
        message: `localhost:18500 wants you to sign in with your Ethereum account:\n${config.subject}\n\nURI: ${config.origin}/login\nVersion: 1\nChain ID: 11155111\nNonce: 12345678\nIssued At: ${issued}\nExpiration Time: ${expires}` };
    const signature = await signer.signMessage(challenge.message);
    const claims = { sub: config.subject, wallet_address: config.subject, chain_id: 11155111, account_kind: phase === 'contract' ? 'erc1271' : 'eoa',
        iat: Math.floor(now / 1000), exp: Math.floor(now / 1000) + (phase === 'contract' ? 60 : 7200), session_id: 'cw:test-session',
        wallet_proof_id: 'test-proof', wallet_session_exp: Math.floor(now / 1000) + 7200, token_type: 'access', iss: 'streamgate', aud: ['streamgate-api'], auth_epoch: 1, restore_epoch: 'unit-epoch' };
    const initialToken = token(claims), initialHash = hash(initialToken), credentials = [{ tokenHash: initialHash, token: initialToken }];
    const turns = new Map([[0, { tokenHash: initialHash, claims }]]);
    if (phase === 'contract') for (const turn of [1, 2]) {
        const refreshed = { ...claims, iat: claims.iat + turn * 25, exp: claims.iat + turn * 25 + 60 };
        const raw = token(refreshed), tokenHash = hash(raw); credentials.push({ tokenHash, token: raw });
        report.refreshes.push({ at: now + turn * 25000, oldTokenHash: turns.get(turn - 1).tokenHash, newTokenHash: tokenHash, oldClaims: turns.get(turn - 1).claims, newClaims: refreshed });
        turns.set(turn, { tokenHash, claims: refreshed });
    }
    const identity = { controller: config.controller, subject: config.subject, chainId: 11155111, kind: claims.account_kind, isDemo: false };
    const state = { tokenHash: initialHash, claims, identity, wallet, time: 1, frames: 30, paused: false, videoError: null };
    report.admissions = [{ at: now, controller: config.controller, subject: config.subject, chainId: 11155111, originBound: true, tokenHash: initialHash,
        challengeIdSHA256: hash(challenge.challenge_id), messageSHA256: hash(challenge.message), signatureSHA256: hash(signature) }];
    report.challenges = [{ sequence: 1, at: now - 100, origin: config.origin, status: 200, chainId: 11155111, subject: config.subject, signingType: 'siwe', messageSHA256: hash(challenge.message) }];
    report.gates = [gate(`${prefix}_SIGN_SIWE`, now + 100, state, 0, 1), gate(`${prefix}_PLAY`, now + 1000, state, 1, 1)];
    report.checks.find(check => check.id === `${prefix}_PROFILE_SUBJECT`).detail = { status: 200, subject: config.subject, accessTokenHash: initialHash };
    const duration = phase === 'contract' ? 71000 : 4000;
    for (let elapsed = 0; elapsed <= duration; elapsed += 1000) {
        const turn = phase === 'contract' ? Math.floor(elapsed / 25000) : 0;
        report.samples.push({ ...state, claims: turns.get(turn).claims, at: now + 1000 + elapsed,
            tokenHash: turns.get(turn).tokenHash, time: 1 + elapsed / 1000, frames: 30 + elapsed / 1000 * 30 });
    }
    for (const elapsed of phase === 'contract' ? [0, 35000, 65000] : [0]) {
        const iat = claims.iat + Math.floor(elapsed / 1000);
        const access = turns.get(phase === 'contract' ? Math.floor(elapsed / 25000) : 0);
        const capabilityClaims = { wallet_address: config.subject, account_kind: claims.account_kind, chain_id: 11155111, content_id: config.content, contract: N, token_type: 'playback',
            iat, exp: Math.min(iat + 60, access.claims.exp), wallet_session_exp: claims.wallet_session_exp, session_id: claims.session_id, wallet_proof_id: claims.wallet_proof_id,
            auth_epoch: 1, restore_epoch: 'unit-epoch', iss: 'streamgate', aud: ['streamgate-playback'] };
        report.media.push({ at: now + 1000 + elapsed, kind: 'segment', status: 200, size: 1000, sha256: hash('unit-segment-bytes'), tokenHash: hash('cap-' + elapsed),
            claims: capabilityClaims });
        report.issues.push({ at: now + 1000 + elapsed, status: 200, accessTokenHash: access.tokenHash, accessClaims: access.claims,
            capabilityHash: hash('cap-' + elapsed), capabilityClaims, manifestSHA256: hash('unit-manifest') });
    }
    const proofsRaw = JSON.stringify([{ challenge, signature, controller: config.controller, tokenHash: initialHash, credentials }]);
    report.privateProofSHA256 = hash(proofsRaw);
    return { config, report, proofsRaw };
}

// Test-only transcript with real ECDSA test signatures and synthetic browser
// bytes; this never establishes a user wallet or a public-chain observation.
async function cuaFixture(phase = 'eoa') {
    const good = await fixture(phase), { config, report, proofsRaw } = good;
    report.testAdapter = 'CUA_SCOPED_READ_ONLY';
    const proofs = proofsRaw ? JSON.parse(proofsRaw) : [], credentials = proofs.flatMap(proof => proof.credentials);
    const tokenFor = digest => credentials.find(credential => credential.tokenHash === digest)?.token;
    const events = [];
    const add = (at, pathname, body, requestBody = '', headers = {}, query = '') => events.push({ at, pathname, body, requestBody, headers, query });
    if (phase === 'negative') {
        for (const challenge of report.challenges) add(challenge.at, '/api/v1/auth/challenge', JSON.stringify({ chain_id: 11155111,
            wallet: config.subject, signing_type: 'siwe', challenge_id: 'negative-' + challenge.sequence, message: 'negative-test-' + challenge.sequence }));
    } else {
        const proof = proofs[0];
        add(report.challenges[0].at, '/api/v1/auth/challenge', JSON.stringify(proof.challenge));
        add(report.admissions[0].at, '/api/v1/auth/login', JSON.stringify({ token: tokenFor(report.admissions[0].tokenHash) }),
            JSON.stringify({ challenge_id: proof.challenge.challenge_id, wallet: config.subject, chain_id: 11155111, signature: proof.signature }));
        for (const renewal of report.refreshes) add(renewal.at, '/api/v1/auth/refresh', JSON.stringify({ token: tokenFor(renewal.newTokenHash) }), JSON.stringify({ token: tokenFor(renewal.oldTokenHash) }));
        for (const [index, issue] of report.issues.entries()) {
            const capability = token(issue.capabilityClaims), prefix = `/api/v1/streaming/${config.content}/`;
            const body = `#EXTM3U\n${prefix}segment/0.ts?playback_token=${capability}`;
            issue.capabilityHash = hash(capability); issue.manifestSHA256 = hash(body);
            const media = report.media[index]; media.tokenHash = issue.capabilityHash; media.size = Buffer.byteLength('unit-segment-bytes'); media.sha256 = hash('unit-segment-bytes');
            add(issue.at, prefix + 'manifest.m3u8', body, '', { authorization: 'Bearer ' + tokenFor(issue.accessTokenHash) });
            add(media.at, prefix + 'segment/0.ts', 'unit-segment-bytes', '', {}, '?playback_token=' + capability);
        }
    }
    events.sort((a, b) => a.at - b.at);
    const trace = { version: 1, candidateSHA: config.candidateSHA, mode: config.mode, phase, origin: config.origin, content: config.content, requests: [], calls: [] };
    let sequence = 0, eventIndex = 0, current = now - 1000;
    function call(kind, at, callback) {
        const entry = { kind, first: trace.requests.length, startedAt: at };
        current = at; callback(); entry.end = trace.requests.length; entry.finishedAt = at; trace.calls.push(entry);
    }
    function request(op, args, value) { trace.requests.push({ op, args, value, startedAt: current, finishedAt: current }); }
    function pump() {
        const afterSequence = sequence, batch = [], bodies = [];
        while (eventIndex < events.length && events[eventIndex].at <= current) {
            const event = events[eventIndex++], id = 'request-' + eventIndex, base = { requestId: id, timestamp: 100, wallTime: event.at / 1000 };
            batch.push({ sequence: ++sequence, method: 'Network.requestWillBeSent', params: { ...base,
                request: { url: config.origin + event.pathname + event.query, method: 'POST', postData: event.requestBody, headers: event.headers } } });
            batch.push({ sequence: ++sequence, method: 'Network.responseReceived', params: { requestId: id, timestamp: 100, response: { status: 200 } } });
            batch.push({ sequence: ++sequence, method: 'Network.loadingFinished', params: { requestId: id, timestamp: 100 } });
            bodies.push({ id, body: event.body });
        }
        request('network-events', { afterSequence }, { cursor: sequence, events: batch, truncated: false, hasMore: false, unresolved: false });
        for (const body of bodies) request('network-body', { requestId: body.id }, { body: body.body, base64Encoded: false });
    }
    function state(expected) {
        call('state', expected.at, () => {
            pump();
            const { at, tokenHash, claims, ...rest } = expected;
            request('state', {}, { origin: config.origin, token: tokenHash ? tokenFor(tokenHash) : null, ...rest });
            pump();
        });
    }
    call('start', current, () => request('network-enable', {}, { cursor: 0 }));
    state(report.gates[0].state);
    if (phase !== 'negative') call('profile', report.gates[0].state.at + 1, () => { pump(); request('profile', {}, { status: 200, data: { wallet_address: config.subject } }); });
    state(report.gates[1].state);
    for (const sample of report.samples) state(sample);
    call('finish', report.samples.at(-1)?.at || report.gates.at(-1).state.at, pump);
    assert.equal(eventIndex, events.length);
    const traceRaw = JSON.stringify(trace) + '\n'; report.cuaTraceSHA256 = hash(traceRaw);
    return { ...good, trace, traceRaw };
}

test('CUA phases require independent replay of original network and page observations', async () => {
    const { validateCuaPhaseEvidence } = await load();
    for (const phase of ['eoa', 'contract', 'negative']) {
        const { report, proofsRaw, traceRaw, config } = await cuaFixture(phase);
        const result = await validateCuaPhaseEvidence(report, proofsRaw, traceRaw, config, 0, ethers);
        assert.equal(result.phaseVerified, true);
        await assert.rejects(validateCuaPhaseEvidence(report, proofsRaw, null, config, 0, ethers));
    }
    // Negative collection does not require a content fixture, even when the
    // final verifier also supplies the content used by the positive phases.
    const negative = await cuaFixture('negative'); negative.trace.content = null;
    const raw = JSON.stringify(negative.trace) + '\n'; negative.report.cuaTraceSHA256 = hash(raw);
    assert.equal((await validateCuaPhaseEvidence(negative.report, null, raw, negative.config, 0, ethers)).phaseVerified, true);
});

test('CUA replay rejects recomputed hashes after changed events, clocks, credentials, origin or states', async () => {
    const { validateCuaPhaseEvidence } = await load();
    const changes = [
        t => { t.requests.find(r => r.op === 'network-events' && r.value.events.length).value.truncated = true; },
        t => { t.requests.find(r => r.op === 'network-events' && r.value.events.length).value.events[0].params.request.url = 'http://evil.example/api/v1/auth/challenge'; },
        t => { t.requests.find(r => r.op === 'network-events' && r.value.events.length).value.events.reverse(); },
        t => { t.requests.find(r => r.op === 'network-body').value.body = '{}'; },
        t => { t.requests.find(r => r.op === 'state').value.identity.subject = '0x' + '9'.repeat(40); },
        t => { t.requests.find(r => r.op === 'state').value.token += 'x'; },
        t => { t.requests.find(r => r.op === 'network-events' && r.value.events.length).value.events[0].params.wallTime -= 100; },
        t => { t.calls[1].first++; },
        t => { t.requests.pop(); },
        t => { t.origin = 'http://localhost:9999'; },
    ];
    for (const change of changes) {
        const { report, proofsRaw, trace, config } = await cuaFixture(); change(trace);
        const raw = JSON.stringify(trace) + '\n'; report.cuaTraceSHA256 = hash(raw);
        await assert.rejects(validateCuaPhaseEvidence(report, proofsRaw, raw, config, 0, ethers));
    }
});

test('CUA CLI requires the original private trace and rejects a changed transcript', async () => {
    const good = await cuaFixture(), directory = fs.mkdtempSync(path.join(os.tmpdir(), 'sg-cua-gate-'));
    const script = path.join(__dirname, 'assert_sepolia_browser.mjs');
    const args = [script, '--candidate', good.config.candidateSHA, '--mode', good.config.mode, '--phase', good.config.phase,
        '--controller', good.config.controller, '--subject', good.config.subject, '--nft', N, '--content', good.config.content,
        '--origin', good.config.origin, '--directory', directory, '--exit-file', path.join(directory, 'original.exit')];
    try {
        fs.writeFileSync(path.join(directory, 'original.exit'), '0\n');
        fs.writeFileSync(path.join(directory, 'result.json'), JSON.stringify(good.report));
        fs.writeFileSync(path.join(directory, 'siwe-proofs.private.json'), good.proofsRaw, { mode: 0o600 });
        assert.equal(spawnSync(process.execPath, args).status, 2);
        fs.writeFileSync(path.join(directory, 'cua-trace.private.json'), good.traceRaw, { mode: 0o600 });
        assert.equal(spawnSync(process.execPath, args).status, 0);
        fs.chmodSync(path.join(directory, 'cua-trace.private.json'), 0o644);
        assert.equal(spawnSync(process.execPath, args).status, 1);
        fs.chmodSync(path.join(directory, 'cua-trace.private.json'), 0o600);
        fs.appendFileSync(path.join(directory, 'cua-trace.private.json'), 'tamper');
        assert.equal(spawnSync(process.execPath, args).status, 1);
    } finally { fs.rmSync(directory, { recursive: true, force: true }); }
});

test('wallet observer preserves an existing private trace before opening any browser', () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'sg-cua-preserve-'));
    const file = path.join(directory, 'cua-trace.private.json'), original = 'original trace bytes\n';
    try {
        fs.writeFileSync(file, original, { mode: 0o600 });
        const env = { ...process.env, W3_PHASE: 'negative', W3_MODE: 'monolith', W3_CANDIDATE_SHA: 'a'.repeat(40),
            W3_H5_BASE: 'http://localhost:18500', W3_BROWSER_TRANSPORT: 'cua', W3_CONTROLLER: signer.address, W3_REPORT_DIR: directory };
        const result = spawnSync(process.execPath, [path.join(__dirname, '../h5-demo-acceptance/wallet-sepolia.mjs')], { env, encoding: 'utf8' });
        assert.equal(result.status, 2); assert.equal(fs.readFileSync(file, 'utf8'), original);
        assert.equal(fs.existsSync(path.join(directory, 'result.json')), false);
    } finally { fs.rmSync(directory, { recursive: true, force: true }); }
});

test('both positive wallet phases require actual proof and original exit, without claiming full acceptance', async () => {
    const { validatePhaseEvidence } = await load();
    for (const phase of ['eoa', 'contract', 'negative']) {
        const { report, config, proofsRaw } = await fixture(phase);
        const result = validatePhaseEvidence(report, proofsRaw, config, 0, ethers);
        assert.equal(result.phaseVerified, true); assert.equal(result.completeAcceptance, false);
        assert.throws(() => validatePhaseEvidence(report, proofsRaw, config, 1, ethers));
    }
});

test('missing, failed, skipped, foreign and simulated reports cannot pass', async () => {
    const { validatePhaseEvidence } = await load(); const good = await fixture();
    for (const change of [r => r.checks.pop(), r => { r.checks[0].ok = 'SKIP'; }, r => r.checks.push({ ...r.checks[0] }), r => { r.candidateSHA = 'b'.repeat(40); },
        r => { r.mode = 'api-gateway'; }, r => { r.testAdapter = 'INJECTED_PROVIDER'; }, r => { r.testAdapter = 'CUA_SCOPED_READ_ONLY'; },
        r => { r.errors.push('late page error'); }, r => { r.publicWalletSignoff = true; }]) {
        const bad = structuredClone(good.report); change(bad);
        assert.throws(() => validatePhaseEvidence(bad, good.proofsRaw, good.config, 0, ethers));
    }
});

test('self-consistent proof tamper cannot manufacture controller A signature or origin', async () => {
    const { validatePhaseEvidence } = await load();
    for (const kind of ['wrong-signer', 'wrong-origin', 'expired']) {
        const { report, config, proofsRaw } = await fixture(); const proofs = JSON.parse(proofsRaw), proof = proofs[0];
        if (kind === 'wrong-origin') proof.challenge.message = proof.challenge.message.replaceAll('localhost:18500', 'evil.example');
        if (kind === 'expired') { proof.challenge.expires_at = new Date(now - 1).toISOString(); proof.challenge.message = proof.challenge.message.replace(/Expiration Time: .*/, 'Expiration Time: ' + proof.challenge.expires_at); }
        const actualSigner = kind === 'wrong-signer' ? new ethers.Wallet('0x' + '22'.repeat(32)) : signer;
        proof.signature = await actualSigner.signMessage(proof.challenge.message);
        report.admissions[0].signatureSHA256 = hash(proof.signature); report.admissions[0].messageSHA256 = hash(proof.challenge.message);
        report.challenges[0].messageSHA256 = report.admissions[0].messageSHA256;
        const raw = JSON.stringify(proofs); report.privateProofSHA256 = hash(raw);
        assert.throws(() => validatePhaseEvidence(report, raw, config, 0, ethers), kind);
    }
});

test('contract playback rejects expired identities, changed sessions, paused video or missing real response bytes', async () => {
    const { validatePhaseEvidence } = await load(); const good = await fixture();
    for (const change of [r => { r.samples[10].claims.exp = 1; }, r => { r.samples[10].claims.session_id = 'cw:other'; }, r => { r.samples[10].claims.wallet_session_exp += 1; },
        r => { r.samples[10].paused = true; }, r => { r.samples.at(-1).time = 1; }, r => { r.media[0].size = 0; }, r => { r.media = []; },
        r => { for (const sample of r.samples) sample.tokenHash = r.samples[0].tokenHash; }, r => { for (const media of r.media) media.tokenHash = r.media[0].tokenHash; },
        r => { r.samples = [r.samples[0], r.samples.at(-1)]; }]) {
        const bad = structuredClone(good.report); change(bad);
        assert.throws(() => validatePhaseEvidence(bad, good.proofsRaw, good.config, 0, ethers));
    }
});

test('negative phases require fresh challenge, actual wrong-chain/rejection state and no half login', async () => {
    const { validatePhaseEvidence } = await load(); const good = await fixture('negative');
    for (const change of [r => { r.challenges = []; }, r => { r.gates[0].state.wallet.chainId = 11155111; }, r => { r.gates[1].state.authError = null; },
        r => { r.gates[1].state.tokenHash = hash('half-login'); }, r => { r.gates[1].challengeSequenceAfter = r.gates[1].challengeSequenceBefore; }]) {
        const bad = structuredClone(good.report); change(bad);
        assert.throws(() => validatePhaseEvidence(bad, good.proofsRaw, good.config, 0, ethers));
    }
});

test('capability is bound to the access token actually presented, not a later longer refresh', async () => {
    const { validatePhaseEvidence } = await load(); const { report, proofsRaw, config } = await fixture();
    report.issues[0].capabilityClaims.exp = report.issues[1].accessClaims.exp;
    report.media[0].claims.exp = report.issues[0].capabilityClaims.exp;
    assert.throws(() => validatePhaseEvidence(report, proofsRaw, config, 0, ethers), /credential actually presented/);
});

test('actual master/media playlists must issue one same-origin same-content capability', async () => {
    const { capabilityFromManifest } = await import('../h5-demo-acceptance/lib/wallet_sepolia_contract.mjs');
    const { config } = await fixture();
    const prefix = `/api/v1/streaming/${config.content}/`;
    for (const body of [`#EXTM3U\n${prefix}manifest.m3u8?quality=360p&playback_token=cap`, `#EXTM3U\n${prefix}segment/0.ts?playback_token=cap\n${prefix}segment/1.ts?playback_token=cap`,
        `#EXTM3U\n#EXT-X-MEDIA:URI="${prefix}manifest.m3u8?playback_token=cap"`]) assert.equal(capabilityFromManifest(body, config), 'cap');
    for (const body of ['#EXTM3U', `https://evil.example${prefix}segment/0.ts?playback_token=cap`,
        `/api/v1/streaming/foreign/segment/0.ts?playback_token=cap`, `${prefix}segment/0.ts?playback_token=one\n${prefix}segment/1.ts?playback_token=two`]) assert.throws(() => capabilityFromManifest(body, config));
});

test('real CLI consumes original exit/private bytes and never prints a malformed SIWE signature', async () => {
    const good = await fixture(), directory = fs.mkdtempSync(path.join(os.tmpdir(), 'sg-phase-gate-'));
    const script = path.join(__dirname, 'assert_sepolia_browser.mjs');
    const args = [script, '--candidate', good.config.candidateSHA, '--mode', good.config.mode, '--phase', good.config.phase,
        '--controller', good.config.controller, '--subject', good.config.subject, '--nft', N, '--content', good.config.content,
        '--origin', good.config.origin, '--directory', directory, '--exit-file', path.join(directory, 'original.exit')];
    try {
        fs.writeFileSync(path.join(directory, 'original.exit'), '0\n');
        fs.writeFileSync(path.join(directory, 'result.json'), JSON.stringify(good.report));
        fs.writeFileSync(path.join(directory, 'siwe-proofs.private.json'), good.proofsRaw, { mode: 0o600 });
        assert.match(execFileSync(process.execPath, args, { encoding: 'utf8' }), /phase only; full acceptance=false/);
        fs.writeFileSync(path.join(directory, 'original.exit'), '1\n');
        assert.equal(spawnSync(process.execPath, args).status, 1);
        fs.writeFileSync(path.join(directory, 'original.exit'), '0\n');
        const proofs = JSON.parse(good.proofsRaw); const sensitive = 'MALFORMED_SIGNATURE_MUST_NOT_APPEAR_IN_LOGS';
        proofs[0].signature = sensitive; const raw = JSON.stringify(proofs); good.report.privateProofSHA256 = hash(raw);
        fs.writeFileSync(path.join(directory, 'result.json'), JSON.stringify(good.report));
        fs.writeFileSync(path.join(directory, 'siwe-proofs.private.json'), raw);
        const failed = spawnSync(process.execPath, args, { encoding: 'utf8' });
        assert.equal(failed.status, 1); assert.match(failed.stderr, /invalid encoding/); assert(!failed.stderr.includes(sensitive));
    } finally { fs.rmSync(directory, { recursive: true, force: true }); }
});
