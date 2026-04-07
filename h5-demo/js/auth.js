class AuthService {
    constructor(apiService, walletService) {
        this.api = apiService;
        this.wallet = walletService;
        this.currentChallenge = null;
        this.isAuthenticated = false;
    }

    async requestChallenge() {
        const address = this.wallet.getAddress();
        if (!address) {
            throw new Error('Wallet not connected');
        }

        const response = await this.api.getChallenge(address, this.wallet.getChainId() || 11155111);
        this.currentChallenge = response;
        return response;
    }

    async login() {
        if (!this.wallet.isConnected()) {
            throw new Error('Wallet not connected');
        }

        if (!this.currentChallenge) {
            await this.requestChallenge();
        }

        const message = this.currentChallenge.message;
        const signature = await this.wallet.signMessage(message);

        const response = await this.api.loginWithChallenge(
            this.wallet.getAddress(),
            this.currentChallenge.challenge_id,
            signature
        );

        if (response.token) {
            this.api.setAuthToken(response.token);
            this.isAuthenticated = true;
        }

        return response;
    }

    async verifyNFT(contractAddress, chainId) {
        const address = this.wallet.getAddress();
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
        return this.isAuthenticated || this.api.getAuthToken() !== null;
    }
}
