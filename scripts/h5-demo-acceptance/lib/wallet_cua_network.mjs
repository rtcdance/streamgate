// Convert origin-scoped CUA CDP observations into bounded original HTTP bytes.
// This component is not an acceptance report or a source of wallet approvals.
export function browserResponseEpoch(row, now = Date.now()) {
    const { requestTimestamp, requestWallTime, responseTimestamp } = row || {};
    if (![requestTimestamp, requestWallTime, responseTimestamp].every(Number.isFinite) ||
        requestTimestamp < 0 || requestWallTime <= 0 || responseTimestamp < requestTimestamp ||
        responseTimestamp - requestTimestamp > 120 || !Number.isSafeInteger(now)) throw Error('invalid browser clock timing');
    // CDP pairs wallTime and monotonic time at the request. Convert the later
    // response using that pair; processing time alone cannot prove revocation.
    const at = Math.round((requestWallTime + responseTimestamp - requestTimestamp) * 1000);
    if (!Number.isSafeInteger(at) || at > now + 1000) throw Error('browser event timing is in the future');
    if (now - at > 5000) throw Error('browser event timing is stale');
    return at;
}

export function selectedHttpTarget(value, origin, content) {
    let url;
    try { url = new URL(value); } catch { return false; }
    if (url.origin !== origin || url.username || url.password) return false;
    return ['/api/v1/auth/challenge', '/api/v1/auth/login', '/api/v1/auth/refresh'].includes(url.pathname) ||
        (content !== null && (url.pathname === `/api/v1/streaming/${content}/manifest.m3u8` ||
        url.pathname.startsWith(`/api/v1/streaming/${content}/segment/`)));
}

export class CuaNetworkCapture {
    constructor({ origin, content, getBody }) {
        const parsed = new URL(origin);
        if (parsed.origin !== origin || !['http:', 'https:'].includes(parsed.protocol) ||
            !['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname) || parsed.username || parsed.password ||
            !(content === null || typeof content === 'string' && /^[0-9a-f-]{36}$/i.test(content)) || typeof getBody !== 'function') throw Error('local origin, content and body reader required');
        this.origin = origin; this.content = content; this.getBody = getBody;
        this.pending = new Map(); this.earlyHeaders = new Map(); this.sequence = 0; this.failed = false; this.finished = false;
    }

    selected(value) {
        return selectedHttpTarget(value, this.origin, this.content);
    }

    async consume(batch) {
        if (this.failed) throw Error('capture already failed');
        if (this.finished) throw Error('capture already finished');
        try { return await this.read(batch); }
        catch (error) { this.failed = true; throw error; }
    }

    finish() {
        if (this.finished) throw Error('capture already finished');
        if (this.failed || this.pending.size) {
            this.failed = true;
            throw Error('capture ended with incomplete evidence');
        }
        this.earlyHeaders.clear();
        this.finished = true;
    }

    async read(batch) {
        if (!batch || batch.truncated !== false || !Array.isArray(batch.events)) throw Error('truncated or malformed event batch');
        const rows = [];
        const authorization = headers => {
            const entry = Object.entries(headers || {}).find(([key]) => key.toLowerCase() === 'authorization');
            return entry ? { authorization: entry[1] } : {};
        };
        const emit = (id, item) => {
            if (item.finished && (!item.expectsExtra || item.hasExtra)) {
                this.pending.delete(id);
                const { finished, expectsExtra, hasExtra, ...row } = item;
                rows.push(row);
            }
        };
        for (const event of batch.events) {
            if (!Number.isSafeInteger(event.sequence) || event.sequence <= this.sequence) throw Error('event sequence is not strictly increasing');
            this.sequence = event.sequence;
            const { method, params = {} } = event, id = params.requestId;
            if (method === 'Network.requestWillBeSent') {
                if (this.pending.has(id) && params.redirectResponse) throw Error('redirect on observed request');
                const early = this.earlyHeaders.get(id);
                this.earlyHeaders.delete(id);
                if (!this.selected(params.request?.url)) continue;
                if (typeof id !== 'string' || this.pending.has(id) || this.pending.size >= 512) throw Error('duplicate or excessive pending requests');
                const request = params.request;
                if (typeof request.postData === 'string' && Buffer.byteLength(request.postData) > 65536) throw Error('request exceeds bounded capture');
                this.pending.set(id, { url: request.url, method: request.method, requestBody: request.postData || '',
                    headers: early || authorization(request.headers), hasExtra: early !== undefined, startedSequence: event.sequence,
                    requestTimestamp: params.timestamp, requestWallTime: params.wallTime });
            } else if (method === 'Network.requestWillBeSentExtraInfo' && typeof id === 'string') {
                const headers = authorization(params.headers);
                if (this.pending.has(id)) {
                    const item = this.pending.get(id);
                    if (item.hasExtra) throw Error('duplicate request extra-info');
                    item.headers = headers; item.hasExtra = true; emit(id, item);
                } else {
                    // Extra-info has no URL and can precede its request. Keep
                    // only authorization temporarily, never cookies or other headers.
                    if (this.earlyHeaders.size >= 512) this.earlyHeaders.delete(this.earlyHeaders.keys().next().value);
                    this.earlyHeaders.set(id, headers);
                }
            } else if (method === 'Network.responseReceived' && this.pending.has(id)) {
                const item = this.pending.get(id);
                if (item.status !== undefined || !Number.isInteger(params.response?.status)) throw Error('malformed response');
                item.status = params.response.status;
                item.responseTimestamp = params.timestamp;
                item.expectsExtra = params.hasExtraInfo === true;
            } else if (method === 'Network.loadingFailed' && this.pending.has(id)) {
                throw Error('observed request failed before completion');
            } else if (method === 'Network.loadingFinished' && this.pending.has(id)) {
                const item = this.pending.get(id);
                if (item.status === undefined) throw Error('completed request has no response');
                if (item.finished) throw Error('duplicate request completion');
                const value = await this.getBody(id);
                if (typeof value?.body !== 'string' || typeof value.base64Encoded !== 'boolean' || value.body.length > 8 * 1024 * 1024) throw Error('invalid or oversized response body');
                if (value.base64Encoded && !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value.body)) throw Error('malformed base64 response');
                Object.assign(item, { body: Buffer.from(value.body, value.base64Encoded ? 'base64' : 'utf8'),
                    finished: true, finishedSequence: event.sequence, finishTimestamp: params.timestamp, processedAt: Date.now() });
                emit(id, item);
            }
        }
        return rows;
    }
}
