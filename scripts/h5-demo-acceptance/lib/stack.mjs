// Stack health pre-check — runs before any browser spec.
// Verifies the stack is up and the API surfaces we depend on are reachable.
// Endpoints come from lib/urls.mjs so the same checks work on the host and
// inside a container on the compose network.

import { H5_BASE, API_BASE, MONOLITH_BASE, ANVIL_BASE } from './urls.mjs';

const TARGETS = [
    { name: 'monolith-health', url: `${MONOLITH_BASE}/health`, expect: { has: 'status', jsonStatus: 'healthy' } },
    { name: 'api-gateway-health', url: `${API_BASE}/health`, expect: { has: 'status', jsonStatus: 'healthy' } },
    { name: 'h5-demo-index', url: `${H5_BASE}/`, expect: { status: 200 } },
    { name: 'h5-demo-flow', url: `${H5_BASE}/flow.html`, expect: { status: 200 } },
    { name: 'h5-demo-debug', url: `${H5_BASE}/debug.html`, expect: { status: 200 } },
    { name: 'h5-demo-playground', url: `${H5_BASE}/playground.html`, expect: { status: 200 } },
    { name: 'h5-demo-trace', url: `${H5_BASE}/trace.html`, expect: { status: 200 } },
    { name: 'anvil-rpc', url: ANVIL_BASE, expect: { status: 200, has: 'result' } },
    { name: 'api-gateway-nft-dev-mint-routed', url: `${API_BASE}/api/v1/nft/dev/mint`, expect: { status: 401 } },
];

export async function checkStack({ timeout = 5000 } = {}) {
    const checks = [];
    for (const t of TARGETS) {
        const start = Date.now();
        try {
            const ctrl = new AbortController();
            const timer = setTimeout(() => ctrl.abort(), timeout);
            const res = await fetch(t.url, {
                method: t.name === 'anvil-rpc' ? 'POST' : 'GET',
                signal: ctrl.signal,
                headers: t.name === 'anvil-rpc' ? { 'Content-Type': 'application/json' } : {},
                body: t.name === 'anvil-rpc' ? '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' : undefined,
            }).finally(() => clearTimeout(timer));
            const text = res.headers.get('content-type')?.includes('json') ? await res.text() : '';
            const body = text ? safeParse(text) : null;
            const statusOk = t.expect.status == null || res.status === t.expect.status;
            const hasOk = !t.expect.has || (body && body[t.expect.has] != null);
            // A /health endpoint that answers 503 {"status":"unhealthy"} carries the
            // dependency failure; only the HTTP status was ever checked, so a degraded
            // stack used to pass. Require the reported status to be healthy too.
            const jsonOk = t.expect.jsonStatus == null || (body && body.status === t.expect.jsonStatus);
            const ok = statusOk && hasOk && jsonOk;
            const detail = ok ? 'OK' : [
                `http=${res.status}` + (t.expect.status != null ? ` expected=${t.expect.status}` : ''),
                body && body.status ? `body.status=${body.status}` + (t.expect.jsonStatus ? ` expected=${t.expect.jsonStatus}` : '') : null,
            ].filter(Boolean).join(' ');
            checks.push({ name: t.name, ok, status: res.status, latency_ms: Date.now() - start, detail });
        } catch (e) {
            checks.push({ name: t.name, ok: false, status: 0, latency_ms: Date.now() - start, detail: e.message });
        }
    }
    const allOk = checks.every((c) => c.ok);
    return { name: '00-stack', passed: allOk, checks, duration_ms: checks.reduce((a, c) => a + c.latency_ms, 0) };
}

function safeParse(s) { try { return JSON.parse(s); } catch { return null; } }
