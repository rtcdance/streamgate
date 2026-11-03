const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const root = path.resolve(__dirname, '../..');
const tool = path.join(root, 'scripts/h5-demo-acceptance/wallet-sepolia.mjs');
const load = () => import('../h5-demo-acceptance/lib/wallet_sepolia_contract.mjs');
const A = '0x' + '1'.repeat(40), C = '0x' + '2'.repeat(40), N = '0x' + '3'.repeat(40);
const env = () => ({ W3_MODE: 'monolith', W3_PHASE: 'contract', W3_CANDIDATE_SHA: 'a'.repeat(40), W3_H5_BASE: 'http://localhost:18000',
    W3_CDP_ENDPOINT: 'http://127.0.0.1:9222', W3_CONTROLLER: A, W3_SUBJECT_C: C, W3_NFT: N, W3_CONTENT_ID: '12345678-1234-1234-1234-123456789abc', W3_REPORT_DIR: '/artifacts' });
function state() {
    return { identity: { controller: A, subject: C, chainId: 11155111, kind: 'erc1271', isDemo: false }, wallet: { metaMask: true, accounts: [A], chainId: 11155111 },
        claims: { sub: C, wallet_address: C, chain_id: 11155111, account_kind: 'erc1271', iat: 1000, exp: 1060, wallet_session_exp: 8200, wallet_proof_id: 'server-proof', session_id: 'cw:server-session' } };
}

test('public observer syntax and read-only browser boundary', () => {
    execFileSync(process.execPath, ['--check', tool]);
    const source = fs.readFileSync(tool, 'utf8');
    assert.match(source, /connectOverCDP/);
    for (const forbidden of ['chromium.launch(', '.newContext(', '.newPage(', '.addInitScript(', 'DEMO_ANVIL_KEY', 'eth_sendTransaction', 'eth_sendRawTransaction', 'personal_sign']) assert(!source.includes(forbidden), forbidden);
    assert.match(source, /publicWalletSignoff: false/);
    assert.match(source, /completeAcceptance: false/);
    assert(!source.includes('TN05_TRANSFER_REVOKES'), 'transfer revocation requires a chain oracle, never a video error');
});

test('configuration rejects remote CDP, wrong chains, missing candidates and unsupported subjects', async () => {
    const { settings } = await load();
    assert.equal(settings(env()).phase, 'contract');
    for (const [key, value] of [['W3_EXPECTED_CHAIN_ID', '31337'], ['W3_EXPECTED_CHAIN_ID', '1'], ['W3_CANDIDATE_SHA', ''],
        ['W3_CDP_ENDPOINT', 'http://remote.example:9222'], ['W3_CDP_ENDPOINT', 'http://user:secret@localhost:9222'],
        ['W3_H5_BASE', 'https://public.example'], ['W3_USER_GATE_TIMEOUT_MS', '1800001'], ['W3_SUBJECT_C', A], ['W3_NFT', ''], ['W3_CONTENT_ID', '']]) {
        assert.throws(() => settings({ ...env(), [key]: value }), key);
    }
});

test('SIWE requires the actual message origin, C address, unique chain and nonce', async () => {
    const { settings, validateChallenge } = await load(); const config = settings(env());
    const good = { wallet: C, chain_id: 11155111, signing_type: 'siwe', nonce: '12345678',
        message: `localhost:18000 wants you to sign in with your Ethereum account:\n${C}\n\nURI: http://localhost:18000/login\nVersion: 1\nChain ID: 11155111\nNonce: 12345678` };
    assert.equal(validateChallenge(good, config), true);
    for (const bad of [{ ...good, signing_type: 'personal_sign' }, { ...good, wallet: A }, { ...good, chain_id: 1 }, { ...good, message: '' },
        { ...good, message: good.message.replaceAll('localhost:18000', 'evil.example') }, { ...good, message: good.message + '\nChain ID: 1' },
        { ...good, message: good.message + '\nNonce: second' }, { ...good, message: good.message.replace(C, A) }, { ...good, nonce: 'wrong' }]) assert.throws(() => validateChallenge(bad, config));
});

test('contract identity needs real provider account and strict 60s metadata', async () => {
    const { settings, validIdentity } = await load(); const config = settings(env());
    assert.equal(validIdentity(state(), config, 1001000), true);
    const mutations = [s => { s.identity.isDemo = true; }, s => { s.identity.subject = A; }, s => { s.wallet.accounts = [C]; },
        s => { s.wallet.chainId = 1; }, s => { s.claims.account_kind = 'eoa'; }, s => { s.claims.exp = 1070; },
        s => { s.claims.wallet_session_exp = 8201; }, s => { delete s.claims.wallet_proof_id; }, s => { s.claims.session_id = 'legacy'; },
        s => { s.claims.sub = A; }, s => { s.claims.exp = 999; }];
    for (const mutate of mutations) { const bad = state(); mutate(bad); assert.equal(validIdentity(bad, config, 1001000), false); }
});

test('EOA and contract subjects are independent phases', async () => {
    const { settings, validIdentity } = await load(); const config = settings({ ...env(), W3_PHASE: 'eoa' });
    const eoa = state(); eoa.identity.subject = A; eoa.identity.kind = 'eoa'; eoa.claims.sub = A; eoa.claims.wallet_address = A; eoa.claims.account_kind = 'eoa'; eoa.claims.exp = 8200;
    assert.equal(validIdentity(eoa, config, 1001000), true);
    assert.equal(validIdentity(state(), config, 1001000), false);
});

test('media capability cannot borrow A, a foreign chain, NFT, content or unlimited lease', async () => {
    const { settings, validCapability } = await load(); const config = settings(env());
    const good = { wallet_address: C, chain_id: 11155111, contract: N, content_id: config.content, token_type: 'playback', account_kind: 'erc1271', iat: 1000, exp: 1060, wallet_session_exp: 8200 };
    assert.equal(validCapability(good, config), true);
    for (const [key, value] of [['wallet_address', A], ['chain_id', 1], ['contract', A], ['content_id', 'other'], ['token_type', 'access'], ['account_kind', 'eoa'], ['exp', 1070], ['wallet_session_exp', 1059]]) assert.equal(validCapability({ ...good, [key]: value }, config), false, key);
});

test('empty, incomplete, skipped, errored and blocked phases never pass', async () => {
    const { PHASE_CHECKS, phaseVerified } = await load();
    for (const phase of Object.keys(PHASE_CHECKS)) {
        const good = { phase, errors: [], blockers: [], checks: PHASE_CHECKS[phase].map(id => ({ id, ok: true })) };
        assert.equal(phaseVerified(good), true);
        for (const mutate of [r => { r.checks = []; }, r => { r.checks.pop(); }, r => { r.checks.push({ ...r.checks[0] }); },
            r => { r.checks[0].ok = 'SKIP'; }, r => { r.errors.push('late page error'); }, r => { r.blockers.push('user timeout'); }]) {
            const bad = structuredClone(good); mutate(bad); assert.equal(phaseVerified(bad), false);
        }
    }
});
