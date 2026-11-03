const assert = require('node:assert/strict');
const fs = require('node:fs');
const test = require('node:test');
const ethers = require('../../h5-demo/vendor/ethers.umd.min.js');
const load = () => import('../../h5-demo/testnet/fixture.mjs');
const A = '0x' + '1'.repeat(40), B = '0x' + '2'.repeat(40), N = '0x' + '3'.repeat(40), C = '0x' + '4'.repeat(40);
const H = '0x' + '5'.repeat(64), BLOCK = '0x' + '6'.repeat(64), ORIGIN = 'http://localhost:18500';
const config = () => ({ schemaVersion: 1, candidateSHA: 'a'.repeat(40), controller: A, recipient: B, chainId: 11155111,
    transactionOrigin: ORIGIN, ciVerified: { head_sha: 'a'.repeat(40), conclusion: 'success' },
    buildIdentitySHA256: 'b'.repeat(64), maxTransactions: 8, maxFeeWei: '20000000000000000' });
const artifacts = () => ({ nft: { abi: ['function mint(address to)', 'function transferFrom(address from,address to,uint256 tokenId)', 'event Transfer(address indexed from,address indexed to,uint256 indexed tokenId)'],
    bytecode: { object: '0x6001' }, deployedBytecode: { object: '0x6002' } },
wallet: { abi: ['constructor(address owner)', 'function execute(address target,uint256 value,bytes data)'], bytecode: { object: '0x6003' }, deployedBytecode: { object: '0x6004' } } });

async function setup(options = {}) {
    const { Fixture } = await load(); const saved = new Map(); const calls = [];
    const storage = { getItem: key => saved.get(key) || null, setItem: (key, value) => { if (options.diskFailure) throw Error('disk failure'); saved.set(key, value); } };
    let tx;
    const provider = { isMetaMask: true, request: async ({ method, params }) => {
        calls.push(method);
        if (options.methods?.[method]) return options.methods[method](params);
        if (method === 'eth_chainId') return '0xaa36a7';
        if (method === 'eth_accounts' || method === 'eth_requestAccounts') return [A];
        if (method === 'eth_getCode') return params[0].toLowerCase() === A ? '0x' : '0x6002';
        if (method === 'eth_getTransactionCount') return '0x0';
        if (method === 'eth_estimateGas') return '0x30d40';
        if (method === 'eth_maxPriorityFeePerGas') return '0x3b9aca00';
        if (method === 'eth_getBalance') return '0xde0b6b3a7640000';
        if (method === 'eth_getBlockByNumber') return { number: '0x10', hash: BLOCK, baseFeePerGas: '0x3b9aca00' };
        if (method === 'eth_sendTransaction') {
            const intent = JSON.parse(storage.getItem(fixture.key));
            assert.equal(intent.operations.at(-1).status, 'intent', 'must persist BEFORE wallet approval');
            assert.equal(intent.operations.at(-1).transaction.nonce, params[0].nonce);
            if (options.sendError) throw options.sendError;
            tx = { ...params[0], input: params[0].data, hash: H }; return H;
        }
        if (method === 'eth_getTransactionByHash') return options.transaction ? options.transaction({ ...tx }) : tx;
        if (method === 'eth_getTransactionReceipt') return options.receipt || { transactionHash: H, status: '0x1', blockNumber: '0x10', blockHash: BLOCK, contractAddress: N, logs: [] };
        throw Error('unexpected method ' + method);
    } };
    const fixture = new Fixture(config(), artifacts(), provider, storage, ethers, options.origin || ORIGIN);
    return { fixture, calls, storage, saved, options };
}

test('only fixed Sepolia, successful same-C CI and original hard budgets are accepted', async () => {
    const { validateConfig } = await load(); validateConfig(config());
    for (const change of [{ chainId: 31337 }, { maxTransactions: 9 }, { maxFeeWei: '21000000000000000' }, { transactionOrigin: 'https://example.com' },
        { ciVerified: { head_sha: 'b'.repeat(40), conclusion: 'success' } }, { ciVerified: { head_sha: 'a'.repeat(40), conclusion: 'failure' } }]) assert.throws(() => validateConfig({ ...config(), ...change }));
});

test('the external recipient is fixed before deployment and cannot be changed by a request', async () => {
    const { validateConfig } = await load();
    for (const recipient of [undefined, '', A, '0x' + '0'.repeat(40)]) assert.throws(() => validateConfig({ ...config(), recipient }));
    const { fixture, calls } = await setup();
    assert.equal(fixture.ledger.recipient, B);
    await assert.rejects(fixture.submit('deploy-nft', C), /B 与本轮/);
    assert(!calls.includes('eth_sendTransaction'));
});

test('wallet is never asked to send on wrong chain, account, origin, storage failure, insufficient funds or fees', async () => {
    for (const options of [{ origin: 'http://localhost:18501' }, { diskFailure: true },
        { methods: { eth_chainId: () => '0x1' } }, { methods: { eth_accounts: () => [B] } },
        { methods: { eth_getBalance: () => '0x1' } }, { methods: { eth_estimateGas: () => '0x7fffffffffffff' } }]) {
        const { fixture, calls } = await setup(options);
        await assert.rejects(fixture.submit('deploy-nft', B)); assert(!calls.includes('eth_sendTransaction'));
    }
});

test('intent is durable before send and pending operation cannot be duplicated', async () => {
    const { fixture, calls } = await setup();
    const op = await fixture.submit('deploy-nft', B);
    assert.equal(op.status, 'submitted'); assert.equal(op.transaction.value, '0x0'); assert.equal(op.transaction.chainId, '0xaa36a7');
    assert.equal(op.transaction.nonce, '0x0'); assert.equal(op.calldataSHA256.length, 64);
    await assert.rejects(fixture.submit('deploy-nft', B));
    await assert.rejects(fixture.submit('deploy-wallet', B));
    assert.equal(calls.filter(method => method === 'eth_sendTransaction').length, 1);
});

test('ambiguous response reserves fee and nonce, preserves UNKNOWN across reload and never automatically resends', async () => {
    const { fixture, calls } = await setup({ sendError: Error('connection lost after broadcast') });
    await assert.rejects(fixture.submit('deploy-nft', B), /UNKNOWN/);
    fixture.reload(); assert.equal(fixture.ledger.operations[0].status, 'unknown');
    assert(BigInt(fixture.ledger.operations[0].reservedFeeWei) > 0n);
    await assert.rejects(fixture.submit('deploy-nft', B));
    assert.equal(calls.filter(method => method === 'eth_sendTransaction').length, 1);
});

test('explicit user rejection is preserved and a later explicit click can retry with a new intent', async () => {
    const state = await setup({ sendError: Object.assign(Error('rejected'), { code: 4001 }) });
    await assert.rejects(state.fixture.submit('deploy-nft', B), /拒绝/);
    state.options.sendError = null;
    await state.fixture.submit('deploy-nft', B);
    assert.deepEqual(state.fixture.ledger.operations.map(op => op.status), ['rejected', 'submitted']);
    assert.notEqual(state.fixture.ledger.operations[0].operationID, state.fixture.ledger.operations[1].operationID);
});

test('canonical receipt and exact original runtime are required before next operation', async () => {
    const { fixture, calls } = await setup(); await fixture.submit('deploy-nft', B);
    assert.equal((await fixture.reconcile('deploy-nft')).status, 'confirmed');
    assert.equal(fixture.confirmed('deploy-nft').contractAddress, N);
    const planned = fixture.plan('deploy-wallet', B);
    const args = ethers.utils.defaultAbiCoder.decode(['address'], '0x' + planned.data.slice(-64));
    assert.equal(args[0], A);
    assert.equal(calls.filter(method => method === 'eth_sendTransaction').length, 1, 'reconcile is strictly read only');
    assert.equal(fixture.ledger.completeAcceptance, false); assert.equal(fixture.ledger.publicWalletSignoff, false);
});

test('foreign from/to/chain/nonce/calldata/value/gas/fee transactions cannot resolve UNKNOWN', async () => {
    const changes = [{ from: B }, { to: N }, { chainId: '0x1' }, { nonce: '0x1' }, { input: '0xdead' }, { value: '0x1' }, { gas: '0xffffffff' }, { maxFeePerGas: '0xffffffffffff' }];
    for (const change of changes) {
        const { fixture } = await setup({ transaction: tx => ({ ...tx, ...change }) }); await fixture.submit('deploy-nft', B);
        await assert.rejects(fixture.reconcile('deploy-nft'), /链上交易与已保存意图不符/); assert.equal(fixture.confirmed('deploy-nft'), undefined);
    }
});

test('reverted, reorged and mismatched-runtime deployments do not advance', async () => {
    for (const options of [{ receipt: { transactionHash: H, status: '0x0', blockNumber: '0x10', blockHash: BLOCK, contractAddress: N, logs: [] } },
        { receipt: { transactionHash: H, status: '0x1', blockNumber: '0x10', blockHash: H, contractAddress: N, logs: [] } },
        { methods: { eth_getCode: params => params[0] === A ? '0x' : '0xdead' } }]) {
        const { fixture } = await setup(options); await fixture.submit('deploy-nft', B);
        await assert.rejects(fixture.reconcile('deploy-nft'), /revert|canonical|runtime/); assert.equal(fixture.confirmed('deploy-nft'), undefined);
        await assert.rejects(fixture.submit('deploy-wallet', B));
    }
});

test('six-step encodings transfer the right owner/token through C.execute and fixed B', async () => {
    const { fixture } = await setup();
    // Unit-only setup: these records test calldata construction, not chain acceptance.
    fixture.ledger.recipient = B;
    fixture.ledger.operations = [
        { id: 'deploy-nft', status: 'confirmed', contractAddress: N }, { id: 'deploy-wallet', status: 'confirmed', contractAddress: C },
        { id: 'mint-a', status: 'confirmed', tokenId: '7' }, { id: 'transfer-a', status: 'confirmed', tokenId: '7' },
        { id: 'mint-c', status: 'confirmed', tokenId: '8' }];
    const planned = fixture.plan('transfer-c', B); assert.equal(planned.to, C);
    const wallet = new ethers.utils.Interface(artifacts().wallet.abi), nft = new ethers.utils.Interface(artifacts().nft.abi);
    const execution = wallet.decodeFunctionData('execute', planned.data);
    assert.equal(execution.target, N); assert.equal(execution.value.toString(), '0');
    const transfer = nft.decodeFunctionData('transferFrom', execution.data);
    assert.equal(transfer.from, C); assert.equal(transfer.to, B); assert.equal(transfer.tokenId.toString(), '8');
    assert.throws(() => fixture.plan('transfer-c', A)); assert.throws(() => fixture.plan('transfer-c', N));
});

test('build index and artifact bytes must match external CI digest, including self-consistent tamper', async () => {
    const { loadBuild, sha256 } = await load();
    const contracts = artifacts(), index = { schemaVersion: 1, candidateSHA: config().candidateSHA, contracts: {} }, files = {};
    for (const role of ['nft', 'wallet']) {
        const path = 'artifacts/' + config().candidateSHA + '/' + role + '.json';
        files['/fixture-build/' + path] = JSON.stringify(contracts[role]);
        index.contracts[role] = { artifactPath: path, artifactSHA256: await sha256(files['/fixture-build/' + path]),
            creationCodeSHA256: await sha256(Uint8Array.from(Buffer.from(contracts[role].bytecode.object.slice(2), 'hex'))),
            runtimeCodeSHA256: await sha256(Uint8Array.from(Buffer.from(contracts[role].deployedBytecode.object.slice(2), 'hex'))) };
    }
    files['/fixture-build/build-identity.json'] = JSON.stringify(index);
    const settings = { ...config(), buildIdentitySHA256: await sha256(files['/fixture-build/build-identity.json']) };
    const get = async path => ({ ok: !!files[path], text: async () => files[path] });
    assert.deepEqual(await loadBuild(settings, get), contracts);
    const path = '/fixture-build/' + index.contracts.nft.artifactPath;
    files[path] += ' '; await assert.rejects(loadBuild(settings, get), /归档摘要/);
    index.contracts.nft.artifactSHA256 = await sha256(files[path]); files['/fixture-build/build-identity.json'] = JSON.stringify(index);
    await assert.rejects(loadBuild(settings, get), /索引摘要/);
});

test('fixture panel uses existing local vendor only and does not authorize signoff', () => {
    const panel = fs.readFileSync(require.resolve('../../h5-demo/testnet/index.html'), 'utf8');
    assert(panel.includes('../vendor/ethers.umd.min.js')); assert(panel.includes('/faucet/ethereum/sepolia'));
    assert(!/<script[^>]+src="https?:/.test(panel));
});
