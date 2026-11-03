const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const root = path.resolve(__dirname, '../..');
const tool = path.join(root, 'scripts/h5-demo-acceptance/wallet-sepolia.mjs');

test('sepolia browser tool is syntactically valid', () => {
    execFileSync(process.execPath, ['--check', tool]);
});

test('sepolia browser tool never wires the local Anvil adapter or demo keys', () => {
    const source = fs.readFileSync(tool, 'utf8');
    assert.ok(source.includes("from './lib/browser_preflight.mjs'"), 'must keep the browser preflight');
    assert.ok(!source.includes('prepare_wallet_anvil'), 'the Anvil adapter must not be imported');
    assert.ok(!source.includes('localProvider'), 'no local EIP-1193 provider may be injected');
    assert.ok(!source.includes('DEMO_ANVIL_KEY'), 'demo keys must never appear in the public tool');
    assert.ok(!source.includes('eth_sendRawTransaction'), 'the tool must not broadcast');
    assert.ok(/31337/.test(source), 'the tool must refuse the local Anvil chain explicitly');
    assert.ok(/headless:\s*false/.test(source), 'the browser must be headed so the user can operate the wallet');
});

test('sepolia browser tool asserts the TN case contract with user gates', () => {
    const source = fs.readFileSync(tool, 'utf8');
    for (const id of ['TN03_SIWE_ORIGIN_BINDING', 'TN03_EOA_SUBJECT', 'TN03_MEDIA_DECODES', 'TN04_SUBJECT_C', 'TN04_SHORT_LEASE', 'TN05_TRANSFER_REVOKES', 'TN06_WRONG_NETWORK_SHOWN', 'TN06_NO_PAGE_ERRORS']) {
        assert.ok(source.includes(`'${id}'`), `missing required check ${id}`);
    }
    assert.ok(source.includes('USER ACTION REQUIRED'), 'user operations must be explicit gates');
    assert.ok(/1800000|30 \* 60|30-minute/.test(source), 'the 30-minute user gate budget from the plan must be present');
    // User-gated outcomes are BLOCKED, never reported as passing.
    assert.ok(source.includes("blocked("), 'gates need an explicit BLOCKED path');
    assert.ok(source.includes("completeAcceptance"), 'the report must carry the acceptance flag');
});
