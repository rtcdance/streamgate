const ETH_SIGN_PREFIX = '\x19Ethereum Signed Message:\n';

class WalletService {
    constructor() {
        this.provider = null;
        this.account = null;
        this.chainId = null;
        this.eventsBound = false;
        this.discoveredProviders = new Map();
    }

    registerDiscoveredProvider(provider, info = {}) {
        if (!provider) {
            return;
        }

        const id = info.uuid || info.rdns || info.name || `provider-${this.discoveredProviders.size + 1}`;
        this.discoveredProviders.set(id, {
            provider,
            info: {
                name: info.name || (provider.isMetaMask ? 'MetaMask' : 'Injected Wallet'),
                rdns: info.rdns || '',
                uuid: info.uuid || '',
                isMetaMask: Boolean(provider.isMetaMask),
            },
        });
    }

    getAvailableProviders() {
        return Array.from(this.discoveredProviders.values());
    }

    getProviderDiagnostics() {
        const discovered = this.getAvailableProviders().map((entry) => ({
            name: entry.info.name || 'Injected Wallet',
            rdns: entry.info.rdns || '',
            isMetaMask: Boolean(entry.info.isMetaMask),
        }));

        return {
            hasWindowEthereum: typeof window !== 'undefined' && typeof window.ethereum !== 'undefined',
            isFileProtocol: typeof window !== 'undefined' && window.location.protocol === 'file:',
            discovered,
        };
    }

    getInjectedProvider() {
        if (typeof window === 'undefined') {
            return null;
        }

        const injected = window.ethereum;
        if (!injected) {
            return null;
        }

        if (Array.isArray(injected.providers) && injected.providers.length > 0) {
            injected.providers.forEach((provider) => this.registerDiscoveredProvider(provider));
            const metaMaskProvider = injected.providers.find((provider) => provider && provider.isMetaMask);
            if (metaMaskProvider) {
                return metaMaskProvider;
            }
        }

        if (injected.isMetaMask) {
            this.registerDiscoveredProvider(injected, { name: 'MetaMask', rdns: 'io.metamask' });
            return injected;
        }

        return null;
    }

    async waitForMetaMask(timeoutMs = 1500) {
        const existing = this.getInjectedProvider();
        if (existing) {
            this.provider = existing;
            return existing;
        }

        return new Promise((resolve) => {
            let settled = false;
            const deadline = Date.now() + timeoutMs;
            let requestedProviders = false;

            const finish = (provider) => {
                if (settled) {
                    return;
                }
                settled = true;
                window.removeEventListener('ethereum#initialized', onInitialized);
                window.removeEventListener('eip6963:announceProvider', onAnnounceProvider);
                resolve(provider || null);
            };

            const getPreferredProvider = () => {
                const injectedProvider = this.getInjectedProvider();
                if (injectedProvider) {
                    return injectedProvider;
                }

                for (const candidate of this.discoveredProviders.values()) {
                    if (candidate.info.isMetaMask) {
                        return candidate.provider;
                    }
                }

                const firstDetected = this.discoveredProviders.values().next();
                if (!firstDetected.done) {
                    return firstDetected.value.provider;
                }

                return null;
            };

            const poll = () => {
                const provider = getPreferredProvider();
                if (provider) {
                    this.provider = provider;
                    finish(provider);
                    return;
                }
                if (Date.now() >= deadline) {
                    finish(null);
                    return;
                }
                window.setTimeout(poll, 50);
            };

            const onInitialized = () => {
                const provider = getPreferredProvider();
                if (provider) {
                    this.provider = provider;
                    finish(provider);
                }
            };

            const onAnnounceProvider = (event) => {
                const detail = event.detail || {};
                this.registerDiscoveredProvider(detail.provider, detail.info || {});
                const provider = getPreferredProvider();
                if (provider) {
                    this.provider = provider;
                    finish(provider);
                }
            };

            window.addEventListener('ethereum#initialized', onInitialized, { once: true });
            window.addEventListener('eip6963:announceProvider', onAnnounceProvider);

            if (!requestedProviders) {
                requestedProviders = true;
                window.dispatchEvent(new Event('eip6963:requestProvider'));
            }
            poll();
        });
    }

    async ensureProvider() {
        if (this.provider && this.provider.isMetaMask) {
            return this.provider;
        }

        const provider = await this.waitForMetaMask();
        if (provider) {
            this.provider = provider;
            return provider;
        }

        return null;
    }

    async isMetaMaskInstalled() {
        const provider = await this.ensureProvider();
        return provider !== null;
    }

    buildProviderError() {
        const diagnostics = this.getAvailableProviders().map((entry) => {
            const name = entry.info.name || 'unknown';
            const rdns = entry.info.rdns || 'unknown-rdns';
            return `${name} (${rdns})`;
        });

        const hints = ['No usable wallet provider was injected into this page.'];

        if (window.location.protocol === 'file:') {
            hints.push('If you opened the demo from a file, enable MetaMask access to file URLs or serve h5-demo over http.');
        }

        if (diagnostics.length > 0) {
            hints.push(`Detected providers: ${diagnostics.join(', ')}.`);
        } else {
            hints.push('No injected EIP-1193 providers were detected.');
        }

        hints.push('If multiple wallets are installed, make sure the intended wallet extension is enabled for this site.');
        return new Error(hints.join(' '));
    }

    async connect() {
        const provider = await this.ensureProvider();
        if (!provider) {
            throw this.buildProviderError();
        }

        try {
            const accounts = await provider.request({
                method: 'eth_requestAccounts',
            });

            if (accounts.length === 0) {
                throw new Error('No accounts found');
            }

            this.account = accounts[0];

            const chainId = await provider.request({
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
        if (!this.provider || this.eventsBound) {
            return;
        }

        this.provider.on('accountsChanged', (accounts) => {
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

        this.provider.on('chainChanged', (chainId) => {
            this.chainId = parseInt(chainId, 16);
            window.dispatchEvent(new CustomEvent('wallet:chainChanged', {
                detail: { chainId: this.chainId }
            }));
        });

        this.eventsBound = true;
    }

    async signMessage(message) {
        if (!this.account) {
            throw new Error('Wallet not connected');
        }

        try {
            const provider = await this.ensureProvider();
            if (!provider) {
                throw this.buildProviderError();
            }

            const signature = await provider.request({
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
            const provider = await this.ensureProvider();
            if (!provider) {
                throw this.buildProviderError();
            }

            const signature = await provider.request({
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
        if (this.provider) {
            return this.provider.request({
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
