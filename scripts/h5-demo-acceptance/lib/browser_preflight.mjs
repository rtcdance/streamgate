import { spawn } from 'node:child_process';
import { accessSync, constants } from 'node:fs';

// sg-all-1019-f perf smoke: the install fallback deliberately retries without
// --with-deps when apt is unreachable, so the bundled chromium can sit in the
// cache WITHOUT its OS dependencies. The failure then surfaced at
// chromium.launch as "Target page, context or browser has been closed" with
// the only actionable line buried in the browser's stderr
// ("error while loading shared libraries: libnspr4.so"). The preflight asks
// the executable itself to print its version before any spec runs, so a
// browser that cannot start fails here with that root cause instead of
// inside a launch whose call log reads like a cleanup problem.
export function diagnoseProbe({ exitCode, stderr, error }) {
    if (error) {
        if (error.code === 'ENOENT') {
            return { ok: false, reason: 'the playwright chromium executable is missing; run `npx playwright install chromium` for this platform' };
        }
        return { ok: false, reason: `the playwright chromium probe failed to start: ${error.message}` };
    }
    if (exitCode === 0) {
        return { ok: true, reason: '' };
    }
    const missing = stderr.match(/error while loading shared libraries: ([^:]+)/);
    if (missing) {
        return {
            ok: false,
            reason: `the playwright chromium executable cannot load ${missing[1].trim()} — its OS dependencies are not installed in this environment; rerun the stage with the browser install enabled (the --with-deps path) or install the chromium OS packages manually`,
        };
    }
    return { ok: false, reason: `the playwright chromium executable exited ${exitCode} on a --version probe: ${stderr.slice(0, 200).trim() || 'no diagnostic output'}` };
}

export async function assertLaunchable(chromium) {
    const executablePath = chromium.executablePath();
    let probe;
    try {
        accessSync(executablePath, constants.X_OK);
        probe = await new Promise((resolve) => {
            let stderr = '';
            const child = spawn(executablePath, ['--version'], { stdio: ['ignore', 'ignore', 'pipe'] });
            const timer = setTimeout(() => {
                child.kill('SIGKILL');
                resolve({ exitCode: -1, stderr: '--version probe timed out' });
            }, 15000);
            child.stderr.on('data', (chunk) => { stderr += chunk; });
            child.on('error', (error) => { clearTimeout(timer); resolve({ exitCode: null, stderr: '', error }); });
            child.on('close', (exitCode) => { clearTimeout(timer); resolve({ exitCode, stderr }); });
        });
    } catch (error) {
        probe = { exitCode: null, stderr: '', error };
    }
    const verdict = diagnoseProbe(probe);
    if (!verdict.ok) {
        throw new Error(`playwright chromium preflight failed: ${verdict.reason} (executable: ${executablePath})`);
    }
    return executablePath;
}
