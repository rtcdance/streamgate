// Tool integration only: an owned browser with NO wallet, no RPC and no keys.
// A missing extension must be BLOCKED; detaching must preserve existing tabs.
import test from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createServer } from 'node:http';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
const execute = promisify(execFile);

test('observer blocks without MetaMask, preserves the existing browser and never claims public approval', { timeout: 40000 }, async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sg-wallet-observer-'));
    const server = createServer((request, response) => { response.end(request.url === '/testnet/' ? '<!doctype html><title>Owned transaction panel</title>' : '<!doctype html><title>Owned observer fixture</title><video id="video-player"></video>'); });
    let context;
    try {
        await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
        const origin = `http://127.0.0.1:${server.address().port}`;
        context = await chromium.launchPersistentContext(join(directory, 'profile'), { headless: true, args: ['--remote-debugging-port=0'] });
        const page = context.pages()[0]; await page.goto(origin);
        const unrelated = await context.newPage();
        await unrelated.goto(origin + '/testnet/');
        const [port] = (await readFile(join(directory, 'profile/DevToolsActivePort'), 'utf8')).split('\n');
        const env = { ...process.env, W3_PHASE: 'negative', W3_MODE: 'monolith', W3_CANDIDATE_SHA: 'a'.repeat(40), W3_H5_BASE: origin,
            W3_CDP_ENDPOINT: `http://127.0.0.1:${port}`, W3_CONTROLLER: '0x' + '1'.repeat(40), W3_REPORT_DIR: join(directory, 'report') };
        let code = 0;
        try { await execute(process.execPath, [new URL('./wallet-sepolia.mjs', import.meta.url).pathname], { env, timeout: 20000 }); }
        catch (error) { code = error.code; }
        assert.equal(code, 2);
        const report = JSON.parse(await readFile(join(env.W3_REPORT_DIR, 'result.json'), 'utf8'));
        assert.equal(report.status, 'BLOCKED'); assert.equal(report.phaseVerified, false);
        assert.equal(report.publicWalletSignoff, false); assert.equal(report.completeAcceptance, false);
        assert(report.blockers.some((reason) => reason.includes('MetaMask was not found')));
        assert.equal(report.errors.length, 0); assert.equal(report.checks.length, 0);
        assert.equal(page.isClosed(), false); assert.equal(unrelated.isClosed(), false);
        assert.equal(await page.title(), 'Owned observer fixture');
        assert.equal(await unrelated.title(), 'Owned transaction panel');
        assert.equal(await page.evaluate(() => typeof window.ethereum), 'undefined');
    } finally {
        if (context) await context.close();
        await new Promise((resolve) => server.close(resolve));
        await rm(directory, { recursive: true, force: true });
    }
});

test('real fixture page without accepted CI stays blocked and cannot request wallet transactions', { timeout: 30000 }, async () => {
    const root = new URL('../../h5-demo/', import.meta.url).pathname;
    const server = createServer(async (request, response) => {
        try {
            let path = new URL(request.url, 'http://localhost').pathname;
            if (path.endsWith('/')) path += 'index.html';
            const file = resolve(root, '.' + path);
            if (!file.startsWith(root)) throw Error('invalid path');
            response.setHeader('Content-Type', path.endsWith('.mjs') ? 'text/javascript' : path.endsWith('.html') ? 'text/html' : 'application/octet-stream');
            response.end(await readFile(file));
        } catch { response.writeHead(404); response.end('Not configured'); }
    });
    let browser;
    try {
        await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
        browser = await chromium.launch({ headless: true });
        const page = await browser.newPage(); const errors = [];
        page.on('pageerror', error => errors.push(error.message));
        await page.goto(`http://127.0.0.1:${server.address().port}/testnet/`);
        await page.waitForFunction(() => document.getElementById('status').textContent.includes('BLOCKED'));
        assert.match(await page.locator('#status').textContent(), /缺少冻结候选配置/);
        assert.equal(await page.locator('[data-op]').count(), 6);
        assert.equal(await page.locator('[data-op]:enabled').count(), 0);
        assert.equal(await page.locator('#connect').isDisabled(), true);
        assert.equal(await page.evaluate(() => typeof window.ethereum), 'undefined');
        assert.deepEqual(errors, []);
        if (process.env.W3_UI_SCREENSHOT_DIR) await page.screenshot({ path: join(process.env.W3_UI_SCREENSHOT_DIR, 'testnet-blocked.png'), fullPage: true });
    } finally {
        if (browser) await browser.close();
        await new Promise(resolve => server.close(resolve));
    }
});

test('observer refuses an existing evidence directory before connecting and preserves its original bytes', { timeout: 10000 }, async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sg-wallet-preserve-'));
    const original = '{"status":"ORIGINAL_FIRST_FAILURE"}\n';
    try {
        await writeFile(join(directory, 'result.json'), original);
        const env = { ...process.env, W3_PHASE: 'negative', W3_MODE: 'monolith', W3_CANDIDATE_SHA: 'a'.repeat(40), W3_H5_BASE: 'http://localhost:18500',
            W3_CDP_ENDPOINT: 'http://127.0.0.1:1', W3_CONTROLLER: '0x' + '1'.repeat(40), W3_REPORT_DIR: directory };
        let code = 0;
        try { await execute(process.execPath, [new URL('./wallet-sepolia.mjs', import.meta.url).pathname], { env, timeout: 8000 }); }
        catch (error) { code = error.code; }
        assert.equal(code, 2);
        assert.equal(await readFile(join(directory, 'result.json'), 'utf8'), original);
    } finally { await rm(directory, { recursive: true, force: true }); }
});
