# Changelog

All notable changes to StreamGate are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] — 2026-05-19

StreamGate v1.0.0 marks the first stable release after 20 rounds of architecture hardening, security review, and production readiness optimization. All P0 core features are stable and tested.

### For Platform Developers

- **Wallet Sign-In**: EIP-191, EIP-712, SIWE (EIP-4361), and Solana ed25519 — verified and hardened
- **NFT Verification**: ERC-721/1155 with ERC-165 auto-detection, CheckApproval for TOCTOU protection
- **Solana Multi-endpoint Failover**: Added `SolanaMultiClient` with score-based failover (matching EVM ChainClient)
- **JWT Key Rotation**: Comma-separated `AUTH_JWT_SECRETS` — first signs, rest verify
- **Per-Wallet Rate Limiting**: `WalletRateLimiter` — 10 req/min default, IP fallback
- **gRPC Standard Health Protocol**: `grpc_health_v1` for Kubernetes readiness probes
- **gRPC TLS**: Certificate-based server-side TLS via config
- **OpenAPI 3.0 Spec**: Complete 1105-line specification at `docs/api/openapi.yaml`

### For Content Creators

- **One-Command Demo**: `make demo` — auto-starts infra, builds, and runs
- **Demo Mode**: `STREAMGATE_DEMO_MODE=true` for blockchain-free testing
- **Chunked Upload**: Resumable with integrity verification
- **HLS Streaming**: Per-user playback tokens (2min TTL, bound to wallet+content+contract)
- **FFmpeg Transcoding**: Multi-profile (1080p/720p/480p/360p) HLS + DASH

### For Node Operators

- **Graceful Shutdown**: Context-aware with 30s global timeout, per-operation timeout
- **Configuration Hot Reload**: Viper-based with thread-safe update, change handlers
- **PostgreSQL Connection Pool**: Configurable with circuit breaker protection
- **EventBus NATS Fallback**: Degrades to MemoryEventBus on NATS failure
- **Database Migrations**: 31 versioned SQL files with rollback and dirty detection
- **CI/CD Pipeline**: GitHub Actions — lint → vet → test → build → security scan → Docker
- **Distroless Docker Image**: `docker build --target runtime-distroless .`
- **Prometheus RED Metrics**: Rate/Errors/Duration with service-level labels
- **OpenTelemetry Tracing**: Gin + gRPC integrated, export to Jaeger

### Security Hardening (20 rounds)

- Configuration redaction for all sensitive fields (DB password, private key, storage keys)
- ERC-1155 approval check added to existing ERC-721 check
- PostgresDB context cancellation bug fixed (rows returned with canceled context)
- K8s Secret template: envsubst required, no literal `${VAR}` in cluster
- CORS wildcard replaced with specific origins in dev/test configs
- Challenge ID UUID format validation
- `X-Content-Type-Options: nosniff` added to HLS manifest endpoint
- Transcoding queue errors logged instead of silently dropped
- ConfigManager deadlock fixed (handlers called outside write lock)
- MockAuthStorage data race fixed (added sync.Mutex)
- Migration 028 FK type mismatch fixed (VARCHAR↔UUID incompatibility)
- ContentRegistry smart contract registerContent() → onlyOwner

### Infrastructure

- Docker Compose services now have `restart: unless-stopped`
- NATS healthcheck improved (TCP port check vs binary existence check)
- Jaeger image pinned to 1.57.0 (was latest)
- Grafana/MinIO default passwords removed (must set via env)
- Request ID propagation across HTTP→gRPC boundaries
- `build-all` Makefile target includes `build-learn`

### Testing

- All 18 Go packages pass tests with race detection enabled
- Go vet: zero warnings across entire codebase
- Benchmarks: all 36 functions execute (was 58% skipped)
- WebSocket auto-reconnect tested with exponential backoff (1s→30s)

### Documentation

- Web3 glossary: 11 terms defined in README
- User personas: Platform Developer / Content Creator / Node Operator
- Product roadmap: P0 Core / P1 Usability / P2 Ecosystem

## [1.0.0] — 2025-05-16

### Added
- Request trace visualizer — middleware chain animation, flame graph, code path display
- H5 debug-learn page with breakpoint hints and code path traces
- Learner tools suite: comparison guide, chaos demo, interactive playground
- Web3 state debug endpoint (`/debug/web3-state`)
- 10 concept demonstration tests in `test/demo/concepts_test.go`
- Full-chain acceptance flow with Docker Compose
- NATSEventBus real NATS implementation
- Progressive NFT verification tutorial (3-stage demo)

### Changed
- Transitioned from microkernel skeleton to service-based architecture
- Migrated multi-Dockerfile layout to monolith-first deployment
- Aligned all builds and CI to Go 1.24

### Fixed
- NATS event bus skeleton replaced with real implementation
- Acceptance documentation aligned with Docker verification path
- Upload security hardening and storage streaming

## [0.9.0] — 2025-04-01

### Added
- NFT-gated HLS streaming with multi-bitrate support
- Wallet sign-in with EIP-191/712/SIWE and Solana ed25519
- NFT verification with ERC-721/1155, ERC-165 detection
- Reorg protection with safe/finalized block tags
- Event indexing subsystem for blockchain events
- Transcoding pipeline with FFmpeg
- Multi-chain support (EVM + Solana) with RPC failover
- JWT-based session management with RS256 signing
- Circuit breaker middleware for external calls
- Rate limiting middleware (initial implementation)

### Infrastructure
- Docker Compose stack: PostgreSQL 15, Redis 7, MinIO, NATS
- Kubernetes manifests with External Secrets Operator
- Prometheus + Grafana observability stack with pre-built dashboards
- Jaeger distributed tracing via OpenTelemetry
- GitHub Actions CI/CD with lint, test, build, security scanning
- Canary and blue-green deployment scripts

### Testing
- 132 test files: unit, integration, e2e, benchmark, load, security
- Foundry Anvil-based Web3 integration tests
- 40% coverage floor enforced in CI
- Load tests sustaining 5000+ req/s at 200 concurrency

### Security
- gosec + slither + Trivy in CI pipeline
- External Secrets Operator for K8s secrets management
- Formal dependency approval process (`DEPENDENCY_APPROVAL.md`)
- RBAC with least privilege on K8s service accounts
- AES-256-GCM encryption utilities
