// Console reporter — prints per-spec pass/fail with a final summary.

const C = {
    reset: '\x1b[0m', bold: '\x1b[1m', dim: '\x1b[2m',
    red: '\x1b[31m', green: '\x1b[32m', yellow: '\x1b[33m', cyan: '\x1b[36m',
};

function badge(ok) { return ok ? `${C.green}✓${C.reset}` : `${C.red}✗${C.reset}`; }

export function reportSpec(spec) {
    const head = `${C.bold}${spec.name}${C.reset} ${badge(spec.passed)} ${C.dim}(${spec.duration_ms}ms)${C.reset}`;
    console.log(head);
    for (const c of spec.checks) {
        const line = `  ${badge(c.ok)} ${c.name}${c.detail ? ` ${C.dim}${c.detail}${C.reset}` : ''}`;
        console.log(line);
    }
    if (spec.errors?.length) {
        const hard = spec.errors.filter((e) => !e?.transient);
        const transient = spec.errors.filter((e) => e?.transient);
        if (hard.length) {
            console.log(`  ${C.red}! ${hard.length} runtime error(s):${C.reset}`);
            for (const e of hard.slice(0, 5)) {
                console.log(`    ${C.dim}- ${e.kind}: ${e.text}${C.reset}`);
            }
            if (hard.length > 5) console.log(`    ${C.dim}… ${hard.length - 5} more${C.reset}`);
        }
        if (transient.length) {
            console.log(`  ${C.dim}· ${transient.length} transient (e.g. cold-start 503) — ignored${C.reset}`);
        }
    }
}

export function reportSummary(results, totals) {
    const ok = results.filter((r) => r.passed).length;
    const total = results.length;
    const allOk = ok === total;
    console.log('');
    console.log(`${C.bold}========== H5 Demo Acceptance ==========${C.reset}`);
    console.log(`stack: ${badge(totals.stackOk)}  specs: ${ok}/${total} ${badge(allOk)}`);
    console.log(`${C.dim}total: ${totals.totalMs}ms, errors: ${totals.totalErrors}${C.reset}`);
    console.log('');
    if (!allOk) {
        console.log(`${C.red}FAILED${C.reset}`);
        process.exitCode = 1;
    } else {
        console.log(`${C.green}PASSED${C.reset}`);
    }
}
