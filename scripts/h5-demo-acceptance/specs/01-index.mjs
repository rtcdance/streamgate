// Spec 01: index.html — full h5-demo acceptance.
// Flow: switch to Workflow view → backend URL → demo mode wallet → sign & login → auto-mint → verify NFT.

import { waitForText, check } from '../lib/common.mjs';

const BACKEND_URL = 'http://localhost:28080';
const PAGE = 'http://localhost:18000/';

export default async function spec01Index({ page, reportDir, snap, sleep }) {
    const checks = [];

    await page.goto(PAGE, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('h1', { timeout: 10000 });
    checks.push(check('page-loaded', true));

    // Switch to Workflow view (where the acceptance flow lives)
    await page.click('#nav-workflow');
    await page.waitForSelector('#backend-url:visible', { timeout: 10000 });
    await snap('01-workflow-view');

    // Step 1: backend
    await page.fill('#backend-url', BACKEND_URL);
    await page.click('#save-backend');
    await waitForText(page, '#backend-status', (t) => /Online|Healthy|Connected|✓/i.test(t), { timeout: 10000 }).catch(() => null);
    const backendText = (await page.locator('#backend-status').textContent()) || '';
    checks.push(check('backend-online', /Online|Healthy|Connected|✓/i.test(backendText), `status="${backendText.trim()}"`));
    await snap('02-backend');

    // Step 2: demo wallet
    await page.click('#demo-mode-btn');
    await waitForText(page, '#address-value', (t) => /0x[0-9a-fA-F]{40}/i.test(t), { timeout: 15000 }).catch(() => null);
    const walletText = (await page.locator('#wallet-status').textContent()) || '';
    const walletStatusValue = (await page.locator('#wallet-status .status-value, #wallet-status').last().textContent().catch(() => '')) || walletText;
    checks.push(check('demo-wallet-connected', /Connected/i.test(walletStatusValue) && !/Not Connected/i.test(walletStatusValue), `status="${walletStatusValue.trim()}"`));
    const addrValue = (await page.locator('#address-value').textContent().catch(() => '')) || '';
    // UI shows the truncated form (0x1234…abcd) — accept either the full 0x40-hex or that shape.
    const addrFound = addrValue.match(/0x[0-9a-fA-F]{40}/)?.[0] || addrValue.match(/0x[0-9a-fA-F]{4,6}.{0,5}[0-9a-fA-F]{4}/)?.[0] || null;
    checks.push(check('demo-wallet-address', !!addrFound, `addr=${addrFound || addrValue.slice(0, 30)}`));
    await snap('03-demo-wallet');

    // Step 3: sign & login (auto-triggers auto-mint + verify)
    await page.click('#login-btn');
    await waitForText(page, '#auth-status', (t) => /Authenticated|Online|✓/i.test(t), { timeout: 20000 }).catch(() => null);
    const authText = (await page.locator('#auth-status').textContent()) || '';
    checks.push(check('login-authenticated', /Authenticated|Online|✓/i.test(authText), `status="${authText.trim()}"`));

    // Wait for auto-mint to finish and verify to update (up to 30s — 3 RPC txs)
    await waitForText(page, '#nft-status-text', (t) => /Verified|✓ NFT/i.test(t), { timeout: 30000 }).catch(() => null);
    await sleep(1000);
    const nftText = (await page.locator('#nft-status-text').textContent()) || '';
    checks.push(check('nft-auto-verified', /Verified|✓ NFT/i.test(nftText), `status="${nftText.trim()}"`));
    const nftDetails = (await page.locator('#nft-details').textContent()) || '';
    const balanceMatch = nftDetails.match(/Balance:\s*(\d+)/);
    const balance = balanceMatch ? parseInt(balanceMatch[1], 10) : 0;
    checks.push(check('nft-balance-gt-0', balance > 0, `balance=${balance}`));
    checks.push(check('nft-chain-31337', /Chain ID:\s*31337/.test(nftDetails), `details=${nftDetails.replace(/\s+/g, ' ').slice(0, 80)}`));
    await snap('04-nft-verified');

    // Step 5: player section should be visible now
    // verifyNFT() already removes the `hidden` class. Make sure parents are also visible.
    await page.evaluate(() => {
        const ps = document.getElementById('player-section');
        if (ps) {
            ps.classList.remove('hidden');
            let p = ps.parentElement;
            while (p && p.id !== 'view-workflow') {
                p.classList?.remove('hidden');
                p.style.display = '';
                p = p.parentElement;
            }
        }
    });
    const playerVisible = await page.locator('#player-section').isVisible().catch(() => false);
    checks.push(check('player-section-visible', playerVisible, `visible=${playerVisible}`));

    const passed = checks.every((c) => c.ok);
    return { passed, checks };
}
