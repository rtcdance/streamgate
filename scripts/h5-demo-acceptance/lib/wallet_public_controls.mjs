import { createHash } from 'node:crypto';
import { lstat, readFile } from 'node:fs/promises';

export const digest = bytes => createHash('sha256').update(bytes).digest('hex');
export const claims = token => JSON.parse(Buffer.from(token.split('.')[1], 'base64url'));
export const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

export async function readCompletedOracle(path, exitPath) {
    let terminal;
    try { terminal = await lstat(exitPath); }
    catch (error) { if (error.code === 'ENOENT') return null; throw error; }
    if (!terminal.isFile() || terminal.isSymbolicLink() || terminal.size > 16) throw Error('Original oracle exit must be a bounded regular file');
    if (terminal.size === 0) return null;
    const code = (await readFile(exitPath, 'utf8')).trim();
    if (!/^-?\d{1,3}$/.test(code)) throw Error('Original oracle exit is malformed');
    if (code !== '0') { const error = Error('Original oracle did not exit successfully'); error.blocked = code === '2'; throw error; }
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || info.size > 8 * 1024 * 1024) throw Error('Original oracle is not a bounded regular file');
    // The producer creates its report before it finishes writing JSON. Its
    // original process exit, rather than file existence, publishes completion.
    return readFile(path);
}

// Only original transport bytes enter this journal. No caller-supplied status,
// decoded-claim object or chain outcome is accepted as an observation.
export class PublicControlJournal {
    constructor(config, rpc, currentToken, http) {
        this.config = config; this.rpc = rpc; this.currentToken = currentToken; this.http = http;
        this.private = { credentials: {}, responses: {} };
        this.credentialRotations = [];
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
        const started = Date.now(); let token = await this.currentToken(), rotation;
        for (let attempt = 0; attempt < 2; attempt++) {
            const value = claims(token);
            if (value.chain_id !== 11155111 || String(value.wallet_address).toLowerCase() !== this.config.subject || value.exp * 1000 < Date.now() + 5000) throw Error('A live original Sepolia subject credential is required');
            const [http, grpc] = await Promise.all([
                this.httpEvent('profile', '/api/v1/auth/profile', token),
                this.grpcEvent('VerifyToken', 'verify', token),
            ]);
            if (http.code === 200) {
                const profile = JSON.parse(this.body(http));
                if (String(profile.wallet_address || profile.subject).toLowerCase() !== this.config.subject) throw Error('Transport responses do not verify the current subject');
            }
            if (grpc.code === 'OK' && JSON.parse(this.body(grpc)).data?.valid !== true) throw Error('Transport responses do not verify the current subject');
            if (http.code === 200 && grpc.code === 'OK') {
                if (rotation && Date.now() - started > 5000) throw Error('Credential rotation exceeded the original observation budget');
                if (rotation) rotation.after = { http, grpc };
                return { http, grpc };
            }
            if (attempt || ![200, 401].includes(http.code) || !['OK', 'Unauthenticated'].includes(grpc.code)) throw Error('Original wallet identity stopped verifying on a transport');
            // H5 alone renews the credential. A 401 can race its atomic JTI
            // rotation; retry once only for an observed replacement of the
            // SAME admission, within the existing five-second sample budget.
            const replacement = await this.replacement(token, started);
            rotation = { operation: 'identity', before: { http, grpc }, replacementSHA256: digest(replacement), after: null };
            this.credentialRotations.push(rotation); token = replacement;
        }
        throw Error('Original wallet identity observation did not complete');
    }

    async replacement(token, started) {
        const next = await this.currentToken(), previous = claims(token), current = claims(next);
        if (next === token || Date.now() - started > 5000 || !sameSession(previous, current) ||
            current.chain_id !== 11155111 || current.chain_id !== previous.chain_id ||
            current.sub !== previous.sub || current.wallet_address !== previous.wallet_address || current.account_kind !== previous.account_kind ||
            !current.jti || current.jti === previous.jti || current.exp * 1000 < Date.now() + 5000) throw Error('No bounded same-admission credential rotation was observed');
        return next;
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

    async mediaBaseline(identity, started = Date.now(), attempt = 0) {
        const token = this.private.credentials[identity.http.credentialSHA256];
        const manifest = await this.rpc.request('manifest', { token, content: this.config.content });
        if (manifest.value.code === 'PermissionDenied') return null;
        if (manifest.value.code === 'Unauthenticated') {
            const before = this.event('grpc', 'GetManifest', manifest.value.code, token, manifest.bytes, manifest.at);
            return this.rotatedBaseline(token, started, attempt, 'manifest', before);
        }
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
        if (media.code !== 200 || grpcMedia.code !== 'OK') {
            if ([200, 401].includes(media.code) && ['OK', 'Unauthenticated'].includes(grpcMedia.code)) {
                return this.rotatedBaseline(token, started, attempt, 'media', { media, grpcMedia });
            }
            throw Error('Original protected media baseline failed on a transport');
        }
        if (attempt && Date.now() - started > 5000) throw Error('Credential rotation exceeded the original observation budget');
        const bytes = this.body(media), remote = JSON.parse(this.body(grpcMedia)).data;
        if (!bytes.length || remote?.size !== bytes.length || remote.sha256 !== digest(bytes)) throw Error('HTTP and gRPC returned different media bytes');
        return { identity, media, grpcMedia, reference: { url: url.href, segment: segment.segment_id, capability } };
    }

    async rotatedBaseline(token, started, attempt, operation, before) {
        if (attempt) throw Error('Original media credential changed more than once during observation');
        const replacement = await this.replacement(token, started), identity = await this.live();
        if (this.private.credentials[identity.http.credentialSHA256] !== replacement) throw Error('Original media credential changed again during observation');
        const record = { operation, before, replacementSHA256: digest(replacement), after: identity };
        this.credentialRotations.push(record);
        return this.mediaBaseline(identity, started, 1);
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

export function playbackForWitness(samples, witness, safe) {
    const at = Math.max(witness.http.at, witness.grpc.at);
    const stopped = samples.find(sample => sample.at >= at && sample.at <= at + 5000 && sample.at <= safe + 70000 &&
        sample.paused === true && sample.state === 'authorization-ended');
    // Slow receipt/oracle work must select the ORIGINAL stop witness. Reading
    // today's stopped element cannot replace or restart its safe-time window.
    return stopped ? samples.filter(sample => sample.at <= stopped.at) : null;
}

export function sameSession(current, original) {
    return ['session_id', 'auth_epoch', 'restore_epoch', 'wallet_proof_id', 'wallet_session_exp']
        .every(key => current[key] === original[key]);
}
