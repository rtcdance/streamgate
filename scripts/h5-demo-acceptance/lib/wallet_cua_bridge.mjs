import { createServer } from 'node:http';
import { randomBytes, timingSafeEqual } from 'node:crypto';
import { mkdtempSync, writeFileSync, unlinkSync, rmdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { validateControlRequest } from './wallet_cua_control_contract.mjs';

const MAX_BYTES = 8 * 1024 * 1024;
const EMPTY = new Set(['state', 'profile', 'network-enable', 'control-state', 'reload']);

// A single-use local relay. It never controls Chrome: a CUA page executor must
// carry out the named, origin-scoped operations and return original results.
export class CuaPageBridge {
    constructor({ targetOrigin, content = null, timeoutMs = 15000 }) {
        const target = new URL(targetOrigin);
        if (target.origin !== targetOrigin || !['http:', 'https:'].includes(target.protocol) ||
            !['localhost', '127.0.0.1', '[::1]'].includes(target.hostname) || target.username || target.password ||
            !(content === null || typeof content === 'string' && /^[0-9a-f-]{36}$/i.test(content)) ||
            !Number.isInteger(timeoutMs) || timeoutMs < 10 || timeoutMs > 30000) throw Error('local target and bounded timeout required');
        this.targetOrigin = targetOrigin; this.content = content; this.timeoutMs = timeoutMs;
        this.secret = randomBytes(32).toString('hex'); this.sequence = 0; this.failure = null;
    }

    async start() {
        if (this.server || this.failure) throw Error('bridge already started or failed');
        // A webpage must never disclose the bearer capability: an unrelated
        // local process could forge browser fetch headers and retrieve it.
        this.sessionDirectory = mkdtempSync(join(tmpdir(), 'streamgate-cua-'));
        this.sessionFile = join(this.sessionDirectory, 'session.key');
        try { writeFileSync(this.sessionFile, this.secret, { mode: 0o600, flag: 'wx' }); }
        catch (error) { rmdirSync(this.sessionDirectory); throw error; }
        this.server = createServer((request, response) => { void this.handle(request, response); });
        try { await new Promise((resolve, reject) => { this.server.once('error', reject); this.server.listen(0, '127.0.0.1', resolve); }); }
        catch (error) { unlinkSync(this.sessionFile); rmdirSync(this.sessionDirectory); throw error; }
        this.origin = `http://127.0.0.1:${this.server.address().port}`;
        return this;
    }

    valid(op, args) {
        if (!args || typeof args !== 'object' || Array.isArray(args)) return false;
        const keys = Object.keys(args);
        if (EMPTY.has(op)) return keys.length === 0;
        if (op === 'network-body') return keys.length === 1 && typeof args.requestId === 'string' && /^[a-zA-Z0-9_.:-]{1,128}$/.test(args.requestId);
        if (op === 'network-events') return keys.length === 1 && Number.isSafeInteger(args.afterSequence) && args.afterSequence >= 0;
        if (op === 'control-http') {
            if (keys.length !== 2 || !keys.includes('url') || !keys.includes('options')) return false;
            try { validateControlRequest({ origin: this.targetOrigin, content: this.content, ...args }); return true; }
            catch { return false; }
        }
        return false;
    }

    request(op, args) {
        if (this.failure || !this.server || this.closed) return Promise.reject(Error('bridge failed or closed'));
        if (!['state', 'profile', 'network-enable', 'network-body', 'network-events', 'control-state', 'control-http', 'reload'].includes(op)) return Promise.reject(Error('unsupported operation'));
        if (!this.valid(op, args)) return Promise.reject(Error('invalid operation arguments'));
        if (this.pending) return Promise.reject(Error('another request is pending'));
        return new Promise((resolve, reject) => {
            const id = ++this.sequence;
            const timer = setTimeout(() => this.fail('bridge request timeout'), this.timeoutMs);
            this.pending = { id, op, args, resolve, reject, timer, delivered: false };
        });
    }

    fail(reason) {
        this.failure ||= Error(reason);
        if (this.pending) {
            const item = this.pending; this.pending = null;
            clearTimeout(item.timer); item.reject(this.failure);
        }
    }

    async handle(request, response) {
        const end = (code, data = '') => { response.writeHead(code, { 'Cache-Control': 'no-store', Connection: 'close', 'Content-Type': 'text/plain; charset=utf-8',
            'Content-Security-Policy': "default-src 'none'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'", 'X-Content-Type-Options': 'nosniff' }); response.end(data); };
        const host = new URL(this.origin).host;
        const fetchSite = request.headers['sec-fetch-site'];
        // Chrome may label a fresh top-level navigation as cross-site. An
        // embedded or scripted cross-site request must never obtain a session.
        const topLevelNavigation = request.method === 'GET' && request.url === '/' && !request.headers.origin &&
            fetchSite === 'cross-site' && request.headers['sec-fetch-mode'] === 'navigate' && request.headers['sec-fetch-dest'] === 'document';
        if (request.headers.host !== host || (request.headers.origin && request.headers.origin !== this.origin) ||
            (!['none', 'same-origin'].includes(fetchSite) && !topLevelNavigation)) return end(403);
        if (request.method === 'GET' && request.url === '/') {
            response.writeHead(200, { 'Cache-Control': 'no-store', Connection: 'close', 'Content-Type': 'text/html; charset=utf-8',
                'Content-Security-Policy': "default-src 'none'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'", 'X-Content-Type-Options': 'nosniff' });
            return response.end('<!doctype html><title>Local observation relay</title><label>Local session file <input id="session" type="file"></label>');
        }
        const token = request.headers.authorization || '';
        const expected = Buffer.from(`Bearer ${this.secret}`), received = Buffer.from(token);
        if (fetchSite !== 'same-origin' || expected.length !== received.length || !timingSafeEqual(expected, received)) return end(403);
        if (request.method === 'GET' && request.url === '/next') {
            if (!this.pending || this.pending.delivered || this.failure) return end(204);
            this.pending.delivered = true;
            return end(200, JSON.stringify({ id: this.pending.id, op: this.pending.op, args: this.pending.args }));
        }
        if (request.method !== 'POST' || request.url !== '/reply' || request.headers.origin !== this.origin) return end(404);
        if (Number(request.headers['content-length'] || 0) > MAX_BYTES) { request.resume(); this.fail('bridge reply exceeds budget'); return end(413); }
        const chunks = []; let size = 0;
        try {
            for await (const chunk of request) { size += chunk.length; if (size > MAX_BYTES) throw Error('oversized'); chunks.push(chunk); }
            const value = JSON.parse(Buffer.concat(chunks, size).toString('utf8'));
            if (!this.pending || !this.pending.delivered || value.id !== this.pending.id || typeof value.ok !== 'boolean') {
                this.fail('bridge response sequence mismatch'); return end(409);
            }
            if (value.targetOrigin !== this.targetOrigin || (value.ok && ['state', 'control-state'].includes(this.pending.op) && value.value?.origin !== this.targetOrigin)) {
                this.fail('bridge target origin mismatch'); return end(409);
            }
            const item = this.pending; this.pending = null; clearTimeout(item.timer);
            if (!value.ok) { this.failure = Error('page observation failed'); item.reject(this.failure); }
            else {
                // Resolve only after the browser-facing acknowledgement has
                // been written; the owner may close the relay immediately.
                response.once('finish', () => item.resolve(value.value));
                response.once('close', () => {
                    if (!response.writableFinished) { this.failure = Error('bridge acknowledgement interrupted'); item.reject(this.failure); }
                });
            }
            return end(204);
        } catch { this.fail('invalid bridge reply'); return end(400); }
    }

    async close() {
        if (this.closed) return;
        this.closed = true;
        if (this.pending) this.fail('bridge closed before response');
        if (this.server) await new Promise(resolve => {
            const timer = setTimeout(() => this.server.closeAllConnections(), 500);
            this.server.close(() => { clearTimeout(timer); resolve(); });
        });
        if (this.sessionFile) { unlinkSync(this.sessionFile); rmdirSync(this.sessionDirectory); }
        this.secret = '';
    }
}
