import { CuaEventFilter } from './wallet_cua_filter.mjs';
import { validateControlRequest } from './wallet_cua_control_contract.mjs';

const METHODS = ['Network.requestWillBeSent', 'Network.requestWillBeSentExtraInfo', 'Network.responseReceived',
    'Network.loadingFinished', 'Network.loadingFailed', 'Runtime.exceptionThrown'];
const STATE = origin => `(async () => {
    if (location.origin !== ${JSON.stringify(origin)}) throw Error('H5 target origin changed');
    const video = document.getElementById('video-player');
    const provider = window.ethereum?.providers?.find(item => item.isMetaMask) || (window.ethereum?.isMetaMask ? window.ethereum : null);
    let wallet = null;
    if (provider) wallet = { metaMask: true, accounts: await provider.request({method:'eth_accounts'}), chainId: Number(await provider.request({method:'eth_chainId'})) };
    const stored = sessionStorage.getItem('streamgate_identity');
    return { origin: location.origin, token: sessionStorage.getItem('streamgate_token'), identity: stored ? JSON.parse(stored) : null, wallet,
        time: video?.currentTime || 0, frames: video?.getVideoPlaybackQuality?.().totalVideoFrames || 0, paused: video?.paused ?? true,
        playbackState: document.getElementById('player-status')?.dataset.sgPlaybackState,
        videoError: video?.error?.code || null, authError: document.getElementById('login-result')?.dataset.authError || null };
})()`;
const PROFILE = origin => `(async () => {
    if (location.origin !== ${JSON.stringify(origin)}) throw Error('H5 target origin changed');
    const response = await fetch('/api/v1/auth/profile', {headers:{Authorization:'Bearer '+sessionStorage.getItem('streamgate_token')}});
    return {status:response.status,data:await response.json()};
})()`;
const CONTROL_STATE = origin => `(async () => {
    if (location.origin !== ${JSON.stringify(origin)}) throw Error('H5 target origin changed');
    const video=document.getElementById('video-player');
    const p=window.ethereum?.providers?.find(item=>item.isMetaMask)||(window.ethereum?.isMetaMask?window.ethereum:null);
    const token=sessionStorage.getItem('streamgate_token'),identity=sessionStorage.getItem('streamgate_identity');
    return {origin:location.origin,token,identity:identity?JSON.parse(identity):null,wallet:p?{metaMask:true,
        accounts:await p.request({method:'eth_accounts'}),chainId:Number(await p.request({method:'eth_chainId'}))}:null,
        uiState:document.getElementById('login-session-state')?.textContent||'',navigationType:performance.getEntriesByType('navigation')[0]?.type,
        playback:{at:Date.now(),time:video?.currentTime||0,duration:video?.duration,frames:video?.getVideoPlaybackQuality?.().totalVideoFrames||0,
            paused:video?.paused??true,ended:video?.ended??true,loop:video?.loop===true,rate:video?.playbackRate,
            error:video?.error?.code||null,state:document.getElementById('player-status')?.dataset.sgPlaybackState||'unknown'}};
})()`;
const CONTROL_HTTP = (origin, request) => `(async () => {
    if (location.origin !== ${JSON.stringify(origin)}) throw Error('H5 target origin changed');
    const request=${JSON.stringify(request)};
    const response=await fetch(request.url,{...request.options,redirect:'error',signal:AbortSignal.timeout(12000)});
    const bytes=new Uint8Array(await response.arrayBuffer());
    if(bytes.length>2*1024*1024)throw Error('Observed body exceeds media budget');
    let encoded='';for(let i=0;i<bytes.length;i+=8192)encoded+=String.fromCharCode(...bytes.subarray(i,i+8192));
    return {status:response.status,base64:btoa(encoded),at:Date.now()};
})()`;

// Operates only through the documented CUA tab/capability objects supplied by
// the caller. No wallet extension, signing method or browser-wide endpoint is
// accessed; task operations have fixed implementations rather than scripts.
export class CuaPeer {
    constructor({ targetPage, targetCap, origin, content }) {
        if (!targetPage || typeof targetPage.url !== 'function' || !targetCap || typeof targetCap.send !== 'function' || typeof targetCap.readEvents !== 'function') throw Error('CUA tab and capability required');
        const parsed = new URL(origin);
        if (parsed.origin !== origin || !['http:', 'https:'].includes(parsed.protocol) || !['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname) || parsed.username || parsed.password) throw Error('local origin required');
        this.page = targetPage; this.cap = targetCap; this.origin = origin; this.content = content; this.filter = null; this.failed = false;
    }

    async assertOrigin() {
        let actual;
        try { actual = new URL(await this.page.url()).origin; } catch { throw Error('H5 target origin unavailable'); }
        if (actual !== this.origin) throw Error('H5 target origin changed');
    }

    async execute(task) {
        if (this.failed) throw Error('CUA peer failed');
        try { return await this.perform(task); } catch (error) { this.failed = true; throw error; }
    }

    async relayOnce({ relayPage, relayCap, relayOrigin, sessionKey }) {
        if (this.failed) throw Error('CUA peer failed');
        try {
            const relay = new URL(relayOrigin);
            if (relay.origin !== relayOrigin || relay.protocol !== 'http:' || relay.hostname !== '127.0.0.1' || !relay.port ||
                relay.username || relay.password || relayOrigin === this.origin || !/^[a-f0-9]{64}$/.test(sessionKey) ||
                !relayPage || typeof relayPage.url !== 'function' || !relayCap || typeof relayCap.send !== 'function') throw Error('invalid local relay');
            const evaluate = async body => {
                if (new URL(await relayPage.url()).origin !== relayOrigin) throw Error('relay origin changed');
                const expression = `(async () => { if (location.origin !== ${JSON.stringify(relayOrigin)}) throw Error('relay origin changed'); ${body} })()`;
                const result = await relayCap.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
                if (result.exceptionDetails || !result.result || !Object.hasOwn(result.result, 'value')) throw Error('relay observation failed');
                if (new URL(await relayPage.url()).origin !== relayOrigin) throw Error('relay origin changed');
                return result.result.value;
            };
            const auth = JSON.stringify('Bearer ' + sessionKey);
            const next = await evaluate(`const r=await fetch('/next',{headers:{Authorization:${auth}},cache:'no-store'}); return {status:r.status,body:r.status===200?await r.json():null};`);
            if (next.status === 204) return false;
            if (next.status !== 200 || !Number.isSafeInteger(next.body?.id) || next.body.id <= 0) throw Error('invalid relay request');
            let value, ok = true;
            try { value = await this.execute(next.body); } catch { ok = false; }
            let targetOrigin;
            try { targetOrigin = new URL(await this.page.url()).origin; } catch { targetOrigin = ''; }
            const payload = JSON.stringify({ id: next.body.id, targetOrigin, ok, ...(ok ? { value } : {}) });
            const status = await evaluate(`const r=await fetch('/reply',{method:'POST',headers:{Authorization:${auth},'Content-Type':'application/json'},body:${JSON.stringify(payload)}}); return r.status;`);
            if (status !== 204 || !ok) throw Error('relay operation failed');
            return true;
        } catch (error) { this.failed = true; throw error; }
    }

    async perform(task) {
        if (!task || typeof task.op !== 'string' || !task.args || typeof task.args !== 'object' || Array.isArray(task.args)) throw Error('invalid operation');
        const keys = Object.keys(task.args), op = task.op;
        if (!['state', 'profile', 'network-enable', 'network-events', 'network-body', 'control-state', 'control-http', 'reload'].includes(op)) throw Error('unsupported operation');
        if (['state', 'profile', 'network-enable', 'control-state', 'reload'].includes(op) && keys.length) throw Error('invalid operation arguments');
        if (op === 'network-events' && (keys.length !== 1 || !Number.isSafeInteger(task.args.afterSequence) || task.args.afterSequence < 0)) throw Error('invalid event cursor');
        if (op === 'network-body' && (keys.length !== 1 || typeof task.args.requestId !== 'string' || !/^[a-zA-Z0-9_.:-]{1,128}$/.test(task.args.requestId))) throw Error('invalid request identity');
        let control;
        if (op === 'control-http') {
            if (keys.length !== 2 || !keys.includes('url') || !keys.includes('options')) throw Error('invalid control arguments');
            control = validateControlRequest({ origin: this.origin, content: this.content, ...task.args });
        }
        await this.assertOrigin();
        let value;
        if (op === 'network-enable') {
            if (this.filter) throw Error('network capture already enabled');
            await this.cap.send('Network.enable'); await this.cap.send('Runtime.enable');
            const marker = await this.cap.readEvents();
            this.filter = new CuaEventFilter({ origin: this.origin, content: this.content, initialCursor: marker.cursor });
            value = { cursor: marker.cursor };
        } else if (op === 'network-events') {
            if (!this.filter || task.args.afterSequence !== this.filter.cursor) throw Error('event cursor mismatch');
            const raw = await this.cap.readEvents({ afterSequence: this.filter.rawCursor, methods: METHODS, limit: 1000, timeoutMs: 1000 });
            value = this.filter.consume(raw);
        } else if (op === 'network-body') {
            if (!this.filter || this.filter.selected.get(task.args.requestId) !== true) throw Error('request is not classified to the H5 origin');
            value = await this.cap.send('Network.getResponseBody', { requestId: task.args.requestId });
        } else if (op === 'reload') {
            if (typeof this.page.reload !== 'function') throw Error('CUA reload unavailable');
            await this.page.reload(); value = { origin: this.origin };
        } else {
            const expression = op === 'state' ? STATE(this.origin) : op === 'profile' ? PROFILE(this.origin) :
                op === 'control-state' ? CONTROL_STATE(this.origin) : CONTROL_HTTP(this.origin, control);
            const result = await this.cap.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
            if (result.exceptionDetails || !result.result || !Object.hasOwn(result.result, 'value')) throw Error('page observation failed');
            value = result.result.value;
            if (['state', 'control-state'].includes(op) && value?.origin !== this.origin) throw Error('H5 target origin changed');
        }
        await this.assertOrigin();
        return value;
    }
}
