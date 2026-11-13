// Reconstruct the phase from the private relay transcript. The transcript is
// sensitive and must never be printed by this verifier.
import { createHash } from 'node:crypto';
import { isDeepStrictEqual } from 'node:util';
import { CuaPageSession } from './wallet_cua_session.mjs';
import { WalletPhaseNetwork } from './wallet_phase_network.mjs';

const hash = value => createHash('sha256').update(value).digest('hex');
const require = (condition, reason) => { if (!condition) throw Error(reason); };
const same = (a, b) => isDeepStrictEqual(a, b);
const clean = value => JSON.parse(JSON.stringify(value));

export async function validateCuaTrace(raw, report, proofsRaw, config, ethers) {
    require(typeof raw === 'string' && Buffer.byteLength(raw) <= 128 * 1024 * 1024 && hash(raw) === report.cuaTraceSHA256, 'missing or changed private CUA trace');
    const trace = JSON.parse(raw);
    require(trace && trace.version === 1 && trace.candidateSHA === config.candidateSHA && trace.mode === config.mode && trace.phase === config.phase &&
        trace.origin === config.origin && (trace.content === (config.content || null) || (config.phase === 'negative' && trace.content === null)) &&
        Array.isArray(trace.calls) && Array.isArray(trace.requests) && trace.calls.length >= 3 && trace.requests.length > 0 &&
        trace.calls[0].kind === 'start' && trace.calls.at(-1).kind === 'finish', 'foreign or incomplete CUA trace');
    let cursor = 0, clock = 0, lastEnd = 0;
    const recordedStates = [], recordedProfiles = [];
    const rows = { challenges: [], admissions: [], refreshes: [], issues: [], media: [] };
    const network = new WalletPhaseNetwork({ config, report: rows, ethers });
    let call;
    const bridge = { async request(op, args) {
        require(cursor < call.end, 'CUA trace ended inside an operation');
        const item = trace.requests[cursor++];
        require(item && item.op === op && same(item.args, args) && Number.isSafeInteger(item.startedAt) && Number.isSafeInteger(item.finishedAt) &&
            item.startedAt >= call.startedAt && item.finishedAt >= item.startedAt && item.finishedAt <= call.finishedAt && item.startedAt >= clock &&
            Object.hasOwn(item, 'value'), 'CUA request sequence or timing differs');
        clock = item.finishedAt;
        return item.value;
    } };
    const session = new CuaPageSession({ bridge, origin: config.origin, content: trace.content,
        now: () => clock, onResponse: row => network.observe(row), onPageError: () => { throw Error('original CUA page error'); } });
    for (let index = 0; index < trace.calls.length; index++) {
        call = trace.calls[index];
        require(call && ['start', 'state', 'profile', 'finish'].includes(call.kind) && Number.isSafeInteger(call.first) && Number.isSafeInteger(call.end) &&
            call.first === cursor && call.end > call.first && call.end <= trace.requests.length && Number.isSafeInteger(call.startedAt) &&
            Number.isSafeInteger(call.finishedAt) && call.startedAt >= lastEnd && call.finishedAt >= call.startedAt &&
            (index === 0 ? call.kind === 'start' : call.kind !== 'start') && (index === trace.calls.length - 1 ? call.kind === 'finish' : call.kind !== 'finish'), 'invalid CUA call sequence or timing');
        clock = call.startedAt;
        const value = await session[call.kind]();
        require(cursor === call.end, 'unconsumed CUA observations');
        if (call.kind === 'state') recordedStates.push({ value, finishedAt: call.finishedAt });
        if (call.kind === 'profile') recordedProfiles.push(value);
        lastEnd = call.finishedAt;
    }
    require(cursor === trace.requests.length && same(rows, Object.fromEntries(Object.keys(rows).map(key => [key, report[key]]))), 'CUA network evidence differs from the phase report');
    if (config.phase !== 'negative') require(same(network.privateProofs, JSON.parse(proofsRaw)), 'CUA original SIWE proofs or credentials differ');
    const observations = [...report.gates.map(gate => gate.state), ...report.samples].sort((a, b) => a.at - b.at);
    let position = 0;
    for (const expected of observations) {
        let match = false;
        while (position < recordedStates.length) {
            const item = recordedStates[position++], state = item.value, token = state.token;
            const derived = clean({ tokenHash: token ? hash(token) : null,
                claims: token ? JSON.parse(Buffer.from(token.split('.')[1], 'base64url')) : null,
                identity: state.identity, wallet: state.wallet, time: state.time, frames: state.frames, paused: state.paused,
                playbackState: state.playbackState, videoError: state.videoError, authError: state.authError });
            const { at, ...observed } = expected;
            if (item.finishedAt <= at && at <= item.finishedAt + 5000 && same(derived, observed)) { match = true; break; }
        }
        require(match, 'CUA page state is not backed by a fresh original observation');
    }
    if (config.phase !== 'negative') {
        const id = (config.phase === 'contract' ? 'TN04' : 'TN03') + '_PROFILE_SUBJECT';
        const detail = report.checks.find(check => check.id === id)?.detail;
        require(recordedProfiles.some(profile => profile.status === detail?.status && profile.data?.wallet_address?.toLowerCase() === config.subject.toLowerCase()), 'CUA original profile response missing');
    }
}
