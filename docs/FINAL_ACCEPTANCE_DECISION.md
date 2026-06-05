# StreamGate Final Acceptance Decision

> **Note (2026-06-05)**: Port references in this historical document have been updated to match the current dual-mode architecture. Original decision was made against the pre-fullchain port map; the gateway path that was `http://localhost:29090` is now `http://localhost:28080` (api-gateway in microservices mode) or `http://localhost:18080` (monolith mode). See [DEPLOY.md](../DEPLOY.md) for the current 5-minute walkthrough.

## Decision

StreamGate is now acceptable for:

- functional acceptance
- demo walkthroughs
- interview presentation
- continued incremental hardening

It is not yet a fully finished production platform, but the core acceptance path is working and demonstrable.

## Accepted Scope

The following paths are considered accepted:

- `go build ./...` passes
- Dockerized gateway acceptance path is available on `http://localhost:28080` (microservices) or `http://localhost:18080` (monolith)
- wallet challenge generation works
- wallet signature login works
- NFT verification works
- protected manifest access works
- playback-token-protected segment access works
- RPC status visibility works
- transcoding submit / status / tasks / profiles work
- `/health` works
- `/metrics` works on the current gateway path
- `h5-demo` is aligned with the current acceptance entrypoint

## Current Acceptance Entrypoints

- API Gateway (microservices): `http://localhost:28080` — `make deploy-microservices`
- Monolith: `http://localhost:18080` — `make deploy-monolith`
- H5 Demo (monolith nginx): `http://localhost:18000/`
- H5 Demo (microservices nginx): `http://localhost:18001/`
- Scripted acceptance:
  - `scripts/self-test-deploy.sh` (legacy self-test compose, port `29090`)
  - `scripts/run-docker-acceptance.sh` (defaults to `http://localhost:18080`, override with arg)
  - `scripts/verify-deploy.sh` (8-point health check, accepts `monolith|microservices`)
  - `scripts/fullchain-acceptance.sh` (11-step API acceptance)
  - targeted gateway route tests for manifest and playback-token enforcement

## What Is Still Not Claimed

The current acceptance does not mean:

- all legacy `pkg/api/v1/*` endpoints have been fully retired
- all monitoring/export paths are fully standardized for production
- the transcoding control plane is fully productionized
- all CI / automated end-to-end coverage is complete

## Practical Conclusion

If the question is:

- "Can this project be accepted for demo and interview use?" -> Yes
- "Can this project be accepted as a complete production-ready platform?" -> Not yet

## Recommended Next Steps

### P1

- continue unifying old `pkg/api/v1/*` historical entrypoints
- standardize external `/metrics` scraping and monitoring docs
- tighten Docker acceptance automation

### P2

- deepen transcoding execution and FFmpeg workflow integration
- continue production hardening
