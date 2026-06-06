// Stack health pre-check — runs before any browser spec.
// Verifies the docker-compose stack is up and the API surfaces we depend on are reachable.

const TARGETS = [
    { name: 'monolith-health', url: 'http://localhost:18080/health', expect: { has: 'status' } },
    { name: 'api-gateway-health', url: 'http://localhost:28080/health', expect: { has: 'status' } },
    { name: 'h5-demo-index', url: 'http://localhost:18000/', expect: { status: 200 } },
    { name: 'h5-demo-flow', url: 'http://localhost:18000/flow.html', expect: { status: 200 } },
    { name: 'h5-demo-debug', url: 'http://localhost:18000/debug.html', expect: { status: 200 } },
    { name: 'h5-demo-playground', url: 'http://localhost:18000/playground.html', expect: { status: 200 } },
    { name: 'h5-demo-trace', url: 'http://localhost:18000/trace.html', expect: { status: 200 } },
    { name: 'anvil-rpc', url: 'http://localhost:18545', expect: { status: 200, has: 'result' } },
    { name: 'api-gateway-nft-dev-mint-routed', url: 'http://localhost:28080/api/v1/nft/dev/mint', expect: { status: 401 } },
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
            const ok = statusOk && hasOk;
            checks.push({ name: t.name, ok, status: res.status, latency_ms: Date.now() - start, detail: ok ? 'OK' : `status=${res.status} expected=${t.expect.status}` });
        } catch (e) {
            checks.push({ name: t.name, ok: false, status: 0, latency_ms: Date.now() - start, detail: e.message });
        }
    }
    const allOk = checks.every((c) => c.ok);
    return { name: '00-stack', passed: allOk, checks, duration_ms: checks.reduce((a, c) => a + c.latency_ms, 0) };
}

function safeParse(s) { try { return JSON.parse(s); } catch { return null; } }
