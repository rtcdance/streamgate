// Synthetic observation contracts. No real wallet, signature or public-chain
// admission is created or accepted by these tests.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const load = () => import('../h5-demo-acceptance/lib/wallet_public_controls.mjs');
const subject = '0x' + '2'.repeat(40);
// One synthetic session mints one absolute deadline: deriving it per call from
// the wall clock makes consecutive tokens straddle a second boundary and then
// correctly look like different admissions to sameSession.
const issued = Math.floor(Date.now() / 1000);

function token(overrides = {}) {
    const body = { sub: subject, wallet_address: subject, chain_id: 11155111, account_kind: 'erc1271',
        iat: issued, exp: issued + 60, wallet_session_exp: issued + 7200, session_id: 'cw:admitted',
        wallet_proof_id: 'original-proof', auth_epoch: 1, restore_epoch: 'original-restore', jti: 'old', ...overrides };
    return Buffer.from(JSON.stringify({ alg: 'HS256' })).toString('base64url') + '.' + Buffer.from(JSON.stringify(body)).toString('base64url') + '.synthetic';
}

function fixture(next = token({ jti: 'naturally-renewed' }), failure = 'Unauthenticated') {
    const old = token(); let current = old, requests = 0;
    const rpc = { request: async (_method, args) => {
        requests++;
        const code = args.token === old ? failure : 'OK';
        if (args.token === old) current = next;
        const value = { method: 'verify', code, data: code === 'OK' ? { valid: true } : null };
        return { value, bytes: Buffer.from(JSON.stringify(value)), at: Date.now() };
    } };
    const http = async () => ({ status: 200, bytes: Buffer.from(JSON.stringify({ wallet_address: subject })), at: Date.now() });
    return { old, rpc, http, currentToken: async () => current, requests: () => requests };
}

test('one natural H5 renewal between transport probes retries only the same admission', async () => {
    const { PublicControlJournal } = await load(), f = fixture();
    const journal = new PublicControlJournal({ subject, origin: 'http://localhost:18500' }, f.rpc, f.currentToken, f.http);
    const identity = await journal.live();
    assert.equal(identity.http.code, 200); assert.equal(identity.grpc.code, 'OK');
    assert.equal(f.requests(), 2);
    assert.equal(journal.credentialRotations.length, 1);
    assert(Object.values(journal.private.responses).some(raw => JSON.parse(Buffer.from(raw, 'base64')).code === 'Unauthenticated'), 'original failed response must remain in private evidence');
    assert(!JSON.stringify(journal.credentialRotations).includes(f.old), 'audit metadata must not contain the original JWT');
});

test('unchanged, foreign-session or foreign-proof credentials never authorize retry', async () => {
    const { PublicControlJournal } = await load();
    for (const changed of [{}, { session_id: 'cw:replacement' }, { wallet_proof_id: 'different' }, { auth_epoch: 2 },
        { restore_epoch: 'restored' }, { wallet_session_exp: 1 }, { chain_id: 31337 }, { wallet_address: '0x' + '3'.repeat(40) }]) {
        const next = Object.keys(changed).length ? token({ jti: 'new', ...changed }) : token();
        const f = fixture(next), journal = new PublicControlJournal({ subject, origin: 'http://localhost:18500' }, f.rpc, f.currentToken, f.http);
        await assert.rejects(journal.live()); assert.equal(f.requests(), 1);
    }
});

test('dependency failures remain failures even when the stored token changes', async () => {
    const { PublicControlJournal } = await load(), f = fixture(token({ jti: 'new' }), 'Unavailable');
    const journal = new PublicControlJournal({ subject, origin: 'http://localhost:18500' }, f.rpc, f.currentToken, f.http);
    await assert.rejects(journal.live()); assert.equal(f.requests(), 1);
});

test('manifest and media renewal races are distinct from NFT permission denial', async () => {
    const { PublicControlJournal, digest } = await load();
    for (const stage of ['manifest', 'segment']) {
        const old = token(), next = token({ jti: 'renewed' }), content = 'unit-content';
        const capability = token({ jti: 'capability', content_id: content });
        const media = Buffer.from('synthetic-media-bytes'); let current = old;
        const rpc = { request: async (method, args) => {
            let code = 'OK', data;
            if (method === stage && args.token === old) { code = 'Unauthenticated'; current = next; }
            else if (method === 'verify') data = { valid: true };
            else if (method === 'manifest') data = { segments: [{ segment_id: 'one.ts', url: '/one.ts?playback_token=' + capability }] };
            else if (method === 'segment') data = { size: media.length, sha256: digest(media) };
            const value = { method, code, data };
            return { value, bytes: Buffer.from(JSON.stringify(value)), at: Date.now() };
        } };
        const http = async url => ({ status: 200, bytes: url.includes('/profile') ? Buffer.from(JSON.stringify({ wallet_address: subject })) : media, at: Date.now() });
        const journal = new PublicControlJournal({ subject, origin: 'http://localhost:18500', content }, rpc, async () => current, http);
        const first = await journal.live(), baseline = await journal.mediaBaseline(first);
        assert.equal(baseline.media.code, 200); assert.equal(baseline.grpcMedia.code, 'OK');
        assert.equal(journal.private.credentials[baseline.identity.http.credentialSHA256], next);
        assert.equal(journal.credentialRotations.length, 1);
        assert.equal(journal.credentialRotations[0].operation, stage === 'segment' ? 'media' : 'manifest');
    }
});

test('expired observation budgets and repeated rotations never become successful observations', async () => {
    const { PublicControlJournal } = await load(), f = fixture();
    const journal = new PublicControlJournal({ subject, origin: 'http://localhost:18500' }, f.rpc, async () => token({ jti: 'new' }), f.http);
    await assert.rejects(journal.replacement(f.old, Date.now() - 6000));
    await assert.rejects(journal.rotatedBaseline(f.old, Date.now(), 1, 'media', {}));
    assert.equal(f.requests(), 0);
});

test('late oracle completion selects an original timely stop without moving the safe clock', async () => {
    const { playbackForWitness } = await load();
    const safe = 100000, witness = { http: { at: safe + 60000 }, grpc: { at: safe + 60500 } };
    const samples = [{ at: safe - 2000, paused: false, state: 'playing' },
        { at: safe + 61000, paused: true, state: 'authorization-ended' },
        { at: safe + 90000, paused: true, state: 'authorization-ended' }];
    assert.deepEqual(playbackForWitness(samples, witness, safe), samples.slice(0, 2));
    assert.equal(playbackForWitness(samples, { http: { at: safe + 80000 }, grpc: { at: safe + 80000 } }, safe), null);
    assert.equal(playbackForWitness(samples.map(sample => ({ ...sample, state: 'dependency-unavailable' })), witness, safe), null);
});

test('a partial oracle is not consumed before its original process exit', async t => {
    const { readCompletedOracle } = await load();
    const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'sg-oracle-publication-'));
    t.after(() => fs.rm(directory, { recursive: true, force: true }));
    const report = path.join(directory, 'oracle.json'), exit = path.join(directory, 'oracle.exit');
    await fs.writeFile(report, '{');
    assert.equal(await readCompletedOracle(report, exit), null);
    await fs.writeFile(exit, ''); assert.equal(await readCompletedOracle(report, exit), null);
    await fs.writeFile(exit, '2\n'); await assert.rejects(readCompletedOracle(report, exit), error => error.blocked === true);
    await fs.writeFile(exit, '0\n'); await assert.rejects(async () => JSON.parse(await readCompletedOracle(report, exit)), SyntaxError);
    await fs.writeFile(report, JSON.stringify({ original: true }));
    assert.deepEqual(JSON.parse(await readCompletedOracle(report, exit)), { original: true });
    await fs.unlink(report); await assert.rejects(readCompletedOracle(report, exit), { code: 'ENOENT' });
});

test('synthetic session tokens never slide their absolute session deadline', async () => {
    const { claims } = await load();
    // The rotation contract compares wallet_session_exp for exact equality:
    // one admission keeps one absolute deadline across renewals. A fixture that
    // mints it from a per-call wall clock flakes whenever consecutive calls
    // straddle a second boundary (run 42546543088 Go Tests, 2026-11-09:
    // "No bounded same-admission credential rotation was observed").
    const real = Date.now; let tick = 1700000000000;
    Date.now = () => (tick += 1500);
    try {
        const first = claims(token()), second = claims(token());
        assert.equal(second.wallet_session_exp, first.wallet_session_exp,
            'one synthetic session must mint one absolute deadline');
    } finally { Date.now = real; }
});
