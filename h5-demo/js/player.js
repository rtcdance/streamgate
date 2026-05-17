class HLSPlayer {
    constructor(videoElement) {
        this.video = videoElement;
        this.hls = null;
        this.currentVideoId = null;
        this.manifestUrl = null;
        this.authToken = null;
    }

    init() {
        if (Hls.isSupported()) {
            this.hls = new Hls({
                enableWorker: true,
                lowLatencyMode: true,
                backBufferLength: 90,
                xhrSetup: (xhr, url) => {
                    // Attach JWT to requests that don't already carry playback_token.
                    // Segment URLs from the manifest include playback_token; the
                    // Authorization header is a fallback for other HLS sub-requests.
                    if (this.authToken && !url.includes('playback_token=')) {
                        xhr.setRequestHeader('Authorization', `Bearer ${this.authToken}`);
                    }
                },
            });
            this.hls.on(Hls.Events.ERROR, (event, data) => this.handleError(event, data));
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
        this.manifestUrl = `${baseUrl}/api/v1/streaming/${videoId}/manifest.m3u8?contract=${encodeURIComponent(contract)}${chainId ? `&chain_id=${encodeURIComponent(chainId)}` : ''}`;

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
            this.hls.loadSource(manifestBlobUrl);
            this.hls.attachMedia(this.video);
            this.hls.on(Hls.Events.MANIFEST_PARSED, () => this.handleManifestParsed());
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
        console.log('HLS manifest parsed, attempting to play');
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

    handleError(event, data) {
        console.error('HLS Error:', data);
        
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
