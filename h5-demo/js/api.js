const DEFAULT_API_BASE = 'http://localhost:18000';
const ACCEPTANCE_BACKEND_PORTS = new Set(['18080', '18000', '18001', '19090', '28080', '29091']);

function normalizeBaseUrl(url) {
    return url.replace(/\/$/, '');
}

function inferApiBase() {
    if (window.location.protocol === 'file:') {
        const stored = localStorage.getItem('streamgate_api_base');
        return stored ? normalizeBaseUrl(stored) : DEFAULT_API_BASE;
    }

    try {
        const current = new URL(window.location.href);
        const port = current.port || (current.protocol === 'https:' ? '443' : '80');
        if (ACCEPTANCE_BACKEND_PORTS.has(port)) {
            const pageOrigin = normalizeBaseUrl(current.origin);
            const stored = localStorage.getItem('streamgate_api_base');
            if (stored && normalizeBaseUrl(stored) !== pageOrigin) {
                localStorage.setItem('streamgate_api_base', pageOrigin);
                console.log(`Auto-corrected API base: ${stored} → ${pageOrigin}`);
            }
            return pageOrigin;
        }
    } catch (error) {
        console.warn('Failed to infer API base from current location:', error);
    }

    const stored = localStorage.getItem('streamgate_api_base');
    return stored ? normalizeBaseUrl(stored) : DEFAULT_API_BASE;
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
        sessionStorage.setItem('streamgate_token', token);
    }

    getAuthToken() {
        if (!this.authToken) {
            this.authToken = sessionStorage.getItem('streamgate_token');
        }
        return this.authToken;
    }

    clearAuthToken() {
        this.authToken = null;
        sessionStorage.removeItem('streamgate_token');
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
                const errMsg = data?.error || data?.message || `HTTP ${response.status}`;
                const errDetail = data?.details ? ` (${data.details})` : data?.detail ? ` (${data.detail})` : '';
                const err = new Error(errMsg + errDetail);
                err.status = response.status;
                if (response.status === 401) {
                    this.clearAuthToken();
                }
                throw err;
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

    async healthCheck(retries = 3) {
        for (let i = 0; i < retries; i++) {
            try {
                return await this.get('/health');
            } catch (error) {
                if (i < retries - 1 && (error.status === 503 || error.status === 502 || error.status === 207)) {
                    await new Promise(r => setTimeout(r, 2000));
                    continue;
                }
                throw error;
            }
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

    async getChallenge(walletAddress, chainId = 31337, signType = 'eip712') {
        return this.post('/api/v1/auth/challenge', {
            wallet: walletAddress,
            chain_id: chainId,
            sign_type: signType,
        });
    }

    async loginWithChallenge(walletAddress, challengeId, signature, chainId) {
        return this.post('/api/v1/auth/login', {
            wallet: walletAddress,
            challenge_id: challengeId,
            signature: signature,
            chain_id: chainId || 31337,
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

    // --- Upload API ---

    async uploadWholeFile(file, onProgress) {
        const formData = new FormData();
        formData.append('file', file);

        const url = `${this.baseUrl}/api/v1/upload`;
        const headers = {};
        const token = this.getAuthToken();
        if (token) {
            headers['Authorization'] = `Bearer ${token}`;
        }

        return new Promise((resolve, reject) => {
            const xhr = new XMLHttpRequest();
            xhr.open('POST', url);

            Object.entries(headers).forEach(([k, v]) => xhr.setRequestHeader(k, v));

            if (onProgress) {
                xhr.upload.addEventListener('progress', (e) => {
                    if (e.lengthComputable) {
                        onProgress(e.loaded, e.total);
                    }
                });
            }

            xhr.addEventListener('load', () => {
                try {
                    const data = JSON.parse(xhr.responseText);
                    if (xhr.status >= 200 && xhr.status < 300) {
                        resolve(data);
                    } else {
                        reject(new Error(data?.error || data?.message || `HTTP ${xhr.status}`));
                    }
                } catch {
                    reject(new Error(`HTTP ${xhr.status}`));
                }
            });

            xhr.addEventListener('error', () => reject(new Error('Network error')));
            xhr.addEventListener('abort', () => reject(new Error('Upload aborted')));
            xhr.send(formData);
        });
    }

    async initChunkedUpload(filename, totalSize, totalChunks) {
        return this.post('/api/v1/upload/init', {
            filename,
            total_size: totalSize,
            total_chunks: totalChunks,
        });
    }

    async uploadChunk(uploadId, chunkIndex, chunkData) {
        const formData = new FormData();
        formData.append('upload_id', uploadId);
        formData.append('chunk_index', String(chunkIndex));
        formData.append('chunk', chunkData);

        const url = `${this.baseUrl}/api/v1/upload/chunk`;
        const headers = {};
        const token = this.getAuthToken();
        if (token) {
            headers['Authorization'] = `Bearer ${token}`;
        }

        const response = await fetch(url, {
            method: 'POST',
            headers,
            body: formData,
        });

        const data = await response.json().catch(() => null);
        if (!response.ok) {
            throw new Error(data?.error || data?.message || `HTTP ${response.status}`);
        }
        return data;
    }

    async completeChunkedUpload(uploadId, totalChunks) {
        return this.post(`/api/v1/upload/${uploadId}/complete`, {
            total_chunks: totalChunks,
        });
    }

    async getUploadStatus(uploadId) {
        return this.get(`/api/v1/upload/${uploadId}/status`);
    }

    async completeUpload(uploadId) {
        return this.post(`/api/v1/upload/${uploadId}/complete-upload`);
    }

    async getDownloadURL(uploadId, expiryMinutes) {
        const params = {};
        if (expiryMinutes) {
            params.expiry_minutes = expiryMinutes;
        }
        return this.get(`/api/v1/upload/${uploadId}/download-url`, params);
    }

    async listContents(limit = 50, offset = 0) {
        return this.get(`/api/v1/content?limit=${limit}&offset=${offset}`);
    }

    async getContent(contentId) {
        return this.get(`/api/v1/content/${contentId}`);
    }

    async listUploads(limit = 50, offset = 0) {
        return this.get(`/api/v1/upload/list?limit=${limit}&offset=${offset}`);
    }
}

const api = new APIService();
