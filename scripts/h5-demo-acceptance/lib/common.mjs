// Shared helpers for browser-based specs.

import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

export async function newContext(browser, options = {}) {
    return browser.newContext({
        viewport: { width: 1440, height: 900 },
        ignoreHTTPSErrors: true,
        ...options,
    });
}

// Capture console + network for the whole page lifetime and emit them to disk.
export function attachProbes(page, reportDir) {
    const consoleEvents = [];
    const networkEvents = [];
    const errors = [];
    const ignorable = (url) => url.includes('favicon.ico') || (url.includes('/health') && url.includes('localhost:28080'));

    page.on('console', (msg) => {
        consoleEvents.push({ type: msg.type(), text: msg.text(), location: msg.location() });
        if (msg.type() === 'error') {
            const text = msg.text();
            const url = msg.location()?.url || '';
            if (text.includes('favicon.ico')) return;
            // 503 noise during cold start is expected — flag but don't fail.
            if (text.includes('503') || text.includes('Service Unavailable')) return;
            // 404 on streaming manifest is expected when no real video was uploaded.
            if (text.includes('404') && url.includes('/streaming/') && url.includes('/manifest.m3u8')) return;
            errors.push({ kind: 'console', text });
        }
    });
    page.on('pageerror', (err) => {
        errors.push({ kind: 'pageerror', text: err.message, stack: err.stack });
    });
    page.on('requestfailed', (req) => {
        const failure = req.failure();
        networkEvents.push({ kind: 'requestfailed', url: req.url(), method: req.method(), error: failure?.errorText });
        if (!ignorable(req.url())) {
            errors.push({ kind: 'requestfailed', text: `${req.method()} ${req.url()} failed: ${failure?.errorText}` });
        }
    });
    page.on('response', (res) => {
        if (res.status() >= 500) {
            networkEvents.push({ kind: '5xx', url: res.url(), status: res.status() });
            if (!ignorable(res.url())) {
                errors.push({ kind: 'http-5xx', text: `${res.status()} ${res.url()}`, transient: true });
            }
        }
        // 404 on streaming manifest is expected in smoke tests (no real video uploaded).
        if (res.status() === 404 && res.url().includes('/streaming/') && res.url().includes('/manifest.m3u8')) {
            // do nothing — record to events only
        }
    });

    return {
        async flush() {
            await mkdir(reportDir, { recursive: true });
            await writeFile(join(reportDir, 'console.json'), JSON.stringify(consoleEvents, null, 2));
            await writeFile(join(reportDir, 'network.json'), JSON.stringify(networkEvents, null, 2));
        },
        errors,
    };
}

export async function snap(page, dir, name) {
    await mkdir(dir, { recursive: true });
    const path = join(dir, `${name}.png`);
    await page.screenshot({ path, fullPage: true });
    return path;
}

// Soft-wait for a DOM text-content change.
export async function waitForText(page, selector, predicate, { timeout = 10000, interval = 200 } = {}) {
    const start = Date.now();
    while (Date.now() - start < timeout) {
        const txt = (await page.locator(selector).textContent().catch(() => '')) || '';
        if (predicate(txt)) return txt;
        await new Promise((r) => setTimeout(r, interval));
    }
    throw new Error(`waitForText timeout: ${selector} did not match predicate within ${timeout}ms`);
}

// Hard-wait helper.
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Check object builder.
export function check(name, ok, detail = '') {
    return { name, ok, detail };
}
