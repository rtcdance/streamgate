import { createHash } from 'node:crypto';

export const digest = bytes => createHash('sha256').update(bytes).digest('hex');
export const claims = token => JSON.parse(Buffer.from(token.split('.')[1], 'base64url'));
export const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

// Only original transport bytes enter this journal. No caller-supplied status,
// decoded-claim object or chain outcome is accepted as an observation.
export class PublicControlJournal {
    constructor(config, rpc, currentToken, http) {
        this.config = config; this.rpc = rpc; this.currentToken = currentToken; this.http = http;
        this.private = { credentials: {}, responses: {} };
    }

    event(transport, method, code, token, bytes, at) {
        const bodySHA256 = digest(bytes), credentialSHA256 = digest(token);
        this.private.credentials[credentialSHA256] = token;
        this.private.responses[bodySHA256] = Buffer.from(bytes).toString('base64');
        if (Object.keys(this.private.credentials).length > 2048 || Object.keys(this.private.responses).length > 10000) throw Error('Public observation exceeded its bounded evidence budget');
        return { transport, method, code, origin: this.config.origin, credentialSHA256, bodySHA256, at };
    }

    async httpEvent(method, url, token, options = {}, capability = false) {
        const target = new URL(url, this.config.origin);
        if (target.origin !== this.config.origin || target.username || target.password) throw Error('Public media/request escaped the configured application origin');
        const response = await this.http(target.href, {
            ...options, headers: { ...options.headers, ...(capability ? {} : { Authorization: `Bearer ${token}` }) },
        });
        return this.event('http', method, response.status, token, response.bytes, response.at);
    }

    async grpcEvent(method, command, token, args = {}, capability) {
        const response = await this.rpc.request(command, { ...args, token, ...(capability ? { capability } : {}) });
        return this.event('grpc', method, response.value.code, capability || token, response.bytes, response.at);
    }

    async live() {
        const token = await this.currentToken(), value = claims(token);
        if (value.chain_id !== 11155111 || String(value.wallet_address).toLowerCase() !== this.config.subject || value.exp * 1000 < Date.now() + 5000) throw Error('A live original Sepolia subject credential is required');
        const [http, grpc] = await Promise.all([
            this.httpEvent('profile', '/api/v1/auth/profile', token),
            this.grpcEvent('VerifyToken', 'verify', token),
        ]);
        if (http.code !== 200 || grpc.code !== 'OK') throw Error('Original wallet identity stopped verifying on a transport');
        const profile = JSON.parse(this.body(http)), verified = JSON.parse(this.body(grpc));
        if (String(profile.wallet_address || profile.subject).toLowerCase() !== this.config.subject || verified.data?.valid !== true) throw Error('Transport responses do not verify the current subject');
        return { http, grpc };
    }

    body(event) { return Buffer.from(this.private.responses[event.bodySHA256], 'base64'); }

    async anonymous() {
        const [http, grpc] = await Promise.all([
            this.http(new URL(`/api/v1/streaming/${this.config.content}/manifest.m3u8`, this.config.origin).href, {}),
            this.rpc.request('manifest', { content: this.config.content }),
        ]);
        const event = (transport, code, bytes, at) => {
            const bodySHA256 = digest(bytes); this.private.responses[bodySHA256] = Buffer.from(bytes).toString('base64');
            return { transport, method: transport === 'http' ? 'manifest' : 'GetManifest', code, at, bodySHA256, origin: this.config.origin };
        };
        return { http: event('http', http.status, http.bytes, http.at), grpc: event('grpc', grpc.value.code, grpc.bytes, grpc.at) };
    }

    async mediaBaseline(identity) {
        const token = this.private.credentials[identity.http.credentialSHA256];
        const manifest = await this.rpc.request('manifest', { token, content: this.config.content });
        if (manifest.value.code === 'PermissionDenied') return null;
        const segment = manifest.value.data?.segments?.[0];
        if (manifest.value.code !== 'OK' || !segment?.url || !segment.segment_id) throw Error('A real protected gRPC segment is required');
        const url = new URL(segment.url, this.config.origin), capability = url.searchParams.get('playback_token');
        if (!capability) throw Error('The actual manifest did not issue a playback capability');
        const value = claims(capability), access = claims(token);
        if (value.chain_id !== 11155111 || String(value.wallet_address).toLowerCase() !== this.config.subject ||
            value.content_id !== this.config.content || value.exp > access.exp || value.session_id !== access.session_id) throw Error('Playback capability lost its original subject/session/lease');
        const [media, grpcMedia] = await Promise.all([
            this.httpEvent('segment', url.href, capability, {}, true),
            this.grpcEvent('GetSegment', 'segment', token, { content: this.config.content, segment: segment.segment_id }, capability),
        ]);
        if (media.code !== 200 || grpcMedia.code !== 'OK') return null;
        const bytes = this.body(media), remote = JSON.parse(this.body(grpcMedia)).data;
        if (!bytes.length || remote?.size !== bytes.length || remote.sha256 !== digest(bytes)) throw Error('HTTP and gRPC returned different media bytes');
        return { identity, media, grpcMedia, reference: { url: url.href, segment: segment.segment_id, capability } };
    }

    async refusal(reference, token) {
        const [httpManifest, grpcManifest, httpMedia, grpcMedia] = await Promise.all([
            this.httpEvent('manifest', `/api/v1/streaming/${this.config.content}/manifest.m3u8`, token),
            this.grpcEvent('GetManifest', 'manifest', token, { content: this.config.content }),
            this.httpEvent('segment', reference.url, reference.capability, {}, true),
            this.grpcEvent('GetSegment', 'segment', token, { content: this.config.content, segment: reference.segment }, reference.capability),
        ]);
        return { httpManifest, grpcManifest, httpMedia, grpcMedia };
    }
}

export function chooseBaseline(baselines, safe) {
    // The oracle can finish after its FIRST safe response. Select from the
    // rolling original tape; never move the safe clock to the file-read time.
    return baselines.findLast(value => {
        const times = [value.identity.http.at, value.identity.grpc.at, value.media.at, value.grpcMedia.at];
        return times.every(at => at <= safe && at >= safe - 5000) &&
            claims(value.reference.capability).exp * 1000 > safe + 5000;
    });
}

export function sameSession(current, original) {
    return ['session_id', 'auth_epoch', 'restore_epoch', 'wallet_proof_id', 'wallet_session_exp']
        .every(key => current[key] === original[key]);
}
