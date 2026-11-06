import { CuaNetworkCapture, browserResponseEpoch } from './wallet_cua_network.mjs';

// An origin-scoped session does not open or control a browser. A CUA peer
// must serve the relay operations; missing, stale or incomplete observations
// terminate this session rather than manufacturing a successful phase.
export class CuaPageSession {
    constructor({ bridge, origin, content, onResponse, onPageError, now = Date.now }) {
        if (!bridge || typeof bridge.request !== 'function' || typeof onResponse !== 'function' || typeof onPageError !== 'function') throw Error('bridge and observation callbacks required');
        if (typeof now !== 'function') throw Error('observation clock required');
        this.bridge = bridge; this.origin = origin; this.onResponse = onResponse; this.onPageError = onPageError; this.now = now;
        this.network = new CuaNetworkCapture({ origin, content, getBody: requestId => bridge.request('network-body', { requestId }) });
        this.cursor = null; this.failed = false; this.closed = false; this.unresolved = false;
    }

    async guarded(operation) {
        if (this.failed || this.closed) throw Error('CUA session failed or closed');
        try { return await operation(); } catch (error) { this.failed = true; throw error; }
    }

    async start() {
        return this.guarded(async () => {
            if (this.cursor !== null) throw Error('CUA session already started');
            const ready = await this.bridge.request('network-enable', {});
            if (!Number.isSafeInteger(ready?.cursor) || ready.cursor < 0) throw Error('invalid initial event cursor');
            this.cursor = ready.cursor;
        });
    }

    async pump() {
        if (this.cursor === null) throw Error('CUA session not started');
        for (let page = 0; page < 64; page++) {
            const previous = this.cursor;
            const batch = await this.bridge.request('network-events', { afterSequence: previous });
            if (!batch || batch.truncated !== false) throw Error('browser event stream truncated');
            if (!Array.isArray(batch.events) || typeof batch.hasMore !== 'boolean' || typeof batch.unresolved !== 'boolean' || !Number.isSafeInteger(batch.cursor) ||
                batch.cursor < previous || (batch.hasMore && batch.cursor === previous) ||
                batch.events.some(event => !Number.isSafeInteger(event.sequence) || event.sequence <= previous || event.sequence > batch.cursor)) throw Error('browser event stream made invalid progress');
            for (const event of batch.events) if (event.method === 'Runtime.exceptionThrown') this.onPageError('H5 page error observed');
            const rows = await this.network.consume(batch);
            this.cursor = batch.cursor; this.unresolved = batch.unresolved;
            // Check freshness when evidence is delivered, including any wait
            // for delayed headers; an earlier processing time is insufficient.
            for (const row of rows) await this.onResponse({ ...row, at: browserResponseEpoch(row, this.now()) });
            if (!batch.hasMore) return;
        }
        throw Error('browser event stream exceeded page budget');
    }

    async state() {
        return this.guarded(async () => {
            await this.pump();
            const value = await this.bridge.request('state', {});
            if (!value || value.origin !== this.origin) throw Error('H5 target origin changed');
            await this.pump();
            return value;
        });
    }

    async profile() {
        return this.guarded(async () => {
            await this.pump();
            const value = await this.bridge.request('profile', {});
            if (!value || !Number.isInteger(value.status) || !value.data || typeof value.data !== 'object') throw Error('invalid profile observation');
            return value;
        });
    }

    async finish() {
        return this.guarded(async () => {
            await this.pump();
            if (this.unresolved) throw Error('unresolved browser request origin');
            this.network.finish(); this.closed = true;
        });
    }
}
