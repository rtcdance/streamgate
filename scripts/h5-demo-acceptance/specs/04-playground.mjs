// Spec 04: playground.html — Refresh the Web3 status dashboard (chains, RPCs, events, reorg).

import { waitForText, check } from '../lib/common.mjs';

const BACKEND_URL = 'http://localhost:28080';
const PAGE = 'http://localhost:18000/playground.html';

export default async function spec04Playground({ page, reportDir, snap, sleep }) {
    const checks = [];

    await page.goto(PAGE, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('h1', { timeout: 10000 });
    checks.push(check('playground-page-loaded', true));
    await snap('01-loaded');

    const backendInput = await page.locator('#backend-url').count();
    if (backendInput > 0) {
        await page.fill('#backend-url', BACKEND_URL).catch(() => null);
        await page.click('#save-backend').catch(() => null);
        await sleep(500);
    }

    const refreshBtn = await page.locator('#refresh-btn');
    if (await refreshBtn.count() > 0) {
        await refreshBtn.click();
        await waitForText(page, '#stat-chains, #status-msg', (t) => t.trim().length > 5, { timeout: 15000 }).catch(() => null);
        await sleep(2000);
    }

    for (const id of ['stat-chains', 'stat-rpcs', 'stat-events', 'stat-reorg']) {
        const txt = (await page.locator('#' + id).textContent().catch(() => '')) || '';
        checks.push(check(`playground-${id}-populated`, txt.trim().length > 0, `len=${txt.trim().length}`));
    }

    const rpcContent = (await page.locator('#rpc-content, #rpc-detail').first().textContent().catch(() => '')) || '';
    checks.push(check('playground-rpc-detail-populated', rpcContent.trim().length > 0, `len=${rpcContent.trim().length}`));

    const cfgTable = (await page.locator('#config-table').textContent().catch(() => '')) || '';
    checks.push(check('playground-config-table-populated', cfgTable.trim().length > 0, `len=${cfgTable.trim().length}`));

    await snap('02-after-refresh');
    const passed = checks.every((c) => c.ok);
    return { passed, checks };
}
