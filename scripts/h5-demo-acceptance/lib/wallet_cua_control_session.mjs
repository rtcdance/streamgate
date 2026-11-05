import { validateControlRequest } from './wallet_cua_control_contract.mjs';

// Public controls issue parallel HTTP/gRPC checks. The browser relay is a
// single observed tab, so its HTTP work is serialized and bounded here.
export class CuaControlSession {
    constructor({ bridge, origin, content }) {
        if (!bridge || typeof bridge.request !== 'function') throw Error('CUA control bridge required');
        this.bridge = bridge; this.origin = origin; this.content = content;
        this.tail = Promise.resolve(); this.pending = 0; this.failed = false; this.closed = false; this.closing = false;
    }

    enqueue(op, args, verify) {
        if (this.closed || this.closing || this.failed) return Promise.reject(Error('CUA control session failed, closing or closed'));
        if (this.pending >= 32) { this.failed = true; return Promise.reject(Error('CUA control queue budget exceeded')); }
        this.pending++;
        const operation = this.tail.then(async () => {
            if (this.closed || this.failed) throw Error('CUA control session failed or closed');
            const started = Date.now(), value = await this.bridge.request(op, args), finished = Date.now();
            if (finished < started || finished - started > 15000) throw Error('invalid control response time');
            return verify(value, started, finished);
        }).catch(error => { this.failed = true; throw error; });
        this.tail = operation.then(() => undefined, () => undefined);
        return operation.finally(() => { this.pending--; });
    }

    async state() {
        return this.enqueue('control-state', {}, (value, started, finished) => {
            const at = value?.playback?.at;
            if (value?.origin !== this.origin || !Number.isSafeInteger(at) || at < started - 1000 || at > finished + 1000) throw Error('invalid control state time or origin');
            return value;
        });
    }

    async http(url, options) {
        let request;
        try { request = validateControlRequest({ origin: this.origin, content: this.content, url, options }); }
        catch (error) { this.failed = true; throw error; }
        return this.enqueue('control-http', request, (value, started, finished) => {
            if (!Number.isInteger(value?.status) || value.status < 100 || value.status > 599 ||
                !Number.isSafeInteger(value.at) || value.at < started - 1000 || value.at > finished + 1000 ||
                typeof value.base64 !== 'string' || value.base64.length > Math.ceil(2 * 1024 * 1024 / 3) * 4 ||
                !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value.base64)) throw Error('invalid control response bytes or time');
            const bytes = Buffer.from(value.base64, 'base64');
            if (bytes.length > 2 * 1024 * 1024) throw Error('invalid control response size');
            return { status: value.status, bytes, at: value.at };
        });
    }

    async reload() {
        return this.enqueue('reload', {}, value => {
            if (value?.origin !== this.origin) throw Error('invalid reload origin');
            return value;
        });
    }

    async close() {
        if (this.closing) return this.closePromise;
        this.closing = true;
        this.closePromise = (async () => {
            await this.tail; this.closed = true;
            if (this.failed) throw Error('CUA control session failed');
        })();
        return this.closePromise;
    }
}
