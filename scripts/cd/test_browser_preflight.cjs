const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { test } = require('node:test');

const source = fs.readFileSync(path.join(__dirname, '../h5-demo-acceptance/run.mjs'), 'utf8');

// The exact sg-all-1019-f perf-smoke failure, verbatim from
// h5-demo-acceptance/reports/playback.log: the bundled chromium was missing
// its OS dependencies, the process died with exit 127, and the old
// tail -c 200 DETAIL reported the truncated tail of a SUCCESSFUL cleanup
// ("finished temporary directories cleanup" -> "shed temporary directories
// cleanup") as if cleanup were the cause.
const MISSING_LIBRARY = 'error while loading shared libraries: libnspr4.so: cannot open shared object file: No such file or directory';

const diagnose = async (probe) => {
    const { diagnoseProbe } = await import('../h5-demo-acceptance/lib/browser_preflight.mjs');
    return diagnoseProbe(probe);
};
test('a browser missing its OS dependencies is rejected naming the library', async () => {
    const verdict = await diagnose({ exitCode: 127, stderr: `/x/chrome-headless-shell: ${MISSING_LIBRARY}` });
    assert.equal(verdict.ok, false);
    assert.match(verdict.reason, /libnspr4\.so/);
    assert.match(verdict.reason, /OS dependencies/);
});

test('a browser that reports its version is launchable', async () => {
    assert.deepEqual(await diagnose({ exitCode: 0, stderr: '' }), { ok: true, reason: '' });
});

test('a missing executable points at the platform install, not a cleanup step', async () => {
    const verdict = await diagnose({ exitCode: null, stderr: '', error: Object.assign(new Error('spawn ENOENT'), { code: 'ENOENT' }) });
    assert.equal(verdict.ok, false);
    assert.match(verdict.reason, /playwright install chromium/);
});

test('an unexpected probe failure surfaces the diagnostic output', async () => {
    const verdict = await diagnose({ exitCode: 1, stderr: 'some unrelated boom' });
    assert.equal(verdict.ok, false);
    assert.match(verdict.reason, /unrelated boom/);
});

test('run.mjs still runs the preflight before chromium.launch', () => {
    // The preflight is load-bearing: without it the missing-dependency failure
    // surfaces inside launch, whose call log ends in a successful temporary
    // directories cleanup and misreads as a cleanup failure.
    const launchAt = source.indexOf('chromium.launch(');
    const preflightAt = source.indexOf('await assertLaunchable(');
    assert.ok(preflightAt >= 0, 'assertLaunchable call is missing from run.mjs');
    assert.ok(launchAt > preflightAt, 'the preflight must run before chromium.launch');
});
