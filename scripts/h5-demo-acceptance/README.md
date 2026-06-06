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

- Live full chain stack on `localhost:18000` (h5-demo) and `localhost:28080` (api-gateway). Start with `make fullchain-deploy`.
- Playwright Node bindings installed: `cd scripts/h5-demo-acceptance && npm install` (committed `package-lock.json` is enough).
- System Chrome (macOS: `/Applications/Google Chrome.app`). The harness launches `chromium` with `channel: 'chrome'` to avoid Playwright's chromium download.
- Run on a host that can reach the Anvil RPC at `http://localhost:18545` and the NFT dev-mint endpoint at `http://localhost:28080/api/v1/nft/dev/mint`.

## What is filtered as transient

The console + network probes record everything to `console.json` / `network.json`, but the following are *recorded, not failed*:

- 503 on `/health` during the first probe (cold-start race)
- 404 on `/api/v1/streaming/<id>/manifest.m3u8` (no real video uploaded in the smoke test)
- `favicon.ico` 404s

If any of those patterns change in the real app, the filters here should be updated in `lib/common.mjs`.

## Exit codes

- `0` — every probe and every spec passed
- `1` — at least one probe or spec failed (stack pre-check or browser check); the failing checks are listed in stdout and persisted under `reports/<timestamp>/`

The script writes a final verdict line (`PASSED` / `FAILED`) and a `summary.json` so CI can gate on either.
