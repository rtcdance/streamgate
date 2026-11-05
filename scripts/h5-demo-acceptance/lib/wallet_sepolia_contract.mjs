export const PHASE_CHECKS = Object.freeze({
    negative: ['TN06_WRONG_NETWORK_SHOWN', 'TN06_SIGNATURE_REJECTED', 'TN06_NO_PAGE_ERRORS'],
    eoa: ['TN03_SIWE_ORIGIN_BINDING', 'TN03_EOA_SUBJECT', 'TN03_PROFILE_SUBJECT', 'TN03_MEDIA_DECODES', 'TN06_NO_PAGE_ERRORS'],
    contract: ['TN04_SIWE_ORIGIN_BINDING', 'TN04_SUBJECT_C', 'TN04_PROFILE_SUBJECT', 'TN04_SHORT_LEASE', 'TN04_MEDIA_RENEWS', 'TN06_NO_PAGE_ERRORS'],
});

export function settings(env) {
    const phase = env.W3_PHASE || 'eoa';
    if (!Object.hasOwn(PHASE_CHECKS, phase)) throw new Error('W3_PHASE must be eoa, contract or negative');
    if (!['monolith', 'api-gateway'].includes(env.W3_MODE)) throw new Error('W3_MODE is required');
    if (Number(env.W3_EXPECTED_CHAIN_ID || 11155111) !== 11155111) throw new Error('Ethereum Sepolia only; local chain 31337 is refused');
    if (!/^[0-9a-f]{40}$/.test(env.W3_CANDIDATE_SHA || '')) throw new Error('W3_CANDIDATE_SHA is required');
    const controller = evmAddress(env.W3_CONTROLLER);
    const subject = phase === 'contract' ? evmAddress(env.W3_SUBJECT_C) : controller;
    if (phase === 'contract' && controller === subject) throw new Error('C must differ from controller A');
    const inputBase = new URL(env.W3_H5_BASE);
    const origin = inputBase.origin;
    if (!['http:', 'https:'].includes(inputBase.protocol) || !loopback(inputBase.hostname) || inputBase.username || inputBase.password) throw new Error('The approved application is local-only');
    const transport = env.W3_BROWSER_TRANSPORT || 'cdp';
    if (!['cdp', 'cua'].includes(transport)) throw new Error('Unsupported browser transport');
    let cdp = null;
    if (transport === 'cdp') {
        cdp = new URL(env.W3_CDP_ENDPOINT);
        if (!['http:', 'https:', 'ws:', 'wss:'].includes(cdp.protocol) || !loopback(cdp.hostname) || cdp.username || cdp.password || !cdp.port) throw new Error('An explicit loopback CDP port for the existing MetaMask browser is required');
    } else if (env.W3_CDP_ENDPOINT) throw new Error('CUA transport must not use a browser-wide endpoint');
    const waitMs = Number(env.W3_USER_GATE_TIMEOUT_MS || 1800000);
    if (!Number.isInteger(waitMs) || waitMs < 1000 || waitMs > 1800000) throw new Error('User wait must be between one second and 30 minutes');
    if (phase !== 'negative' && !/^[0-9a-f-]{36}$/i.test(env.W3_CONTENT_ID || '')) throw new Error('W3_CONTENT_ID is required for the fixed fixture');
    if (!env.W3_REPORT_DIR) throw new Error('W3_REPORT_DIR is required');
    const nft = phase === 'negative' ? null : evmAddress(env.W3_NFT);
    return { phase, mode: env.W3_MODE, candidateSHA: env.W3_CANDIDATE_SHA, controller, subject, nft, origin, transport, cdp: cdp?.href || null, waitMs, content: env.W3_CONTENT_ID || null, output: env.W3_REPORT_DIR };
}

function loopback(host) { return ['localhost', '127.0.0.1', '[::1]'].includes(host); }
export function evmAddress(value) {
    if (typeof value !== 'string' || !/^0x[0-9a-fA-F]{40}$/.test(value) || /^0x0{40}$/i.test(value)) throw new Error('Nonzero EVM address required');
    return value.toLowerCase();
}

export function validateChallenge(challenge, config) {
    const lines = String(challenge.message || '').split('\n');
    const uri = lines.filter((line) => line.startsWith('URI: '));
    if (challenge.signing_type !== 'siwe' || challenge.chain_id !== 11155111 ||
        evmAddress(challenge.wallet || challenge.address) !== config.subject ||
        lines[0] !== `${new URL(config.origin).host} wants you to sign in with your Ethereum account:` ||
        evmAddress(lines[1]) !== config.subject || uri.length !== 1 || new URL(uri[0].slice(5)).origin !== config.origin ||
        lines.filter((line) => line.startsWith('Chain ID: ')).length !== 1 || !lines.includes('Chain ID: 11155111') ||
        !challenge.nonce || lines.filter((line) => line.startsWith('Nonce: ')).length !== 1 || !lines.includes(`Nonce: ${challenge.nonce}`)) throw new Error('The observed SIWE challenge does not bind this origin, subject and Sepolia');
    return true;
}

export function validIdentity(state, config, now = Date.now()) {
    try {
        const { identity: i, claims: c, wallet: w } = state;
        if (!i || !c || !w?.metaMask || i.isDemo ||
            evmAddress(i.controller) !== config.controller || evmAddress(i.subject) !== config.subject ||
            evmAddress(w.accounts?.[0]) !== config.controller || w.chainId !== 11155111 || i.chainId !== 11155111 ||
            evmAddress(c.sub) !== config.subject || evmAddress(c.wallet_address) !== config.subject || c.chain_id !== 11155111 ||
            !Number.isInteger(c.exp) || !Number.isInteger(c.iat) || c.exp * 1000 <= now || c.iat * 1000 > now + 5000) return false;
        const kind = config.phase === 'contract' ? 'erc1271' : 'eoa';
        if (i.kind !== kind || (c.account_kind || 'eoa') !== kind) return false;
        if (kind === 'erc1271' && (!(c.exp > c.iat && c.exp - c.iat <= 60) ||
            !Number.isInteger(c.wallet_session_exp) || c.wallet_session_exp < c.exp || c.wallet_session_exp > c.iat + 7200 ||
            !c.wallet_proof_id || typeof c.session_id !== 'string' || !c.session_id.startsWith('cw:'))) return false;
        return true;
    } catch { return false; }
}

export function phaseVerified(report) {
    const required = PHASE_CHECKS[report.phase];
    if (!required || !Array.isArray(report.errors) || report.errors.length || !Array.isArray(report.blockers) || report.blockers.length || !Array.isArray(report.checks)) return false;
    const ids = report.checks.map((check) => check.id);
    if (new Set(ids).size !== ids.length || report.checks.some((check) => check.ok !== true)) return false;
    return required.every((id) => ids.includes(id));
}

export function validCapability(claims, config) {
    try {
        return evmAddress(claims.wallet_address) === config.subject && claims.chain_id === 11155111 &&
            claims.content_id === config.content && evmAddress(claims.contract) === config.nft && claims.token_type === 'playback' &&
            Number.isInteger(claims.exp) && Number.isInteger(claims.iat) && 0 < claims.exp - claims.iat && claims.exp - claims.iat <= 60 &&
            (config.phase !== 'contract' || (claims.account_kind === 'erc1271' && Number.isInteger(claims.wallet_session_exp) && claims.exp <= claims.wallet_session_exp));
    } catch { return false; }
}

export function capabilityFromManifest(body, config) {
    const tokens = new Set();
    for (const match of body.matchAll(/(?:^|["\s])([^"\s]*playback_token=[^"\s]*)/gm)) {
        const url = new URL(match[1], config.origin);
        if (url.origin !== config.origin || !url.pathname.startsWith(`/api/v1/streaming/${config.content}/`)) throw new Error('Manifest issued a foreign media URL');
        tokens.add(url.searchParams.get('playback_token'));
    }
    if (tokens.size !== 1) throw new Error('Manifest issuance lacks exactly one capability');
    return [...tokens][0];
}
