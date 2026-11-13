class AuthService {
    constructor(apiService, walletService) {
        this.api = apiService;
        this.wallet = walletService;
        this.currentChallenge = null;
        this.isAuthenticated = false;
        this.identity = null;
        this.loginSubject = '';
        this.generation = 0;
        this.refreshTimer = null;
        this.renewal = null;
        for (const event of ['wallet:accountChanged', 'wallet:chainChanged', 'wallet:disconnected']) {
            window.addEventListener(event, () => this.logout('wallet-changed'));
        }
        window.addEventListener('pagehide', () => this.stopRenewal());
        window.addEventListener('pageshow', () => this.scheduleRenewal());
    }

    _getActiveWallet() {
        return typeof demoWallet !== 'undefined' && demoWallet.isDemoMode ? demoWallet : this.wallet;
    }

    setLoginSubject(address = '') {
        const value = address.trim();
        if (value && (!/^0x[0-9a-fA-F]{40}$/.test(value) || /^0x0{40}$/i.test(value))) {
            throw new Error('Enter a deployed contract wallet address');
        }
        this.logout('subject-changed');
        this.loginSubject = value;
    }

    sameAddress(a, b) { return typeof a === 'string' && typeof b === 'string' && a.toLowerCase() === b.toLowerCase(); }

    async assertWallet(context) {
        const w = this._getActiveWallet();
        if (!this.sameAddress(w.getAddress(), context.controller) || w.getChainId() !== context.chainId) {
            throw Object.assign(new Error('Wallet account or network changed. Sign in again.'), { code: 'WALLET_CHANGED' });
        }
        if (w === this.wallet) {
            const provider = await w.ensureProvider();
            if (!provider) throw new Error('Wallet provider unavailable');
            const accounts = await provider.request({ method: 'eth_accounts' });
            const chain = Number(await provider.request({ method: 'eth_chainId' }));
            if (!this.sameAddress(accounts[0], context.controller) || chain !== context.chainId) {
                throw Object.assign(new Error('Wallet account or network changed. Sign in again.'), { code: 'WALLET_CHANGED' });
            }
        }
    }

    async requestChallenge() {
        const w = this._getActiveWallet();
        const controller = w.getAddress();
        if (!controller) throw new Error('Wallet not connected');
        const subject = this.loginSubject || controller;
        const isDemo = w !== this.wallet;
        if (isDemo && w.getChainId() !== 31337) throw new Error('Demo keys are restricted to local Anvil');
        const context = { controller, subject, chainId: w.getChainId(), isDemo, generation: this.generation };
        await this.assertWallet(context);
        // Zero asks the server for its profile chain. Never silently sign for
        // the connected wallet's arbitrary network or a hard-coded testnet.
        const response = await this.api.getChallenge(subject, 0, isDemo && !this.loginSubject ? 'personal_sign' : 'siwe');
        if (context.generation !== this.generation) throw new Error('Login cancelled by wallet change');
        if (Number(response.chain_id) !== context.chainId) throw Object.assign(new Error(`Wrong network: backend requires chain ${response.chain_id}`), { code: 'WALLET_WRONG_CHAIN' });
        if (!this.sameAddress(response.wallet || response.address, subject)) throw new Error('Challenge subject mismatch');
        if (!isDemo || this.loginSubject) this.validateSIWEOrigin(response);
        this.currentChallenge = response;
        this.challengeContext = context;
        return response;
    }

    validateSIWEOrigin(challenge) {
        const message = String(challenge.message || '');
        const lines = message.split('\n');
        const domain = `${window.location.host} wants you to sign in with your Ethereum account:`;
        const uri = lines.find((line) => line.startsWith('URI: '));
        if (challenge.signing_type !== 'siwe' || lines[0] !== domain || !uri || new URL(uri.slice(5)).origin !== window.location.origin) {
            throw new Error('SIWE origin does not match this page. Check the backend profile.');
        }
        if (!this.sameAddress(lines[1], challenge.wallet || challenge.address)) throw new Error('SIWE subject mismatch');
        if (!lines.includes(`Chain ID: ${challenge.chain_id}`) || !lines.includes(`Nonce: ${challenge.nonce}`)) throw new Error('SIWE challenge mismatch');
    }

    async login() {
        const challenge = this.currentChallenge;
        const context = this.challengeContext;
        if (!challenge || !context) throw new Error('Request a new challenge first');
        await this.assertWallet(context);
        const w = this._getActiveWallet();
        const signature = challenge.signing_type === 'eip712'
            ? await w.signTypedData(this.buildEIP712TypedData(challenge))
            : await w.signMessage(challenge.message);
        await this.assertWallet(context);
        if (context.generation !== this.generation) throw new Error('Login cancelled by wallet change');
        const response = await this.api.loginWithChallenge(context.subject, challenge.challenge_id, signature, challenge.chain_id);
        if (context.generation !== this.generation) throw new Error('Login cancelled by wallet change');
        this.acceptToken(response, context);
        this.currentChallenge = null;
        this.challengeContext = null;
        return response;
    }

    buildEIP712TypedData(challenge) {
        // The backend sends the exact typed data; rebuilding it with a fixed
        // domain could sign different bytes from the admission challenge.
        const typed = JSON.parse(challenge.message);
        if (Number(typed.domain?.chainId) !== Number(challenge.chain_id) || !this.sameAddress(typed.message?.wallet, challenge.wallet)) throw new Error('Typed challenge mismatch');
        return typed;
    }

    tokenClaims(token) {
        const encoded = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
        return JSON.parse(atob(encoded));
    }

    acceptToken(response, context) {
        if (!response.token) throw new Error('Missing access token');
        const c = this.tokenClaims(response.token);
        const kind = c.account_kind || 'eoa';
        if (!this.sameAddress(c.wallet_address, context.subject) || Number(c.chain_id) !== context.chainId || !c.exp || c.exp * 1000 <= Date.now()) throw new Error('Login identity mismatch');
        if (!this.sameAddress(context.subject, context.controller) && kind !== 'erc1271') throw new Error('Backend did not admit a contract wallet');
        if (!['eoa', 'erc1271'].includes(kind)) throw new Error('Unsupported wallet type');
        // Decoded claims are UI hints only. Every protected request still
        // goes through server JWT/session/NFT verification.
        this.identity = { ...context, kind, expiresAt: c.exp * 1000, absoluteExpiresAt: (c.wallet_session_exp || c.exp) * 1000 };
        this.api.setAuthToken(response.token);
        sessionStorage.setItem('streamgate_identity', JSON.stringify(this.identity));
        this.isAuthenticated = true;
        this.emitState('authenticated');
        this.scheduleRenewal();
    }

    stopRenewal() { window.clearTimeout(this.refreshTimer); this.refreshTimer = null; }

    scheduleRenewal(delayMs) {
        this.stopRenewal();
        if (this.identity?.kind !== 'erc1271') return;
        const remaining = this.identity.expiresAt - Date.now();
        const wait = delayMs ?? Math.min(30000, Math.max(0, Math.floor(remaining / 2)));
        this.refreshTimer = window.setTimeout(() => this.refresh().catch(() => {}), wait);
    }

    async refresh() {
        if (this.renewal) return this.renewal;
        const generation = this.generation;
        const identity = this.identity;
        const token = this.api.getAuthToken();
        if (!identity || !token) throw new Error('Sign in first');
        const operation = (async () => {
            try {
                await this.assertWallet(identity);
                const response = await this.api.refreshToken(token);
                if (generation !== this.generation) throw new Error('Renewal cancelled by wallet change');
                this.acceptToken(response, identity);
                return response;
            } catch (error) {
                if (generation !== this.generation) throw error;
                if (error.code === 'WALLET_CHANGED') this.logout('wallet-changed');
                else if (!error.status || error.status === 503 || error.status === 429) {
                    this.emitState('dependency-unavailable', error);
                    if (identity.expiresAt > Date.now() + 5000) this.scheduleRenewal(5000);
                    else this.logout('lease-expired');
                } else this.logout(error.status === 401 ? 'authorization-ended' : 'renewal-failed');
                throw error;
            }
        })();
        this.renewal = operation;
        try { return await operation; } finally { if (this.renewal === operation) this.renewal = null; }
    }

    async restore() {
        const token = this.api.getAuthToken();
        const saved = sessionStorage.getItem('streamgate_identity');
        if (!token || !saved) { this.logout('signed-out'); return false; }
        try {
            const identity = JSON.parse(saved);
            if (identity.isDemo) {
                if (identity.chainId !== 31337) throw new Error('Invalid demo network');
                demoWallet.setDemoMode(true);
                await demoWallet.connect();
            } else if (!await this.wallet.restore()) throw new Error('Wallet disconnected');
            await this.assertWallet(identity);
            this.loginSubject = this.sameAddress(identity.controller, identity.subject) ? '' : identity.subject;
            this.acceptToken({ token }, { ...identity, generation: this.generation });
            if (this.identity.kind === 'erc1271') await this.refresh();
            return true;
        } catch { this.logout('session-unavailable'); return false; }
    }

    async verifyNFT(contractAddress, chainId) {
        if (!this.isLoggedIn()) throw new Error('Sign in first');
        return this.api.verifyNFT(this.identity.subject, contractAddress, chainId);
    }

    emitState(state, error) {
        window.dispatchEvent(new CustomEvent('auth:state', { detail: { state, identity: this.identity, error: error?.message || '' } }));
    }

    logout(reason = 'signed-out') {
        this.generation++;
        this.stopRenewal();
        this.currentChallenge = null;
        this.challengeContext = null;
        this.identity = null;
        this.renewal = null;
        this.isAuthenticated = false;
        this.api.clearAuthToken();
        sessionStorage.removeItem('streamgate_identity');
        this.emitState(reason);
    }

    getToken() { return this.api.getAuthToken(); }

    isLoggedIn() {
        const valid = Boolean(this.identity && this.api.getAuthToken() && this.identity.expiresAt > Date.now());
        this.isAuthenticated = valid;
        return valid;
    }
}
