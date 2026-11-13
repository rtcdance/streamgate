// Private original relay observations. This file contains access credentials;
// it is evidence input, never a public report or a wallet approval.
export class CuaTrace {
    constructor({ bridge, candidateSHA, mode, phase, origin, content, limit = 128 * 1024 * 1024 }) {
        if (!bridge || typeof bridge.request !== 'function' || !/^[0-9a-f]{40}$/.test(candidateSHA) ||
            !['monolith', 'api-gateway'].includes(mode) || !['eoa', 'contract', 'negative'].includes(phase) ||
            !Number.isSafeInteger(limit) || limit < 1024) throw Error('invalid trace configuration');
        this.bridge = bridge; this.limit = limit; this.bytes = 0; this.active = null; this.failed = false;
        this.document = { version: 1, candidateSHA, mode, phase, origin, content, requests: [], calls: [] };
    }

    async request(op, args) {
        if (this.failed || !this.active) throw Error('relay request outside trace call');
        const startedAt = Date.now();
        try {
            const value = await this.bridge.request(op, args);
            const entry = { op, args, value, startedAt, finishedAt: Date.now() };
            const raw = JSON.stringify(entry);
            this.bytes += Buffer.byteLength(raw);
            if (this.bytes > this.limit) throw Error('private CUA evidence exceeds budget');
            this.document.requests.push(JSON.parse(raw));
            return value;
        } catch (error) { this.failed = true; throw error; }
    }

    async run(kind, operation) {
        if (this.failed || this.active || !['start', 'state', 'profile', 'finish'].includes(kind)) throw Error('invalid CUA trace call');
        const call = { kind, first: this.document.requests.length, startedAt: Date.now() };
        this.active = call;
        try {
            const result = await operation();
            Object.assign(call, { end: this.document.requests.length, finishedAt: Date.now() });
            this.document.calls.push(call);
            return result;
        } catch (error) { this.failed = true; throw error; }
        finally { this.active = null; }
    }

    serialize() {
        if (this.failed || this.active || this.document.calls.at(-1)?.kind !== 'finish') throw Error('incomplete CUA trace');
        const raw = JSON.stringify(this.document) + '\n';
        if (Buffer.byteLength(raw) > this.limit) throw Error('private CUA evidence exceeds budget');
        return raw;
    }
}
