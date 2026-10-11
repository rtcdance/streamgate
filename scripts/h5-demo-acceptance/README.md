# h5-demo-acceptance

Playwright-driven smoke test for the 5 h5-demo HTML pages. Runs after `make fullchain-deploy` (or any equivalent live stack) and verifies the front-end can talk to the back-end through the same flows a human would exercise.

## What it runs

1. **stack pre-check** (`lib/stack.mjs`) — 9 probes, exits early if any fail:
   - monolith `/health`
   - api-gateway `/health`
   - all 5 h5-demo HTML pages return 200
   - Anvil RPC `eth_chainId`
   - api-gateway routes the NFT dev-mint endpoint (expects 401 — auth middleware must be live)

2. **5 browser specs** (`specs/01..05-*.mjs`) — one per HTML file:
   - `01-index.mjs` — full acceptance: backend URL → demo wallet → sign & login → auto-mint → verify NFT → player visible
   - `02-flow.mjs` — upload UI present, MetaMask connect click handled
   - `03-debug.mjs` — Run Full Auto Flow + auth + NFT (skipped gracefully if click races)
   - `04-playground.mjs` — refresh Web3 status dashboard; verifies all 4 stat blocks + RPC detail + config table populate
   - `05-trace.mjs` — pick scenario, run, verify middleware chain + response + timing summary populate

3. **report artefacts** under `reports/<timestamp>/`:
   - `summary.json` — top-level pass/fail + per-spec durations
   - `<spec>/result.json` — checks + errors
   - `<spec>/console.json` — full page console log
   - `<spec>/network.json` — request/response summary
   - `<spec>/<name>.png` — full-page screenshots at key steps

## Usage

```bash
# Run the entire suite (≈95 s on a warm stack)
make h5-demo-acceptance

# Run a single spec
make h5-demo-acceptance-spec SPEC=01-index
# or
cd scripts/h5-demo-acceptance && node run.mjs --only=02-flow

# Pin the report directory (handy for CI artefact upload)
cd scripts/h5-demo-acceptance && node run.mjs --report-dir=reports/ci-$(date +%Y%m%d)
```

## Requirements

- A live stack: `make fullchain-deploy`, or any equivalent running stack. Endpoints are
  resolved from the environment (below), not hardcoded.
- Playwright Node bindings: `cd scripts/h5-demo-acceptance && npm install` (the committed
  `package-lock.json` is enough). Host runs only — see the Docker section.
- Browser: `PW_CHANNEL='chrome'` (default) uses the host's Google Chrome and avoids a
  Playwright chromium download. Inside a container set `PW_CHANNEL=` to use the bundled
  Chromium instead.

### Endpoints (env-driven)

`lib/urls.mjs` is the single place endpoints are resolved, so the same specs run on the
host and inside a container with no edits:

| Env var | Host default | Inside the compose network |
|---|---|---|
| `H5_BASE` | `http://localhost:18000` | `http://h5-demo:8080` |
| `API_BASE` | `http://localhost:28080` | `http://api-gateway:8080` |
| `MONOLITH_BASE` | `http://localhost:18080` | `http://monolith:8080` |
| `ANVIL_BASE` | `http://localhost:18545` | `http://anvil:8545` |

### Docker (preferred)

Per the project's rule that build/run/acceptance happen in Docker, run both the stack and
the browser in containers:

```bash
~/.workbuddy/skills/streamgate-docker-acceptance/scripts/docker-acceptance.sh --repo <repo-root>
```

That script copies this suite into a Playwright container attached to the compose network
and points the env vars above at the service names. **Do not `npm install` on the host in
order to run in Docker** — the image is Linux, and installing over the host's
`node_modules` replaces the macOS Playwright binaries and breaks later host runs.

## What is filtered as transient

The console + network probes record everything to `console.json` / `network.json`, but the following are *recorded, not failed*:

- 503 on `/health` during the first probe (cold-start race)
- 404 on `/api/v1/streaming/<id>/manifest.m3u8` (no real video uploaded in the smoke test)
- `favicon.ico` 404s

If any of those patterns change in the real app, the filters here should be updated in `lib/common.mjs`.

## What a pass does not prove

Read this before reporting a green run as "the stack is fine":

- **The health probes only assert that `/health` returns JSON containing a `status`
  field** (`lib/stack.mjs`) — they do **not** assert the status is `healthy`. A stack
  with a degraded dependency, which returns `503 {"status":"unhealthy",...}`, passes
  the pre-check.
- **`503` on `/health` is filtered as transient** in the console probe ("cold-start
  race"), so a persistent dependency outage does not fail a spec either.
- No media pipeline coverage: nothing is uploaded or transcoded, so HLS manifest
  generation and segment delivery are untested (`404` on manifests is expected and
  filtered).
- The demo-wallet path is exercised, not a real injected wallet extension.

To actually verify dependency health, inspect `/health` yourself:

```bash
curl -s http://localhost:18080/health | jq -r '.status, (.checks | to_entries[] | "\(.key)=\(.value.status)")'
```

Tightening the pre-check to require `status == "healthy"` would be a behaviour change
to this suite — propose it rather than assuming it.

## Exit codes

- `0` — every probe and every spec passed
- `1` — at least one probe or spec failed (stack pre-check or browser check); the failing checks are listed in stdout and persisted under `reports/<timestamp>/`

The script writes a final verdict line (`PASSED` / `FAILED`) and a `summary.json` so CI can gate on either.
