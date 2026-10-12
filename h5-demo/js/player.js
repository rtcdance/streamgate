class HLSPlayer {
    constructor(videoElement) {
        this.video = videoElement;
        this.hls = null;
        this.currentVideoId = null;
        this.manifestUrl = null;
        this.authToken = null;
        this._manifestParsedBound = null;
    }

    init() {
        if (Hls.isSupported()) {
            this.hls = new Hls({
                enableWorker: true,
                lowLatencyMode: false,
                backBufferLength: 90,
                maxBufferHole: 0.5,
                maxSeekHole: 2,
                xhrSetup: (xhr, url) => {
                    if (this.authToken && !url.includes('playback_token=')) {
                        xhr.setRequestHeader('Authorization', `Bearer ${this.authToken}`);
                    }
                },
            });
            this.hls.on(Hls.Events.ERROR, (event, data) => this.handleError(event, data));
            this.hls.on(Hls.Events.LEVEL_SWITCHED, (event, data) => this.handleLevelSwitched(data));
            return true;
        } else if (this.video.canPlayType('application/vnd.apple.mpegurl')) {
            this.video.addEventListener('loadedmetadata', () => this.handleMetadata());
            return true;
        } else {
            console.error('HLS is not supported in this browser');
            return false;
        }
    }

    async loadVideo(videoId, options = {}) {
        this.currentVideoId = videoId;
        this.authToken = options.token || null;
        const baseUrl = (options.baseUrl || window.location.origin || 'http://localhost:9090').replace(/\/$/, '');
        const token = options.token || null;
        const contract = options.contract || '';
        const chainId = options.chainId || '';
        let manifestUrl = `${baseUrl}/api/v1/streaming/${videoId}/manifest.m3u8`;
        const params = new URLSearchParams();
        if (contract) params.set('contract', contract);
        if (chainId) params.set('chain_id', String(chainId));
        const qs = params.toString();
        if (qs) manifestUrl += '?' + qs;
        this.manifestUrl = manifestUrl;

        const response = await fetch(this.manifestUrl, {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || `HTTP ${response.status}`);
        }
        const rawManifest = await response.text();
        const manifestText = this.rewriteManifest(rawManifest, baseUrl);
        const manifestBlobUrl = URL.createObjectURL(new Blob([manifestText], { type: 'application/vnd.apple.mpegurl' }));

        if (this.hls) {
            if (this.hls.media) {
                this.hls.detachMedia();
            }
            if (this._manifestParsedBound) {
                this.hls.off(Hls.Events.MANIFEST_PARSED, this._manifestParsedBound);
            }
            this._manifestParsedBound = () => this.handleManifestParsed();
            this.hls.on(Hls.Events.MANIFEST_PARSED, this._manifestParsedBound);
            this.hls.loadSource(manifestBlobUrl);
            this.hls.attachMedia(this.video);
        } else {
            this.video.src = manifestBlobUrl;
            this.video.addEventListener('loadedmetadata', () => this.handleMetadata());
        }
    }

    rewriteManifest(manifestText, baseUrl) {
        return manifestText
            .split('\n')
            .map((line) => {
                if (line.startsWith('/api/')) {
                    return `${baseUrl}${line}`;
                }
                return line;
            })
            .join('\n');
    }

    handleManifestParsed() {
        this.updateQualitySelector();
        this.video.play().catch(err => {
            console.warn('Auto-play failed:', err.message);
        });
    }

    handleMetadata() {
        console.log('Video metadata loaded');
        this.video.play().catch(err => {
            console.warn('Auto-play failed:', err.message);
        });
    }

    handleLevelSwitched(data) {
        const select = document.getElementById('quality-select');
        if (select) select.value = String(data.level);
        const current = document.getElementById('quality-current');
        if (current && this.hls && this.hls.levels) {
            const level = this.hls.levels[data.level];
            if (data.level === -1) {
                current.textContent = 'Auto (adaptive)';
            } else if (level) {
                const h = level.height || '?';
                current.textContent = `${h}p (${Math.round(level.bitrate / 1000)}kbps)`;
            }
        }
    }

    handleError(event, data) {
        console.error('HLS Error:', data);

        // An authorization failure is NOT a network hiccup. Retrying it loops
        // forever while the video keeps playing out of its buffer, which looks
        // exactly like "permission is still granted" — the failure this
        // distinction exists to prevent. Stop, and say which it was.
        if (this.isAuthorizationFailure(data)) {
            console.warn('Playback authorization ended:', data.details);
            this.endPlayback('authorization-ended');
            return;
        }

        if (data.fatal) {
            switch (data.type) {
                case Hls.ErrorTypes.NETWORK_ERROR:
                    console.log('Fatal network error, trying to recover');
                    this.hls.startLoad();
                    break;
                case Hls.ErrorTypes.MEDIA_ERROR:
                    console.log('Fatal media error, trying to recover');
                    this.hls.recoverMediaError();
                    break;
                default:
                    console.error('Fatal error, cannot recover');
                    this.destroy();
                    break;
            }
        }
    }

    // A capability that has expired, been revoked, or been invalidated by a
    // transfer comes back as 401/403. hls.js files that under NETWORK_ERROR, so
    // the status code is what tells the two apart.
    isAuthorizationFailure(data) {
        if (!data) return false;
        const status = data.response && data.response.code;
        if (status === 401 || status === 403) return true;
        const details = String(data.details || '');
        return /unauthoriz|forbidden/i.test(details);
    }

    endPlayback(reason) {
        try {
            if (this.hls) this.hls.destroy();
        } catch (e) {
            /* already torn down */
        }
        this.hls = null;

        // Stopping the element is what actually ends playback: leaving the
        // buffer playing would keep showing content after permission ended.
        if (this.video) {
            try { this.video.pause(); } catch (e) { /* ignore */ }
            this.video.removeAttribute('src');
            try { this.video.load(); } catch (e) { /* ignore */ }
        }

        // The capability is spent; keeping it only invites a retry loop.
        try { sessionStorage.removeItem('streamgate_playback_token'); } catch (e) { /* ignore */ }

        const el = document.getElementById('player-status');
        if (el) {
            el.textContent = reason === 'authorization-ended'
                ? 'Playback authorization ended — sign in again to continue.'
                : 'Playback stopped.';
            // Machine-readable so the acceptance can assert WHICH happened,
            // rather than inferring it from prose.
            el.dataset.sgPlaybackState = reason;
        }
    }

    updateQualitySelector() {
        const container = document.getElementById('quality-selector');
        const select = document.getElementById('quality-select');
        if (!container || !select || !this.hls) return;
        const levels = this.hls.levels;
        if (!levels || levels.length <= 1) {
            container.classList.add('hidden');
            return;
        }
        container.classList.remove('hidden');
        select.innerHTML = '';

        const autoOpt = document.createElement('option');
        autoOpt.value = '-1';
        autoOpt.textContent = 'Auto (adaptive)';
        autoOpt.selected = this.hls.currentLevel === -1;
        select.appendChild(autoOpt);

        levels.forEach((level, i) => {
            const opt = document.createElement('option');
            opt.value = String(i);
            const h = level.height || '?';
            opt.textContent = `${h}p (${Math.round(level.bitrate/1000)}kbps)`;
            opt.selected = this.hls.currentLevel === i;
            select.appendChild(opt);
        });

        select.onchange = () => this.setQuality(parseInt(select.value));
        this.handleLevelSwitched({ level: this.hls.currentLevel });
    }

    setQuality(level) {
        if (!this.hls || !this.hls.levels) return;
        this.hls.currentLevel = level;
        const select = document.getElementById('quality-select');
        if (select) select.value = String(level);
    }

    play() {
        return this.video.play();
    }

    pause() {
        this.video.pause();
    }

    destroy() {
        if (this.hls) {
            this.hls.destroy();
            this.hls = null;
        }
        this.video.src = '';
        this.currentVideoId = null;
        this.manifestUrl = null;
    }

    getCurrentTime() {
        return this.video.currentTime;
    }

    getDuration() {
        return this.video.duration;
    }

    isPlaying() {
        return !this.video.paused && !this.video.ended;
    }
}
