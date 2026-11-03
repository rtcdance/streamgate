const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const ethers = require('../../h5-demo/vendor/ethers.umd.min.js');

// The repository ships the browser UMD bundle. Native fetch supplies its Node
// transport without installing a second ethers package or faking chain reads.
function localProvider(rpcURL) {
    const target = new URL(rpcURL);
    if (!['anvil', 'localhost', '127.0.0.1'].includes(target.hostname) || target.protocol !== 'http:') throw new Error('Local Anvil fixture only');
    const provider = new ethers.providers.JsonRpcProvider(rpcURL, 31337);
    let id = 0;
    provider.send = async (method, params) => {
        const current = ++id;
        const response = await fetch(rpcURL, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: current, method, params }), signal: AbortSignal.timeout(5000) });
        if (!response.ok) throw new Error(`Local RPC HTTP ${response.status}`);
        const result = await response.json();
        if (result.id !== current || result.error || !Object.hasOwn(result, 'result')) throw Object.assign(new Error(`Local RPC ${method} rejected`), { code: result.error?.code });
        return result.result;
    };
    return provider;
}

async function prepare(rpcURL, artifactDirectory, output) {
    const target = new URL(rpcURL);
    if (!['anvil', 'localhost', '127.0.0.1'].includes(target.hostname) || target.protocol !== 'http:') throw new Error('Local Anvil fixture only');
    const provider = localProvider(rpcURL);
    if (Number(await provider.send('eth_chainId', [])) !== 31337) throw new Error('Refusing test keys outside chain 31337');
    const demo = fs.readFileSync(path.resolve(__dirname, '../../h5-demo/js/demo.js'), 'utf8');
    const key = demo.match(/const DEMO_ANVIL_KEY\s*=\s*['"](0x[0-9a-fA-F]{64})['"]/)[1];
    const owner = new ethers.Wallet(key, provider);
    const records = {};
    async function deploy(name, args) {
        const file = path.join(artifactDirectory, `${name}.sol`, `${name}.json`);
        const bytes = fs.readFileSync(file); const artifact = JSON.parse(bytes);
        const bytecode = artifact.bytecode.object.replace(/^0x/, '');
        const contract = await new ethers.ContractFactory(artifact.abi, '0x' + bytecode, owner).deploy(...args);
        const receipt = await contract.deployTransaction.wait();
        if (receipt.status !== 1) throw new Error(`${name} deployment failed`);
        const code = await provider.getCode(contract.address, receipt.blockNumber);
        if (code !== artifact.deployedBytecode.object && code !== '0x' + artifact.deployedBytecode.object) throw new Error(`${name} runtime does not match its build artifact`);
        records[name] = { address: contract.address, transactionHash: receipt.transactionHash, blockNumber: receipt.blockNumber, blockHash: receipt.blockHash, runtimeKeccak256: ethers.utils.keccak256(code), artifactSHA256: crypto.createHash('sha256').update(bytes).digest('hex') };
        return contract;
    }
    const nft = await deploy('DemoNFT', []);
    const wallet = await deploy('Study1271Wallet', [owner.address]);
    const mint = await (await nft.mint(wallet.address)).wait();
    if (mint.status !== 1) throw new Error('Fixture mint failed');
    const transfer = mint.events.find((event) => event.event === 'Transfer');
    if (!transfer || transfer.args.to !== wallet.address) throw new Error('Missing exact mint event');
    await provider.send('anvil_mine', ['0x6']);
    const safe = await provider.send('eth_getBlockByNumber', ['safe', false]);
    if (parseInt(safe.number, 16) < mint.blockNumber) throw new Error('Safe head does not contain mint');
    if (!(await nft.balanceOf(owner.address, { blockTag: parseInt(safe.number, 16) })).isZero() || !(await nft.balanceOf(wallet.address, { blockTag: parseInt(safe.number, 16) })).eq(1)) throw new Error('Expected A=0 and C=1 at safe');
    const fixture = { schemaVersion: 1, network: 'ANVIL_LOCAL_ONLY', chainId: 31337, controller: owner.address, contractWallet: wallet.address, nft: nft.address, tokenId: transfer.args.tokenId.toString(), safe: { number: safe.number, hash: safe.hash }, contracts: records, mint: { transactionHash: mint.transactionHash, blockNumber: mint.blockNumber, blockHash: mint.blockHash } };
    fs.writeFileSync(output, JSON.stringify(fixture, null, 2) + '\n');
    console.log(`Prepared local fixture: A=${owner.address} C=${wallet.address} NFT=${nft.address}`);
    return fixture;
}
module.exports = { prepare, localProvider };
if (require.main === module) prepare(process.env.W3_ANVIL_RPC, process.env.W3_CONTRACT_ARTIFACTS || 'contracts/out', process.argv[2]).catch((error) => { console.error(error.message); process.exitCode = 1; });
