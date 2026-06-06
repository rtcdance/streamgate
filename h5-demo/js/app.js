class StreamGateApp {
    constructor() {
        this.api = new APIService();
        this.wallet = walletService;
        this.auth = new AuthService(this.api, this.wallet);
        this.player = null;
        this.isPlaying = false;
        this.lastTranscodeTaskId = null;
        this.lastUploadId = null;
        this.stepStates = {
            backend: 'pending',
            wallet: 'pending',
            auth: 'pending',
            nft: 'pending',
            upload: 'pending',
            transcode: 'pending',
            playback: 'pending',
            rpc: 'pending',
        };
    }

    async init() {
        this.initBackendConfig();
        this.initPlayer();
        this.bindEvents();
        this.refreshAcceptanceSummary();
        this.inspectWalletEnvironment();
        await this.checkBackend();
        this.restoreSession();
    }

    initBackendConfig() {
        const backendInput = document.getElementById('backend-url');
        if (backendInput) {
            backendInput.value = this.api.getBaseUrl();
        }
    }

    inspectWalletEnvironment() {
        const diagnostics = this.wallet.getProviderDiagnostics();
        const summary = diagnostics.discovered.length > 0
            ? diagnostics.discovered.map((entry) => `${entry.name}${entry.isMetaMask ? ' [MetaMask]' : ''}`).join(', ')
            : 'No injected provider detected yet';

        if (diagnostics.discovered.some((entry) => entry.isMetaMask)) {
            this.setTroubleshooting(
                'Wallet provider detected',
                `Detected providers: ${summary}. You can try Connect Wallet now.`
            );
            return;
        }

        if (diagnostics.hasWindowEthereum) {
            this.setTroubleshooting(
                'Injected wallet detected',
                `Detected providers: ${summary}. If MetaMask is expected, make sure it is enabled for this site.`
            );
            return;
        }

        if (diagnostics.isFileProtocol) {
            this.setTroubleshooting(
                'Wallet provider not injected',
                'The page is running from file://. Enable MetaMask access to file URLs or serve h5-demo over HTTP.'
            );
            return;
        }

        this.setTroubleshooting(
            'Wallet provider not injected',
            'No EIP-1193 wallet provider was detected. Check your browser profile and wallet extension state.'
        );
    }

    initPlayer() {
        const videoElement = document.getElementById('video-player');
        if (!videoElement) {
            console.warn('Video element not found, player initialization deferred');
            return false;
        }
        this.player = new HLSPlayer(videoElement);
        return this.player.init();
    }

    switchView(viewName) {
        document.querySelectorAll('.view').forEach(v => v.style.display = 'none');
        const target = document.getElementById('view-' + viewName);
        if (target) target.style.display = '';
        document.querySelectorAll('.sidebar-nav a').forEach(a => {
            a.classList.toggle('active', a.dataset.view === viewName);
        });
        // Refresh data when switching to dashboard
        if (viewName === 'dashboard') {
            this.refreshAcceptanceSummary();
            if (this.api.isReachable()) {
                this.api.healthCheck().then(h => this.updateBackendStatus(h)).catch(() => {});
            }
        }
        if (viewName === 'admin') {
            this.loadAdminData();
        }
    }

    bindEvents() {
        // Sidebar navigation
        document.querySelectorAll('.sidebar-nav a').forEach(link => {
            link.addEventListener('click', (e) => {
                e.preventDefault();
                this.switchView(link.dataset.view);
            });
        });

        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => {
            if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
            const map = { '1': 'dashboard', '2': 'workflow', '3': 'upload', '4': 'transcode', '5': 'playback', '6': 'rpc', '7': 'admin' };
            if (map[e.key]) { e.preventDefault(); this.switchView(map[e.key]); }
        });

        this._bindClick('refresh-my-videos', () => this.loadMyVideos());
        this._bindClick('connect-wallet', () => this.connectWallet());
        this._bindClick('demo-mode-btn', () => this.connectDemoWallet());
        this._bindClick('login-btn', () => this.doLogin());
        this._bindClick('verify-nft', () => this.verifyNFT());
        this._bindClick('mint-nft-btn', () => this.mintDemoNFT());
        this._bindClick('play-video', () => this.playVideo());
        this._bindClick('save-backend', () => this.saveBackendUrl());
        this._bindClick('check-rpc', () => this.loadRPCStatus());
        this._bindClick('submit-transcode', () => this.submitTranscode());
        this._bindClick('load-transcode-status', () => this.loadTranscodeStatus());
        this._bindClick('load-transcode-tasks', () => this.loadTranscodeTasks());
        this._bindClick('load-transcode-profiles', () => this.loadTranscodeProfiles());
        this._bindClick('upload-whole-btn', () => this.uploadWholeFile());
        this._bindClick('upload-chunked-btn', () => this.uploadChunked());
        this._bindClick('check-upload-status-btn', () => this.checkUploadStatus());
        this._bindClick('get-download-url-btn', () => this.getDownloadURL());
        this._bindClick('refresh-videos-btn', () => this.loadMyVideos());
        this._bindClick('test-health', () => this.testHealth());
        this._bindClick('test-challenge', () => this.testChallenge());
        document.querySelectorAll('.demo-item').forEach((item) => {
            item.addEventListener('click', () => {
                document.getElementById('video-id').value = item.dataset.id;
                this.showToast(`Selected demo video: ${item.dataset.id}`, 'info');
            });
        });

        window.addEventListener('wallet:accountChanged', (e) => this.onAccountChanged(e));
        window.addEventListener('wallet:chainChanged', (e) => this.onChainChanged(e));

        const chainSelect = document.getElementById('chain-select');
        if (chainSelect) {
            chainSelect.addEventListener('change', () => {
                const selected = chainSelect.options[chainSelect.selectedIndex];
                const contract = selected.getAttribute('data-contract');
                if (contract) {
                    document.getElementById('nft-contract').value = contract;
                }
            });
        }

        // Admin tab navigation
        document.querySelectorAll('.admin-tab').forEach(tab => {
            tab.addEventListener('click', () => this.switchAdminTab(tab.dataset.adminTab));
        });
        this._bindClick('admin-refresh-btn', () => this.loadAdminData());
        this._bindClick('admin-health-check-btn', () => this.runAdminHealthCheck());
        this._bindClick('admin-metrics-btn', () => this.loadAdminMetrics());
    }

    _bindClick(elementId, handler) {
        const el = document.getElementById(elementId);
        if (el) {
            el.addEventListener('click', handler);
        }
    }

    async checkBackend() {
        try {
            const result = await this.api.ensureReachable();
            document.getElementById('backend-url').value = this.api.getBaseUrl();
            this.updateStatus('backend', 'online', 'Online');
            this.updateStep('backend', 'done');
            this.setTroubleshooting(
                'Backend connected',
                'Good start. Next connect MetaMask so the page can request a real challenge from the backend.'
            );
            const mode = (result && result.mode) || '';
            const modeEl = document.getElementById('deployment-mode');
            const modeVal = document.getElementById('mode-value');
            if (mode && modeEl && modeVal) {
                modeEl.classList.remove('hidden');
                modeVal.textContent = mode === 'monolith' ? 'Monolith' : mode === 'microservice' ? 'Microservices' : mode;
                modeVal.style.background = mode === 'monolith' ? 'rgba(191,91,43,0.12)' : 'rgba(46,139,87,0.12)';
                modeVal.style.color = mode === 'monolith' ? 'var(--primary)' : 'var(--success)';
            }
            if (result && result.recovered_from) {
                this.showToast(`Backend auto-switched from ${result.recovered_from} to ${this.api.getBaseUrl()}`, 'info');
            }
        } catch (error) {
            this.updateStatus('backend', 'offline', 'Offline');
            this.updateStep('backend', 'failed');
            this.setTroubleshooting(
                'Backend unreachable',
                'Confirm the gateway is running on the expected port, usually http://localhost:29090, and check browser CORS/network errors.'
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
            this.loadMyVideos();
        }
    }

    async saveBackendUrl() {
        const value = document.getElementById('backend-url').value.trim();
        if (!value) {
            this.showToast('Backend URL is required', 'error');
            return;
        }
        this.api.setBaseUrl(value);
        const apiEl = document.getElementById('sidebar-api-url');
        if (apiEl) apiEl.textContent = value;
        await this.checkBackend();
        this.showToast('Backend URL updated', 'success');
    }

    async connectDemoWallet() {
        demoWallet.setDemoMode(true);
        this.showLoading(true);
        try {
            await demoWallet.connect();
            this._walletAddress = demoWallet.address;
            this._chainId = demoWallet.chainId;
            this.updateWalletUI(demoWallet.address);
            this.updateStatus('wallet', 'online', 'Connected');
            this.updateStep('wallet', 'done');
            document.getElementById('connect-wallet').disabled = true;
            document.getElementById('demo-mode-btn').textContent = 'Demo: Connected (Anvil)';
            document.getElementById('demo-mode-btn').className = 'btn btn-success';
            document.getElementById('login-section').classList.remove('hidden');
            document.getElementById('nft-section').classList.remove('hidden');

            const chainSelect = document.getElementById('chain-select');
            if (chainSelect) {
                chainSelect.value = '31337';
                chainSelect.dispatchEvent(new Event('change'));
            }

            this.showToast('Demo wallet connected (Anvil account #0)', 'success');
            this.updateAcceptance('wallet');

            try {
                await this._autoMintDemoNFT(demoWallet.address);
            } catch (mintErr) {
                console.warn('Auto-mint failed (non-fatal):', mintErr.message);
            }
        } catch (err) {
            this.showToast('Demo wallet failed: ' + err.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

async _autoMintDemoNFT(address) {
        if (typeof ethers === 'undefined') return;
        try {
            const provider = new ethers.providers.JsonRpcProvider('http://localhost:18545');
            const signer = new ethers.Wallet(DEMO_ANVIL_KEY, provider);
            const contractAddr = document.getElementById('nft-contract').value;
            const abi = ['function balanceOf(address) view returns (uint256)', 'function mint(address) returns (uint256)'];
            const contract = new ethers.Contract(contractAddr, abi, signer);

            const balance = await contract.balanceOf(address);
            if (balance.gt(0)) {
                this.showToast(`NFT ready (balance: ${balance})`, 'info');
                return;
            }

            const tx = await contract.mint(address);
            await tx.wait();
            this.showToast('NFT auto-minted for demo!', 'success');
        } catch (e) {
            // use ensureAnvilNFT as fallback (deploy + mint)
            if (typeof ensureAnvilNFT !== 'undefined') {
                const provider = new ethers.providers.JsonRpcProvider('http://localhost:18545');
                const signer = new ethers.Wallet(DEMO_ANVIL_KEY, provider);
                const addr = await ensureAnvilNFT(provider, signer, address);
                document.getElementById('nft-contract').value = addr;
                document.getElementById('nft-contract-playback').value = addr;
                this.showToast('DemoNFT deployed and minted!', 'success');
            }
        }
    }

    async _autoMintViaBackend() {
        try {
            this.showToast('No NFTs found — requesting backend to mint 3...', 'info');
            const result = await this.api.mintDemoNFT(3);
            const balance = result?.balance ?? '0';
            this.showToast(`Auto-minted 3 NFTs (new balance: ${balance})`, 'success');
            await this.verifyNFT();
        } catch (e) {
            this.showToast('Backend auto-mint failed: ' + e.message, 'warning');
        }
    }

    async _autoEnsureAnvilNFT(address) {
        if (typeof ethers === 'undefined' || typeof ensureAnvilNFT === 'undefined') return;
        try {
            // Use Anvil deployer key directly for minting, works with any wallet
            const anvilProvider = new ethers.providers.JsonRpcProvider('http://localhost:18545');
            const deployer = new ethers.Wallet(DEMO_ANVIL_KEY, anvilProvider);
            const contractAddr = document.getElementById('nft-contract').value;
            // Contract deployed at 0x5FbDB231... by forge create on Anvil startup
            const abi = ['function balanceOf(address) view returns (uint256)', 'function mint(address) returns (uint256)'];
            const c = new ethers.Contract(contractAddr, abi, anvilProvider);
            const bal = await c.balanceOf(address);
            if (bal.toNumber() > 0) {
                this.showToast(`NFT ready (balance: ${bal})`, 'info');
                return;
            }
            // Mint to the connected wallet using deployer key
            const signed = new ethers.Contract(contractAddr, abi, deployer);
            for (let i = 0; i < 3; i++) {
                const tx = await signed.mint(address);
                await tx.wait();
            }
            this.showToast('Auto-minted 3 NFTs for you!', 'success');
        } catch (e) {
            console.warn('Auto-ensure NFT failed:', e.message);
        }
    }

    async mintDemoNFT() {
        if (!demoWallet.isDemoMode && !walletService.isConnected()) {
            this.showToast('Connect a wallet first', 'warning');
            return;
        }
        this.showLoading(true);
        try {
            const isDemo = demoWallet.isDemoMode;
            const provider = isDemo
                ? new ethers.providers.JsonRpcProvider('http://localhost:18545')
                : walletService.provider;
            const signer = isDemo
                ? new ethers.Wallet(DEMO_ANVIL_KEY, provider)
                : provider.getSigner();
            const addr = isDemo ? demoWallet.address : walletService.getAddress();
            const input = document.getElementById('nft-contract');
            let contractAddr = input.value;

            // In demo mode, auto-deploy TestNFT if no contract or contract doesn't exist
            if (isDemo) {
                const code = await provider.getCode(contractAddr || ethers.constants.AddressZero);
                if (!contractAddr || contractAddr === '0x...' || code === '0x') {
                    contractAddr = '0x9fE46736679d2D9a65F0992F2272dE9f3c7fa6e0';
                    input.value = contractAddr;
                    this.showToast('Using pre-deployed StreamNFT contract', 'info');
                }
            }

            const streamNFTAbi = [
                'function mintStreamNFT(address to, string contentId, string uri, string streamURL, uint256 duration, uint256 qualityBitrate, bool isPremium) returns (uint256)',
                'function balanceOf(address owner) view returns (uint256)',
                'function ownerOf(uint256 tokenId) view returns (address)',
            ];
            const simpleAbi = [
                'function mint(address to) returns (uint256)',
                'function safeMint(address to, uint256 tokenId)',
                'function ownerOf(uint256 tokenId) view returns (address)',
            ];
            const isStreamNFT = isDemo;
            const abi = isStreamNFT ? streamNFTAbi : simpleAbi;
            const contract = new ethers.Contract(contractAddr, abi, signer);

            let tx;
            try {
                if (isStreamNFT) {
                    const idx = Math.floor(Math.random() * 100000);
                    tx = await contract.mintStreamNFT(
                        addr,
                        `demo-content-${idx}`,
                        `https://streamgate.example/nft/${idx}`,
                        `https://streamgate.example/stream/${idx}`,
                        3600,
                        5000000,
                        true
                    );
                } else {
                    tx = await contract.mint(addr);
                }
            } catch {
                const tokenId = Math.floor(Math.random() * 1000000);
                tx = await contract.safeMint(addr, tokenId);
            }
            const receipt = await tx.wait();
            this.showToast('NFT minted! Tx: ' + receipt.transactionHash.slice(0, 10) + '...', 'success');
        } catch (err) {
            this.showToast('Mint failed: ' + err.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

    async connectWallet() {
        try {
            this.showLoading(true);
            const result = await this.wallet.connect();
            this._walletAddress = result.address;
            this._chainId = result.chainId;
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

            // Auto-ensure NFTs on Anvil local chain
            if (this.wallet.chainId === 31337 || this.wallet.chainId === '0x7a69') {
                this._autoEnsureAnvilNFT(result.address);
            }
        } catch (error) {
            this.updateStatus('wallet', 'offline', 'Unavailable');
            this.setTroubleshooting(
                'Wallet connection failed',
                error.message
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
        const ws = document.getElementById('wallet-status');
        const wsSpan = ws.querySelector('span:last-child');
        if (wsSpan) {
            wsSpan.innerHTML = '<span class="status-dot online"></span> Connected';
        }
        const swTextEls = document.querySelectorAll('#sidebar-wallet #wallet-status-text');
        swTextEls.forEach(el => {
            el.textContent = 'Connected: ' + formatted;
            el.className = 'status-value online';
        });
    }

    async doLogin() {
        try {
            this.showLoading(true);
            await this.auth.requestChallenge();
            const result = await this.auth.login();
            
            document.getElementById('login-result').classList.remove('hidden');
            const loginEl = document.getElementById('login-result');
            loginEl.textContent = '';
            const span = document.createElement('span');
            span.className = 'success';
            span.textContent = `✓ Login successful! Token: ${result.token?.slice(0, 20)}...`;
            loginEl.appendChild(span);
            this.updateStatus('auth', 'online', 'Authenticated');
            this.updateStep('auth', 'done');
            this.setTroubleshooting(
                'Login succeeded',
                'JWT is ready. Now verify NFT ownership against the configured contract and chain.'
            );
            this.showToast('Login successful', 'success');

            const walletAddress = this._walletAddress || demoWallet.address;
            const chainId = parseInt(document.getElementById('chain-select').value);
            if (chainId === 31337 && walletAddress) {
                try {
                    await this._autoMintDemoNFT(walletAddress);
                } catch (mintErr) {
                    console.warn('Auto-mint after login failed:', mintErr.message);
                }
            }

            this.verifyNFT().catch((vErr) => {
                console.warn('Auto NFT verify after login failed:', vErr.message);
            });
        } catch (error) {
            document.getElementById('login-result').classList.remove('hidden');
            const errEl = document.getElementById('login-result');
            errEl.textContent = '';
            const errSpan = document.createElement('span');
            errSpan.className = 'error';
            errSpan.textContent = `✗ ${error.message}`;
            errEl.appendChild(errSpan);
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
            this.updateStatus('nft', 'pending', 'Verifying…');
            this.updateStep('nft', 'active');
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
                const onAnvil = chainId === 31337;
                this.setTroubleshooting(
                    'NFT not found',
                    onAnvil
                        ? 'Connected wallet has 0 NFTs on this contract. Click "Mint Demo NFT (Anvil)" to mint, or switch to Demo Mode for an auto-funded test wallet.'
                        : 'Check the connected wallet, contract address, and chain ID. Full acceptance needs a wallet that actually owns the NFT on this chain.'
                );
                if (onAnvil && this.auth.isAuthenticated) {
                    this._autoMintViaBackend();
                }
            }

            const nftDetails = document.getElementById('nft-details');
            nftDetails.textContent = '';
            const p1 = document.createElement('p');
            p1.textContent = `Balance: ${result.balance}`;
            nftDetails.appendChild(p1);
            const p2 = document.createElement('p');
            p2.textContent = `Chain ID: ${result.chain_id}`;
            nftDetails.appendChild(p2);
            const p3 = document.createElement('p');
            p3.textContent = `Cache: ${result.cache_hit ? 'Yes' : 'No'}`;
            nftDetails.appendChild(p3);

            this.showToast(result.has_nft ? 'NFT verified!' : 'No NFT found',
                result.has_nft ? 'success' : 'warning');
        } catch (error) {
            this.updateStatus('nft', 'offline', 'Failed');
            this.updateStep('nft', 'failed');
            this.setTroubleshooting(
                'NFT verification failed',
                `Last error: ${error.message}. Check RPC availability, contract address, chain selection, and whether the backend can reach the configured chain client.`
            );
            this.showToast(error.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

    async playVideo() {
        const videoId = document.getElementById('video-id').value || 'demo';
        
        try {
            this.showLoading(true);
            
            const token = this.auth.getToken();
            await this.player.loadVideo(videoId, {
                token,
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

    // --- Upload methods ---

    async uploadWholeFile() {
        const fileInput = document.getElementById('upload-file');
        if (!fileInput.files.length) {
            this.showToast('Please select a file first', 'error');
            return;
        }
        const file = fileInput.files[0];

        try {
            const progressContainer = document.getElementById('upload-progress-area');
            const progressBar = document.getElementById('upload-progress-bar');
            const progressText = document.getElementById('upload-progress-text');
            progressContainer.classList.remove('hidden');
            progressBar.style.width = '0%';
            progressBar.style.background = 'var(--primary)';
            progressText.textContent = `Uploading 0% (${(file.size / 1024 / 1024).toFixed(1)} MB)`;

            const result = await this.api.uploadWholeFile(file, (loaded, total) => {
                const pct = Math.round((loaded / total) * 100);
                progressBar.style.width = `${pct}%`;
                progressText.textContent = `Uploading ${pct}% (${(loaded / 1024 / 1024).toFixed(1)} / ${(total / 1024 / 1024).toFixed(1)} MB)`;
            });

            progressBar.style.width = '100%';
            progressBar.style.background = 'var(--success, #22c55e)';
            progressText.textContent = 'Upload done — creating content record...';

            this.lastUploadId = result.upload_id;
            document.getElementById('upload-id-display').value = result.upload_id;
            document.getElementById('check-upload-status-btn').disabled = false;
            document.getElementById('get-download-url-btn').disabled = false;
            this.updateStatus('upload', 'online', 'Uploaded');
            this.updateStep('upload', 'done');

            await this.bridgeUploadToTranscode(result.upload_id);
            await this.autoSubmitTranscodeFromUpload();

            this.loadMyVideos();

            this.setTroubleshooting(
                'Upload completed',
                'Content record created. Transcode auto-submitted — monitoring progress below.'
            );
            this.showOutput('upload-output', 'Upload Result', JSON.stringify(result, null, 2));
            this.showToast('Upload successful', 'success');
        } catch (error) {
            this.updateStatus('upload', 'offline', 'Failed');
            this.updateStep('upload', 'failed');
            this.setTroubleshooting('Upload failed', error.message);
            this.showOutput('upload-output', 'Upload Failed', error.message);
            this.showToast('Upload failed: ' + error.message, 'error');
        }
    }

    async uploadChunked() {
        const fileInput = document.getElementById('upload-file');
        if (!fileInput.files.length) {
            this.showToast('Please select a file first', 'error');
            return;
        }
        const file = fileInput.files[0];
        const CHUNK_SIZE = 5 * 1024 * 1024; // 5 MB per chunk
        const totalChunks = Math.ceil(file.size / CHUNK_SIZE);

        try {
            const progressContainer = document.getElementById('upload-progress-area');
            const progressBar = document.getElementById('upload-progress-bar');
            const progressText = document.getElementById('upload-progress-text');
            progressContainer.classList.remove('hidden');
            progressBar.style.width = '0%';
            progressBar.style.background = 'var(--primary)';
            progressText.textContent = `Initiating... (${(file.size / 1024 / 1024).toFixed(1)} MB, ${totalChunks} chunks)`;

            const initResult = await this.api.initChunkedUpload(file.name, file.size, totalChunks);
            const uploadId = initResult.upload_id;
            document.getElementById('upload-id-display').value = uploadId;
            this.lastUploadId = uploadId;

            for (let i = 0; i < totalChunks; i++) {
                const start = i * CHUNK_SIZE;
                const end = Math.min(start + CHUNK_SIZE, file.size);
                const chunkBlob = file.slice(start, end);

                progressText.textContent = `Uploading chunk ${i + 1}/${totalChunks}`;
                await this.api.uploadChunk(uploadId, i, chunkBlob);

                const pct = Math.round(((i + 1) / totalChunks) * 100);
                progressBar.style.width = `${pct}%`;
            }

            progressBar.style.background = 'var(--success, #22c55e)';
            progressText.textContent = 'Merging chunks...';
            const completeResult = await this.api.completeChunkedUpload(uploadId, totalChunks);

            document.getElementById('check-upload-status-btn').disabled = false;
            document.getElementById('get-download-url-btn').disabled = false;
            this.updateStatus('upload', 'online', 'Uploaded');
            this.updateStep('upload', 'done');

            await this.bridgeUploadToTranscode(uploadId);
            await this.autoSubmitTranscodeFromUpload();

            this.loadMyVideos();

            this.setTroubleshooting(
                'Chunked upload completed',
                'Content record created. Transcode auto-submitted — monitoring progress below.'
            );
            this.showOutput('upload-output', 'Chunked Upload Result', JSON.stringify(completeResult, null, 2));
            this.showToast('Chunked upload successful', 'success');
        } catch (error) {
            this.updateStatus('upload', 'offline', 'Failed');
            this.updateStep('upload', 'failed');
            this.setTroubleshooting('Chunked upload failed', error.message);
            this.showOutput('upload-output', 'Chunked Upload Failed', error.message);
            this.showToast('Upload failed: ' + error.message, 'error');
        }
    }

    // Bridge upload → transcode: create content record, fetch download URL, fill form
    async bridgeUploadToTranscode(uploadId) {
        try {
            // 1. Complete upload to create content record (upload_id = content_id)
            const completeResult = await this.api.completeUpload(uploadId);
            const contentId = completeResult.content_id || uploadId;

            // 2. Auto-fill transcode content ID
            document.getElementById('transcode-content-id').value = contentId;

            // 3. Fetch download URL for transcode input_url
            try {
                const dlResult = await this.api.getDownloadURL(uploadId, 60);
                if (dlResult.download_url) {
                    document.getElementById('transcode-input-url').value = dlResult.download_url;
                }
            } catch (dlErr) {
                // Download URL may fail if presigner not configured — not fatal
                console.warn('Could not fetch download URL:', dlErr.message);
            }

            this.showToast('Transcode form auto-filled', 'info');
        } catch (err) {
            // completeUpload may fail if DB not available — not fatal for the upload itself
            console.warn('Could not complete upload to content:', err.message);
            // Still fill content_id with upload_id as fallback
            document.getElementById('transcode-content-id').value = uploadId;
        }
    }

    async checkUploadStatus() {
        const uploadId = document.getElementById('upload-id-display').value.trim() || this.lastUploadId;
        if (!uploadId) {
            this.showToast('No upload ID available', 'error');
            return;
        }
        try {
            const result = await this.api.getUploadStatus(uploadId);
            this.showOutput('upload-output', 'Upload Status', JSON.stringify(result, null, 2));
        } catch (error) {
            this.showOutput('upload-output', 'Status Check Failed', error.message);
        }
    }

    async getDownloadURL() {
        const uploadId = document.getElementById('upload-id-display').value.trim() || this.lastUploadId;
        if (!uploadId) {
            this.showToast('No upload ID available', 'error');
            return;
        }
        try {
            const result = await this.api.getDownloadURL(uploadId, 60);
            this.showOutput('upload-output', 'Download URL', JSON.stringify(result, null, 2));
            this.showToast('Download URL generated', 'success');
        } catch (error) {
            this.showOutput('upload-output', 'Download URL Failed', error.message);
        }
    }

    async loadMyVideos() {
        try {
            const result = await this.api.listContents(50, 0);
            const items = result.items || [];
            const list = document.getElementById('my-videos-list');
            const section = document.getElementById('my-videos-section');

            if (items.length === 0) {
                list.innerHTML = '<p style="color:var(--muted);font-size:13px;padding:8px;text-align:center">No videos yet. Upload one above.</p>';
                section.style.display = 'none';
                return;
            }

            section.style.display = '';
            list.innerHTML = items.map((v) => {
                const title = v.title || v.filename || v.id;
                const size = v.size ? ` (${(v.size / 1024 / 1024).toFixed(1)}MB)` : '';
                const status = v.status || 'unknown';
                const statusClass = status === 'ready' ? 'success' : status === 'pending' ? 'pending' : 'muted';
                return `<button class="demo-item my-video-item" data-id="${v.id}" data-status="${status}">
                    <span class="demo-title">${title}${size}</span>
                    <span class="demo-badge ${statusClass}">${status}</span>
                </button>`;
            }).join('');

            list.querySelectorAll('.my-video-item').forEach((item) => {
                item.addEventListener('click', () => this.selectVideo(item.dataset.id));
            });
        } catch (error) {
            console.warn('Failed to load my videos:', error.message);
            if (error.status === 401) {
                this.auth.isAuthenticated = false;
                this.updateStatus('auth', 'offline', 'Session expired');
                this.updateStep('auth', 'failed');
                this.showToast('Session expired, please re-login', 'error');
            }
        }
    }

    async selectVideo(contentId) {
        document.getElementById('video-id').value = contentId;
        this.showToast(`Selected: ${contentId}`, 'info');

        const playerSection = document.getElementById('player-section');
        if (playerSection) {
            playerSection.classList.remove('hidden');
        }
        this.updateStep('playback', 'active');
        await this.playVideo();
    }

    async autoSubmitTranscodeFromUpload() {
        this._showTranscodeProgress('Preparing transcode...', 0);
        this._scrollToTranscodeSection();

        const contentId = document.getElementById('transcode-content-id').value.trim();
        if (!contentId) {
            this._showTranscodeProgress('Upload not yet linked to content', 10);
            return;
        }

        this._showTranscodeProgress('Searching for transcode task...', 10);

        this.showToast('Looking for auto-transcode task...', 'info');
        this.updateStatus('transcode', 'online', 'Searching...');

        const existingTask = await this._findExistingTranscodeTask(contentId);
        if (existingTask) {
            this.lastTranscodeTaskId = existingTask;
            document.getElementById('transcode-task-id').value = existingTask;
            this.updateStatus('transcode', 'online', 'Task Found');
            this.updateStep('transcode', 'done');
            this.setTroubleshooting(
                'Transcode task found',
                'Waiting for transcoding to complete...'
            );
            this._showTranscodeProgress('Task found — starting...', 0);
            this._pollTranscodeProgress(contentId, existingTask);
            return;
        }

        const inputUrl = document.getElementById('transcode-input-url').value.trim();
        const profile = 'abr';
        const priority = parseInt(document.getElementById('transcode-priority').value, 10) || 5;
        if (!inputUrl) {
            console.warn('Transcode input URL not set');
            this.updateStatus('transcode', 'offline', 'No Input URL');
            this.updateStep('transcode', 'failed');
            this._showTranscodeProgress('No input URL — download URL generation failed', 0);
            this._showTranscodeProgressBarError();
            this.showToast('Cannot start transcoding: input URL is empty. The download URL may not have been generated.', 'error');
            this.setTroubleshooting(
                'Transcode input URL missing',
                'The upload completed but the download URL could not be generated. This usually means the object storage (MinIO/S3) presigned URL feature is not working. Check that MinIO is accessible and the storage endpoint is correctly configured.'
            );
            return;
        }

        this._showTranscodeProgress('Submitting transcode task...', 0);
        this.showToast('Auto-submitting transcode task...', 'info');
        try {
            const result = await this.api.submitTranscode(contentId, inputUrl, profile, priority);
            const taskId = result.task_id || '';
            this.lastTranscodeTaskId = taskId;
            document.getElementById('transcode-task-id').value = taskId;
            if (taskId) {
                this.updateStatus('transcode', 'online', 'Task Submitted');
                this.updateStep('transcode', 'done');
                this.setTroubleshooting(
                    'Transcode task submitted',
                    'Waiting for transcoding to complete...'
                );
                this._showTranscodeProgress('Task submitted — starting...', 0);
                this._pollTranscodeProgress(contentId, taskId);
            } else {
                this._showTranscodeProgress('Searching for existing task...', 0);
                const retryTask = await this._findExistingTranscodeTask(contentId);
                if (retryTask) {
                    this.lastTranscodeTaskId = retryTask;
                    document.getElementById('transcode-task-id').value = retryTask;
                    this.updateStatus('transcode', 'online', 'Task Found');
                    this.updateStep('transcode', 'done');
                    this._showTranscodeProgress('Task found — starting...', 0);
                    this._pollTranscodeProgress(contentId, retryTask);
                } else {
                    this.updateStatus('transcode', 'offline', 'Not Found');
                    this.updateStep('transcode', 'failed');
                    this._showTranscodeProgress('Task not found', 0);
                    this._showTranscodeProgressBarError();
                    this.showToast('Transcode task not found after submit', 'error');
                }
            }
        } catch (submitErr) {
            console.warn('Transcode submit failed, searching for existing task:', submitErr.message);
            this._showTranscodeProgress('Submit failed — searching...', 0);
            const fallbackTask = await this._findExistingTranscodeTask(contentId);
            if (fallbackTask) {
                this.lastTranscodeTaskId = fallbackTask;
                document.getElementById('transcode-task-id').value = fallbackTask;
                this.updateStatus('transcode', 'online', 'Task Found');
                this.updateStep('transcode', 'done');
                this._showTranscodeProgress('Task found — starting...', 0);
                this._pollTranscodeProgress(contentId, fallbackTask);
                return;
            }
            this.updateStatus('transcode', 'offline', 'Submit Failed');
            this.updateStep('transcode', 'failed');
            this._showTranscodeProgress('Failed: ' + submitErr.message, 0);
            this._showTranscodeProgressBarError();
            this.showToast('Transcode submit failed: ' + submitErr.message, 'error');
        }
    }

    async _findExistingTranscodeTask(contentId) {
        for (let attempt = 0; attempt < 5; attempt++) {
            try {
                const tasksResult = await this.api.listTranscodeTasks(contentId, 5, 0);
                const items = tasksResult.items || tasksResult.tasks || [];
                const active = items.find(t =>
                    ['pending', 'processing', 'queued'].includes(t.status)
                );
                if (active) return active.task_id || active.id;
                if (items.length > 0) return items[0].task_id || items[0].id;
            } catch (e) {
                console.warn('Failed to list transcode tasks (attempt ' + (attempt + 1) + '):', e);
            }
            await new Promise(r => setTimeout(r, 1000));
        }
        return null;
    }

    _showTranscodeProgress(text, pct) {
        const area = document.getElementById('transcode-progress-area');
        const bar = document.getElementById('transcode-progress-bar');
        const txt = document.getElementById('transcode-progress-text');
        if (area) area.classList.remove('hidden');
        if (bar) { bar.style.width = Math.min(pct, 100) + '%'; bar.style.background = 'var(--primary)'; }
        if (txt) txt.textContent = text || 'Waiting...';
    }

    _showTranscodeProgressBarError() {
        const bar = document.getElementById('transcode-progress-bar');
        if (bar) bar.style.background = 'var(--danger, #bb3f2d)';
    }

    _scrollToTranscodeSection() {
        this.switchView('transcode');
        const el = document.getElementById('transcode-section');
        if (el) {
            setTimeout(() => el.scrollIntoView({ behavior: 'smooth', block: 'start' }), 300);
        }
    }

    _buildProfileDisplayMap(result, backendVariantProgress) {
        const submittedProfile = (result && result.profile) ? String(result.profile).toLowerCase() : '';
        const overallProgress = (result && typeof result.progress === 'number') ? result.progress : 0;
        const status = (result && result.status) || '';
        const completed = ['completed', 'done', 'success'].includes(status);
        const failed = ['failed', 'error', 'timeout'].includes(status);
        const defaultPct = completed ? 100 : (failed ? 0 : overallProgress);

        const abrProfileList = ['1080p', '720p', '480p', '360p'];
        const hasBackend = backendVariantProgress && typeof backendVariantProgress === 'object'
            && Object.keys(backendVariantProgress).length > 0;

        if (hasBackend) {
            return backendVariantProgress;
        }

        if (overallProgress === 0 && !completed && !failed) {
            return null;
        }

        if (submittedProfile === 'abr' || submittedProfile === '') {
            const map = {};
            abrProfileList.forEach(p => { map[p] = defaultPct; });
            return map;
        }

        return { [submittedProfile]: defaultPct };
    }

    _pollTranscodeProgress(contentId, taskId) {
        this._transcodePollActive = false;
        this._transcodePollActive = true;
        this._transcodePollCount = 0;
        let consecutiveErrors = 0;
        const MAX_POLL_COUNT = 900;

        const progressArea = document.getElementById('transcode-progress-area');
        const progressBar = document.getElementById('transcode-progress-bar');
        const progressText = document.getElementById('transcode-progress-text');
        const output = document.getElementById('transcode-output');
        const variantContainer = document.getElementById('variant-progress-container');
        if (progressArea) progressArea.classList.remove('hidden');
        if (variantContainer) {
            variantContainer.innerHTML = '';
            variantContainer.style.display = 'none';
        }

        const statusLabels = {
            pending: 'Waiting',
            queued: 'Queued',
            processing: 'Transcoding',
            running: 'Transcoding',
        };

        const setBarWaiting = () => {
            if (progressBar) {
                progressBar.classList.add('progress-waiting');
                progressBar.style.width = '30%';
            }
        };

        const setBarProgress = (pct) => {
            if (progressBar) {
                progressBar.classList.remove('progress-waiting');
                progressBar.style.width = Math.min(pct, 100) + '%';
                progressBar.style.background = 'var(--primary)';
            }
        };

        const setBarCompleted = () => {
            if (progressBar) {
                progressBar.classList.remove('progress-waiting');
                progressBar.style.width = '100%';
                progressBar.style.background = 'var(--success, #22c55e)';
            }
        };

        const setBarFailed = () => {
            if (progressBar) {
                progressBar.classList.remove('progress-waiting');
                progressBar.style.background = 'var(--danger, #bb3f2d)';
            }
        };

        const updateVariantProgress = (variantProgress) => {
            if (!variantProgress || !variantContainer) return;
            const variantKeys = Object.keys(variantProgress);
            const hasVariants = variantKeys.length > 0;
            variantContainer.style.display = hasVariants ? 'flex' : 'none';
            variantContainer.style.flexDirection = 'column';
            variantContainer.style.gap = '8px';

            if (hasVariants && !variantContainer.querySelector('.variant-header')) {
                const header = document.createElement('div');
                header.className = 'variant-header';
                header.style.cssText = 'font-size:12px;color:var(--mist);text-transform:uppercase;letter-spacing:0.15em;margin-bottom:4px';
                header.textContent = `${variantKeys.length} Resolution Profiles`;
                variantContainer.insertBefore(header, variantContainer.firstChild);
            }

            const existingRows = variantContainer.querySelectorAll('.variant-row');
            existingRows.forEach(row => {
                if (!variantKeys.includes(row.dataset.variant)) {
                    row.remove();
                }
            });

            variantKeys.forEach(variant => {
                let row = variantContainer.querySelector(`.variant-row[data-variant="${variant}"]`);
                if (!row) {
                    row = document.createElement('div');
                    row.className = 'variant-row';
                    row.dataset.variant = variant;
                    row.style.cssText = 'display:flex;align-items:center;gap:8px';

                    const label = document.createElement('span');
                    label.style.cssText = 'font-size:12px;color:var(--sand);width:72px;flex-shrink:0;font-weight:500';
                    const parts = variant.split('x');
                    const height = parts.length === 2 ? parseInt(parts[1]) : 0;
                    const profileNames = { 1080: '1080p FHD', 720: '720p HD', 480: '480p SD', 360: '360p LD' };
                    if (profileNames[height]) {
                        label.textContent = profileNames[height];
                    } else {
                        const heightFromName = parseInt(String(variant).replace(/[^0-9]/g, ''), 10);
                        if (profileNames[heightFromName]) {
                            label.textContent = profileNames[heightFromName];
                        } else {
                            label.textContent = parts.length === 2 ? parts[1] + 'p' : variant;
                        }
                    }
                    row.appendChild(label);

                    const barWrap = document.createElement('div');
                    barWrap.style.cssText = 'flex:1;height:6px;border-radius:3px;background:var(--line);overflow:hidden';
                    const bar = document.createElement('div');
                    bar.className = 'variant-bar';
                    bar.style.cssText = 'height:100%;width:0%;border-radius:3px;background:var(--primary);transition:width .3s ease';
                    barWrap.appendChild(bar);
                    row.appendChild(barWrap);

                    const pctLabel = document.createElement('span');
                    pctLabel.className = 'variant-pct';
                    pctLabel.style.cssText = 'font-size:11px;color:var(--muted);width:32px;text-align:right';
                    pctLabel.textContent = '0%';
                    row.appendChild(pctLabel);

                    variantContainer.appendChild(row);
                }

                const pct = variantProgress[variant];
                const bar = row.querySelector('.variant-bar');
                const pctLabel = row.querySelector('.variant-pct');
                if (typeof pct === 'number') {
                    if (bar) {
                        bar.style.width = Math.min(pct, 100) + '%';
                        bar.style.background = pct >= 100 ? 'var(--success, #22c55e)' : 'var(--primary)';
                    }
                    if (pctLabel) pctLabel.textContent = pct + '%';
                }
            });
        };

        const poll = async () => {
            if (!this._transcodePollActive) return;
            this._transcodePollCount++;
            if (this._transcodePollCount > MAX_POLL_COUNT) {
                this._transcodePollActive = false;
                if (progressText) progressText.textContent = 'Polling timed out — check status manually';
                return;
            }
            try {
                const result = await this.api.getTranscodeStatus(taskId);
                consecutiveErrors = 0;
                const status = result.status || 'unknown';
                const label = statusLabels[status] || status;

                if (typeof result.progress === 'number' && result.progress > 0) {
                    setBarProgress(result.progress);
                    if (progressText) progressText.textContent = `${label} ${result.progress}%`;
                } else if (status === 'processing' || status === 'running') {
                    setBarWaiting();
                    if (progressText) progressText.textContent = label;
                } else if (status === 'pending' || status === 'queued') {
                    setBarWaiting();
                    if (progressText) progressText.textContent = `${label}...`;
                } else {
                    setBarWaiting();
                    if (progressText) progressText.textContent = label;
                }

                const backendVariantProgress = result.metadata && result.metadata.variant_progress;
                const displayProfileMap = this._buildProfileDisplayMap(result, backendVariantProgress);
                if (displayProfileMap) {
                    updateVariantProgress(displayProfileMap);
                }

                if (output) {
                    output.classList.remove('hidden');
                    output.textContent = 'Transcode Status:\n' + JSON.stringify(result, null, 2);
                }

                if (['completed', 'done', 'success'].includes(status)) {
                    setBarCompleted();
                    if (progressText) progressText.textContent = 'Completed! Loading player...';
                    this._transcodePollActive = false;
                    await new Promise(r => setTimeout(r, 1500));
                    try {
                        await this.onTranscodeComplete(contentId);
                    } catch (e) {
                        console.warn('onTranscodeComplete error:', e);
                    }
                    return;
                }

                if (['failed', 'error', 'timeout'].includes(status)) {
                    setBarFailed();
                    if (progressText) progressText.textContent = 'Failed: ' + (result.error || status);
                    this._transcodePollActive = false;
                    this.showToast('Transcode failed', 'error');
                    return;
                }

                setTimeout(poll, 2000);
            } catch (err) {
                consecutiveErrors++;
                console.warn('Transcode poll error:', err);
                const httpStatus = err.status || (err.response && err.response.status) || 0;
                if (httpStatus === 401 || httpStatus === 403) {
                    this._transcodePollActive = false;
                    if (progressText) progressText.textContent = 'Session expired, please re-login';
                    this.showToast('Session expired, please re-login', 'error');
                    return;
                }
                if (consecutiveErrors >= 10) {
                    this._transcodePollActive = false;
                    if (progressText) progressText.textContent = 'Server unreachable — check status manually';
                    this.showToast('Server unreachable, polling stopped', 'error');
                    return;
                }
                const backoff = Math.min(4000 * Math.pow(1.5, consecutiveErrors - 1), 30000);
                if (this._transcodePollActive && this._transcodePollCount < MAX_POLL_COUNT) {
                    setTimeout(poll, backoff);
                } else {
                    this._transcodePollActive = false;
                    if (progressText) progressText.textContent = 'Polling stopped — check status manually';
                }
            }
        };

        setBarWaiting();
        if (progressText) progressText.textContent = 'Waiting...';
        setTimeout(poll, 1000);
    }

    async onTranscodeComplete(contentId) {
        document.getElementById('video-id').value = contentId;
        document.getElementById('player-section').classList.remove('hidden');
        this.loadMyVideos();
        this.showToast('Transcode complete, starting playback...', 'success');
        this.setTroubleshooting(
            'Transcode completed',
            'Playback started automatically.'
        );
        this.switchView('playback');
        await this.playVideo();
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
        const contentId = document.getElementById('transcode-content-id').value.trim();
        const inputUrl = document.getElementById('transcode-input-url').value.trim();
        const profile = document.getElementById('transcode-profile').value.trim();
        const priority = parseInt(document.getElementById('transcode-priority').value, 10) || 5;

        this._showTranscodeProgress('Submitting transcode task...', 0);

        try {
            const result = await this.api.submitTranscode(contentId, inputUrl, profile, priority);
            this.lastTranscodeTaskId = result.task_id;
            document.getElementById('transcode-task-id').value = result.task_id || '';
            this.updateStatus('transcode', 'online', 'Task Submitted');
            this.updateStep('transcode', 'done');

            this.setTroubleshooting(
                'Transcode task submitted',
                'Polling for completion...'
            );
            this.showOutput('transcode-output', 'Transcode Submit', JSON.stringify(result, null, 2));
            this._scrollToTranscodeSection();
            this._pollTranscodeProgress(contentId, result.task_id);
        } catch (error) {
            this.updateStatus('transcode', 'offline', 'Submit Failed');
            this.updateStep('transcode', 'failed');
            this.setTroubleshooting(
                'Transcode submit failed',
                'Check that the gateway exposes `/api/v1/transcode/submit` and that the payload uses `content_id`, `input_url`, `profile`, and `priority`.'
            );
            this._showTranscodeProgress('Failed: ' + error.message, 0);
            this._showTranscodeProgressBarError();
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
        const statusEl = document.getElementById(`${type}-status`)
            || document.getElementById(`${type}-status-global`)
            || document.getElementById(`${type}-status-text`);
        if (statusEl) {
            statusEl.textContent = text;
            statusEl.className = `status-value ${status}`;
        }
        const siblingTextEls = document.querySelectorAll(`#${type}-status-text, #${type}-status-global, #${type}-status`);
        siblingTextEls.forEach(el => {
            if (el === statusEl) return;
            el.textContent = text;
            el.className = `status-value ${status}`;
        });
        const dotEl = document.getElementById(`${type}-dot`);
        if (dotEl) {
            dotEl.className = `status-dot ${status}`;
        }
    }

    updateStep(step, state) {
        const mapping = {
            backend: 'step-backend',
            wallet: 'step-wallet',
            auth: 'step-auth',
            nft: 'step-nft',
            upload: 'step-upload',
            transcode: 'step-transcode',
            playback: 'step-playback',
            rpc: 'step-rpc',
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
            ['upload', 'Upload creator video', 'Upload a file and verify status + download URL'],
            ['transcode', 'Exercise transcoding flow', 'Run submit / status / tasks / profiles'],
            ['playback', 'Load protected playback', 'Open manifest with JWT and validate playback'],
            ['rpc', 'Inspect RPC failover status', 'Load RPC status and confirm active endpoint'],
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
        if (!container) {
            console.warn('Toast container not found:', message);
            return;
        }
        const toast = document.createElement('div');
        toast.className = `toast toast-${type}`;
        toast.textContent = message;
        container.appendChild(toast);
        
        setTimeout(() => toast.classList.add('show'), 10);
        setTimeout(() => {
            toast.classList.remove('show');
            setTimeout(() => {
                if (container.contains(toast)) {
                    container.removeChild(toast);
                }
            }, 300);
        }, 3000);
    }

    // === Admin Tab Methods ===

    switchAdminTab(tabName) {
        document.querySelectorAll('.admin-content').forEach(el => el.style.display = 'none');
        const target = document.getElementById('admin-' + tabName);
        if (target) target.style.display = '';
        document.querySelectorAll('.admin-tab').forEach(t => {
            t.classList.toggle('active', t.dataset.adminTab === tabName);
        });
    }

    async loadAdminData() {
        try {
            let health = await this.api.healthCheck();

            // Auto-correct: when health doesn't report mode, try the page origin.
            // This handles stale localStorage pointing at a wrong backend
            // (e.g. old api-gateway :28080 while monolith runs on :18080 via nginx :18000).
            if (!health.mode && !health.deployment_mode) {
                const pageOrigin = window.location.origin;
                if (this.api.getBaseUrl() !== pageOrigin) {
                    try {
                        const altResp = await fetch(`${pageOrigin}/health`);
                        if (altResp.ok) {
                            const altHealth = await altResp.json();
                            if (altHealth.mode || altHealth.deployment_mode) {
                                console.log(`Auto-correcting backend URL: ${this.api.getBaseUrl()} → ${pageOrigin}`);
                                this.api.setBaseUrl(pageOrigin);
                                health = altHealth;
                            }
                        }
                    } catch (_) { /* page origin not reachable, use what we have */ }
                }
            }

            this._updateAdminOverview(health);
            await this._loadServiceMatrix(health);
        } catch (err) {
            console.warn('Admin data load failed:', err.message);
        }
    }

    _updateAdminOverview(health) {
        const mode = health.mode || health.deployment_mode || '';
        // Detect deployment mode: if health API doesn't report mode,
        // check if we're running on the microservices port (28080 = api-gateway)
        // Monolith runs on port 8080, microservices api-gateway on 9090 (mapped to 28080)
        let isMonolith;
        if (mode === 'monolith') {
            isMonolith = true;
        } else if (mode === 'microservice' || mode === 'microservices') {
            isMonolith = false;
        } else {
            // Heuristic: if we can reach individual microservice ports, it's microservices
            // api-gateway in microservices mode is on port 28080 (mapped from 9090)
            // monolith would be on port 8080
            const baseUrl = this.api.getBaseUrl();
            isMonolith = baseUrl.includes(':8080') || baseUrl.includes(':18080') || baseUrl.includes(':18000');
        }

        const modeEl = document.getElementById('admin-deployment-mode');
        if (modeEl) {
            modeEl.textContent = isMonolith ? 'Monolith' : 'Microservices';
        }
        const modeLabel = document.getElementById('admin-deployment-mode-label');
        if (modeLabel) {
            modeLabel.textContent = isMonolith ? '1 Binary (Monolith)' : '9 Microservices';
        }
        const healthEl = document.getElementById('admin-health-status');
        if (healthEl) {
            const ok = health.status === 'healthy' || health.status === 'ok';
            healthEl.textContent = health.status || '—';
            healthEl.style.color = ok ? 'var(--emerald)' : 'var(--rose)';
        }
        const chainsEl = document.getElementById('admin-active-chains');
        if (chainsEl) {
            const checks = health.checks || {};
            const dbOk = checks.database && checks.database.status === 'healthy';
            const storageOk = checks.storage && checks.storage.status === 'healthy';
            const okCount = [dbOk, storageOk].filter(Boolean).length;
            chainsEl.textContent = `${okCount}/2`;
            chainsEl.style.color = okCount === 2 ? 'var(--emerald)' : 'var(--amber)';
        }
        const uptimeEl = document.getElementById('admin-uptime');
        if (uptimeEl) {
            uptimeEl.textContent = health.version || health.release || '—';
        }

        // Gateway Evidence
        const evDep = document.getElementById('evidence-deployment');
        if (evDep) evDep.textContent = isMonolith ? 'Monolith (single binary)' : 'Microservices (9 independent services)';
        const evRun = document.getElementById('evidence-runtime');
        if (evRun) evRun.textContent = isMonolith ? 'In-process plugins' : 'Distributed via Docker Compose';
        const evTs = document.getElementById('evidence-timestamp');
        if (evTs) evTs.textContent = health.timestamp ? new Date(health.timestamp).toLocaleString() : new Date().toLocaleString();
        const evEp = document.getElementById('evidence-endpoint');
        if (evEp) evEp.textContent = this.api.getBaseUrl();
        const evVer = document.getElementById('evidence-version');
        if (evVer) evVer.textContent = health.version || health.release || '—';
        const evDb = document.getElementById('evidence-database');
        if (evDb) {
            const db = health.checks && health.checks.database;
            evDb.textContent = db ? `${db.status} (${db.duration_ms}ms)` : '—';
            evDb.style.color = db && db.status === 'healthy' ? 'var(--emerald)' : 'var(--rose)';
        }
    }

    async _loadServiceMatrix(health) {
        const services = [
            { id: 'api-gateway', name: 'API Gateway', role: 'HTTP + gRPC entry point', probePaths: ['/health', '/api/v1/auth/profile'] },
            { id: 'auth', name: 'Auth Service', role: 'API endpoint via gateway', probePaths: ['/api/v1/auth/profile'] },
            { id: 'transcoder', name: 'Transcoder', role: 'API endpoint via gateway', probePaths: ['/api/v1/transcode/profiles'] },
            { id: 'streaming', name: 'Streaming', role: 'API endpoint via gateway', probePaths: ['/api/v1/web3/rpc-status'] },
            { id: 'upload', name: 'Upload Service', role: 'API endpoint via gateway', probePaths: ['/api/v1/upload/list'] },
            { id: 'metadata', name: 'Metadata', role: 'API endpoint via gateway', probePaths: ['/api/v1/content'] },
            { id: 'cache', name: 'Cache Service', role: 'API endpoint via gateway', probePaths: ['/health'] },
            { id: 'worker', name: 'Worker', role: 'API endpoint via gateway', probePaths: ['/health'] },
            { id: 'monitor', name: 'Monitor', role: 'API endpoint via gateway', probePaths: ['/health'] },
        ];

        const mode = health.mode || health.deployment_mode || '';
        let isMonolith;
        if (mode === 'monolith') {
            isMonolith = true;
        } else if (mode === 'microservice' || mode === 'microservices') {
            isMonolith = false;
        } else {
            const baseUrl = this.api.getBaseUrl();
            isMonolith = baseUrl.includes(':8080') || baseUrl.includes(':18080') || baseUrl.includes(':18000');
        }

        const baseUrl = this.api.getBaseUrl();
        const matrixEl = document.getElementById('service-matrix');
        const summaryEl = document.getElementById('service-matrix-summary');
        if (!matrixEl) return;

        if (isMonolith) {
            const probes = await this._probeEndpoints(baseUrl, [
                { path: '/health', label: 'Health' },
                { path: '/api/v1/auth/profile', label: 'Auth' },
                { path: '/api/v1/web3/rpc-status', label: 'RPC Status' },
                { path: '/metrics', label: 'Metrics' },
            ]);
            const okCount = probes.filter(p => p.ok).length;
            if (summaryEl) {
                summaryEl.innerHTML = `<span style="color:var(--emerald)">${okCount}/${probes.length}</span> probes passed — <strong>Monolith mode</strong> (all services in one process)`;
            }
            matrixEl.innerHTML = this._renderServiceCard('monolith', 'Monolith (All-in-One)', 'Single binary running all 9 plugins', baseUrl, probes);
        } else {
            // Microservices mode: all probes go through API Gateway (CORS-restricted from browser)
            let totalOk = 0;
            let totalProbes = 0;
            let cardsHtml = '';

            for (const svc of services) {
                const endpoints = svc.probePaths.map(p => {
                    if (typeof p === 'string') return { path: p, label: p.split('/').pop() || p };
                    return { path: p.path, method: p.method, label: p.label || p.path.split('/').pop() };
                });
                const probes = await this._probeEndpoints(baseUrl, endpoints);
                totalOk += probes.filter(p => p.ok).length;
                totalProbes += probes.length;
                cardsHtml += this._renderServiceCard(svc.id, svc.name, svc.role, baseUrl, probes);
            }

            if (summaryEl) {
                summaryEl.innerHTML = `<span style="color:${totalOk === totalProbes ? 'var(--emerald)' : 'var(--amber)'}">${totalOk}/${totalProbes}</span> API endpoints available via <strong>microservices gateway</strong>`;
            }
            matrixEl.innerHTML = cardsHtml;
        }
    }

    async _probeEndpoints(baseUrl, endpoints) {
        const probes = [];
        const token = this.api.getAuthToken();
        for (const ep of endpoints) {
            try {
                const opts = { signal: AbortSignal.timeout(3000) };
                const headers = {};
                if (token) {
                    headers['Authorization'] = `Bearer ${token}`;
                }
                if (ep.method === 'POST') {
                    opts.method = 'POST';
                    headers['Content-Type'] = 'application/json';
                    opts.body = '{}';
                }
                opts.headers = headers;
                const resp = await fetch(`${baseUrl}${ep.path}`, opts);
                // 2xx = healthy, 401/403 = service up but needs auth
                const ok = resp.ok || resp.status === 401 || resp.status === 403;
                probes.push({ path: ep.path, ok, status: String(resp.status), summary: ok ? `${ep.label} OK` : `HTTP ${resp.status}` });
            } catch (e) {
                probes.push({ path: ep.path, ok: false, status: 'ERR', summary: e.name === 'TimeoutError' ? 'Timeout' : 'Unreachable' });
            }
        }
        return probes;
    }

    _renderServiceCard(_id, name, role, baseUrl, probes) {
        const okCount = probes.filter(p => p.ok).length;
        const allOk = okCount === probes.length;
        const toneClass = allOk ? 'healthy' : okCount > 0 ? 'warning' : 'error';

        return `
        <div class="service-card">
            <div class="service-card-header">
                <div>
                    <h3>${name}</h3>
                    <p>${role}</p>
                    <p class="service-url">${baseUrl}</p>
                </div>
                <div class="probe-badge ${toneClass}">${okCount}/${probes.length} ready</div>
            </div>
            <div class="probe-list">
                ${probes.map(p => `
                <div class="probe-item ${p.ok ? 'healthy' : 'error'}">
                    <div class="probe-row">
                        <span class="probe-path">${p.path}</span>
                        <span class="probe-status">${p.status}</span>
                    </div>
                    <pre class="probe-summary">${p.summary}</pre>
                </div>
                `).join('')}
            </div>
        </div>`;
    }

    async runAdminHealthCheck() {
        const output = document.getElementById('admin-health-output');
        if (!output) return;
        output.classList.remove('hidden');
        output.textContent = 'Running health check...';
        try {
            const result = await this.api.healthCheck();
            output.textContent = 'Health Check Result:\n' + JSON.stringify(result, null, 2);
        } catch (err) {
            output.textContent = 'Health Check Failed:\n' + err.message;
        }
    }

    async loadAdminMetrics() {
        const output = document.getElementById('admin-metrics-output');
        if (!output) return;
        output.classList.remove('hidden');
        output.textContent = 'Loading metrics...';
        try {
            const baseUrl = this.api.getBaseUrl();
            const resp = await fetch(`${baseUrl}/metrics`, { headers: { 'Authorization': `Bearer ${this.api.getAuthToken()}` } });
            const text = await resp.text();
            // Third-party workaround: nginx serves index.html when /metrics route is missing
            const contentType = resp.headers.get('content-type') || '';
            const isPrometheus = contentType.includes('text/plain') || text.trimStart().startsWith('#');
            if (!resp.ok) {
                output.textContent = `Metrics Load Failed: HTTP ${resp.status} ${resp.statusText}`;
            } else if (!isPrometheus) {
                const preview = text.slice(0, 200).replace(/\n/g, ' ');
                output.textContent = `Metrics endpoint not available.\n` +
                    `Expected Prometheus text format from ${baseUrl}/metrics, got ${contentType || 'unknown'}.\n` +
                    `Response preview: ${preview}...`;
            } else {
                const lines = text.split('\n').filter(l => l && !l.startsWith('#')).slice(0, 50);
                output.textContent = `Prometheus Metrics (${lines.length} samples shown):\n\n` + lines.join('\n');
            }
        } catch (err) {
            output.textContent = 'Metrics Load Failed:\n' + err.message;
        }
    }
}

document.addEventListener('DOMContentLoaded', () => {
    const app = new StreamGateApp();
    app.init();
});
