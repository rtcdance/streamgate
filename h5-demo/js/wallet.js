const ETH_SIGN_PREFIX = '\x19Ethereum Signed Message:\n';

class WalletService {
    constructor() {
        this.provider = null;
        this.account = null;
        this.chainId = null;
    }

    isMetaMaskInstalled() {
        return typeof window.ethereum !== 'undefined' && 
               window.ethereum.isMetaMask;
    }

    async connect() {
        if (!this.isMetaMaskInstalled()) {
            throw new Error('MetaMask is not installed');
        }

        try {
            const accounts = await window.ethereum.request({
                method: 'eth_requestAccounts',
            });

            if (accounts.length === 0) {
                throw new Error('No accounts found');
            }

            this.account = accounts[0];

            const chainId = await window.ethereum.request({
                method: 'eth_chainId',
            });
            this.chainId = parseInt(chainId, 16);

            this.setupEventListeners();
            
            return {
                address: this.account,
                chainId: this.chainId,
            };
        } catch (error) {
            console.error('Wallet connection error:', error);
            throw error;
        }
    }

    setupEventListeners() {
        window.ethereum.on('accountsChanged', (accounts) => {
            if (accounts.length === 0) {
                this.disconnect();
                window.dispatchEvent(new CustomEvent('wallet:disconnected'));
            } else {
                this.account = accounts[0];
                window.dispatchEvent(new CustomEvent('wallet:accountChanged', {
                    detail: { address: this.account }
                }));
            }
        });

        window.ethereum.on('chainChanged', (chainId) => {
            this.chainId = parseInt(chainId, 16);
            window.dispatchEvent(new CustomEvent('wallet:chainChanged', {
                detail: { chainId: this.chainId }
            }));
        });
    }

    async signMessage(message) {
        if (!this.account) {
            throw new Error('Wallet not connected');
        }

        try {
            const signature = await window.ethereum.request({
                method: 'personal_sign',
                params: [message, this.account],
            });
            return signature;
        } catch (error) {
            console.error('Sign message error:', error);
            throw error;
        }
    }

    async signTypedData(domain, challenge) {
        if (!this.account) {
            throw new Error('Wallet not connected');
        }

        const domainSeparator = this.encodeDomainSeparator(domain);
        const challengeHash = this.hashChallenge(challenge);
        const messageHash = this.hashMessage(
            ETH_SIGN_PREFIX + challenge.message
        );

        const encodedData = this.encodeData(domainSeparator, challengeHash, messageHash);
        const hash = this.hashTypedData(encodedData);

        try {
            const signature = await window.ethereum.request({
                method: 'eth_signTypedData_v4',
                params: [this.account, JSON.stringify({
                    domain: domain,
                    message: challenge.message,
                    primaryType: 'Challenge',
                    types: {
                        Challenge: [
                            { name: 'wallet', type: 'address' },
                            { name: 'nonce', type: 'string' },
                            { name: 'issued_at', type: 'string' },
                            { name: 'expires_at', type: 'string' },
                        ],
                        EIP712Domain: [
                            { name: 'name', type: 'string' },
                            { name: 'version', type: 'string' },
                            { name: 'chainId', type: 'uint256' },
                        ],
                    },
                })],
            });
            return signature;
        } catch (error) {
            console.error('Sign typed data error:', error);
            throw error;
        }
    }

    encodeDomainSeparator(domain) {
        return this.keccak256(JSON.stringify(domain));
    }

    hashChallenge(challenge) {
        return this.keccak256(challenge.nonce + challenge.issued_at);
    }

    hashMessage(message) {
        return this.keccak256(message);
    }

    hashTypedData(encodedData) {
        return this.keccak256(encodedData);
    }

    keccak256(data) {
        if (typeof window.ethereum !== 'undefined') {
            return window.ethereum.request({
                method: 'web3_sha3',
                params: [data],
            }).then(hash => hash);
        }
        return Promise.resolve(data);
    }

    encodeData(domainSep, challengeHash, messageHash) {
        return '0x1901' + domainSep.slice(2) + challengeHash.slice(2) + messageHash.slice(2);
    }

    disconnect() {
        this.account = null;
        this.chainId = null;
    }

    getAddress() {
        return this.account;
    }

    getChainId() {
        return this.chainId;
    }

    isConnected() {
        return this.account !== null;
    }

    formatAddress(address) {
        if (!address) return '';
        return `${address.slice(0, 6)}...${address.slice(-4)}`;
    }
}

const walletService = new WalletService();
