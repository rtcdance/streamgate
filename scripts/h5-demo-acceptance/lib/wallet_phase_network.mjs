import { createHash } from 'node:crypto';
import { evmAddress, validateChallenge, validCapability, capabilityFromManifest } from './wallet_sepolia_contract.mjs';

const hash = value => createHash('sha256').update(value).digest('hex');
const claims = token => JSON.parse(Buffer.from(token.split('.')[1], 'base64url'));

// Both browser transports must feed the same evidence rules. The caller owns
// the origin-scoped transport, event time provenance and pending-operation drain.
export class WalletPhaseNetwork {
    constructor({ config, report, ethers }) {
        this.config = config; this.report = report; this.ethers = ethers;
        this.challenges = new Map(); this.admissions = new Map(); this.privateProofs = [];
        this.capabilities = new Set(); this.issuance = new Set(); this.challengeSequence = 0;
    }

    observe(record) {
        const url = new URL(record.url);
        const issuance = url.origin === this.config.origin && url.pathname === `/api/v1/streaming/${this.config.content}/manifest.m3u8` && !url.searchParams.has('playback_token');
        const operation = this.process(record, url);
        if (issuance) this.issuance.add(operation);
        return operation.finally(() => this.issuance.delete(operation));
    }

    async process(record, url) {
        const { config, report } = this;
        if (url.origin !== config.origin) return;
        if (!Number.isSafeInteger(record.at) || record.at <= 0 || !Buffer.isBuffer(record.body)) throw Error('original response time and bytes required');
        const { at, status } = record;
        if (url.pathname === '/api/v1/auth/challenge' && status === 200) {
            const challenge = JSON.parse(record.body.toString());
            this.challenges.set(challenge.challenge_id, challenge); this.challengeSequence++;
            report.challenges.push({ sequence: this.challengeSequence, at, origin: url.origin, status, chainId: challenge.chain_id,
                subject: challenge.wallet || challenge.address, signingType: challenge.signing_type, messageSHA256: hash(challenge.message || '') });
        } else if (url.pathname === '/api/v1/auth/login' && status === 200) {
            const request = JSON.parse(record.requestBody), result = JSON.parse(record.body.toString());
            const challenge = this.challenges.get(request.challenge_id);
            if (!challenge || evmAddress(request.wallet) !== config.subject) return;
            validateChallenge(challenge, config);
            let signer;
            try { signer = evmAddress(this.ethers.utils.verifyMessage(challenge.message, request.signature)); }
            catch { throw Error('Observed SIWE signature has invalid encoding'); }
            if (signer !== config.controller || request.chain_id !== 11155111) throw Error('Observed wallet signature/controller or chain mismatch');
            const admission = { at, controller: signer, subject: config.subject, chainId: 11155111, tokenHash: hash(result.token),
                challengeIdSHA256: hash(request.challenge_id), messageSHA256: hash(challenge.message), signatureSHA256: hash(request.signature), originBound: true };
            this.admissions.set(admission.tokenHash, admission); report.admissions.push(admission);
            this.privateProofs.push({ challenge, signature: request.signature, controller: signer, tokenHash: admission.tokenHash,
                credentials: [{ tokenHash: admission.tokenHash, token: result.token }] });
        } else if (url.pathname === '/api/v1/auth/refresh' && status === 200) {
            const request = JSON.parse(record.requestBody), result = JSON.parse(record.body.toString());
            const proof = this.privateProofs.find(item => item.credentials.some(credential => credential.tokenHash === hash(request.token)));
            if (!proof) return;
            proof.credentials.push({ tokenHash: hash(result.token), token: result.token });
            report.refreshes.push({ at, oldTokenHash: hash(request.token), newTokenHash: hash(result.token), oldClaims: claims(request.token), newClaims: claims(result.token) });
        } else if (url.pathname === `/api/v1/streaming/${config.content}/manifest.m3u8` || url.pathname.startsWith(`/api/v1/streaming/${config.content}/segment/`)) {
            const token = url.searchParams.get('playback_token');
            if (!token && url.pathname.endsWith('/manifest.m3u8') && status === 200) {
                const header = record.headers?.authorization;
                if (!header?.startsWith('Bearer ')) throw Error('Manifest issuance lacks its original access credential');
                const access = header.slice(7), body = record.body.toString();
                if (!this.privateProofs.some(item => item.credentials.some(credential => credential.tokenHash === hash(access)))) return;
                const capability = capabilityFromManifest(body, config);
                this.capabilities.add(hash(capability));
                report.issues.push({ at, status, accessTokenHash: hash(access), accessClaims: claims(access),
                    capabilityHash: hash(capability), capabilityClaims: claims(capability), manifestSHA256: hash(body) });
            }
            if (token) {
                await Promise.all([...this.issuance]);
                if (!this.capabilities.has(hash(token))) return;
                const value = claims(token);
                if (!validCapability(value, config)) throw Error('Observed foreign or unbounded media capability');
                const kind = url.pathname.includes('/segment/') ? 'segment' : 'manifest';
                const payload = status === 200 && kind === 'segment' ? record.body : null;
                report.media.push({ at, kind, status, tokenHash: hash(token), claims: value,
                    ...(payload ? { size: payload.length, sha256: hash(payload) } : {}) });
            }
        }
    }
}
