// Independent, read-only phase gate. Boolean PASS fields cannot substitute for
// the original SIWE signature, live identities or decoded-media observations.
import { createHash } from 'node:crypto';
import { PHASE_CHECKS, evmAddress, phaseVerified, validIdentity, validCapability, validateChallenge } from './wallet_sepolia_contract.mjs';
const hash = value => createHash('sha256').update(value).digest('hex');
const require = (ok, reason) => { if (!ok) throw Error(reason); };
const digest = value => { require(typeof value === 'string' && /^[0-9a-f]{64}$/.test(value), 'invalid evidence digest'); return value; };
const time = value => { require(Number.isSafeInteger(value) && value > 0, 'missing original observation time'); return value; };
const accessPurpose = claims => claims.token_type === 'access' && claims.iss === 'streamgate' && Array.isArray(claims.aud) && claims.aud.includes('streamgate-api');
const equalClaims = (left, right) => JSON.stringify(Object.fromEntries(Object.entries(left).sort())) === JSON.stringify(Object.fromEntries(Object.entries(right).sort()));

export function validatePhaseEvidence(report, proofsRaw, config, originalExit, ethers) {
    require(/^[0-9a-f]{40}$/.test(config.candidateSHA) && ['monolith', 'api-gateway'].includes(config.mode) && Object.hasOwn(PHASE_CHECKS, config.phase), 'explicit candidate, mode and phase are required');
    config = { ...config, controller: evmAddress(config.controller), subject: evmAddress(config.subject) };
    const origin = new URL(config.origin);
    require(['http:', 'https:'].includes(origin.protocol) && ['localhost', '127.0.0.1', '[::1]'].includes(origin.hostname) && !origin.username && !origin.password && origin.origin === config.origin, 'explicit loopback H5 origin required');
    if (config.phase !== 'negative') { config.nft = evmAddress(config.nft); require(/^[0-9a-f-]{36}$/i.test(config.content || ''), 'explicit content fixture required'); }
    require(config.phase === 'contract' ? config.subject !== config.controller : config.subject === config.controller, 'wrong phase subject');
    require(originalExit === 0 && report.component === 'SEPOLIA_REAL_WALLET_BROWSER_PHASE' && report.testAdapter === 'EXISTING_METAMASK_CDP_READ_ONLY' &&
        report.candidateSHA === config.candidateSHA && report.mode === config.mode && report.phase === config.phase &&
        report.publicWalletSignoff === false && report.completeAcceptance === false && report.status === 'WALLET_PHASE_OBSERVED' && report.phaseVerified === true && phaseVerified(report), 'missing, failed, foreign or simulated wallet phase');
    require(report.checks.length === PHASE_CHECKS[config.phase].length, 'phase check set differs from the fixed contract');
    const checks = Object.fromEntries(report.checks.map(check => [check.id, check.detail || {}]));
    require(Array.isArray(report.gates) && Array.isArray(report.challenges) && Array.isArray(report.samples) && Array.isArray(report.media) && Array.isArray(report.admissions) && Array.isArray(report.issues) && Array.isArray(report.refreshes), 'missing original phase observations');
    const gates = Object.fromEntries(report.gates.map(gate => [gate.id, gate]));
    require(Object.keys(gates).length === report.gates.length, 'duplicate user gate');
    for (const gate of report.gates) {
        require(time(gate.finishedAt) >= time(gate.startedAt) && gate.finishedAt - gate.startedAt === gate.elapsedMs && gate.elapsedMs <= 1800000 &&
            Number.isInteger(gate.challengeSequenceBefore) && Number.isInteger(gate.challengeSequenceAfter) && gate.challengeSequenceAfter >= gate.challengeSequenceBefore &&
            time(gate.state?.at) >= gate.finishedAt && gate.state.at <= gate.finishedAt + 5000, 'invalid original user-gate timing');
    }
    for (const [index, challenge] of report.challenges.entries()) {
        require(challenge.sequence === index + 1 && challenge.origin === config.origin && challenge.status === 200 && challenge.chainId === 11155111 && challenge.signingType === 'siwe', 'foreign or missing actual challenge response');
        evmAddress(challenge.subject); digest(challenge.messageSHA256); time(challenge.at);
    }
    if (config.phase === 'negative') {
        require(report.gates.length === 2 && report.admissions.length === 0 && report.samples.length === 0 && report.media.length === 0 && report.issues.length === 0 && report.refreshes.length === 0, 'negative phase contains a half login or unrequested media');
        for (const [id, error, wrong] of [['TN06_WRONG_NETWORK', 'wrong-network', true], ['TN06_USER_CANCEL', 'signature-rejected', false]]) {
            const gate = gates[id], state = gate?.state;
            require(state && state.authError === error && state.tokenHash === null && state.identity === null && state.claims === null && state.wallet?.metaMask === true &&
                evmAddress(state.wallet.accounts?.[0]) === config.controller && Number.isInteger(state.wallet.chainId) && (wrong ? state.wallet.chainId !== 11155111 : state.wallet.chainId === 11155111), 'negative phase did not observe the expected real wallet/UI state');
            require(gate.challengeSequenceAfter > gate.challengeSequenceBefore && report.challenges.some(challenge => challenge.sequence > gate.challengeSequenceBefore &&
                challenge.sequence <= gate.challengeSequenceAfter && challenge.at >= gate.startedAt && challenge.at <= gate.finishedAt), 'negative gate reused a stale or missing challenge');
        }
        require(gates.TN06_USER_CANCEL.startedAt >= gates.TN06_WRONG_NETWORK.finishedAt, 'wrong-network and user-rejection gates overlap');
        return { scope: 'SEPOLIA_WALLET_PHASE_ONLY', phaseVerified: true, completeAcceptance: false, walletControlVerified: false };
    }
    require(typeof proofsRaw === 'string' && hash(proofsRaw) === digest(report.privateProofSHA256), 'original private SIWE proof file is missing or changed');
    const proofs = JSON.parse(proofsRaw);
    require(Array.isArray(proofs) && proofs.length > 0 && proofs.length === report.admissions.length, 'missing original fresh wallet signatures');
    const admissions = new Map(), credentialClaims = new Map();
    for (const proof of proofs) {
        validateChallenge(proof.challenge, config);
        let signer;
        try { signer = evmAddress(ethers.utils.verifyMessage(proof.challenge.message, proof.signature)); }
        catch { throw Error('Original SIWE signature has invalid encoding'); }
        require(signer === config.controller && evmAddress(proof.controller) === signer, 'SIWE signature is not from controller A');
        const tokenHash = digest(proof.tokenHash);
        require(!admissions.has(tokenHash), 'duplicate wallet admission');
        const admission = report.admissions.find(item => item.tokenHash === tokenHash);
        require(admission && evmAddress(admission.controller) === signer && evmAddress(admission.subject) === config.subject && admission.chainId === 11155111 && admission.originBound === true &&
            admission.challengeIdSHA256 === hash(proof.challenge.challenge_id) && admission.messageSHA256 === hash(proof.challenge.message) && admission.signatureSHA256 === hash(proof.signature), 'admission differs from the original SIWE proof');
        const lines = proof.challenge.message.split('\n');
        const issued = Date.parse(proof.challenge.issued_at), expires = Date.parse(proof.challenge.expires_at);
        require(Number.isFinite(issued) && Number.isFinite(expires) && issued <= admission.at + 5000 && admission.at < expires && expires - issued > 0 && expires - issued <= 300000 &&
            lines.filter(line => line.startsWith('Issued At: ')).length === 1 && lines.includes('Issued At: ' + proof.challenge.issued_at) &&
            lines.filter(line => line.startsWith('Expiration Time: ')).length === 1 && lines.includes('Expiration Time: ' + proof.challenge.expires_at) &&
            lines.filter(line => line.startsWith('Version: ')).length === 1 && lines.includes('Version: 1') &&
            lines.includes('URI: ' + config.origin + '/login'), 'SIWE admission has expired, duplicate or inconsistent lifetime/origin fields');
        require(report.challenges.some(challenge => challenge.messageSHA256 === admission.messageSHA256 && challenge.at <= time(admission.at)), 'signature lacks an actual preceding challenge response');
        require(Array.isArray(proof.credentials) && proof.credentials.length > 0, 'original access credentials are absent from private evidence');
        for (const credential of proof.credentials) {
            require(typeof credential.token === 'string' && hash(credential.token) === digest(credential.tokenHash) && !credentialClaims.has(credential.tokenHash), 'private credential bytes differ from recorded digest');
            const parts = credential.token.split('.');
            require(parts.length === 3 && /^[A-Za-z0-9_-]{43}$/.test(parts[2]) && JSON.parse(Buffer.from(parts[0], 'base64url')).alg === 'HS256', 'wrong credential format or signing algorithm');
            credentialClaims.set(credential.tokenHash, JSON.parse(Buffer.from(parts[1], 'base64url')));
        }
        require(credentialClaims.has(tokenHash), 'original admitted token is missing');
        admissions.set(tokenHash, admission);
    }
    const prefix = config.phase === 'contract' ? 'TN04' : 'TN03';
    const admitted = gates[`${prefix}_SIGN_SIWE`]?.state, playing = gates[`${prefix}_PLAY`]?.state;
    require(report.gates.length === 2 && admitted && playing && accessPurpose(admitted.claims) && validIdentity(admitted, config, admitted.at) && admissions.has(admitted.tokenHash) &&
        equalClaims(admitted.claims, credentialClaims.get(admitted.tokenHash)) &&
        validIdentity(playing, config, playing.at) && gates[`${prefix}_PLAY`].startedAt >= gates[`${prefix}_SIGN_SIWE`].finishedAt, 'fresh admission or playback gate is missing');
    const profile = checks[`${prefix}_PROFILE_SUBJECT`];
    require(profile.status === 200 && evmAddress(profile.subject) === config.subject && profile.accessTokenHash === admitted.tokenHash, 'profile response did not confirm the admitted subject');
    require(report.samples.length >= 2, 'missing decoded playback samples');
    const absolute = playing.claims.wallet_session_exp;
    const admissionId = playing.claims.session_id;
    const proofId = playing.claims.wallet_proof_id;
    let previous = playing.at, previousTime = playing.time, previousFrames = playing.frames;
    const access = new Set();
    for (const sample of report.samples) {
        require(time(sample.at) >= previous && sample.at - previous <= 5000 && credentialClaims.has(sample.tokenHash) && equalClaims(sample.claims, credentialClaims.get(sample.tokenHash)) && accessPurpose(sample.claims) && validIdentity(sample, config, sample.at) && sample.paused === false && !sample.videoError &&
            Number.isFinite(sample.time) && sample.time >= previousTime && Number.isInteger(sample.frames) && sample.frames >= previousFrames && sample.frames > 0, 'playback sample uses an invalid identity, observation gap or undecoded video');
        if (config.phase === 'contract') require(sample.claims.wallet_session_exp === absolute && sample.claims.session_id === admissionId && sample.claims.wallet_proof_id === proofId, 'playback switched admission or extended absolute lifetime');
        access.add(digest(sample.tokenHash)); previous = sample.at; previousTime = sample.time; previousFrames = sample.frames;
    }
    const last = report.samples.at(-1), contract = config.phase === 'contract';
    require(last.at - playing.at >= (contract ? 70000 : 3500) && last.frames > playing.frames && last.time - playing.time >= (contract ? 65 : 2), 'decoded media did not advance for the required duration');
    const issued = new Map(), renewed = new Set(admissions.keys());
    for (const renewal of [...report.refreshes].sort((a, b) => a.at - b.at)) {
        time(renewal.at); digest(renewal.oldTokenHash); digest(renewal.newTokenHash);
        require(renewed.has(renewal.oldTokenHash) && !renewed.has(renewal.newTokenHash) && accessPurpose(renewal.oldClaims) && accessPurpose(renewal.newClaims) &&
            credentialClaims.has(renewal.newTokenHash) && equalClaims(renewal.oldClaims, credentialClaims.get(renewal.oldTokenHash)) && equalClaims(renewal.newClaims, credentialClaims.get(renewal.newTokenHash)) &&
            validIdentity({ ...admitted, claims: renewal.newClaims }, config, renewal.at) && renewal.newClaims.session_id === admissionId &&
            (!contract || (renewal.newClaims.wallet_proof_id === proofId && renewal.newClaims.wallet_session_exp === absolute)), 'renewal did not preserve the original admission');
        renewed.add(renewal.newTokenHash);
    }
    require([...access].every(tokenHash => renewed.has(tokenHash)), 'sample contains an unobserved login or refresh result');
    for (const issue of report.issues) {
        time(issue.at); digest(issue.accessTokenHash); digest(issue.capabilityHash); digest(issue.manifestSHA256);
        const cap = issue.capabilityClaims, claim = issue.accessClaims;
        require(issue.status === 200 && renewed.has(issue.accessTokenHash) && equalClaims(claim, credentialClaims.get(issue.accessTokenHash)) && accessPurpose(claim) && validIdentity({ ...admitted, claims: claim }, config, issue.at) && validCapability(cap, config) &&
            cap.iss === 'streamgate' && Array.isArray(cap.aud) && cap.aud.includes('streamgate-playback') && cap.exp <= claim.exp && cap.iat <= issue.at / 1000 + 5 &&
            cap.session_id === claim.session_id && cap.auth_epoch === claim.auth_epoch && cap.restore_epoch === claim.restore_epoch &&
            (!contract || (cap.wallet_proof_id === proofId && cap.wallet_session_exp === absolute)), 'capability exceeds or differs from the credential actually presented for issuance');
        issued.set(issue.capabilityHash, { claims: cap, at: issue.at });
    }
    const capabilities = new Set(); let bytes = 0;
    for (const media of report.media) {
        require(time(media.at) > 0 && ['segment', 'manifest'].includes(media.kind) && validCapability(media.claims, config) && Number.isInteger(media.status), 'foreign or unbounded media capability');
        digest(media.tokenHash);
        require(issued.has(media.tokenHash) && media.at >= issued.get(media.tokenHash).at && equalClaims(issued.get(media.tokenHash).claims, media.claims), 'media used an unobserved, future or changed capability');
        if (media.status === 200 && media.kind === 'segment' && media.at >= (contract ? playing.at : admitted.at) && media.at <= last.at) {
            require(Number.isSafeInteger(media.size) && media.size > 0, 'successful segment has no actual response bytes'); digest(media.sha256);
            capabilities.add(media.tokenHash); bytes += media.size;
        }
    }
    require(bytes > 0 && (!contract || (access.size >= 3 && capabilities.size >= 2)), 'missing actual media bytes, renewed access or renewed capabilities');
    return { scope: 'SEPOLIA_WALLET_PHASE_ONLY', phaseVerified: true, completeAcceptance: false, walletControlVerified: true, accessTokens: access.size, capabilities: capabilities.size };
}
