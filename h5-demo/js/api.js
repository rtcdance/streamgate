const DEFAULT_API_BASE = 'http://localhost:9090';

function inferApiBase() {
    const stored = localStorage.getItem('streamgate_api_base');
    if (stored) {
        return stored.replace(/\/$/, '');
    }

    if (window.location.protocol === 'file:') {
        return DEFAULT_API_BASE;
    }

    const origin = window.location.origin;
    if (/:(8080|9090)$/.test(origin)) {
        return origin;
    }

    return DEFAULT_API_BASE;
}

const API_BASE = inferApiBase();

class APIService {
    constructor(baseUrl = API_BASE) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.authToken = null;
    }

    setBaseUrl(baseUrl) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        localStorage.setItem('streamgate_api_base', this.baseUrl);
    }

    getBaseUrl() {
        return this.baseUrl;
    }

    setAuthToken(token) {
        this.authToken = token;
        localStorage.setItem('streamgate_token', token);
    }

    getAuthToken() {
        if (!this.authToken) {
            this.authToken = localStorage.getItem('streamgate_token');
        }
        return this.authToken;
    }

    clearAuthToken() {
        this.authToken = null;
        localStorage.removeItem('streamgate_token');
    }

    async request(endpoint, options = {}) {
        const url = `${this.baseUrl}${endpoint}`;
        const headers = {
            'Content-Type': 'application/json',
            ...options.headers,
        };

        const token = this.getAuthToken();
        if (token) {
            headers['Authorization'] = `Bearer ${token}`;
        }

        const config = {
            ...options,
            headers,
        };

        try {
            const response = await fetch(url, config);
            const data = await response.json().catch(() => null);

            if (!response.ok) {
                throw new Error(data?.error || data?.message || `HTTP ${response.status}`);
            }

            return data;
        } catch (error) {
            console.error(`API Error [${endpoint}]:`, error);
            throw error;
        }
    }

    async get(endpoint, params = {}) {
        const queryString = new URLSearchParams(params).toString();
        const url = queryString ? `${endpoint}?${queryString}` : endpoint;
        return this.request(url, { method: 'GET' });
    }

    async post(endpoint, data = {}) {
        return this.request(endpoint, {
            method: 'POST',
            body: JSON.stringify(data),
        });
    }

    async put(endpoint, data = {}) {
        return this.request(endpoint, {
            method: 'PUT',
            body: JSON.stringify(data),
        });
    }

    async delete(endpoint) {
        return this.request(endpoint, { method: 'DELETE' });
    }

    async healthCheck() {
        try {
            return await this.get('/health');
        } catch (error) {
            return this.get('/api/v1/health');
        }
    }

    async getChallenge(walletAddress, chainId = 11155111) {
        return this.post('/api/v1/auth/challenge', {
            wallet: walletAddress,
            chain_id: chainId,
        });
    }

    async loginWithChallenge(walletAddress, challengeId, signature) {
        return this.post('/api/v1/auth/login', {
            wallet: walletAddress,
            challenge_id: challengeId,
            signature: signature,
        });
    }

    async verifyNFT(walletAddress, contractAddress, chainId) {
        return this.post('/api/v1/nft/verify', {
            wallet: walletAddress,
            contract: contractAddress,
            chain_id: chainId,
        });
    }

    async getRPCStatus() {
        return this.get('/api/v1/web3/rpc-status');
    }

    async submitTranscode(contentId, inputUrl, profile = '720p', priority = 5) {
        return this.post('/api/v1/transcode/submit', {
            content_id: contentId,
            input_url: inputUrl,
            profile,
            priority,
        });
    }

    async getTranscodeStatus(taskId) {
        return this.get(`/api/v1/transcode/status/${taskId}`);
    }

    async listTranscodeTasks(contentId = '', limit = 20, offset = 0) {
        return this.get('/api/v1/transcode/tasks', {
            content_id: contentId,
            limit,
            offset,
        });
    }

    async getTranscodeProfiles() {
        return this.get('/api/v1/transcode/profiles');
    }
}

const api = new APIService();
