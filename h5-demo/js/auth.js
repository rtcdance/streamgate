class AuthService {
    constructor(apiService, walletService) {
        this.api = apiService;
        this.wallet = walletService;
        this.currentChallenge = null;
        this.isAuthenticated = false;
    }

    _getActiveWallet() {
        return demoWallet.isDemoMode ? demoWallet : this.wallet;
    }

    async requestChallenge() {
        const w = this._getActiveWallet();
        const address = w.getAddress();
        if (!address) {
            throw new Error('Wallet not connected');
        }

        // Demo mode uses personal_sign for maximum cross-library compatibility.
        // MetaMask can use EIP-712 typed data signing.
        const signType = demoWallet.isDemoMode ? 'personal_sign' : 'eip712';
        const response = await this.api.getChallenge(address, w.getChainId() || 31337, signType);
        this.currentChallenge = response;
        return response;
    }

async login() {
        if (!this.currentChallenge) {
            throw new Error('No challenge. Call requestChallenge() first.');
        }

        // Use demo wallet if in demo mode, otherwise use MetaMask wallet
        const w = this._getActiveWallet();

        let signature;
        if (this.currentChallenge.signing_type === 'eip712') {
            const typedData = this.buildEIP712TypedData(this.currentChallenge);
            signature = await w.signTypedData(typedData);
        } else {
            // Fallback to personal_sign (EIP-191)
            const message = this.currentChallenge.message;
            signature = await w.signMessage(message);
        }

        const response = await this.api.loginWithChallenge(
            w.getAddress(),
            this.currentChallenge.challenge_id,
            signature,
            this.currentChallenge.chain_id
        );

        if (response.token) {
            this.api.setAuthToken(response.token);
            this.isAuthenticated = true;
        }

        return response;
    }

    // buildEIP712TypedData constructs the EIP-712 typed data structure
    // matching the backend's buildEIP712Challenge in auth_wallet.go.
    buildEIP712TypedData(challenge) {
            const chainId = challenge.chain_id || 31337;

        return {
            domain: {
                name: 'StreamGate',
                version: '1',
                chainId: chainId,
            },
            primaryType: 'Authentication',
            types: {
                EIP712Domain: [
                    { name: 'name', type: 'string' },
                    { name: 'version', type: 'string' },
                    { name: 'chainId', type: 'uint256' },
                ],
                Authentication: [
                    { name: 'wallet', type: 'address' },
                    { name: 'nonce', type: 'string' },
                    { name: 'issuedAt', type: 'string' },
                    { name: 'expiresAt', type: 'string' },
                    { name: 'domain', type: 'string' },
                    { name: 'uri', type: 'string' },
                    { name: 'version', type: 'string' },
                ],
            },
            message: {
                wallet: challenge.wallet,
                nonce: challenge.nonce,
                issuedAt: challenge.issued_at,
                expiresAt: challenge.expires_at,
                domain: 'streamgate.io',
                uri: 'https://streamgate.io/login',
                version: '1',
            },
        };
    }

    async verifyNFT(contractAddress, chainId) {
        const w = this._getActiveWallet();
        const address = w.getAddress();
        if (!address) {
            throw new Error('Wallet not connected');
        }

        return this.api.verifyNFT(address, contractAddress, chainId);
    }

    logout() {
        this.currentChallenge = null;
        this.isAuthenticated = false;
        this.api.clearAuthToken();
    }

    getToken() {
        return this.api.getAuthToken();
    }

    isLoggedIn() {
        if (this.isAuthenticated) return true;
        const token = this.api.getAuthToken();
        if (!token) return false;
        try {
            const payload = JSON.parse(atob(token.split('.')[1]));
            if (payload.exp && payload.exp * 1000 < Date.now()) {
                this.api.clearAuthToken();
                this.isAuthenticated = false;
                return false;
            }
            return true;
        } catch {
            return false;
        }
    }
}
