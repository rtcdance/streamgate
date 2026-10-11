# StreamGate Development Guide

NFT-gated streaming platform in Go. Core flow: **wallet sign-in → NFT ownership check → protected HLS streaming**.

- Microkernel plugin architecture, dual-mode: monolith (dev) or 9 microservices (prod)
- 11 build targets (`make build-all`) · 254 test files · ~73.4% coverage · 60% CI floor

## Stack

Go 1.26 · Gin · go-ethereum + solana-go · PostgreSQL 15 · Redis 7 · MinIO/S3 · NATS · Consul · HLS/DASH via FFmpeg · Zap + Prometheus + OpenTelemetry · gRPC · Docker Compose + Kubernetes · testify + uber/mock

## Commands

**Build, run and acceptance all happen in Docker.** The host targets below are for fast local edits only — acceptance evidence comes from the Docker stack or CI, never from a host build.

```bash
# Build / run
make build-monolith        # single binary
make build-all             # all 11 binaries
make run-monolith          # build + run on :8080 (also: dev, dev-setup)
make demo                  # infra (PG+Redis+MinIO) + monolith on :8080

# Test
make test                  # all tests, -race, coverage HTML
make test-ci               # CI mode (60% floor)
make test-anvil            # Foundry/Anvil Web3 integration tests
make test-testnet          # needs SEPOLIA_RPC

# Quality / codegen
make lint                  # golangci-lint (also: lint-fix, fmt)
make mocks                 # go generate ./...
make proto-gen             # protobuf stubs

# Docker / deploy
make fullchain-deploy      # PG+Redis+MinIO+NATS+Anvil(+DemoNFT)+monolith+microservices+h5-demo
make docker-up             # compose stack (also: docker-down, docker-build, docker-bake)
make demo-down             # stop demo infra and clean up
make migrate-up            # also: migrate-down, migrate-reset
make k8s-deploy            # also: k8s-status

# Contracts (Foundry)
make contracts-test        # also: contracts-build, contracts-deploy-anvil
```

## Layout

| Path | What it is |
|------|-----------|
| `cmd/monolith/` | monolith entry — every plugin in one process (port 8080) |
| `cmd/microservices/` | 9 standalone entries: api-gateway, auth, transcoder, streaming, upload, metadata, cache, worker, monitor |
| `pkg/core/` | microkernel, `GenericPlugin`, config, event bus, logger |
| `pkg/plugins/` | 9 thin plugin wrappers — delegate to `pkg/service/` |
| `pkg/service/` | business logic; subpackages `content/ transcoding/ streamingsvc/ gating/ upload/ category/ playbackstats/ sigchain/` |
| `pkg/gateway/` | REST handlers, one file per domain (`<domain>_handlers.go`); `routes.go` is the single registration point |
| `pkg/middleware/` | auth, `nft_gate`, `playback_token_auth`, cors, circuitbreaker, ratelimit, tracing |
| `pkg/web3/` | signature, `nft/` verifier, `contract/`, `solana/`, reorg, gas, events |
| `pkg/storage/` | PostgreSQL, Redis, MinIO/S3, NATS, cache |
| `test/` | unit, integration, e2e, benchmark, security |
| `migrations/` | 35 up / 19 down SQL migrations |
| `h5-demo/`, `examples/` | frontend demo and standalone samples — not part of the Go build |

## Component Status

`Ready` means tested and wired into the running system; everything else — auth, NFT verification, gating, streaming, transcoding, upload, worker, storage — is Ready (full table in `README.md` → Product Roadmap). These two are not:

| Component | Status | Notes |
|---|---|---|
| `pkg/web3/account_abstraction.go` | **Unused** | `IAccount` / `UserOperation` / `AAProvider` exist, including entry-point validation and nonce expiry, but nothing constructs them. ERC-4337 is out of scope — see `TRANSITION_PLAN.md` appendix D |
| `pkg/web3/contract/registry.go` (`SmartContractRegistry`) | **Unused** | complete in-memory registry API (register, get by name / address / chain) but never constructed outside tests. Contains no TODO markers |

## Conventions

### Code
- **Business logic lives in `pkg/service/`**, never in plugins. Plugins are thin wrappers over `core.GenericPlugin` plus a server struct.
- Service names are lowercase-with-dashes (`api-gateway`, `transcoder`).
- **Comments: none by default.** Required only for security-sensitive operations (signature verification, replay protection, block-tag safety), complex regex, or third-party workarounds.
- Linter `.golangci.yml`: govet, staticcheck, errcheck, gocritic, misspell, gocyclo (max 35).

### Testing
- `testify` for assertions, `uber/mock` for mocks — run `go generate ./...` after changing an interface.
- Table-driven preferred (see `pkg/service/auth_wallet_test.go`). Names: `Test<Function>_<Scenario>`. `-race` always on.
- Categories: unit (no tag) · integration (PostgreSQL + Redis; CI provides them) · `anvil` tag (Foundry) · `testnet` tag (`SEPOLIA_RPC`).
- Coverage floor 60%, enforced in CI.

### Dependencies
- Ask before adding one; record every change in `DEPENDENCY_APPROVAL.md`. Prefer stdlib. Gin is already in use.

### Git
- `type(scope): description` — `feat | fix | refactor | test | docs | chore`. One logical change per commit.
- No force-push to shared branches. A PR must pass build + lint + test.
- **Push only when asked** — each push triggers CI, and rapid pushes cancel the previous run.

### Documentation
- `TRANSITION_PLAN.md` is the **only** plan document. **Never create process documents** — no review reports, handovers, status files or dated scratch `.md`. Durable conclusions go there.
- Where content belongs: agent knowledge → this file / `AGENTS.md`; evidence → a CI run id or commit hash; dependency changes → `DEPENDENCY_APPROVAL.md`; job material → `docs/resume/`.
- Clean up generated artifacts when done: `bin/`, `out/`, `cache/`, `target/`, `coverage.out`, `deploy/docker/*-linux`, `scripts/h5-demo-acceptance/reports/`. Do **not** delete `.workbuddy/`.
- Before deleting or renaming a document, `grep -rn "<path>" --include="*.md" .` and repoint inbound links.

## Task Completion Checklist

```
[ ] go build ./cmd/monolith/streamgate   # fast compile check only
[ ] make fmt && make lint
[ ] make test          # pre-existing failures: document, do not chase
[ ] make test-anvil    # if touching pkg/web3/
[ ] go mod tidy
```
**Authoritative gate: CI (`ci.yml` + `build.yml`)** — it compiles, lints, tests and Trivy-scans inside containers. A green host run is a pre-check, not acceptance.

## Known Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| `make test` fails on DB connection | PostgreSQL not running | `docker-compose up -d postgres redis` |
| `make test-anvil` fails | Foundry not installed | `brew install foundry` or `foundryup` |
| `make lint` fails on lines you didn't touch | pre-existing baseline | fix only your changed lines |
| NFT verify returns a wrong result | using the `latest` block tag | use `safe` / `finalized` |
| go-ethereum RPC timeout | no RPC, or rate limited | set `WEB3_ETHEREUM_RPC` in `.env` |
| `go mod tidy` drops needed packages | stale `go.sum` | `go mod download && go mod tidy` |
| Docker build fails | BuildKit disabled | `DOCKER_BUILDKIT=1` |
| Host `golangci-lint` refuses the module | host binary built with Go ≤ 1.25 | use a v2.12+ build, or let CI lint |
| Microservice events error outside Docker | the Dockerfiles pass `-tags nats_integration`; a host `make build-all` does not, so `NATSEventBus` is the stub | run the service from its image |

## Anti-Patterns

- **NEVER** skip the NFT ownership check before delivering an HLS manifest.
- **NEVER** expose raw video segments without authentication.
- **NEVER** use a single RPC endpoint — always a fallback chain.
- **NEVER** introduce speculative features or premature abstractions.
- **NEVER** take the authorization subject (contract, token id, chain) from the request — it comes from server-side `content_gating_rules` only.

## Key Design Decisions

1. **Monolith-first** — one process for development; split into microservices only when scale requires it.
2. **Plugins are thin wrappers** over `pkg/service/`, which holds the logic.
3. **Redis Lua for challenge consumption** — atomic, so a challenge cannot be replayed (TOCTOU).
4. **`safe`/`finalized` block tags, never `latest`** — a reorg must not grant access.
5. **The gate fails closed** — no rules → `403`, resolver error → `500`, gate disabled → `503`.
6. **Playback tokens are wallet-bound capabilities**, carrying an explicit `token_type` claim and minted only after the gate passes. HLS.js cannot send `Authorization` on media requests, so the token travels in the query string — middleware validates it cryptographically; its mere presence never grants access.

## For agents

### Read exclusion
`.claudeignore` is the single source of truth for what not to read. Don't restate that list here — duplicating it is what makes the two files disagree. Also skip `Makefile` and `README.md`: every command you need is above, and `README.md` is written for human visitors.

### Core execution path
These 10 files explain auth → gate → stream. Read them first, then grep for specifics.

```
cmd/monolith/streamgate/main.go         entry point, plugin loading
pkg/plugins/auth/plugin.go              auth plugin → route registration
pkg/service/auth_wallet.go              wallet sign-in, playback tokens
pkg/web3/signature/                     EIP-191 / EIP-712 / SIWE verification
pkg/middleware/nft_gate.go              NFT gate (fail-closed), rule resolution
pkg/middleware/playback_token_auth.go   validates playback_token ahead of JWT
pkg/web3/nft/verifier.go                ownership check, ERC-165 detection
pkg/gateway/routes.go                   THE route registration point, middleware order
pkg/service/streamingsvc/streaming.go   manifest delivery
pkg/service/transcoding/transcoding.go  FFmpeg pipeline and worker pool
```

### Route map
Every route registers in `pkg/gateway/routes.go` (`registerRoutes`, called from `SetupRouter`). The public list below is exempted from JWT via `SkipPaths`.

```
Public (no JWT):
  GET  /health /metrics /ready /docs
  POST /api/v1/auth/{challenge,login,register,refresh}
  GET  /api/v1/web3/{rpc-status,supported-chains}

Streaming group — chain [PlaybackTokenAuth, JWT, NFTGate]
(routes.go: streamGroup :68, playbackAuth :69, JWT :72, NFTGate :227, routes :235-236):
  GET  /api/v1/streaming/:id/segment/:num     requires a validated playback_token — NOT public
  GET  /api/v1/streaming/:id/manifest.m3u8

JWT-protected:
  /api/v1/auth/{profile,change-password,logout,verify}
  /api/v1/nft{/,/:id}   POST /api/v1/nft/verify   POST /api/v1/nft/dev/mint (dev-only, needs AnvilDeployerKey)
  /api/v1/upload{/,/init,/chunk}   POST /api/v1/upload/:id/{complete,complete-upload}
  GET /api/v1/upload/{list,:id/status,:id/download-url}
  /api/v1/content{/,/:id}   PUT + DELETE /api/v1/content/:id
  /api/v1/transcode/{submit,cancel/:id,status/:id,tasks,profiles}
  /api/v1/categories{/:id}   /api/v1/gating-rules{/:id}
  /api/v1/playback/{stats,events}
```

### Interfaces

| Interface | File | Implementations |
|---|---|---|
| `Plugin` | `pkg/core/microkernel.go` | 9 plugins |
| `ServerLifecycle` | `pkg/core/generic_plugin.go` | all plugin servers (Start/Stop/Health) |
| `EventBus` | `pkg/core/event/event.go` | MemoryEventBus (monolith), NATSEventBus (microservices) |

Microservice mains do their own wiring (89 lines each; api-gateway 118) — `core.RunMicroservice()` exists but has no callers.

### Workflow
- `grep` before LSP when locating a symbol; `pkg/service/` is the primary logic layer. Use relative paths in file references.
- Verify locally with `make test`, but take acceptance from CI or the Docker stack — not from a host build.
