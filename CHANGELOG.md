# Changelog

All notable changes to StreamGate are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.1-alpha] — 2026-05-16

### Added
- Interactive CLI learning tool (`cmd/learn/`) — 5 modules on Web3+Go concepts
- Challenge mode exercises (`examples/challenges/`) — 4 bug-fix challenges
- Formal SLO definitions with Prometheus burn-rate alerts
- Prometheus recording rules for SLO metrics

### Added
- Interactive CLI learning tool (`cmd/learn/`) — 5 modules on Web3+Go concepts
- Challenge mode exercises (`examples/challenges/`) — 4 bug-fix challenges
- Formal SLO definitions with Prometheus burn-rate alerts
- Prometheus recording rules for SLO metrics

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
