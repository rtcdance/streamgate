// Spec 05: trace.html — Select a request scenario, run it, verify middleware chain + response populated.

import { waitForText, check } from '../lib/common.mjs';

const BACKEND_URL = 'http://localhost:28080';
const PAGE = 'http://localhost:18000/trace.html';

export default async function spec05Trace({ page, reportDir, snap, sleep }) {
    const checks = [];

    await page.goto(PAGE, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('h1', { timeout: 10000 });
    checks.push(check('trace-page-loaded', true));
    await snap('01-loaded');

    const backendInput = await page.locator('#backend-url').count();
    if (backendInput > 0) {
        await page.fill('#backend-url', BACKEND_URL).catch(() => null);
        await page.click('#save-backend').catch(() => null);
        await sleep(500);
    }

    const sel = await page.locator('#scenario-select');
    if (await sel.count() > 0) {
        const opts = await sel.locator('option').allTextContents().catch(() => []);
        if (opts.length > 0) {
            checks.push(check('trace-scenarios-listed', opts.length > 0, `count=${opts.length} first="${opts[0]}"`));
            const target = opts.find((o) => /health|auth|nft|verify|get/i.test(o)) || opts[0];
            await sel.selectOption({ label: target }).catch(() => null);
        }
    }

    const runBtn = await page.locator('button:has-text("Run")');
    if (await runBtn.count() > 0) {
        await runBtn.click();
        await waitForText(page, '#response-box, #middleware-chain', (t) => t.trim().length > 5, { timeout: 20000 }).catch(() => null);
        await sleep(2000);
    }

    const mHttp = (await page.locator('#m-http').textContent().catch(() => '')) || '';
    checks.push(check('trace-m-http-present', mHttp.trim().length > 0, `len=${mHttp.trim().length}`));

    const respBox = (await page.locator('#response-box').textContent().catch(() => '')) || '';
    checks.push(check('trace-response-populated', respBox.trim().length > 0, `len=${respBox.trim().length}`));

    const timing = (await page.locator('#timing-summary').textContent().catch(() => '')) || '';
    checks.push(check('trace-timing-summary-populated', timing.trim().length > 0, `len=${timing.trim().length}`));

    await snap('02-after-run');
    const passed = checks.every((c) => c.ok);
    return { passed, checks };
}
