// Shared helpers for browser-based specs.

import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { HEALTH_HOSTS } from './urls.mjs';

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
    // Last status seen per absolute URL. A 5xx that the page's own retry then
    // recovered from (the demo's healthCheck retries 502/503 twice, two seconds
    // apart) is a cold-start blip, not a runtime failure: the browser reported
    // one 502 on /health and two later 200s in the same page lifetime. It stays
    // a hard error when the URL's LAST status is still 5xx — that is the
    // mid-run outage the strict /health rule exists to catch.
    const lastStatus = new Map();
    const ignorable = (url) => url.includes('favicon.ico') || (url.includes('/health') && HEALTH_HOSTS.some((h) => url.includes(h)));

    page.on('console', (msg) => {
        consoleEvents.push({ type: msg.type(), text: msg.text(), location: msg.location() });
        if (msg.type() === 'error') {
            const text = msg.text();
            const url = msg.location()?.url || '';
            if (text.includes('favicon.ico')) return;
            // 503 noise during cold start is expected — but never for /health,
            // where a 503 means a dependency is down. Hiding it there would mask
            // a mid-run outage that the now-strict pre-check would otherwise catch.
            if ((text.includes('503') || text.includes('Service Unavailable')) && !url.includes('/health')) return;
            // 404 on streaming manifest is expected when no real video was uploaded.
            if (text.includes('404') && url.includes('/streaming/') && url.includes('/manifest.m3u8')) return;
            // 401/403 on streaming manifest are the EXPECTED fail-closed
            // denials (TRANSITION_PLAN A8/A9): unauthenticated => 401,
            // unowned/unruled => 403. Specs assert these explicitly (01-index
            // manifest-unauth-401 / manifest-holder differential); here they
            // are only marked transient so the expected denial stays visible
            // in the report without failing the spec.
            if ((text.includes('401') || text.includes('403')) && url.includes('/streaming/') && url.includes('/manifest.m3u8')) {
                errors.push({ kind: 'console', text, transient: true, note: 'expected fail-closed denial (asserted explicitly by the spec)' });
                return;
            }
            // Resolve the endpoint a 5xx console error is about, so a later
            // success for that same URL can reclassify it. The demo's own
            // "API Error [/health]: Error: HTTP 502" names the endpoint in
            // brackets while its location is api.js, so the bracketed path
            // wins; a bare resource-load message has no bracket and its
            // location IS the failed URL.
            let probeUrl = '';
            if (/\b5\d\d\b/.test(text)) {
                const bracket = text.match(/\[(\/[^\]]*)\]/);
                if (bracket) {
                    try {
                        probeUrl = new URL(bracket[1], page.url()).href;
                    } catch {
                        probeUrl = '';
                    }
                }
                if (!probeUrl && /^https?:\/\//.test(url)) probeUrl = url;
            }
            errors.push({ kind: 'console', text, probeUrl });
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
        lastStatus.set(res.url(), res.status());
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
        // A getter, not the raw array: run.mjs reads this AFTER the spec has
        // finished, which is the only point where "did the page recover?" can
        // be answered. The entry stays in the report either way, with the
        // recovery recorded in `note`.
        get errors() {
            return errors.map((e) => {
                if (e.transient || !e.probeUrl) return e;
                const last = lastStatus.get(e.probeUrl);
                if (last === undefined || last >= 500) return e;
                return { ...e, transient: true, note: 'recovered: ' + e.probeUrl + ' answered ' + last + ' after this failure' };
            });
        },
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
