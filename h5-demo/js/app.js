class StreamGateApp {
    constructor() {
        this.api = new APIService();
        this.wallet = walletService;
        this.auth = new AuthService(this.api, this.wallet);
        this.player = null;
        this.isPlaying = false;
        this.lastTranscodeTaskId = null;
        this.stepStates = {
            backend: 'pending',
            wallet: 'pending',
            auth: 'pending',
            nft: 'pending',
            playback: 'pending',
            rpc: 'pending',
            transcode: 'pending',
        };
    }

    async init() {
        this.initBackendConfig();
        this.initPlayer();
        this.bindEvents();
        this.refreshAcceptanceSummary();
        await this.checkBackend();
        this.restoreSession();
    }

    initBackendConfig() {
        const backendInput = document.getElementById('backend-url');
        backendInput.value = this.api.getBaseUrl();
    }

    initPlayer() {
        const videoElement = document.getElementById('video-player');
        this.player = new HLSPlayer(videoElement);
        this.player.init();
    }

    bindEvents() {
        document.getElementById('connect-wallet').addEventListener('click', () => this.connectWallet());
        document.getElementById('login-btn').addEventListener('click', () => this.doLogin());
        document.getElementById('verify-nft').addEventListener('click', () => this.verifyNFT());
        document.getElementById('play-video').addEventListener('click', () => this.playVideo());
        document.getElementById('save-backend').addEventListener('click', () => this.saveBackendUrl());
        document.getElementById('check-rpc').addEventListener('click', () => this.loadRPCStatus());
        document.getElementById('submit-transcode').addEventListener('click', () => this.submitTranscode());
        document.getElementById('load-transcode-status').addEventListener('click', () => this.loadTranscodeStatus());
        document.getElementById('load-transcode-tasks').addEventListener('click', () => this.loadTranscodeTasks());
        document.getElementById('load-transcode-profiles').addEventListener('click', () => this.loadTranscodeProfiles());
        document.getElementById('test-health').addEventListener('click', () => this.testHealth());
        document.getElementById('test-challenge').addEventListener('click', () => this.testChallenge());
        document.querySelectorAll('.demo-item').forEach((item) => {
            item.addEventListener('click', () => {
                document.getElementById('video-id').value = item.dataset.id;
                this.showToast(`Selected demo video: ${item.dataset.id}`, 'info');
            });
        });

        window.addEventListener('wallet:accountChanged', (e) => this.onAccountChanged(e));
        window.addEventListener('wallet:chainChanged', (e) => this.onChainChanged(e));
    }

    async checkBackend() {
        try {
            await this.api.healthCheck();
            this.updateStatus('backend', 'online', 'Online');
            this.updateStep('backend', 'done');
            this.setTroubleshooting(
                'Backend connected',
                'Good start. Next connect MetaMask so the page can request a real challenge from the backend.'
            );
        } catch (error) {
            this.updateStatus('backend', 'offline', 'Offline');
            this.updateStep('backend', 'failed');
            this.setTroubleshooting(
                'Backend unreachable',
                'Confirm the gateway is running on the expected port, usually http://localhost:9090, and check browser CORS/network errors.'
            );
            this.showToast('Backend unreachable', 'error');
        }
    }

    restoreSession() {
        const token = this.api.getAuthToken();
        if (token) {
            this.auth.isAuthenticated = true;
            this.updateStatus('auth', 'online', 'Authenticated');
            this.updateStep('auth', 'done');
        }
    }

    async saveBackendUrl() {
        const value = document.getElementById('backend-url').value.trim();
        if (!value) {
            this.showToast('Backend URL is required', 'error');
            return;
        }
        this.api.setBaseUrl(value);
        await this.checkBackend();
        this.showToast('Backend URL updated', 'success');
    }

    async connectWallet() {
        try {
            this.showLoading(true);
            const result = await this.wallet.connect();
            this.updateWalletUI(result.address);
            this.updateStatus('wallet', 'online', 'Connected');
            this.updateStep('wallet', 'done');
            this.setTroubleshooting(
                'Wallet connected',
                'Great. The next step is signing the backend challenge to get a JWT.'
            );
            this.showToast('Wallet connected', 'success');
            document.getElementById('login-section').classList.remove('hidden');
            document.getElementById('nft-section').classList.remove('hidden');
        } catch (error) {
            this.setTroubleshooting(
                'Wallet connection failed',
                'Check whether MetaMask is installed, unlocked, and allowed to connect to this page.'
            );
            this.showToast(error.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

    updateWalletUI(address) {
        const formatted = this.wallet.formatAddress(address);
        document.getElementById('address-value').textContent = formatted;
        document.getElementById('wallet-address').classList.remove('hidden');
        document.getElementById('wallet-status').innerHTML = 
            '<span class="status-dot online"></span><span>Connected</span>';
    }

    async doLogin() {
        try {
            this.showLoading(true);
            await this.auth.requestChallenge();
            const result = await this.auth.login();
            
            document.getElementById('login-result').classList.remove('hidden');
            document.getElementById('login-result').innerHTML = 
                `<span class="success">✓ Login successful! Token: ${result.token?.slice(0, 20)}...</span>`;
            this.updateStatus('auth', 'online', 'Authenticated');
            this.updateStep('auth', 'done');
            this.setTroubleshooting(
                'Login succeeded',
                'JWT is ready. Now verify NFT ownership against the configured contract and chain.'
            );
            this.showToast('Login successful', 'success');
        } catch (error) {
            document.getElementById('login-result').classList.remove('hidden');
            document.getElementById('login-result').innerHTML = 
                `<span class="error">✗ ${error.message}</span>`;
            this.updateStep('auth', 'failed');
            this.setTroubleshooting(
                'Login failed',
                'Confirm the wallet signs the exact backend challenge message and that `/api/v1/auth/login` receives `wallet`, `challenge_id`, and `signature`.'
            );
            this.showToast(error.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

    async verifyNFT() {
        const contract = document.getElementById('nft-contract').value;
        const chainId = parseInt(document.getElementById('chain-select').value);

        try {
            this.showLoading(true);
            const result = await this.auth.verifyNFT(contract, chainId);
            
            const resultDiv = document.getElementById('nft-result');
            resultDiv.classList.remove('hidden');
            
            const statusText = document.getElementById('nft-status-text');
            const icon = resultDiv.querySelector('.nft-icon');
            
            if (result.has_nft) {
                statusText.textContent = '✓ NFT Verified - You can watch!';
                icon.textContent = '🎉';
                this.updateStatus('nft', 'online', 'Verified');
                this.updateStep('nft', 'done');
                this.setTroubleshooting(
                    'NFT verified',
                    'Ownership passed. You can now validate protected manifest loading and playback.'
                );
                document.getElementById('player-section').classList.remove('hidden');
            } else {
                statusText.textContent = '✗ No NFT found';
                icon.textContent = '❌';
                this.updateStatus('nft', 'offline', 'Not Verified');
                this.updateStep('nft', 'failed');
                this.setTroubleshooting(
                    'NFT not found',
                    'Check the connected wallet, contract address, and chain ID. Full acceptance needs a wallet that actually owns the NFT.'
                );
            }

            document.getElementById('nft-details').innerHTML = `
                <p>Balance: ${result.balance}</p>
                <p>Chain ID: ${result.chain_id}</p>
                <p>Cache: ${result.cache_hit ? 'Yes' : 'No'}</p>
            `;
            
            this.showToast(result.has_nft ? 'NFT verified!' : 'No NFT found', 
                result.has_nft ? 'success' : 'warning');
        } catch (error) {
            this.updateStep('nft', 'failed');
            this.setTroubleshooting(
                'NFT verification failed',
                'Check RPC availability, contract address, chain selection, and whether the backend can reach the configured chain client.'
            );
            this.showToast(error.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

    async playVideo() {
        const videoId = document.getElementById('video-id').value || 'demo';
        const contract = document.getElementById('nft-contract').value;
        const chainId = parseInt(document.getElementById('chain-select').value, 10);
        
        try {
            this.showLoading(true);
            
            const token = this.auth.getToken();
            await this.player.loadVideo(videoId, {
                token,
                contract,
                chainId,
                baseUrl: this.api.getBaseUrl(),
            });
            
            document.getElementById('player-status').textContent = 'Playing...';
            this.isPlaying = true;
            this.updateStatus('streaming', 'online', 'Manifest OK');
            this.updateStep('playback', 'done');
            this.setTroubleshooting(
                'Playback path is working',
                'Manifest access succeeded with Bearer JWT. You can now inspect RPC status or continue with transcoding acceptance.'
            );
            this.showToast('Video loading...', 'info');
        } catch (error) {
            this.updateStatus('streaming', 'offline', 'Load Failed');
            this.updateStep('playback', 'failed');
            this.setTroubleshooting(
                'Playback failed',
                'Confirm login and NFT verification already passed, and make sure the selected video ID exists on the backend streaming route.'
            );
            this.showToast('Failed to load video: ' + error.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

    async testHealth() {
        try {
            const result = await this.api.healthCheck();
            this.showTestOutput('Health Check:', JSON.stringify(result, null, 2));
        } catch (error) {
            this.showTestOutput('Health Check Failed:', error.message);
        }
    }

    async testChallenge() {
        if (!this.wallet.isConnected()) {
            this.showTestOutput('Challenge Test:', 'Wallet not connected');
            return;
        }

        try {
            const challenge = await this.auth.requestChallenge();
            this.showTestOutput('Challenge:', JSON.stringify(challenge, null, 2));
        } catch (error) {
            this.showTestOutput('Challenge Failed:', error.message);
        }
    }

    async loadRPCStatus() {
        try {
            const result = await this.api.getRPCStatus();
            this.updateStatus('rpc', 'online', 'Visible');
            this.updateStep('rpc', 'done');
            this.setTroubleshooting(
                'RPC status loaded',
                'You can now verify active endpoint, failed endpoints, and cooldown behavior directly from the gateway.'
            );
            this.showOutput('rpc-output', 'RPC Status', JSON.stringify(result, null, 2));
        } catch (error) {
            this.updateStatus('rpc', 'offline', 'Unavailable');
            this.updateStep('rpc', 'failed');
            this.setTroubleshooting(
                'RPC status unavailable',
                'Confirm `/api/v1/web3/rpc-status` is registered and the backend has initialized the chain clients.'
            );
            this.showOutput('rpc-output', 'RPC Status Failed', error.message);
        }
    }

    async submitTranscode() {
        try {
            const contentId = document.getElementById('transcode-content-id').value.trim();
            const inputUrl = document.getElementById('transcode-input-url').value.trim();
            const profile = document.getElementById('transcode-profile').value.trim();
            const priority = parseInt(document.getElementById('transcode-priority').value, 10) || 5;
            const result = await this.api.submitTranscode(contentId, inputUrl, profile, priority);
            this.lastTranscodeTaskId = result.task_id;
            document.getElementById('transcode-task-id').value = result.task_id || '';
            this.updateStatus('transcode', 'online', 'Task Submitted');
            this.updateStep('transcode', 'done');
            this.setTroubleshooting(
                'Transcode task submitted',
                'Next, load task status and task list to confirm the control plane path is working.'
            );
            this.showOutput('transcode-output', 'Transcode Submit', JSON.stringify(result, null, 2));
        } catch (error) {
            this.updateStatus('transcode', 'offline', 'Submit Failed');
            this.updateStep('transcode', 'failed');
            this.setTroubleshooting(
                'Transcode submit failed',
                'Check that the gateway exposes `/api/v1/transcode/submit` and that the payload uses `content_id`, `input_url`, `profile`, and `priority`.'
            );
            this.showOutput('transcode-output', 'Transcode Submit Failed', error.message);
        }
    }

    async loadTranscodeStatus() {
        try {
            const taskId = document.getElementById('transcode-task-id').value.trim() || this.lastTranscodeTaskId;
            if (!taskId) {
                throw new Error('No task ID available yet');
            }
            const result = await this.api.getTranscodeStatus(taskId);
            this.updateStatus('transcode', 'online', 'Status Loaded');
            this.showOutput('transcode-output', 'Transcode Status', JSON.stringify(result, null, 2));
        } catch (error) {
            this.showOutput('transcode-output', 'Load Status Failed', error.message);
        }
    }

    async loadTranscodeTasks() {
        try {
            const contentId = document.getElementById('transcode-content-id').value.trim();
            const result = await this.api.listTranscodeTasks(contentId, 20, 0);
            this.updateStatus('transcode', 'online', 'Tasks Loaded');
            this.showOutput('transcode-output', 'Transcode Tasks', JSON.stringify(result, null, 2));
        } catch (error) {
            this.showOutput('transcode-output', 'Load Tasks Failed', error.message);
        }
    }

    async loadTranscodeProfiles() {
        try {
            const result = await this.api.getTranscodeProfiles();
            this.updateStatus('transcode', 'online', 'Profiles Loaded');
            this.showOutput('transcode-output', 'Transcode Profiles', JSON.stringify(result, null, 2));
        } catch (error) {
            this.showOutput('transcode-output', 'Load Profiles Failed', error.message);
        }
    }

    showTestOutput(title, content) {
        this.showOutput('test-output', title, content);
    }

    showOutput(elementId, title, content) {
        const output = document.getElementById(elementId);
        output.classList.remove('hidden');
        output.textContent = title + '\n' + content;
    }

    updateStatus(type, status, text) {
        const statusEl = document.getElementById(`${type}-status`) || document.getElementById(`${type}-status-global`);
        if (statusEl) {
            statusEl.textContent = text;
            statusEl.className = `status-value ${status}`;
        }
    }

    updateStep(step, state) {
        const mapping = {
            backend: 'step-backend',
            wallet: 'step-wallet',
            auth: 'step-auth',
            nft: 'step-nft',
            playback: 'step-playback',
            rpc: 'step-rpc',
            transcode: 'step-transcode',
        };
        const el = document.getElementById(mapping[step] || step);
        if (!el) {
            return;
        }
        this.stepStates[step] = state || 'pending';
        el.classList.remove('active', 'done', 'failed');
        if (state) {
            el.classList.add(state);
        }
        this.refreshAcceptanceSummary();
    }

    refreshAcceptanceSummary() {
        const orderedSteps = [
            ['backend', 'Connect the backend', 'Set Backend URL and run health check'],
            ['wallet', 'Connect wallet', 'Connect MetaMask and expose wallet address'],
            ['auth', 'Sign challenge login', 'Sign the backend challenge and obtain JWT'],
            ['nft', 'Verify NFT ownership', 'Run NFT verify and confirm has_nft / cache_hit'],
            ['playback', 'Load protected playback', 'Open manifest with JWT and validate playback'],
            ['rpc', 'Inspect RPC failover status', 'Load RPC status and confirm active endpoint'],
            ['transcode', 'Exercise transcoding flow', 'Run submit / status / tasks / profiles'],
        ];
        const doneCount = Object.values(this.stepStates).filter((value) => value === 'done').length;
        const failedEntry = orderedSteps.find(([step]) => this.stepStates[step] === 'failed');
        const nextPending = orderedSteps.find(([step]) => this.stepStates[step] !== 'done');

        const progressEl = document.getElementById('acceptance-progress-text');
        const focusEl = document.getElementById('acceptance-focus-text');
        const nextEl = document.getElementById('acceptance-next-text');

        if (progressEl) {
            progressEl.textContent = `${doneCount} / ${orderedSteps.length} completed`;
        }

        if (failedEntry) {
            if (focusEl) {
                focusEl.textContent = failedEntry[1];
            }
            if (nextEl) {
                nextEl.textContent = `Retry failed step: ${failedEntry[2]}`;
            }
            return;
        }

        if (nextPending) {
            if (focusEl) {
                focusEl.textContent = nextPending[1];
            }
            if (nextEl) {
                nextEl.textContent = nextPending[2];
            }
            return;
        }

        if (focusEl) {
            focusEl.textContent = 'Full acceptance path completed';
        }
        if (nextEl) {
            nextEl.textContent = 'You can now use this page for demo and interview walkthroughs';
        }
    }

    setTroubleshooting(title, message) {
        const titleEl = document.getElementById('troubleshooting-title');
        const messageEl = document.getElementById('troubleshooting-message');
        if (titleEl) {
            titleEl.textContent = title;
        }
        if (messageEl) {
            messageEl.textContent = message;
        }
    }

    onAccountChanged(event) {
        this.updateWalletUI(event.detail.address);
        this.showToast('Account changed', 'info');
    }

    onChainChanged(event) {
        this.showToast(`Chain changed to ${event.detail.chainId}`, 'info');
    }

    showLoading(show) {
        const loading = document.getElementById('loading');
        if (show) {
            loading.classList.remove('hidden');
        } else {
            loading.classList.add('hidden');
        }
    }

    showToast(message, type = 'info') {
        const container = document.getElementById('toast-container');
        const toast = document.createElement('div');
        toast.className = `toast toast-${type}`;
        toast.textContent = message;
        container.appendChild(toast);
        
        setTimeout(() => toast.classList.add('show'), 10);
        setTimeout(() => {
            toast.classList.remove('show');
            setTimeout(() => container.removeChild(toast), 300);
        }, 3000);
    }
}

document.addEventListener('DOMContentLoaded', () => {
    const app = new StreamGateApp();
    app.init();
});
