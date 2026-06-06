// Main entry point — runs stack pre-check + all h5-demo HTML specs in sequence.
// Usage: node run.mjs [--only=01-index,02-flow] [--report-dir=reports/2026-06-06]

import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { checkStack } from './lib/stack.mjs';
import { newContext, attachProbes, snap, sleep } from './lib/common.mjs';
import { reportSpec, reportSummary } from './lib/reporter.mjs';

import specIndex from './specs/01-index.mjs';
import specFlow from './specs/02-flow.mjs';
import specDebug from './specs/03-debug.mjs';
import specPlayground from './specs/04-playground.mjs';
import specTrace from './specs/05-trace.mjs';

const args = parseArgs(process.argv.slice(2));
const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
const reportRoot = args['report-dir'] || join('reports', ts);
await mkdir(reportRoot, { recursive: true });

const allSpecs = [
    { name: '01-index', fn: specIndex },
    { name: '02-flow', fn: specFlow },
    { name: '03-debug', fn: specDebug },
    { name: '04-playground', fn: specPlayground },
    { name: '05-trace', fn: specTrace },
];

const only = args.only ? args.only.split(',').map((s) => s.trim()) : null;
const specs = only ? allSpecs.filter((s) => only.includes(s.name)) : allSpecs;

const results = [];
let stackResult = null;

console.log(`${'\x1b[36m'}== H5 Demo Acceptance (reports → ${reportRoot}) ==${'\x1b[0m'}`);

// 1. Stack pre-check
console.log('\n--- stack pre-check ---');
const stackStart = Date.now();
stackResult = await checkStack();
stackResult.duration_ms = Date.now() - stackStart;
reportSpec(stackResult);
results.push(stackResult);
if (!stackResult.passed) {
    console.log('\n\x1b[31mStack unhealthy — aborting browser specs.\x1b[0m');
    await writeReport(reportRoot, results);
    reportSummary(results, totalsOf(results));
    process.exit(1);
}

// 2. Browser specs
const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
    for (const s of specs) {
        console.log(`\n--- spec: ${s.name} ---`);
        const ctx = await newContext(browser);
        const page = await ctx.newPage();
        const reportDir = join(reportRoot, s.name);
        const probes = attachProbes(page, reportDir);
        const start = Date.now();
        let result;
        try {
            result = await s.fn({ page, reportDir, snap: (n) => snap(page, reportDir, n), sleep });
            result.name = s.name;
            result.duration_ms = Date.now() - start;
        } catch (e) {
            result = { name: s.name, passed: false, checks: [{ name: 'spec-throw', ok: false, detail: e.message }], errors: [{ kind: 'spec-throw', text: e.message, stack: e.stack }], duration_ms: Date.now() - start };
        }
        result.errors = [...(result.errors || []), ...probes.errors];
        // Spec only fails on hard (non-transient) errors.
        const hardErrors = result.errors.filter((e) => !e?.transient);
        result.passed = result.passed && hardErrors.length === 0;
        await probes.flush();
        await writeFile(join(reportDir, 'result.json'), JSON.stringify(result, null, 2));
        reportSpec(result);
        results.push(result);
        await ctx.close();
    }
} finally {
    await browser.close();
}

await writeReport(reportRoot, results);
reportSummary(results, totalsOf(results));

function parseArgs(argv) {
    const o = {};
    for (const a of argv) {
        if (a.startsWith('--only=')) o.only = a.slice(7);
        else if (a.startsWith('--report-dir=')) o['report-dir'] = a.slice(13);
    }
    return o;
}

async function writeReport(dir, results) {
    const summary = {
        timestamp: new Date().toISOString(),
        stack_passed: results[0]?.passed || false,
        specs: results.slice(1).map((r) => ({ name: r.name, passed: r.passed, errors: r.errors?.length || 0, duration_ms: r.duration_ms })),
    };
    await writeFile(join(dir, 'summary.json'), JSON.stringify(summary, null, 2));
}

function totalsOf(results) {
    const totalMs = results.reduce((a, r) => a + (r.duration_ms || 0), 0);
    const totalErrors = results.reduce((a, r) => a + (r.errors?.length || 0), 0);
    const stackOk = results[0]?.passed || false;
    return { totalMs, totalErrors, stackOk };
}
