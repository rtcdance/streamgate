import { selectedHttpTarget } from './wallet_cua_network.mjs';

// Runs on the CUA side before any event crosses the local relay. Extra-info
// can precede its URL; hold only its authorization header until classified.
export class CuaEventFilter {
    constructor({ origin, content, initialCursor }) {
        const url = new URL(origin);
        if (url.origin !== origin || !['http:', 'https:'].includes(url.protocol) ||
            !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname) || url.username || url.password ||
            !(content === null || typeof content === 'string' && /^[0-9a-f-]{36}$/i.test(content)) ||
            !Number.isSafeInteger(initialCursor) || initialCursor < 0) throw Error('local origin, content and cursor required');
        this.origin = origin; this.content = content; this.rawCursor = initialCursor; this.cursor = initialCursor;
        this.selected = new Map(); this.buffer = []; this.failed = false; this.closed = false;
    }

    consume(batch) {
        if (this.failed || this.closed) throw Error('event filter failed or closed');
        try { return this.read(batch); } catch (error) { this.failed = true; this.buffer = []; throw error; }
    }

    read(batch) {
        if (!batch || batch.truncated !== false) throw Error('browser event stream truncated');
        if (!Number.isSafeInteger(batch.cursor) || batch.cursor < this.rawCursor || !Array.isArray(batch.events) || typeof batch.hasMore !== 'boolean') throw Error('invalid raw event cursor');
        let previous = this.rawCursor;
        const auth = headers => {
            const entry = Object.entries(headers || {}).find(([key]) => key.toLowerCase() === 'authorization');
            return entry && typeof entry[1] === 'string' ? { authorization: entry[1] } : {};
        };
        for (const event of batch.events) {
            if (!Number.isSafeInteger(event.sequence) || event.sequence <= previous || event.sequence > batch.cursor) throw Error('raw event sequence mismatch');
            previous = event.sequence;
            const { method, params = {} } = event, id = params.requestId;
            let data = null, wait = false;
            if (method === 'Network.requestWillBeSent' && typeof id === 'string') {
                const selected = selectedHttpTarget(params.request?.url, this.origin, this.content);
                if (params.redirectResponse && (selected || this.selected.get(id))) throw Error('selected request redirect');
                if (this.selected.has(id) && !params.redirectResponse) throw Error('duplicate request identity');
                if (!this.selected.has(id) && this.selected.size >= 4096) throw Error('request classification budget exceeded');
                this.selected.set(id, selected);
                if (selected) data = { sequence: event.sequence, method, params: { requestId: id, timestamp: params.timestamp, wallTime: params.wallTime,
                    request: { url: params.request.url, method: params.request.method, postData: params.request.postData, headers: auth(params.request.headers) } } };
            } else if (method === 'Network.requestWillBeSentExtraInfo' && typeof id === 'string') {
                data = { sequence: event.sequence, method, params: { requestId: id, headers: auth(params.headers) } };
                wait = !this.selected.has(id);
                if (!wait && !this.selected.get(id)) data = null;
            } else if (method === 'Runtime.exceptionThrown') {
                data = { sequence: event.sequence, method, params: {} };
            } else if (this.selected.get(id) && method === 'Network.responseReceived') {
                data = { sequence: event.sequence, method, params: { requestId: id, timestamp: params.timestamp,
                    hasExtraInfo: params.hasExtraInfo, response: { status: params.response?.status } } };
            } else if (this.selected.get(id) && ['Network.loadingFinished', 'Network.loadingFailed'].includes(method)) {
                data = { sequence: event.sequence, method, params: { requestId: id, timestamp: params.timestamp } };
            }
            this.buffer.push({ sequence: event.sequence, id, wait, data, since: Date.now() });
            if (this.buffer.length > 1024) throw Error('event classification budget exceeded');
        }
        this.rawCursor = batch.cursor;
        const events = []; const before = this.cursor;
        while (this.buffer.length) {
            const item = this.buffer[0];
            if (item.wait && !this.selected.has(item.id)) {
                if (Date.now() - item.since > 5000) throw Error('unresolved request origin exceeded time budget');
                break;
            }
            this.buffer.shift(); this.cursor = item.sequence;
            if (item.wait && !this.selected.get(item.id)) item.data = null;
            if (item.data) events.push(item.data);
        }
        if (!this.buffer.length) this.cursor = batch.cursor;
        return { cursor: this.cursor, truncated: false, hasMore: batch.hasMore && this.cursor > before, unresolved: this.buffer.length > 0, events };
    }

    finish() {
        if (this.failed || this.closed) throw Error('event filter failed or closed');
        if (this.buffer.length) { this.failed = true; this.buffer = []; throw Error('unresolved request origins'); }
        this.selected.clear(); this.closed = true;
    }
}
