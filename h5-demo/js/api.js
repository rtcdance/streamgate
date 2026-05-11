const DEFAULT_API_BASE = 'http://localhost:29090';
const ACCEPTANCE_BACKEND_PORTS = new Set(['18080', '19090', '28080', '29090']);

function normalizeBaseUrl(url) {
    return url.replace(/\/$/, '');
}

function inferApiBase() {
    const stored = localStorage.getItem('streamgate_api_base');
    if (stored) {
        return normalizeBaseUrl(stored);
    }

    if (window.location.protocol === 'file:') {
        return DEFAULT_API_BASE;
    }

    try {
        const current = new URL(window.location.href);
        const port = current.port || (current.protocol === 'https:' ? '443' : '80');
        if (ACCEPTANCE_BACKEND_PORTS.has(port)) {
            return normalizeBaseUrl(current.origin);
        }
    } catch (error) {
        console.warn('Failed to infer API base from current location:', error);
    }

    return DEFAULT_API_BASE;
}

const API_BASE = inferApiBase();

class APIService {
    constructor(baseUrl = API_BASE) {
        this.baseUrl = normalizeBaseUrl(baseUrl);
        this.authToken = null;
    }

    setBaseUrl(baseUrl) {
        this.baseUrl = normalizeBaseUrl(baseUrl);
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

    async ensureReachable() {
        try {
            return await this.healthCheck();
        } catch (error) {
            if (this.baseUrl !== DEFAULT_API_BASE) {
                const previousBase = this.baseUrl;
                this.baseUrl = DEFAULT_API_BASE;
                try {
                    const result = await this.healthCheck();
                    localStorage.setItem('streamgate_api_base', this.baseUrl);
                    return {
                        ...result,
                        recovered_from: previousBase,
                    };
                } catch (fallbackError) {
                    this.baseUrl = previousBase;
                    throw fallbackError;
                }
            }
            throw error;
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
