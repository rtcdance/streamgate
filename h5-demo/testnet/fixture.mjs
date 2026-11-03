// Demo fixture transactions are always requested through the user's MetaMask.
// Persist the intent before requesting approval; uncertain delivery must never
// turn into a second mint/deployment or a reused nonce.
export const CHAIN = 11155111;
export const OPERATIONS = ['deploy-nft', 'deploy-wallet', 'mint-a', 'transfer-a', 'mint-c', 'transfer-c'];
const HASH = /^0x[0-9a-fA-F]{64}$/;
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const ZERO = '0x' + '0'.repeat(40);
const same = (a, b) => String(a).toLowerCase() === String(b).toLowerCase();
const hex = value => '0x' + BigInt(value).toString(16);
const bytes = value => {
    if (typeof value !== 'string' || !/^0x(?:[0-9a-fA-F]{2})+$/.test(value)) throw Error('缺少完整合约字节码');
    return Uint8Array.from(value.slice(2).match(/../g), pair => parseInt(pair, 16));
};
export async function sha256(value) {
    const digest = await globalThis.crypto.subtle.digest('SHA-256', typeof value === 'string' ? new TextEncoder().encode(value) : value);
    return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
}

export function validateConfig(config) {
    if (config?.schemaVersion !== 1 || !/^[0-9a-f]{40}$/.test(config.candidateSHA) || config.chainId !== CHAIN ||
        !ADDRESS.test(config.controller) || same(config.controller, ZERO) || !ADDRESS.test(config.recipient) ||
        same(config.recipient, ZERO) || same(config.recipient, config.controller) || config.maxTransactions !== 8 || config.maxFeeWei !== '20000000000000000' ||
        config.ciVerified?.head_sha !== config.candidateSHA || config.ciVerified?.conclusion !== 'success' ||
        !/^http:\/\/localhost:[0-9]{1,5}$/.test(config.transactionOrigin) ||
        !/^[0-9a-f]{64}$/.test(config.buildIdentitySHA256)) throw Error('缺少同候选成功 CI 配置，禁止发送交易');
    return config;
}

export async function loadBuild(config, get = fetch) {
    const read = async path => {
        const result = await get(path, { cache: 'no-store' });
        if (!result.ok) throw Error('原始编译归档不可用');
        return result.text();
    };
    const raw = await read('/fixture-build/build-identity.json');
    if (await sha256(raw) !== config.buildIdentitySHA256) throw Error('编译索引摘要不符');
    const index = JSON.parse(raw);
    if (index.candidateSHA !== config.candidateSHA || index.schemaVersion !== 1 || Object.keys(index.contracts || {}).sort().join(',') !== 'nft,wallet') throw Error('编译索引候选或角色不符');
    const result = {};
    for (const role of ['nft', 'wallet']) {
        const identity = index.contracts[role];
        if (!/^artifacts\/[0-9a-f]{40}\/[A-Za-z0-9]+\.json$/.test(identity.artifactPath) || !identity.artifactPath.includes(config.candidateSHA)) throw Error('非法编译归档路径');
        const artifactRaw = await read('/fixture-build/' + identity.artifactPath);
        if (await sha256(artifactRaw) !== identity.artifactSHA256) throw Error('合约归档摘要不符');
        const artifact = JSON.parse(artifactRaw);
        if (await sha256(bytes(artifact.bytecode?.object)) !== identity.creationCodeSHA256 || await sha256(bytes(artifact.deployedBytecode?.object)) !== identity.runtimeCodeSHA256) throw Error('编译字节码摘要不符');
        result[role] = artifact;
    }
    return result;
}

export class Fixture {
    constructor(config, artifacts, ethereum, storage, ethers, origin = globalThis.location?.origin) {
        this.config = validateConfig(config); this.artifacts = artifacts; this.ethereum = ethereum; this.storage = storage; this.ethers = ethers;
        this.origin = origin;
        this.key = 'streamgate.sepolia.fixture.' + config.candidateSHA + '.' + config.controller.toLowerCase();
        this.reload();
    }
    reload() {
        const raw = this.storage.getItem(this.key);
        this.ledger = raw ? JSON.parse(raw) : { schemaVersion: 1, candidateSHA: this.config.candidateSHA, controller: this.config.controller,
            chainId: CHAIN, recipient: this.config.recipient, operations: [], completeAcceptance: false, publicWalletSignoff: false };
        const ledger = this.ledger;
        if (ledger.schemaVersion !== 1 || ledger.candidateSHA !== this.config.candidateSHA || !same(ledger.controller, this.config.controller) || ledger.chainId !== CHAIN ||
            !Array.isArray(ledger.operations) || ledger.operations.length > 128 || new Set(ledger.operations.map(op => op.operationID)).size !== ledger.operations.length ||
            ledger.operations.some(op => !OPERATIONS.includes(op.id) || !['intent', 'unknown', 'submitted', 'confirmed', 'reverted', 'rejected', 'invalid'].includes(op.status) ||
                typeof op.operationID !== 'string' || !/^[0-9]+$/.test(op.reservedFeeWei) || !/^[0-9a-f]{64}$/.test(op.calldataSHA256) || !Number.isFinite(op.createdAt))) throw Error('交易账本损坏，请保留导出并人工核对');
    }
    save() {
        const raw = JSON.stringify(this.ledger);
        this.storage.setItem(this.key, raw);
        if (this.storage.getItem(this.key) !== raw) throw Error('交易意图无法持久保存，禁止继续');
    }
    rpc(method, params = []) { return this.ethereum.request({ method, params }); }
    async account(connect = false) {
        if (!this.ethereum?.isMetaMask) throw Error('请使用 MetaMask');
        const chain = await this.rpc('eth_chainId');
        const accounts = await this.rpc(connect ? 'eth_requestAccounts' : 'eth_accounts');
        if (BigInt(chain) !== BigInt(CHAIN) || !accounts?.length || !same(accounts[0], this.config.controller)) throw Error('MetaMask 必须连接指定 A 地址及 Ethereum Sepolia (11155111)');
        if (await this.rpc('eth_getCode', [this.config.controller, 'safe']) !== '0x') throw Error('A 必须是普通 EOA');
        return accounts[0];
    }
    confirmed(id) { return this.ledger.operations.find(op => op.id === id && op.status === 'confirmed'); }
    plan(id, recipient) {
        if (!same(recipient, this.config.recipient)) throw Error('B 与本轮已确认配置不符，禁止发送交易');
        const position = OPERATIONS.indexOf(id);
        if (position < 0 || OPERATIONS.slice(0, position).some(previous => !this.confirmed(previous))) throw Error('请按六笔交易顺序完成，先核对上一笔 receipt');
        if (this.ledger.operations.some(op => op.id === id && op.status !== 'rejected') || this.ledger.operations.some(op => !['confirmed', 'rejected'].includes(op.status))) throw Error('存在已提交或 UNKNOWN 交易；先对账，禁止重发');
        const A = this.config.controller;
        const N = this.confirmed('deploy-nft')?.contractAddress;
        const C = this.confirmed('deploy-wallet')?.contractAddress;
        if (!ADDRESS.test(recipient) || [ZERO, A, N, C].some(value => value && same(value, recipient))) throw Error('请填写独立接收地址 B，不能等于 A/C/N/零地址');
        if (this.ledger.recipient && !same(this.ledger.recipient, recipient)) throw Error('本轮 B 已固定，不能中途更换');
        const nft = new this.ethers.utils.Interface(this.artifacts.nft.abi);
        const wallet = new this.ethers.utils.Interface(this.artifacts.wallet.abi);
        if (id === 'deploy-nft') return { data: this.artifacts.nft.bytecode.object };
        if (id === 'deploy-wallet') return { data: new this.ethers.ContractFactory(this.artifacts.wallet.abi, this.artifacts.wallet.bytecode.object).getDeployTransaction(A).data };
        if (id === 'mint-a' || id === 'mint-c') return { to: N, data: nft.encodeFunctionData('mint', [id === 'mint-a' ? A : C]) };
        const token = this.confirmed(id === 'transfer-a' ? 'mint-a' : 'mint-c').tokenId;
        const data = nft.encodeFunctionData('transferFrom', [id === 'transfer-a' ? A : C, recipient, token]);
        return id === 'transfer-a' ? { to: N, data } : { to: C, data: wallet.encodeFunctionData('execute', [N, 0, data]) };
    }
    async submit(id, recipient) {
        if (this.origin !== this.config.transactionOrigin) throw Error('六笔交易只在主入口操作，两个模式共用这组资产；请返回 ' + this.config.transactionOrigin + '/testnet/');
        this.reload(); await this.account();
        const transaction = { from: this.config.controller, chainId: hex(CHAIN), value: '0x0', ...this.plan(id, recipient) };
        const nonce = await this.rpc('eth_getTransactionCount', [transaction.from, 'pending']);
        const gas = (BigInt(await this.rpc('eth_estimateGas', [transaction])) * 120n + 99n) / 100n;
        const block = await this.rpc('eth_getBlockByNumber', ['latest', false]);
        const tip = BigInt(await this.rpc('eth_maxPriorityFeePerGas'));
        if (!block?.baseFeePerGas || tip <= 0n) throw Error('当前 EIP-1559 费用不可用');
        const maxFee = 2n * BigInt(block.baseFeePerGas) + tip;
        const reserved = gas * maxFee;
        const previous = this.ledger.operations.filter(op => op.status !== 'rejected');
        const total = previous.reduce((sum, op) => sum + BigInt(op.reservedFeeWei), reserved);
        const balance = BigInt(await this.rpc('eth_getBalance', [transaction.from, 'pending']));
        if (previous.length >= 8 || total * 120n > BigInt(this.config.maxFeeWei) * 100n || balance * 100n < reserved * 120n) throw Error('超出交易数、0.02 ETH 费用预算或余额余量，禁止提交');
        Object.assign(transaction, { nonce, gas: hex(gas), maxFeePerGas: hex(maxFee), maxPriorityFeePerGas: hex(tip) });
        await this.account();
        if (this.ledger.operations.length >= 128) throw Error('本轮操作记录已满，请保留账本并停止');
        const operation = { id, operationID: globalThis.crypto.randomUUID(), status: 'intent', createdAt: Date.now(), transaction, calldataSHA256: await sha256(bytes(transaction.data)), reservedFeeWei: reserved.toString() };
        this.ledger.recipient = recipient; this.ledger.operations.push(operation); this.save();
        try {
            const hash = await this.rpc('eth_sendTransaction', [transaction]);
            if (!HASH.test(hash)) throw Error('钱包未返回有效交易 hash');
            operation.hash = hash; operation.status = 'submitted'; this.save();
            return operation;
        } catch (error) {
            operation.status = error?.code === 4001 ? 'rejected' : 'unknown';
            this.save();
            throw Error(operation.status === 'rejected' ? '用户已拒绝本次交易；账本已保留' : 'UNKNOWN：交易可能已广播。请在 MetaMask 活动中取 hash 对账，禁止重发');
        }
    }
    async reconcile(id, suppliedHash = '') {
        this.reload(); await this.account();
        const op = this.ledger.operations.findLast(item => item.id === id && item.status !== 'rejected');
        if (!op || op.status === 'rejected') throw Error('没有可对账交易');
        const hash = op.hash || suppliedHash;
        if (!HASH.test(hash) || (suppliedHash && !same(hash, suppliedHash))) throw Error('需要该交易的真实 hash');
        const tx = await this.rpc('eth_getTransactionByHash', [hash]);
        if (!tx) throw Error('节点尚未找到交易；保持原状态，不重发');
        const expected = op.transaction;
        if (!same(tx.hash, hash) || !same(tx.from, expected.from) || !same(tx.to || '', expected.to || '') ||
            BigInt(tx.chainId) !== BigInt(CHAIN) || BigInt(tx.nonce) !== BigInt(expected.nonce) || BigInt(tx.value) !== 0n ||
            await sha256(bytes(tx.input)) !== op.calldataSHA256 || BigInt(tx.gas) > BigInt(expected.gas) ||
            BigInt(tx.maxFeePerGas) > BigInt(expected.maxFeePerGas)) throw Error('链上交易与已保存意图不符，禁止采用此 hash');
        op.hash = hash; op.status = 'submitted'; this.save();
        const receipt = await this.rpc('eth_getTransactionReceipt', [hash]);
        if (!receipt) return op;
        const block = await this.rpc('eth_getBlockByNumber', [receipt.blockNumber, false]);
        if (!same(receipt.transactionHash, hash) || !same(block?.hash, receipt.blockHash)) throw Error('receipt 尚未 canonical，保留 pending');
        op.receipt = receipt;
        if (BigInt(receipt.status) !== 1n) { op.status = 'reverted'; this.save(); throw Error('交易已 revert，保留原始 receipt 并停止'); }
        // A receipt alone is insufficient: deployment code and emitted NFT
        // identity must match the exact candidate operation before advancing.
        try {
            if (id.startsWith('deploy-')) {
                const address = receipt.contractAddress;
                const role = id === 'deploy-nft' ? 'nft' : 'wallet';
                if (!ADDRESS.test(address) || [ZERO, this.config.controller, this.confirmed(id === 'deploy-nft' ? 'deploy-wallet' : 'deploy-nft')?.contractAddress].some(value => value && same(value, address))) throw Error('部署地址不符');
                const code = await this.rpc('eth_getCode', [address, receipt.blockNumber]);
                if (!same(code, this.artifacts[role].deployedBytecode.object)) throw Error('部署 runtime 与原编译归档不符');
                op.contractAddress = address;
            } else {
                const N = this.confirmed('deploy-nft').contractAddress;
                const C = this.confirmed('deploy-wallet').contractAddress;
                const nft = new this.ethers.utils.Interface(this.artifacts.nft.abi);
                const from = id.startsWith('mint-') ? ZERO : id === 'transfer-a' ? this.config.controller : C;
                const to = id === 'mint-a' ? this.config.controller : id === 'mint-c' ? C : this.ledger.recipient;
                const logs = receipt.logs.filter(log => same(log.address, N)).flatMap(log => { try { return [nft.parseLog(log)]; } catch { return []; } })
                    .filter(log => log.name === 'Transfer' && same(log.args.from, from) && same(log.args.to, to));
                if (logs.length !== 1) throw Error('NFT Transfer 日志不符');
                op.tokenId = logs[0].args.tokenId.toString();
                if (id.startsWith('transfer-') && op.tokenId !== this.confirmed(id === 'transfer-a' ? 'mint-a' : 'mint-c').tokenId) throw Error('转移了错误的 token');
            }
            const reread = await this.rpc('eth_getBlockByNumber', [receipt.blockNumber, false]);
            if (!same(reread?.hash, receipt.blockHash)) throw Error('对账期间发生 reorg');
            op.status = 'confirmed'; this.save();
        } catch (error) { op.status = 'invalid'; this.save(); throw error; }
        return op;
    }
}
