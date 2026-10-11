# PROJECT KNOWLEDGE BASE

> **⚠️ Claude Code: do NOT read this file for project context.**
> Read `CLAUDE.md` instead — it is the canonical source.
> This file exists for the Sisyphus/OpenCode orchestration system.

> **Scope:** this file is a condensed, agent-neutral digest. Where it and `CLAUDE.md`
> disagree, `CLAUDE.md` wins — and the disagreement is a bug to fix. Volatile facts
> (counts, sizes, file trees) deliberately live only in `CLAUDE.md`; restating them here
> is exactly how the two files drifted apart.

## OVERVIEW

Go-based NFT-gated streaming platform. Combines traditional high-concurrency architecture with blockchain permission control. Core flow: wallet sign-in → NFT verification → protected HLS streaming.

## STRUCTURE

```
streamgate/
├── cmd/                # Entry points (1 monolith + 9 microservices)
│   ├── monolith/       # Single binary (port 8080)
│   └── microservices/  # api-gateway, auth, transcoder, streaming, upload,
│                       # metadata, cache, worker, monitor
├── pkg/
│   ├── core/           # Microkernel, GenericPlugin, RunMicroservice, config, event bus, logger
│   ├── plugins/        # 9 plugin implementations (api, auth, cache, metadata, monitor, streaming, transcoder, upload, worker)
│   ├── service/        # Business logic layer; subpackages content/, transcoding/, streamingsvc/, gating/, upload/, category/
│   ├── web3/           # Blockchain: signature, nft/, contract/, solana/, tx/, reorg, gas, event/
│   ├── gateway/        # REST API handlers (auth, category, content, gating_rule, NFT, playback_stats, streaming, transcode, upload, web3)
│   ├── middleware/     # HTTP middleware: auth, cors, circuitbreaker, ratelimit, nft_gate, tracing
│   ├── models/         # Data models (Content, NFT, User, Task, Transaction, Transcoding)
│   ├── storage/        # Storage layer: PostgreSQL, Redis, MinIO, S3, NATS, cache
│   ├── resilience/     # Circuit breaker (gobreaker-backed), retry helpers
│   ├── monitoring/     # Prometheus metrics, alerts, OTEL tracing
│   └── api/            # protobuf-generated gRPC stubs
├── config/             # Multi-env configs (dev, test, prod)
├── deploy/             # Docker + K8s deployment manifests
├── migrations/         # SQL schema migrations
├── test/               # unit, integration, e2e, benchmark, performance, security
├── examples/           # Demo code (nft-verify, signature-verify, streaming, upload)
└── h5-demo/            # Frontend demo (wallet, NFT verify, video player)
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Wallet sign-in | `pkg/service/auth_wallet.go` | EIP-191/712/SIWE, Solana ed25519 |
| NFT verification | `pkg/web3/nft/` | ERC-721/1155, ERC-165 detection |
| Transcoding | `pkg/service/transcoding/transcoding.go` | FFmpeg worker pipeline, HLS output |
| Streaming | `pkg/service/streamingsvc/streaming.go` | HLS/DASH manifest delivery |
| API routes | `pkg/gateway/` | REST handlers (auth_handlers, content_handlers, etc.) |
| NFT-gated access | `pkg/middleware/nft_gate.go` | Middleware that blocks unauthenticated streaming |
| Web3 utils | `pkg/web3/` | signature, nft/, contract/, solana/, tx/, reorg, gas, event/ |
| DB/storage | `pkg/storage/` | PostgreSQL, Redis, MinIO, S3, NATS queue |
| Plugin entry | `pkg/plugins/<name>/plugin.go` | Each microservice has a plugin implementation |
| Config | `config/config.yaml` | Default config; env-specific in config.dev/prod/test.yaml |

## CONVENTIONS (THIS PROJECT)

- **Plugin Architecture**: `pkg/core/` provides microkernel, GenericPlugin, and RunMicroservice; plugins at `pkg/plugins/` are 20-line wrappers delegating to server structs + `pkg/service/`
- **Dual deployment**: Monolith runs API Gateway plugin in-process (dev); microservices run individual plugins standalone (prod)
- **Service naming**: Lowercase with dashes (api-gateway, transcoder)
- **Code generation**: `go generate ./...` for mocks (uber/mock)
- **Comments**: Default is no comments; MUST comment security-sensitive operations (signature verification, replay protection, block-tag safety), complex regex, and third-party workarounds
- **Testing**: testify for assertions; build tags (`anvil`, `testnet`) for chain-dependent tests; 60% coverage minimum (CI)
- **Documentation**: `TRANSITION_PLAN.md` is the **only** plan document. **NEVER** create temporary/process docs (review reports, handovers, status files, dated `*.md` scratch files) — durable conclusions go into `TRANSITION_PLAN.md`. Clean up generated artifacts when done (`bin/`, `out/`, `cache/`, `target/`, `coverage.out`, `deploy/docker/*-linux`, `scripts/h5-demo-acceptance/reports/`). Full rule in `CLAUDE.md` → Documentation.

## ANTI-PATTERNS (THIS PROJECT)

- NEVER skip NFT ownership check before HLS manifest delivery
- NEVER expose raw video segments without auth
- NEVER use single RPC endpoint (must have fallback chain)
- NEVER introduce speculative features or premature abstractions
- NEVER create temporary or process documents, or leave generated artifacts behind (see `CLAUDE.md` → Documentation)

## COMMANDS

```bash
make build-monolith       # Build single binary
make build-all            # Build all 11 binaries (1 monolith + 9 services + cmd/learn)
make test                 # Run tests with race detection + coverage HTML
make test-ci              # CI mode with 60% coverage threshold
make test-anvil           # Foundry Anvil Web3 integration tests
make test-testnet         # Testnet integration tests (needs SEPOLIA_RPC)
make docker-up            # Start Docker Compose
make docker-down          # Stop Docker Compose
make fullchain-deploy     # PG + Redis + MinIO + NATS + Anvil(+DemoNFT) + monolith + microservices + H5
make lint                 # Run golangci-lint
make fmt                  # Format code
make bench                # Run benchmarks
make mocks                # Generate mocks
make k8s-deploy           # Deploy to Kubernetes
```

## NOTES

- Core flow (auth → NFT → streaming) is implemented end-to-end and pinned by regression tests
- 11 build targets: 1 monolith + 9 microservices + cmd/learn, all compiled and runnable
- Dual-mode deployment (monolith first, microservices for scale)
- Project goals: interview preparation, Web3 capability demonstration