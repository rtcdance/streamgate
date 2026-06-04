// Demo Mode — software wallet for local Anvil-based testing without MetaMask.
// Uses ethers.js loaded from CDN and the Anvil default account.

const DEMO_ANVIL_KEY = '0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80';
const DEMO_ANVIL_ADDR = '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266';
const DEMO_ANVIL_RPC  = 'http://localhost:18545';
const DEMO_NFT_ADDR   = '0xDc64a140Aa3E981100a9becA4E685f962f0cF6C9';
const DEMO_NFT_ABI    = [{"type":"constructor","inputs":[],"stateMutability":"payable"},{"type":"function","name":"approve","inputs":[{"name":"to","type":"address","internalType":"address"},{"name":"tokenId","type":"uint256","internalType":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},{"type":"function","name":"balanceOf","inputs":[{"name":"owner","type":"address","internalType":"address"}],"outputs":[{"name":"","type":"uint256","internalType":"uint256"}],"stateMutability":"view"},{"type":"function","name":"getApproved","inputs":[{"name":"tokenId","type":"uint256","internalType":"uint256"}],"outputs":[{"name":"","type":"address","internalType":"address"}],"stateMutability":"view"},{"type":"function","name":"isApprovedForAll","inputs":[{"name":"owner","type":"address","internalType":"address"},{"name":"operator","type":"address","internalType":"address"}],"outputs":[{"name":"","type":"bool","internalType":"bool"}],"stateMutability":"view"},{"type":"function","name":"mint","inputs":[{"name":"to","type":"address","internalType":"address"}],"outputs":[{"name":"","type":"uint256","internalType":"uint256"}],"stateMutability":"nonpayable"},{"type":"function","name":"name","inputs":[],"outputs":[{"name":"","type":"string","internalType":"string"}],"stateMutability":"view"},{"type":"function","name":"ownerOf","inputs":[{"name":"tokenId","type":"uint256","internalType":"uint256"}],"outputs":[{"name":"","type":"address","internalType":"address"}],"stateMutability":"view"},{"type":"function","name":"setApprovalForAll","inputs":[{"name":"operator","type":"address","internalType":"address"},{"name":"approved","type":"bool","internalType":"bool"}],"outputs":[],"stateMutability":"nonpayable"},{"type":"function","name":"setTokenURI","inputs":[{"name":"tokenId","type":"uint256","internalType":"uint256"},{"name":"uri","type":"string","internalType":"string"}],"outputs":[],"stateMutability":"nonpayable"},{"type":"function","name":"supportsInterface","inputs":[{"name":"interfaceId","type":"bytes4","internalType":"bytes4"}],"outputs":[{"name":"","type":"bool","internalType":"bool"}],"stateMutability":"view"},{"type":"function","name":"symbol","inputs":[],"outputs":[{"name":"","type":"string","internalType":"string"}],"stateMutability":"view"},{"type":"function","name":"tokenURI","inputs":[{"name":"tokenId","type":"uint256","internalType":"uint256"}],"outputs":[{"name":"","type":"string","internalType":"string"}],"stateMutability":"view"},{"type":"function","name":"totalSupply","inputs":[],"outputs":[{"name":"","type":"uint256","internalType":"uint256"}],"stateMutability":"view"},{"type":"function","name":"transferFrom","inputs":[{"name":"from","type":"address","internalType":"address"},{"name":"to","type":"address","internalType":"address"},{"name":"tokenId","type":"uint256","internalType":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},{"type":"event","name":"Approval","inputs":[{"name":"owner","type":"address","indexed":true,"internalType":"address"},{"name":"approved","type":"address","indexed":true,"internalType":"address"},{"name":"tokenId","type":"uint256","indexed":true,"internalType":"uint256"}],"anonymous":false},{"type":"event","name":"ApprovalForAll","inputs":[{"name":"owner","type":"address","indexed":true,"internalType":"address"},{"name":"operator","type":"address","indexed":true,"internalType":"address"},{"name":"approved","type":"bool","indexed":false,"internalType":"bool"}],"anonymous":false},{"type":"event","name":"Transfer","inputs":[{"name":"from","type":"address","indexed":true,"internalType":"address"},{"name":"to","type":"address","indexed":true,"internalType":"address"},{"name":"tokenId","type":"uint256","indexed":true,"internalType":"uint256"}],"anonymous":false}];
const DEMO_NFT_BIN   = '0x608060405234801561000f575f80fd5b50600436106100e5575f3560e01c80636352211e1161008857806395d89b411161006357806395d89b41146101db578063a22cb465146101e3578063c87b56dd146101f1578063e985e9c514610204575f80fd5b80636352211e146101a25780636a627842146101b557806370a08231146101c8575f80fd5b8063095ea7b3116100c3578063095ea7b314610151578063162094c41461016657806318160ddd1461017957806323b872dd1461018f575f80fd5b806301ffc9a7146100e957806306fdde0314610111578063081812fc14610126575b5f80fd5b6100fc6100f736600461081b565b610219565b60405190151581526020015b60405180910390f35b610119610285565b6040516101089190610849565b610139610134366004610895565b505f90565b6040516001600160a01b039091168152602001610108565b61016461015f3660046108c7565b610310565b005b610164610174366004610903565b61034f565b6101816103dc565b604051908152602001610108565b61016461019d3660046109b8565b6103f1565b6101396101b0366004610895565b610569565b6101816101c33660046109f1565b6105df565b6101816101d63660046109f1565b61067c565b6101196106fd565b61016461015f366004610a0a565b6101196101ff366004610895565b61070a565b5f610212610212366004610a43565b5f92915050565b5f6301ffc9a760e01b6001600160e01b03198316148061024957506380ac58cd60e01b6001600160e01b03198316145b806102645750635b5e139f60e01b6001600160e01b03198316145b8061027f575063780e9d6360e01b6001600160e01b03198316145b92915050565b5f805461029190610a74565b80601f01602080910402602001604051908101604052809291908181526020018280546102bd90610a74565b80156103085780601f106102df57610100808354040283529160200191610308565b820191905f5260205f20905b8154815290600101906020018083116102eb57829003601f168201915b505050505081565b60405162461bcd60e51b815260206004820152600f60248201526e1b9bdd081a5b5c1b195b595b9d1959608a1b60448201526064015b60405180910390fd5b5f828152600260205260409020546001600160a01b03166103c05760405162461bcd60e51b815260206004820152602560248201527f4552433732313a205552492073657420666f72206e6f6e6578697374656e74206044820152643a37b5b2b760d91b6064820152608401610346565b5f8281526004602052604090206103d78282610af7565b505050565b5f60016005546103ec9190610bcb565b905090565b826001600160a01b031661040482610569565b6001600160a01b0316146104685760405162461bcd60e51b815260206004820152602560248201527f4552433732313a207472616e736665722066726f6d20696e636f72726563742060448201526437bbb732b960d91b6064820152608401610346565b6001600160a01b0382166104be5760405162461bcd60e51b815260206004820181905260248201527f4552433732313a207472616e7366657220746f207a65726f20616464726573736044820152606401610346565b6001600160a01b0383165f9081526003602052604081208054916104e183610bde565b90915550506001600160a01b0382165f90815260036020526040812080549161050983610bf3565b90915550505f8181526002602052604080822080546001600160a01b0319166001600160a01b0386811691821790925591518493918716917fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef91a4505050565b5f818152600260205260408120546001600160a01b03168061027f5760405162461bcd60e51b815260206004820152602960248201527f4552433732313a206f776e657220717565727920666f72206e6f6e657869737460448201526832b73a103a37b5b2b760b91b6064820152608401610346565b600580545f91829190826105f283610bf3565b909155505f81815260026020908152604080832080546001600160a01b0319166001600160a01b03891690811790915583526003909152812080549293509061063a83610bf3565b909155505060405181906001600160a01b038516905f907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef908290a492915050565b5f6001600160a01b0382166106e25760405162461bcd60e51b815260206004820152602660248201527f4552433732313a2062616c616e636520717565727920666f72207a65726f206160448201526564647265737360d01b6064820152608401610346565b506001600160a01b03165f9081526003602052604090205490565b6001805461029190610a74565b5f818152600260205260409020546060906001600160a01b03166107805760405162461bcd60e51b815260206004820152602760248201527f4552433732313a2055524920717565727920666f72206e6f6e6578697374656e6044820152663a103a37b5b2b760c91b6064820152608401610346565b5f828152600460205260409020805461079890610a74565b80601f01602080910402602001604051908101604052809291908181526020018280546107c490610a74565b801561080f5780601f106107e65761010080835404028352916020019161080f565b820191905f5260205f20905b8154815290600101906020018083116107f257829003601f168201915b50505050509050919050565b5f6020828403121561082b575f80fd5b81356001600160e01b031981168114610842575f80fd5b9392505050565b5f602080835283518060208501525f5b8181101561087557858101830151858201604001528201610859565b505f604082860101526040601f19601f8301168501019250505092915050565b5f602082840312156108a5575f80fd5b5035919050565b80356001600160a01b03811681146108c2575f80fd5b919050565b5f80604083850312156108d8575f80fd5b6108e1836108ac565b946020939093013593505050565b634e487b7160e01b5f52604160045260245ffd5b5f8060408385031215610914575f80fd5b82359150602083013567ffffffffffffffff80821115610932575f80fd5b818501915085601f830112610945575f80fd5b813581811115610957576109576108ef565b604051601f8201601f19908116603f0116810190838211818310171561097f5761097f6108ef565b81604052828152886020848701011115610997575f80fd5b826020860160208301375f6020848301015280955050505050509250929050565b5f805f606084860312156109ca575f80fd5b6109d3846108ac565b92506109e1602085016108ac565b9150604084013590509250925092565b5f60208284031215610a01575f80fd5b610842826108ac565b5f8060408385031215610a1b575f80fd5b610a24836108ac565b915060208301358015158114610a38575f80fd5b809150509250929050565b5f8060408385031215610a54575f80fd5b610a5d836108ac565b9150610a6b602084016108ac565b90509250929050565b600181811c90821680610a8857607f821691505b602082108103610aa657634e487b7160e01b5f52602260045260245ffd5b50919050565b601f8211156103d757805f5260205f20601f840160051c81016020851015610ad15750805b601f840160051c820191505b81811015610af0575f8155600101610add565b5050505050565b815167ffffffffffffffff811115610b1157610b116108ef565b610b2581610b1f8454610a74565b84610aac565b602080601f831160018114610b58575f8415610b415750858301515b5f19600386901b1c1916600185901b178555610baf565b5f85815260208120601f198616915b82811015610b8657888601518255948401946001909101908401610b67565b5085821015610ba357878501515f19600388901b60f8161c191681555b505060018460011b0185555b505050505050565b634e487b7160e01b5f52601160045260245ffd5b8181038181111561027f5761027f610bb7565b5f81610bec57610bec610bb7565b505f190190565b5f60018201610c0457610c04610bb7565b506001019056fea2646970667358221220def1fe8a0343ba636ddc6f04c4ef622e758f619921cac40e02ff4756d539718164736f6c63430008180033';

// DemoNFT contract helpers — deployed once per Anvil session
async function ensureAnvilNFT(provider, signer, ownerAddress) {
  const contract = new ethers.Contract(DEMO_NFT_ADDR, DEMO_NFT_ABI, provider);
  const code = await provider.getCode(DEMO_NFT_ADDR);
  // Deploy contract if not on chain yet
  let contractAddr = DEMO_NFT_ADDR;
  if (code === '0x' || code === '0x0') {
    const factory = new ethers.ContractFactory(DEMO_NFT_ABI, DEMO_NFT_BIN, signer);
    const deployed = await factory.deploy();
    await deployed.deployed();
    contractAddr = deployed.address;
    console.log('DemoNFT deployed at', contractAddr);
  }
  // Check balance and mint if needed
  const balance = await contract.balanceOf(ownerAddress);
  if (balance.toNumber() === 0) {
    const c = new ethers.Contract(contractAddr, DEMO_NFT_ABI, signer);
    for (let i = 0; i < 3; i++) {
      const tx = await c.mint(ownerAddress);
      await tx.wait();
    }
    console.log('Minted 3 DemoNFTs to', ownerAddress);
  }
  return contractAddr;
}

class DemoWallet {
    constructor() {
        this._provider = null;
        this._signer  = null;
        this._address = null;
        this._chainId = null;
        this._mode = false; // false = MetaMask, true = Demo
    }

    get isDemoMode() { return this._mode; }
    get address()    { return this._address; }
    get chainId()    { return this._chainId; }

    getAddress() { return this._address; }
    getChainId() { return this._chainId; }

    toggle() {
        this._mode = !this._mode;
        return this._mode;
    }

    setDemoMode(on) {
        this._mode = on;
    }

    async connect() {
        if (!this._mode) return false;
        if (typeof ethers === 'undefined') {
            throw new Error('ethers.js library not loaded — include <script src=\"https://cdn.jsdelivr.net/npm/ethers@5/dist/ethers.min.js\"></script>');
        }
        this._provider = new ethers.providers.JsonRpcProvider(DEMO_ANVIL_RPC);
        this._signer   = new ethers.Wallet(DEMO_ANVIL_KEY, this._provider);
        this._address  = DEMO_ANVIL_ADDR;
        const net = await this._provider.getNetwork();
        this._chainId = typeof net.chainId === 'number' ? net.chainId : net.chainId.toNumber();
        return true;
    }

    disconnect() {
        this._provider = null;
        this._signer = null;
        this._address = null;
        this._chainId = null;
    }

    async signMessage(message) {
        if (!this._signer) throw new Error('Demo wallet not connected');
        return this._signer.signMessage(message);
    }

    async signTypedData(typedData) {
        if (!this._signer) throw new Error('Demo wallet not connected');
        const { EIP712Domain: _domainType, ...types } = typedData.types;
        return this._signer._signTypedData(typedData.domain, types, typedData.message);
    }

    formatAddress() {
        if (!this._address) return '';
        return this._address.slice(0, 6) + '...' + this._address.slice(-4);
    }
}

const demoWallet = new DemoWallet();