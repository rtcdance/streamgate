const TOKEN = /^[A-Za-z0-9._~-]{1,16384}$/;

// Controls may replay only original, same-origin verification requests. The
// allowlist prevents the relay from becoming an arbitrary authenticated fetch.
export function validateControlRequest({ origin, content, url, options }) {
    const base = new URL(origin), target = new URL(url);
    if (base.origin !== origin || !['http:', 'https:'].includes(base.protocol) ||
        !['localhost', '127.0.0.1', '[::1]'].includes(base.hostname) || base.username || base.password ||
        !/^[0-9a-f-]{36}$/i.test(content) || target.origin !== origin || target.username || target.password || target.hash ||
        typeof url !== 'string' || url.length > 16384) throw Error('invalid control request origin or input');
    if (!options || typeof options !== 'object' || Array.isArray(options) ||
        Object.keys(options).some(key => !['method', 'headers', 'body'].includes(key))) throw Error('unsupported control request options');
    const method = options.method || 'GET';
    if (!['GET', 'POST'].includes(method)) throw Error('unsupported control request method');
    const prefix = `/api/v1/streaming/${content}`;
    const profile = target.pathname === '/api/v1/auth/profile', refresh = target.pathname === '/api/v1/auth/refresh';
    const manifest = target.pathname === prefix + '/manifest.m3u8';
    const segmentPrefix = prefix + '/segment/';
    const segment = target.pathname.startsWith(segmentPrefix) && /^[A-Za-z0-9._-]{1,200}$/.test(target.pathname.slice(segmentPrefix.length));
    if (!(profile || refresh || manifest || segment) || (refresh ? method !== 'POST' : method !== 'GET')) throw Error('unsupported control request path or method');
    const query = new Map();
    for (const [key, value] of target.searchParams) {
        if ((!manifest && !segment) || !['quality', 'playback_token'].includes(key) || query.has(key) ||
            !(key === 'quality' ? /^[A-Za-z0-9._-]{1,64}$/.test(value) : TOKEN.test(value))) throw Error('invalid control request query');
        query.set(key, value);
    }
    if (segment && !query.has('playback_token')) throw Error('invalid media capability');
    const inputHeaders = options.headers || {};
    if (typeof inputHeaders !== 'object' || Array.isArray(inputHeaders)) throw Error('invalid control request headers');
    const headers = {};
    for (const [name, value] of Object.entries(inputHeaders)) {
        const key = name.toLowerCase();
        if (!['authorization', 'content-type'].includes(key) || Object.hasOwn(headers, key) || typeof value !== 'string') throw Error('unsupported control request header');
        if (key === 'authorization' && (!value.startsWith('Bearer ') || !TOKEN.test(value.slice(7)))) throw Error('invalid control authorization');
        if (key === 'content-type' && value.toLowerCase() !== 'application/json') throw Error('invalid control content type');
        headers[key] = value;
    }
    let body;
    if (refresh) {
        if (typeof options.body !== 'string' || options.body.length > 32768 || headers['content-type']?.toLowerCase() !== 'application/json' || !headers.authorization) throw Error('invalid refresh request');
        let value;
        try { value = JSON.parse(options.body); } catch { throw Error('invalid refresh body'); }
        if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).length !== 1 ||
            typeof value.token !== 'string' || !TOKEN.test(value.token) || headers.authorization !== 'Bearer ' + value.token) throw Error('invalid refresh credential');
        body = options.body;
    } else if (options.body !== undefined || headers['content-type']) throw Error('invalid GET request body');
    return { url: target.href, options: { method, headers, ...(body === undefined ? {} : { body }) } };
}
