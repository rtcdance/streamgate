#!/usr/bin/env node
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { createRequire } from 'node:module';
import { validatePhaseEvidence } from '../h5-demo-acceptance/lib/wallet_sepolia_evidence.mjs';
const ethers = createRequire(import.meta.url)('../../h5-demo/vendor/ethers.umd.min.js');
try {
    const args = process.argv.slice(2), values = {};
    for (let index = 0; index < args.length; index += 2) {
        if (!/^--[a-z-]+$/.test(args[index]) || !args[index + 1] || Object.hasOwn(values, args[index])) throw Error('explicit, unique option/value pairs required');
        values[args[index]] = args[index + 1];
    }
    for (const name of ['candidate', 'mode', 'phase', 'controller', 'subject', 'origin', 'directory', 'exit-file']) if (!values['--' + name]) throw Error('missing --' + name);
    const directory = values['--directory'];
    const reportRaw = readFileSync(join(directory, 'result.json'), 'utf8');
    if (Buffer.byteLength(reportRaw) > 4 * 1024 * 1024) throw Error('phase evidence exceeds the bounded input size');
    const originalExit = readFileSync(values['--exit-file'], 'utf8').trim();
    if (originalExit !== '0') throw Error('original wallet process did not exit zero');
    const proofs = values['--phase'] === 'negative' ? null : readFileSync(join(directory, 'siwe-proofs.private.json'), 'utf8');
    const config = { candidateSHA: values['--candidate'], mode: values['--mode'], phase: values['--phase'], controller: values['--controller'],
        subject: values['--subject'], origin: values['--origin'], nft: values['--nft'], content: values['--content'] };
    validatePhaseEvidence(JSON.parse(reportRaw), proofs, config, 0, ethers);
    console.log('SEPOLIA_WALLET_PHASE_VERIFIED: phase only; full acceptance=false');
} catch (error) {
    console.error((error.code === 'ENOENT' ? 'BLOCKED: ' : 'FAIL: ') + (error instanceof SyntaxError ? 'Malformed original phase evidence' : error.message));
    process.exitCode = error.code === 'ENOENT' ? 2 : 1;
}
