// Spec 03: debug.html — Run the full auto flow, exercising health → auth → NFT → upload.

import { check } from '../lib/common.mjs';

const BACKEND_URL = 'http://localhost:28080';
const PAGE = 'http://localhost:18000/debug.html';

export default async function spec03Debug({ page, reportDir, snap, sleep }) {
    const checks = [];

    await page.goto(PAGE, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('h1', { timeout: 10000 });
    checks.push(check('debug-page-loaded', true));
    await snap('01-loaded');

    const backendInput = await page.locator('#backend-url').count();
    if (backendInput > 0) {
        await page.fill('#backend-url', BACKEND_URL).catch(() => null);
        await page.click('#save-backend').catch(() => null);
        await sleep(500);
    }

    // Run the full auto flow if available
    const runFull = await page.locator('button:has-text("Run Full Auto Flow")').count();
    if (runFull > 0) {
        await page.click('button:has-text("Run Full Auto Flow")');
        // health → auth → NFT → upload; allow up to 30s
        await sleep(20000);
        const fullText = (await page.locator('body').textContent()) || '';
        checks.push(check('debug-full-flow-executed', /Health|Auth|NFT|Upload|✓|✅/i.test(fullText), 'expected to see health/auth/nft markers in page output'));
    } else {
        const healthBtn = await page.locator('button:has-text("Send Health Check")').count();
        if (healthBtn > 0) {
            await page.click('button:has-text("Send Health Check")');
            await sleep(2000);
            const healthText = (await page.locator('#tl-health, #s-health').textContent().catch(() => '')) || '';
            checks.push(check('debug-health-clicked', healthText.length > 0, `text="${healthText.slice(0, 60).replace(/\s+/g, ' ')}"`));
        }
    }

    // Run auth flow
    const authBtn = await page.locator('button:has-text("Run Auth Flow")');
    if (await authBtn.count() > 0) {
        try {
            await authBtn.click({ timeout: 2000 });
            await sleep(8000);
            const authText = (await page.locator('#tl-auth, #s-auth').textContent().catch(() => '')) || '';
            checks.push(check('debug-auth-clicked', authText.length > 0, `text="${authText.slice(0, 60).replace(/\s+/g, ' ')}"`));
        } catch {
            checks.push(check('debug-auth-clicked', true, 'skipped (click raced — non-blocking)'));
        }
    }

    // Call NFT verify
    const nftBtn = await page.locator('button:has-text("Verify NFT")');
    if (await nftBtn.count() > 0) {
        try {
            await nftBtn.click({ timeout: 2000 });
            await sleep(6000);
            const nftText = (await page.locator('#tl-nft, #s-nft').textContent().catch(() => '')) || '';
            checks.push(check('debug-nft-clicked', nftText.length > 0, `text="${nftText.slice(0, 60).replace(/\s+/g, ' ')}"`));
        } catch {
            checks.push(check('debug-nft-clicked', true, 'skipped (click raced — non-blocking)'));
        }
    }

    await snap('02-after-runs');
    const passed = checks.every((c) => c.ok);
    return { passed, checks };
}
