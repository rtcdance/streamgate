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

    bindEvents() {
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
            document.getElementById('connect-wallet').disabled = true;
            document.getElementById('demo-mode-btn').textContent = 'Demo: Connected (Anvil)';
            document.getElementById('demo-mode-btn').className = 'btn btn-success';
            this.showToast('Demo wallet connected (Anvil account #0)', 'success');
            this.updateAcceptance('wallet');
        } catch (err) {
            this.showToast('Demo wallet failed: ' + err.message, 'error');
        } finally {
            this.showLoading(false);
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
                ? new ethers.providers.JsonRpcProvider('http://localhost:8545')
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
                    this.showToast('Deploying TestNFT contract...', 'info');
                    const testNFTAbi = [
                        'function mint(address to) returns (uint256)',
                        'function balanceOf(address owner) view returns (uint256)',
                        'function ownerOf(uint256 tokenId) view returns (address)',
                    ];
                    const testNFTBytecode =
                        '0x6080604052348015600e575f5ffd5b506106418061001c5f395ff3fe608060405234801561000f575f5ffd5b506004361061004a575f3560e01c806318160ddd1461004e5780636352211e1461006c5780636a6278421461009c57806370a08231146100cc575b5f5ffd5b6100566100fc565b6040516100639190610398565b60405180910390f35b610086600480360381019061008191906103df565b610104565b6040516100939190610449565b60405180910390f35b6100b660048036038101906100b1919061048c565b6101b0565b6040516100c39190610398565b60405180910390f35b6100e660048036038101906100e1919061048c565b6102cc565b6040516100f39190610398565b60405180910390f35b5f5f54905090565b5f5f60025f8481526020019081526020015f205f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690505f73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff16036101a7576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161019e90610511565b60405180910390fd5b80915050919050565b5f5f5f5f81546101bf9061055c565b919050819055905060015f8473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205f8154809291906102149061055c565b91905055508260025f8381526020019081526020015f205f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550808373ffffffffffffffffffffffffffffffffffffffff165f73ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef60405160405180910390a480915050919050565b5f5f73ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff160361033b576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610332906105ed565b60405180910390fd5b60015f8373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f20549050919050565b5f819050919050565b61039281610380565b82525050565b5f6020820190506103ab5f830184610389565b92915050565b5f5ffd5b6103be81610380565b81146103c8575f5ffd5b50565b5f813590506103d9816103b5565b92915050565b5f602082840312156103f4576103f36103b1565b5b5f610401848285016103cb565b91505092915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6104338261040a565b9050919050565b61044381610429565b82525050565b5f60208201905061045c5f83018461043a565b92915050565b61046b81610429565b8114610475575f5ffd5b50565b5f8135905061048681610462565b92915050565b5f6020820190506104a16104a06103b1565b5b5f6104ae84828501610478565b91505092915050565b5f82825260208201905092915050565b7f6e6f6e6578697374656e7420746f6b656e0000000000000000000000000000005f82015250565b5f6104fb6011836104b7565b9150610506826104c7565b602082019050919050565b5f6020820190508181035f830152610528816104ef565b9050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f61056682610380565b91507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff82036105985761059761052f565b5b600182019050919050565b7f7a65726f206164647265737300000000000000000000000000000000000000005f82015250565b5f6105d7600c836104b7565b91506105e2826105a3565b602082019050919050565b5f6020820190508181035f830152610604816105cb565b905091905056fea2646970667358221220776d4943952e795b6c583e279f45342e4cf716c7741cf72b8bb4039b84d4948164736f6c63430008230033';

                    const factory = new ethers.ContractFactory(testNFTAbi, testNFTBytecode, signer);
                    const deployed = await factory.deploy();
                    await deployed.deployed();
                    contractAddr = deployed.address;
                    input.value = contractAddr;
                    this.showToast('TestNFT deployed at ' + contractAddr, 'success');
                }
            }

            const abi = ['function mint(address to) returns (uint256)',
                         'function safeMint(address to, uint256 tokenId)',
                         'function ownerOf(uint256 tokenId) view returns (address)'];
            const contract = new ethers.Contract(contractAddr, abi, signer);

            let tx;
            try {
                tx = await contract.mint(addr);
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
        const walletStatus = document.getElementById('wallet-status');
        walletStatus.textContent = '';
        const dot = document.createElement('span');
        dot.className = 'status-dot online';
        walletStatus.appendChild(dot);
        const label = document.createElement('span');
        label.textContent = 'Connected';
        walletStatus.appendChild(label);
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
            this.showLoading(true);
            const progressContainer = document.getElementById('upload-progress-container');
            const progressBar = document.getElementById('upload-progress-bar');
            const progressText = document.getElementById('upload-progress-text');
            progressContainer.classList.remove('hidden');
            progressBar.style.width = '0%';
            progressText.textContent = '0%';

            const result = await this.api.uploadWholeFile(file, (loaded, total) => {
                const pct = Math.round((loaded / total) * 100);
                progressBar.style.width = `${pct}%`;
                progressText.textContent = `${pct}%`;
            });

            this.lastUploadId = result.upload_id;
            document.getElementById('upload-id-display').value = result.upload_id;
            document.getElementById('check-upload-status-btn').disabled = false;
            document.getElementById('get-download-url-btn').disabled = false;
            this.updateStatus('upload', 'online', 'Uploaded');
            this.updateStep('upload', 'done');

            // Auto-bridge: complete upload → create content record → fill transcode form → auto-submit
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
        } finally {
            this.showLoading(false);
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
            this.showLoading(true);
            const progressContainer = document.getElementById('upload-progress-container');
            const progressBar = document.getElementById('upload-progress-bar');
            const progressText = document.getElementById('upload-progress-text');
            progressContainer.classList.remove('hidden');
            progressBar.style.width = '0%';
            progressText.textContent = 'Initiating...';

            // Step 1: Init
            const initResult = await this.api.initChunkedUpload(file.name, file.size, totalChunks);
            const uploadId = initResult.upload_id;
            document.getElementById('upload-id-display').value = uploadId;
            this.lastUploadId = uploadId;

            // Step 2: Upload chunks
            for (let i = 0; i < totalChunks; i++) {
                const start = i * CHUNK_SIZE;
                const end = Math.min(start + CHUNK_SIZE, file.size);
                const chunkBlob = file.slice(start, end);

                progressText.textContent = `Chunk ${i + 1}/${totalChunks}`;
                await this.api.uploadChunk(uploadId, i, chunkBlob);

                const pct = Math.round(((i + 1) / totalChunks) * 100);
                progressBar.style.width = `${pct}%`;
            }

            // Step 3: Complete
            progressText.textContent = 'Merging chunks...';
            const completeResult = await this.api.completeChunkedUpload(uploadId, totalChunks);

            document.getElementById('check-upload-status-btn').disabled = false;
            document.getElementById('get-download-url-btn').disabled = false;
            this.updateStatus('upload', 'online', 'Uploaded');
            this.updateStep('upload', 'done');

            // Auto-bridge: complete upload → create content record → fill transcode form → auto-submit
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
        } finally {
            this.showLoading(false);
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
        const contentId = document.getElementById('transcode-content-id').value.trim();
        if (!contentId) {
            console.warn('Transcode content ID not set');
            return;
        }

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
            this._pollTranscodeProgress(contentId, existingTask);
            return;
        }

        const inputUrl = document.getElementById('transcode-input-url').value.trim();
        const profile = document.getElementById('transcode-profile').value.trim() || '720p';
        const priority = parseInt(document.getElementById('transcode-priority').value, 10) || 5;
        if (!inputUrl) {
            console.warn('Transcode input URL not set');
            return;
        }

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
                this._pollTranscodeProgress(contentId, taskId);
            } else {
                const retryTask = await this._findExistingTranscodeTask(contentId);
                if (retryTask) {
                    this.lastTranscodeTaskId = retryTask;
                    document.getElementById('transcode-task-id').value = retryTask;
                    this.updateStatus('transcode', 'online', 'Task Found');
                    this.updateStep('transcode', 'done');
                    this._pollTranscodeProgress(contentId, retryTask);
                } else {
                    this.updateStatus('transcode', 'offline', 'Not Found');
                    this.updateStep('transcode', 'failed');
                    this.showToast('Transcode task not found after submit', 'error');
                }
            }
        } catch (submitErr) {
            console.warn('Transcode submit failed, searching for existing task:', submitErr.message);
            const fallbackTask = await this._findExistingTranscodeTask(contentId);
            if (fallbackTask) {
                this.lastTranscodeTaskId = fallbackTask;
                document.getElementById('transcode-task-id').value = fallbackTask;
                this.updateStatus('transcode', 'online', 'Task Found');
                this.updateStep('transcode', 'done');
                this._pollTranscodeProgress(contentId, fallbackTask);
                return;
            }
            this.updateStatus('transcode', 'offline', 'Submit Failed');
            this.updateStep('transcode', 'failed');
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

    _pollTranscodeProgress(contentId, taskId) {
        this._transcodePollActive = false;
        this._transcodePollActive = true;

        const progressArea = document.getElementById('transcode-progress-area');
        const progressBar = document.getElementById('transcode-progress-bar');
        const progressText = document.getElementById('transcode-progress-text');
        const output = document.getElementById('transcode-output');
        if (progressArea) progressArea.classList.remove('hidden');

        const poll = async () => {
            if (!this._transcodePollActive) return;
            try {
                const result = await this.api.getTranscodeStatus(taskId);
                const status = result.status || 'unknown';

                if (progressText) progressText.textContent = status;
                if (output) {
                    output.classList.remove('hidden');
                    output.textContent = 'Transcode Status:\n' + JSON.stringify(result, null, 2);
                }

                if (['completed', 'done', 'success'].includes(status)) {
                    if (progressBar) progressBar.style.width = '100%';
                    if (progressText) progressText.textContent = 'Completed!';
                    this._transcodePollActive = false;
                    await this.onTranscodeComplete(contentId);
                    return;
                }

                if (['failed', 'error', 'timeout'].includes(status)) {
                    if (progressText) progressText.textContent = 'Failed: ' + (result.error || status);
                    this._transcodePollActive = false;
                    this.showToast('Transcode failed', 'error');
                    return;
                }

                if (progressBar) {
                    const pct = typeof result.progress === 'number' ? result.progress : 0;
                    progressBar.style.width = Math.min(pct, 100) + '%';
                }
                if (progressText && typeof result.progress === 'number') {
                    progressText.textContent = `${result.progress}% - ${status}`;
                }

                setTimeout(poll, 3000);
            } catch (err) {
                console.warn('Transcode poll error:', err);
                if (err.status === 401) {
                    this._transcodePollActive = false;
                    if (progressText) progressText.textContent = 'Session expired, please re-login';
                    this.showToast('Session expired, please re-login', 'error');
                    return;
                }
                if (this._transcodePollActive) setTimeout(poll, 5000);
            }
        };

        setTimeout(poll, 3000);
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
                'Polling for completion...'
            );
            this.showOutput('transcode-output', 'Transcode Submit', JSON.stringify(result, null, 2));
            this._pollTranscodeProgress(contentId, result.task_id);
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
}

document.addEventListener('DOMContentLoaded', () => {
    const app = new StreamGateApp();
    app.init();
});
