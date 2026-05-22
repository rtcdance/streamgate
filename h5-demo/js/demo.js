// Demo Mode — software wallet for local Anvil-based testing without MetaMask.
// Uses ethers.js loaded from CDN and the Anvil default account.

const DEMO_ANVIL_KEY = '0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80';
const DEMO_ANVIL_ADDR = '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266';
const DEMO_ANVIL_RPC  = 'http://localhost:8545';

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
        this._chainId = net.chainId;
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
        const { EIP712Domain, ...types } = typedData.types;
        delete types.EIP712Domain;
        return this._signer._signTypedData(typedData.domain, types, typedData.message);
    }

    formatAddress() {
        if (!this._address) return '';
        return this._address.slice(0, 6) + '...' + this._address.slice(-4);
    }
}

const demoWallet = new DemoWallet();