# StreamGate Development Guide

NFT-gated streaming platform built with Go. Combines traditional high-concurrency architecture with blockchain permission control.

## Project Overview

**Core Flow**: Wallet sign-in → NFT verification → Protected HLS streaming

- **Go 1.24** | **Gin** HTTP framework | **go-ethereum** + **Solana SDK** multichain
- **Microkernel Plugin Architecture** with dual-mode deployment (monolith + microservices)
- **10 build targets**: 1 monolith + 9 microservices
- **255 test files**: unit (4), integration (14), E2E (19), benchmark (5), load (4), security (1), performance (1) — plus 206 in-package tests under `pkg/` and the 27K `cmd/microservices/api-gateway/main_test.go`. Total coverage 74.5% (81.7% by function), well above the 40% CI floor.

## Tech Stack

| Layer | Technology |
|-------|-----------|
| HTTP | Gin (pkg/gateway/) |
| DB | PostgreSQL 15 (lib/pq) |
| Cache | Redis 7 (go-redis) |
| Object Storage | MinIO / S3 |
| Message Queue | NATS |
| Service Discovery | Consul |
| Blockchain | go-ethereum (EVM), solana-go |
| Streaming | HLS / DASH via FFmpeg |
| Observability | Zap + Prometheus + OpenTelemetry + Jaeger |
| RPC | gRPC + Protocol Buffers |
| Orchestration | Docker Compose + Kubernetes |
| Test | testify + vegeta + uber/mock |
、
## Project Structure

```
streamgate/
├── cmd/
│   ├── monolith/streamgate/main.go        # Monolith entry (port 8080)
│   └── microservices/                     # 9 microservice entries
│       ├── api-gateway/main.go            # :9090 HTTP + :9091 gRPC
│       ├── auth/main.go                   # :9007 - Wallet + NFT auth
│       ├── transcoder/main.go             # :9092 - FFmpeg transcoding
│       ├── streaming/main.go              # :9093 - HLS/DASH delivery
│       ├── upload/main.go                 # :9091 - Chunked upload
│       ├── metadata/main.go               # :9005 - Content metadata
│       ├── cache/main.go                  # :9006 - LRU + Redis cache
│       ├── worker/main.go                 # :9008 - Task queue worker
│       └── monitor/main.go                # :9009 - Health + metrics
├── pkg/
│   ├── core/           # Microkernel, GenericPlugin, RunMicroservice, config, event bus, logger
│   ├── plugins/        # 9 plugins (api, auth, cache, metadata, monitor, streaming, transcoder, upload, worker)
│   ├── service/        # Business logic layer
│   ├── web3/           # NFT, signature, multichain, reorg, gas, wallet, events, ERC-4337 (79 .go files)
│   ├── middleware/      # auth, cors, circuitbreaker, ratelimit, nft_gate, otel tracing
│   ├── storage/         # PostgreSQL, Redis, MinIO, S3, NATS, cache
│   ├── resilience/      # Circuit breaker (gobreaker-backed), retry helpers
│   ├── models/          # Content, NFT, User, Task, Transaction, Transcoding
│   ├── gateway/         # REST handlers (auth, category, content, gating_rule, NFT, playback_stats, streaming, transcode, upload, web3)
│   ├── monitoring/      # Prometheus metrics, Grafana, alerts, OTEL tracing
│   ├── api/            # protobuf-generated gRPC stubs (21 files under pkg/api/v1/)
│   └── util/ + health/ + cachetypes/   # Shared utilities
├── config/              # Multi-env configs (dev, test, prod)
├── deploy/docker/       # Dockerfiles for all 10 services
├── deploy/k8s/          # Kubernetes deployment manifests
├── test/                # unit/, integration/, e2e/, benchmark/, load/, security/, performance/, helpers/, fixtures/
├── migrations/          # SQL schema migrations
├── docs/                # Extensive documentation
├── examples/            # Demo code (nft-verify, signature-verify, streaming, upload)
└── h5-demo/             # Frontend demo (wallet connect, NFT verify, video player)
```

## Component Status

All components listed below. Production-ready items pass tests and have no TODOs.

| Component | Status | Notes |
|-----------|--------|-------|
| Wallet Sign-In | **Ready** | `pkg/service/auth_wallet.go` — EIP-191/712/SIWE, Solana ed25519 |
| NFT Verification | **Ready** | `pkg/web3/nft.go` — ERC-721/1155, ERC-165 detection |
| HLS Streaming | **Ready** | `pkg/service/streaming.go` — Multi-bitrate, DASH |
| NFT Gate Middleware | **Ready** | `pkg/middleware/nft_gate.go` — Protected content access |
| Reorg Protection | **Ready** | `pkg/web3/reorg.go` — Safe/finalized block tags |
| Event Indexing | **Ready** | `pkg/web3/event_indexer.go` — Full event parsing and storage |
| Transcoding | **Ready** | `pkg/service/transcoding.go` — FFmpeg pipeline with retry logic |
| Multi-chain | **Ready** | EVM + Solana + RPC failover; cross-chain bridge is future scope |
| `pkg/web3/contract.go` | **Ready** | Batch queries and error classification implemented |
| `pkg/web3/solana.go` | **Ready** | RPC fallback and Metaplex support implemented |
| `pkg/plugins/worker/handler.go` | **Ready** | Task distribution and failure recovery implemented |
| `pkg/plugins/streaming/handler.go` | **Ready** | Adaptive bitrate negotiation implemented |
| `pkg/web3/smart_contracts.go` | **Partial** (1 TODO) | Contract ABI resolution mostly complete |
| `pkg/web3/account_abstraction.go` | **Partial** (skeleton) | `IAccount` interface + `UserOperation` struct only; no real bundler/entry-point integration yet |
| `pkg/plugins/metadata/server.go` | **Ready** | gRPC service and search indexing implemented |
| `pkg/plugins/monitor/server.go` | **Ready** | Aggregated health and alert routing implemented |
| Microservice main.go files (9) | **Ready** | 10 lines each via `core.RunMicroservice` |

## Commands

```bash
# Build
make build-monolith        # Single binary
make build-all             # All 10 binaries (1 monolith + 9 services)
make build-api-gateway     # Individual service build

# Run
make run-monolith          # Build + run monolith (port 8080)
make run-api-gateway       # Build + run API gateway (port 9090)
make dev                   # Quick dev mode
make dev-setup             # One-command dev environment setup

# Test
make test                  # All tests with race detection + coverage HTML
make test-ci               # CI mode with 40% coverage threshold
make test-anvil            # Foundry Anvil Web3 integration tests
make test-testnet          # Testnet integration tests (needs SEPOLIA_RPC)

# Docker
make docker-build          # Build all 10 Docker images
make docker-bake           # Build multi-arch images via Docker Bake
make docker-up             # Start docker-compose stack
make docker-down           # Stop docker-compose stack
make fullchain-deploy      # Full stack: PG + Redis + MinIO + NATS + monolith + H5
make fullchain-test        # Test full stack deployment

# Demo
make demo                  # One-command: infra up → build → run
make demo-down             # Stop demo and clean up infra

# Smart Contracts (Foundry)
make contracts-build       # Build Solidity contracts
make contracts-test        # Run Foundry tests
make contracts-deploy-anvil  # Deploy to local Anvil
make contracts-deploy-sepolia # Deploy to Sepolia testnet

# Code Quality
make lint                  # golangci-lint
make lint-fix              # Auto-fix lint issues
make fmt                   # go fmt
make bench                 # Run benchmarks
make mocks                 # Generate mocks (go generate ./...)

# Proto / DB
make proto-gen             # Generate protobuf gRPC stubs
make migrate-up            # Run DB migrations
make migrate-down          # Rollback DB migrations
make migrate-reset         # Rollback all + re-migrate

# K8s
make k8s-deploy            # Deploy to Kubernetes
make k8s-status            # Check deployment status
```

## Task Completion Checklist

Before marking any task complete, run through these checks:

```
[ ] go build ./cmd/monolith/streamgate        # compiles
[ ] make fmt                                   # formatting clean
[ ] make lint                                  # no new warnings
[ ] make test                                  # all tests pass (note pre-existing failures)
[ ] If touching pkg/web3/: make test-anvil     # Web3 integration tests (requires Foundry)
[ ] go mod tidy                                # dependency tree clean
[ ] No `as any` / @ts-ignore / @ts-expect-error  # type safety (not applicable in Go codebase)
```

If a test fails:
1. Check if it's a **pre-existing failure** — run `git stash && make test` on clean code
2. If pre-existing → document it, do not chase
3. If your change caused it → fix minimally, re-verify

## Conventions

### Code Organization
- **Plugin Architecture**: Each microservice is a plugin (`pkg/plugins/<name>/plugin.go`) implementing the core plugin interface
- **Dual deployment**: Monolith runs the API Gateway plugin in-process; microservices run individual plugins as standalone processes
- **Service naming**: Lowercase with dashes (`api-gateway`, `transcoder`)
- **Business logic**: Lives in `pkg/service/`, not in plugins — plugins are thin wrappers
- **Comments**: Default is no comments — code should be self-documenting. Exception: MUST comment security-sensitive operations (signature verification, replay protection, block tag safety), complex regex, workarounds for third-party bugs, and performance-critical hot paths with non-obvious optimizations

### Testing

**Test Framework**: `testify` for assertions, `uber/mock` for mocking.

**Mock Generation**: After changing any interface, run:
```bash
go generate ./...
```
Mock files live alongside interfaces with `//go:generate` directives.

**Test Categories** (in order of preference):

| Category | Tag | External Deps | Running |
|----------|:---:|:---|:---|
| Unit test | none | None | `make test` (default) |
| Integration | — | PostgreSQL, Redis | `make test` (CI starts containers) |
| Web3 (Anvil) | `anvil` | Foundry (anvil) | `make test-anvil` |
| Testnet | `testnet` | SEPOLIA_RPC env | `make test-testnet` |

**Patterns**:
- **Table-driven tests preferred** — see `pkg/service/auth_wallet_test.go` for reference
- **Test names**: `Test<FunctionName>_<Scenario>` — e.g. `TestVerifySignature_InvalidNonce`
- **Coverage**: 40% minimum (CI enforced). Focus on core flow: auth → NFT verify → streaming
- **Race detection**: Always enabled (`-race` flag in `make test`)

### Dependency Management
- **Ask before adding new dependencies** — update `DEPENDENCY_APPROVAL.md`
- Prefer Go stdlib over third-party packages when sufficient
- Gin is already in use for HTTP — use it consistently

### Code Style
- Linter: `.golangci.yml` — govet, staticcheck, errcheck, gocritic, misspell, gocyclo (max 35)
- Format with `go fmt` before commit
- Max function complexity: 35 (gocyclo)

### Git Conventions
- **Commit message**: `type(scope): description` — e.g. `feat(auth): add SIWE sign-in`, `fix(streaming): resolve manifest race condition`
- **Types**: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`
- One logical change per commit (squash WIP commits before PR)
- Rebase onto master before opening PR (no merge commits)
- No force-push to shared branches without confirmation
- PR must pass: build + lint + test

### Common Issues & Recovery

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| `make test` fails with DB connection | PostgreSQL not running | `docker-compose up -d postgres redis` |
| `make test-anvil` fails | Foundry not installed | `brew install foundry` or `curl -L https://foundry.paradigm.xyz | bash` |
| `make lint` fails on pre-existing issues | Baseline lint errors | Only fix issues in your changed lines; ignore unrelated failures |
| NFT verify returns wrong result | Using `latest` block tag | Switch to `safe`/`finalized` in chain config |
| go-ethereum RPC timeout | No RPC endpoint / rate limited | Set `WEB3_ETHEREUM_RPC` in `.env` with a working Infura/Alchemy URL |
| `go mod tidy` removes needed packages | go.sum out of sync | `go mod download && go mod tidy` |
| Build fails in Docker | BuildKit not enabled | Set `DOCKER_BUILDKIT=1` env, or use Docker Desktop 4.0+ |
| Component doesn't work as expected | Component listed as Partial in Component Status | Check Component Status table; Partial components need implementation |

## Anti-Patterns

- **NEVER** skip NFT ownership check before HLS manifest delivery
- **NEVER** expose raw video segments without authentication
- **NEVER** use a single RPC endpoint (must have fallback chain)
- **NEVER** introduce speculative features or premature abstractions
- **NEVER** add unnecessary comments — see Comment Policy above for allowed exceptions

## Key Design Decisions

1. **Monolith-first**: Run everything in one process for development/debugging, split to microservices only when scaling requires it
2. **Plugin as thin wrapper**: Plugins (`pkg/plugins/`) use `core.GenericPlugin` and delegate to server structs + `pkg/service/`. Plugin files are ~20 lines each
3. **Redis Lua scripts for atomicity**: Challenge consumption in auth uses Lua scripts to prevent TOCTOU replay attacks
4. **Block tag safety**: NFT verification uses `safe`/`finalized` block tags, not `latest`, to prevent reorg-related bypass
5. **Dual streaming protocol**: HLS as primary, DASH as secondary — both generated from same transcoding pipeline

## Quick Reference for Common Tasks

| Task | Location |
|------|----------|
| Add new REST endpoint | `pkg/gateway/<domain>_handlers.go` |
| Add business logic | `pkg/service/<domain>.go` |
| Add Web3 functionality | `pkg/web3/` |
| Add middleware | `pkg/middleware/` |
| Add data model | `pkg/models/` |
| Add storage backend | `pkg/storage/` |
| Register a new plugin | `pkg/plugins/<name>/plugin.go` + create `cmd/microservices/<name>/main.go` |
| Add DB migration | `migrations/` |

## For Claude Code

### Token Efficiency

These files/directories are excluded via `.claudeignore` — do not attempt to read them:
- `docs/` (27K lines, 60 files) — design docs; everything needed is in this file
- `examples/` (19 files) — standalone demo code
- `h5-demo/` (16 files) — frontend demo; not part of Go codebase
- `pkg/api/` (21 files) — protobuf-generated gRPC stubs; patterns captured in Interface Map
- `bin/` — compiled binaries
- `dist/` — build artifacts
- `.comate/`, `.deploy/`, `scripts/`, `.github/` — CI/internal tooling
- `.idea/`, `.vscode/` — IDE configs
- `go.sum` (768 lines) — pure checksums
- `.golangci.yml` (86 lines) — lint config; summarized as "govet + staticcheck + errcheck + gocritic + misspell + gocyclo(max 35)"
- `.env.example` (61 lines) — only needed for initial setup

Files to skip unless specifically asked about (commands are listed above):
- `Makefile` — all build/test/docker commands are documented in this file
- `README.md` — written for human GitHub visitors; use this file for project reference

### Core Execution Path

To understand the auth → gate → stream pipeline, read these 10 files:

```
1. cmd/monolith/streamgate/main.go        Entry point, plugin loading
2. pkg/plugins/auth/plugin.go             Auth plugin → route registration
3. pkg/service/auth_wallet.go             Wallet sign-in logic
4. pkg/web3/signature.go                  EIP-191/712 verification
5. pkg/middleware/nft_gate.go             NFT-gated access middleware
6. pkg/web3/nft.go                        NFT ownership verification
7. pkg/plugins/streaming/plugin.go        Streaming plugin → route registration
8. pkg/service/streaming.go               HLS/DASH manifest delivery
9. pkg/plugins/transcoder/plugin.go       Transcoding plugin
10. pkg/service/transcoding.go            FFmpeg pipeline
```

Read these first, then use grep/lsp for specifics.

### Route Map

All routes are registered in `pkg/gateway/routes.go:25 registerRoutes()` (called from `pkg/gateway/gateway.go:120 SetupRouter`):

```
Public (no JWT):
  GET  /health /metrics /ready /docs
  POST /api/v1/auth/{challenge,login,register,refresh}
  GET  /api/v1/web3/{rpc-status,supported-chains}
  GET  /api/v1/streaming/:id/segment/:num       ← playback_token in query

JWT-protected (pkg/gateway/auth_handlers.go):
  GET  /api/v1/auth/profile
  POST /api/v1/auth/{change-password,logout,verify}

JWT + Circuit Breaker (pkg/gateway/nft_handlers.go):
  GET    /api/v1/nft{/,/:id}
  POST   /api/v1/nft/verify
  POST   /api/v1/nft/dev/mint                   ← dev-only, gated by AnvilDeployerKey

JWT (pkg/gateway/upload_handlers.go):
  POST /api/v1/upload{/,/init,/chunk}
  POST /api/v1/upload/:id/{complete,complete-upload}
  GET  /api/v1/upload/{list,:id/status,:id/download-url}

JWT (pkg/gateway/content_handlers.go):
  GET    /api/v1/content{/,/:id}
  POST   /api/v1/content
  PUT    /api/v1/content/:id
  DELETE /api/v1/content/:id

JWT (pkg/gateway/transcode_handlers.go):
  POST /api/v1/transcode/{submit,cancel/:id}
  GET  /api/v1/transcode/{status/:id,tasks,profiles}

JWT + NFT Gate (pkg/gateway/streaming_handlers.go):
  GET  /api/v1/streaming/:id/manifest.m3u8

JWT (pkg/gateway/category_handlers.go):
  GET/POST/PUT/DELETE /api/v1/categories{/:id}

JWT (pkg/gateway/gating_rule_handlers.go):
  GET/POST/PUT/DELETE /api/v1/gating-rules{/:id}

JWT (pkg/gateway/playback_stats_handlers.go):
  GET  /api/v1/playback/stats{/:content_id}
  POST /api/v1/playback/events
```

### Interface Map

| Interface | File | Implementations |
|-----------|------|----------------|
| `Plugin` | `pkg/core/microkernel.go:15` | 9 plugins: api, auth, cache, metadata, monitor, streaming, transcoder, upload, worker |
| `ServerLifecycle` | `pkg/core/generic_plugin.go:12` | All plugin servers (Start/Stop/Health) |
| `EventBus` | `pkg/core/event/event.go:40` | MemoryEventBus (monolith), NATSEventBus (microservices) |
| Storage backends | `pkg/storage/` | postgres, redis, minio, s3, nats, cache |

Plugin implementations are in `pkg/plugins/<name>/plugin.go`. Each uses `core.GenericPlugin` to delegate to a `pkg/service/` layer. Microservice entry points call `core.RunMicroservice()` — see `cmd/microservices/<name>/main.go` (10 lines each).

### Workflow Tips

- **Symbol search**: use `grep` before `lsp_find_references` for faster location.
- **Interface tracing**: `lsp_goto_definition` for single hops; avoid deep chains — `pkg/service/` is the primary logic layer.
- **Before modifying**: check Component Status table — do not debug skeleton code expecting production behavior.
- **Prefer tool discovery**: grep/lsp over re-reading this file for navigation.
- **Use relative paths** for all file references (e.g. `pkg/service/auth_wallet.go`).
- **Test patterns are documented above** — do not read test files to learn conventions. Use `make test` to verify.