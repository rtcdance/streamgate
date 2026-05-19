# StreamGate — Token-Gated Video Streaming for Web3

> **Token-gated video delivery.** Users prove NFT ownership to watch — no password, no piracy, no middleware.
>
> English: [Overview](#overview) · [Quick Start](#quick-start) · [API Docs](docs/api/API_DOCUMENTATION.md)
>
> 中文: [一句话介绍](#-一句话介绍) · [快速开始](#quick-start)

[![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker)](Dockerfile)
[![OpenAPI](https://img.shields.io/badge/API-OpenAPI_3.0-6BA539)](docs/api/openapi.yaml)

---

> 🚀 **One-command demo**: `make demo` — starts infra, builds, runs, ready to test in 60 seconds

---

## 💡 The Problem

Content creators lose revenue to piracy. Access control is either:
- **Passwords** (shared, stolen, phished)
- **DRM** (proprietary, requires licensing fees, locked to platforms)
- **Manual whitelists** (doesn't scale, no transparency)

StreamGate replaces all three with **on-chain NFT ownership**: hold the token, watch the video. No shared passwords. No DRM licensing. Programmable, transparent, permissionless.

## 🎯 一句话介绍

**持有特定 NFT 才能观看视频的内容分发平台**

```
用户持有 NFT → 验证所有权 → 获得观看权限 → 播放视频
```

---

## 📖 你要讲清楚的 4 件事

### 1. Wallet Sign-In
- 用户不需要密码，通过钱包签名登录
- 服务端做 `EIP-191` 验签、`nonce`、过期校验和防重放
- 这是项目里最关键的 Web3 身份入口

### 2. NFT Verify
- 服务端通过真实链上调用校验用户是否持有指定 NFT
- 只有持有 NFT，才允许访问受保护内容
- 这一步把链上所有权接到了业务访问控制上

### 3. Protected Streaming
- 用户通过验证后，才能获取 HLS manifest 或播放地址
- 流媒体主线是这个项目最像企业场景的地方
- 这里也是音视频经验和 Web3 能力结合最自然的点

### 4. Transcoding Worker
- 视频上传后通过 FFmpeg 转码为 HLS
- Worker 负责任务排队、执行、重试和状态更新
- 这是最能体现音视频后端经验的模块

### 一条主链路

```text
钱包签名登录 -> NFT 所有权校验 -> 放行 manifest -> 播放 HLS 视频
```

---

## 📖 Project Overview

StreamGate is a Go-based NFT-gated streaming project for learning and interview preparation. It combines a video distribution pipeline you would expect in a media backend with Web3 capabilities such as wallet sign-in, NFT ownership verification, and RPC reliability handling.

### 🎯 Why StreamGate?

**Token-based access replaces passwords, DRM, and whitelists.**

| Problem | Traditional Solution | StreamGate |
|---------|-------------------|------------|
| Password sharing | Rate limiting, MFA | **Impossible** — access = wallet signature |
| DRM licensing fees | $50K+/yr Widevine/FairPlay | **Free** — open source MIT |
| Manual access management | Admin dashboards, CSV exports | **Automatic** — own the NFT = get access |
| Piracy | Legal threats, DMCA takedowns | **Programmatic** — token validity is on-chain |
| Cross-platform | Multiple SDKs per platform | **One API** — REST/gRPC/OpenAPI |
- Turn the project into a strong interview story for Go + Web3 backend roles

### ✨ Current Focus

- 🔐 **Wallet Sign-In** - EIP-191 signature verification with nonce and replay protection
- 🪙 **NFT Verification** - ERC-721/1155 ownership checks using real chain calls
- 🎬 **Protected Streaming** - Use NFT ownership to gate HLS content access
- 🎞️ **Transcoding Worker** - FFmpeg-based job pipeline with queueing and retries
- 🧩 **Microkernel Architecture** - Keep the business flow clear in monolith mode, then split where it helps

## 🏗️ Architecture Design

### Microkernel Plugin Architecture

StreamGate uses a microkernel architecture with a minimal core and pluggable components:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Microkernel Core                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │
│  │ Plugin Mgr   │  │  Event Bus   │  │  Config Mgr  │           │
│  │ (Registry)   │  │  (In-Memory/ │  │  (YAML/Env)  │           │
│  │              │  │   NATS)      │  │              │           │
│  └──────────────┘  └──────────────┘  └──────────────┘           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │
│  │ Logger       │  │  Health Mgr  │  │  Lifecycle   │           │
│  │              │  │              │  │  Manager     │           │
│  └──────────────┘  └──────────────┘  └──────────────┘           │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
┌───────▼──────────┐  ┌───────▼──────────┐  ┌──────▼────────────┐
│ API Gateway      │  │ Storage/Upload   │  │ Blockchain/Auth  │
│ Plugin           │  │ Plugin           │  │ Plugin           │
│ - REST API       │  │ - File Upload    │  │ - NFT Verify     │
│ - gRPC Gateway   │  │ - S3/MinIO       │  │ - Signature Verify
│ - Rate Limiting  │  │ - Chunking       │  │ - Multi-chain    │
└──────────────────┘  └──────────────────┘  └──────────────────┘
        │                     │                     │
┌───────▼──────────┐  ┌───────▼──────────┐  ┌──────▼────────────┐
│ Transcoding      │  │ Streaming        │  │ Metadata         │
│ Plugin           │  │ Plugin           │  │ Plugin           │
│ - FFmpeg         │  │ - HLS            │  │ - Database       │
│ - Worker Pool    │  │ - DASH           │  │ - Indexing       │
│ - Auto-scaling   │  │ - Adaptive BR    │  │ - Search         │
└──────────────────┘  └──────────────────┘  └──────────────────┘
```

### Dual-Mode Deployment

#### 1. Monolithic Mode (Development)

Single binary with all plugins loaded in-memory:

```
┌─────────────────────────────────────────┐
│         StreamGate Monolith             │
│  ┌───────────────────────────────────┐  │
│  │      Microkernel Core             │  │
│  │  ┌─────────────────────────────┐  │  │
│  │  │ All Plugins (In-Memory)     │  │  │
│  │  │ - API Gateway               │  │  │
│  │  │ - Upload                    │  │  │
│  │  │ - Transcoder                │  │  │
│  │  │ - Streaming                 │  │  │
│  │  │ - Auth                      │  │  │
│  │  │ - Metadata                  │  │  │
│  │  │ - Worker                    │  │  │
│  │  │ - Monitor                   │  │  │
│  │  │ - Cache                     │  │  │
│  │  └─────────────────────────────┘  │  │
│  │  ┌─────────────────────────────┐  │  │
│  │  │ In-Memory Event Bus         │  │  │
│  │  └─────────────────────────────┘  │  │
│  └───────────────────────────────────┘  │
│                                         │
│  Port: 8080 (HTTP)                      │
│  Binary: bin/streamgate                 │
└─────────────────────────────────────────┘
```

**Use Cases**: Local development, debugging, integration testing

**Build**: `make build-monolith`

#### 2. Microservices Mode (Production Target)

3 core services with gRPC communication:

```
                    ┌─────────────────────┐
                    │   Load Balancer     │
                    │   (Nginx/Envoy)     │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │   API Gateway       │
                    │   (Port 9090)       │
                    │   - REST API        │
                    │   - gRPC Gateway    │
                    │   - Rate Limiting   │
                    └──────────┬──────────┘
                               │
         ┌─────────────────────┼─────────────────────┐
         │                     │                     │
┌────────▼────────┐    ┌───────▼──────────┐   ┌──────▼────────────┐
│   Auth Service  │    │  Streaming      │   │  Infrastructure  │
│   (Port 9007)   │    │  (Port 9093)   │   │  - NATS (4222)   │
│                 │    │                 │   │  - Redis (6379)   │
│ - NFT Verify    │    │ - Transcoding  │   │  - PostgreSQL     │
│ - Signature     │    │ - HLS/DASH     │   │  - MinIO (9000)   │
│ - Multi-chain   │    │ - Adaptive BR  │   │  - Prometheus     │
└─────────────────┘    └─────────────────┘   └──────────────────┘
```

**Internal Modules** (not independently deployed):
- Cache: LRU + Redis, called by Auth/Streaming
- Monitor: Prometheus metrics, part of each service
- Worker: Task queue, part of Streaming service

**Use Cases**: Production deployment, horizontal scaling

**Build**: `make build-services` (builds 3 core services only)

### Core Services (P2 Target: 3 Microservices)

For production, the project will deploy 3 core services (the rest are internal modules):

| Service | Port | Responsibility | Scaling |
|---------|------|-----------------|---------|
| **API Gateway** | 9090 | REST API, gRPC gateway, routing, rate limiting | Horizontal |
| **Auth** | 9007 | NFT verification, signature verification, Web3 auth | Horizontal |
| **Streaming** | 9093 | Video transcoding (FFmpeg), HLS/DASH delivery | Horizontal (CPU-bound) |

**Internal Modules** (not independently deployed):
- Cache (LRU + Redis) - Internal module
- Monitor (Prometheus) - Internal module
- Worker (task queue) - Internal component

### Communication Patterns

#### Event-Driven (Asynchronous)

```
Service A ──publish──> NATS ──subscribe──> Service B
                       │
                       ├──> Service C
                       └──> Service D
```

**Use Cases**: File uploads, transcoding tasks, metadata updates

#### gRPC (Synchronous)

```
Service A ──gRPC call──> Service B
          <──response──
```

**Use Cases**: API Gateway to backend services, real-time queries

#### Service Discovery

```
Service ──register──> Consul ──query──> Service A
                        │
                        ├──> Service B
                        └──> Service C
```

**Use Cases**: Dynamic service location, health checking, load balancing

### Data Flow

#### Upload Flow

```
Client ──HTTP POST──> API Gateway
                         │
                         ├──> Upload Service (chunked upload)
                         │       │
                         │       └──> MinIO/S3 (store file)
                         │
                         └──> NATS (publish: file.uploaded)
                                 │
                                 ├──> Transcoder (start job)
                                 ├──> Metadata (index file)
                                 └──> Monitor (log event)
```

#### Streaming Flow

```
Client ──HTTP GET──> API Gateway
                         │
                         ├──> Auth Service (verify NFT)
                         │
                         ├──> Cache Service (check cache)
                         │       │
                         │       ├──> Hit: return cached manifest
                         │       └──> Miss: query Streaming Service
                         │
                         └──> Streaming Service
                                 │
                                 ├──> Metadata (get content info)
                                 ├──> MinIO/S3 (get segments)
                                 └──> Cache (store manifest)
```

#### Transcoding Flow

```
NATS (file.uploaded) ──> Transcoder Service
                             │
                             ├──> Worker Pool (process)
                             │       │
                             │       └──> FFmpeg (transcode)
                             │
                             ├──> MinIO/S3 (store output)
                             │
                             └──> NATS (publish: transcoding.completed)
                                     │
                                     ├──> Metadata (update status)
                                     ├──> Monitor (log metrics)
                                     └──> Cache (invalidate)
```

## 🚀 Quick Start

**One command** (requires Docker + Go 1.24):

```bash
make demo
```

This starts postgres/redis/minio → builds the monolith → runs on `:8080`.

### Prerequisites

- Go 1.24
- Docker & Docker Compose

### Option 1: Local Development (Monolithic Mode)

```bash
# 1. Start infrastructure
docker-compose up -d postgres redis minio

# 2. Configure environment
cp .env.example .env

# 3. Build and run
make run-monolith

# 4. In another terminal:
curl http://localhost:8080/health
```

### Option 2: Docker Compose (Microservices Mode)

```bash
# 1. Clone project
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# 2. Start all services
docker-compose up -d

# 3. Check service status
docker-compose ps

# 4. Access services
# API Gateway: http://localhost:8080
# Consul UI: http://localhost:8500
# Prometheus: http://localhost:9090
# Jaeger: http://localhost:16686

# 5. View logs
docker-compose logs -f api-gateway
docker-compose logs -f transcoder
```

### Option 3: Build All Binaries

```bash
# Build all 9 microservices
make build-all

# Binaries created in bin/
ls -la bin/

# Run individual services
./bin/api-gateway &
./bin/upload &
./bin/transcoder &
./bin/streaming &
./bin/metadata &
./bin/cache &
./bin/auth &
./bin/worker &
./bin/monitor &
```

### Option 4: Production Deployment (Kubernetes)

```bash
# 1. Build Docker images
make docker-build

# 2. Push to registry (optional)
make docker-push

# 3. Deploy to Kubernetes
kubectl apply -f k8s/

# 4. Check service status
kubectl get pods -n streamgate
kubectl get svc -n streamgate

# 5. Access services
kubectl port-forward svc/api-gateway 8080:8080
```

## 📚 Documentation

### Project Structure

```
streamgate/
├── cmd/                                    # Entry points
│   ├── monolith/streamgate/               # Monolithic deployment
│   │   └── main.go                        # Single binary entry point
│   └── microservices/                     # Microservices deployment
│       ├── api-gateway/main.go            # API Gateway (port 9090)
│       ├── upload/main.go                 # Upload Service (port 9091)
│       ├── transcoder/main.go             # Transcoder (port 9092)
│       ├── streaming/main.go              # Streaming (port 9093)
│       ├── metadata/main.go               # Metadata (port 9005)
│       ├── cache/main.go                  # Cache (port 9006)
│       ├── auth/main.go                   # Auth (port 9007)
│       ├── worker/main.go                 # Worker (port 9008)
│       └── monitor/main.go                # Monitor (port 9009)
│
├── pkg/                                   # Core packages
│   ├── core/                              # Microkernel core
│   │   ├── microkernel.go                 # Microkernel implementation
│   │   ├── config/config.go               # Configuration management
│   │   ├── logger/logger.go               # Logging
│   │   └── event/event.go                 # Event bus
│   └── plugins/                           # Plugin implementations
│       ├── transcoder/                    # Transcoding plugin
│       ├── streaming/                     # Streaming plugin
│       ├── auth/                          # Auth plugin
│       └── ...                            # Other plugins
│
├── .kiro/specs/offchain-content-service/ # Specifications
│   ├── requirements.md                    # Functional requirements (1,283 lines)
│   ├── design.md                          # Technical design (4,001 lines)
│   └── tasks.md                           # Implementation tasks (280+)
│
├── docs/                                  # Documentation
│   ├── high-performance-architecture.md   # Performance design
│   ├── web3-setup.md                      # Web3 setup guide
│   ├── web3-best-practices.md             # Best practices
│   ├── web3-testing-guide.md              # Testing guide
│   ├── deployment-architecture.md         # Deployment guide
│   └── ...                                # Other guides
│
├── examples/                              # Example code
│   ├── nft-verify-demo/                   # NFT verification example
│   └── signature-verify-demo/             # Signature verification example
│
├── docker-compose.yml                     # Docker Compose configuration
├── Dockerfile                             # Base Docker image
├── Makefile                               # Build targets
├── go.mod                                 # Go dependencies
├── README.md                              # This file
├── WEB3_ACTION_PLAN.md                    # 10-week implementation plan
├── WEB3_CHECKLIST.md                      # Phase checklist
└── IMPLEMENTATION_READY.md                # Implementation status
```

### Build Commands

```bash
# Build individual services
make build-monolith                        # Build monolithic binary
make build-api-gateway                     # Build API Gateway
make build-upload                          # Build Upload Service
make build-transcoder                      # Build Transcoder
make build-streaming                       # Build Streaming
make build-metadata                        # Build Metadata
make build-cache                           # Build Cache
make build-auth                            # Build Auth
make build-worker                          # Build Worker
make build-monitor                         # Build Monitor

# Build all services
make build-all                             # Build all 9 services

# Docker operations
make docker-build                          # Build all Docker images
make docker-up                             # Start Docker Compose
make docker-down                           # Stop Docker Compose
make docker-push                           # Push images to registry

# Testing and quality
make test                                  # Run tests
make lint                                  # Run linter
make fmt                                   # Format code
make coverage                              # Generate coverage report
```

### 🧭 Quick Start for Learners

Recommended reading order for Web3+Go developers:

```
Step 1: examples/signature-verify-demo/      — EIP-191 签名最小示例
Step 2: examples/nft-verify-demo/            — NFT 验证最小示例
Step 3: examples/streaming-demo/             — HLS 流媒体概念演示
Step 4: examples/challenges/01_eip191_prefix/— 动手改 bug 学习
Step 5: docs/learning-roadmap.md             — 2-3 周系统学习路线
```

### Web3 Glossary

| Term | Meaning | In StreamGate |
|------|---------|---------------|
| **EIP-191** | Personal sign: `\x19Ethereum Signed Message:\n` prefix | `pkg/web3/signature.go` |
| **EIP-712** | Typed structured data signing | `pkg/web3/eip712.go` |
| **EIP-1271** | Smart contract wallet signature validation | `pkg/web3/eip1271.go` |
| **EIP-4361 (SIWE)** | Sign-In with Ethereum, standardized login | `pkg/web3/siwe.go` |
| **ERC-721** | Non-fungible token (NFT) standard | `pkg/web3/nft.go` |
| **ERC-1155** | Multi-token standard (fungible + NFT) | `pkg/web3/nft.go` |
| **ERC-165** | Interface detection (`supportsInterface`) | `pkg/web3/nft.go` |
| **HLS** | HTTP Live Streaming: `.m3u8` + `.ts` segments | `pkg/service/streaming.go` |
| **DASH** | Dynamic Adaptive Streaming over HTTP | `pkg/service/streaming.go` |
| **HS256** | HMAC-SHA256 JWT signing (symmetric) | `pkg/service/auth.go` |
| **SIWE** | Sign-In with Ethereum = EIP-4361 | `pkg/web3/siwe.go` |

### Beginner Guides

- [Learning Roadmap](docs/learning-roadmap.md) ✅ — 2-3 week learning plan
- [Web3 Development Environment Setup](docs/web3-setup.md) 🚧 — Coming soon
- [Architecture Guide](docs/ARCHITECTURE_GUIDE.md) 🚧 — Coming soon
- [Frequently Asked Questions](docs/web3-faq.md) 🚧 — Coming soon

### Development Guides

- [High-Performance Architecture Design](docs/high-performance-architecture.md) - High concurrency, high availability, easy scalability, high performance, debuggability
- [Web3 Best Practices](docs/web3-best-practices.md) - Security, performance, multi-chain support
- [Web3 Integration Testing](docs/web3-testing-guide.md) - Unit tests, integration tests, E2E tests
- [Web3 Troubleshooting](docs/web3-troubleshooting.md) - Common problem diagnosis and solutions
- [Deployment Architecture](docs/deployment-architecture.md) - Production deployment guide

### Example Code

- [NFT Verification Example](examples/nft-verify-demo/) - Simplest NFT verification
- [Signature Verification Example](examples/signature-verify-demo/) - Web3 login implementation

### Project Documentation

- [Requirements Document](.kiro/specs/offchain-content-service/requirements.md) - Complete functional requirements (1,283 lines)
- [Design Document](.kiro/specs/offchain-content-service/design.md) - Detailed technical design (4,001 lines)
- [Task List](.kiro/specs/offchain-content-service/tasks.md) - 280+ development tasks
- [Implementation Plan](WEB3_ACTION_PLAN.md) - 10-week implementation roadmap
- [Implementation Checklist](WEB3_CHECKLIST.md) - Phase-by-phase checklist

## 🛠️ Technology Stack

| Category | Technology | Purpose |
|----------|------------|---------|
| **Language** | Go 1.24 | Backend development |
| **Architecture** | Microkernel + Microservices | Plugin-based, dual-mode deployment |
| **Database** | PostgreSQL 15 | Persistent storage |
| **Cache** | Redis 7 | Distributed caching |
| **Storage** | MinIO / S3 | Object storage |
| **Message Queue** | NATS | Event-driven communication |
| **Service Discovery** | Consul | Service registry & health checks |
| **Video Processing** | FFmpeg | Video transcoding |
| **Streaming** | HLS / DASH | Adaptive bitrate streaming |
| **Monitoring** | Prometheus + Grafana | Metrics collection & visualization |
| **Tracing** | OpenTelemetry + Jaeger | Distributed tracing |
| **RPC** | gRPC + Protocol Buffers | Inter-service communication |
| **Container** | Docker + Kubernetes | Containerization & orchestration |
| **Blockchain** | go-ethereum + Solana SDK | Multi-chain support |
| **Web3** | ethers.js / web3.js | Wallet integration |

## 🎯 Features

### 👥 Who Is It For?

| Role | Goal | Quick Start |
|------|------|-------------|
| **Platform Developer** | Integrate NFT-gated video via API | Read the [OpenAPI spec](docs/api/openapi.yaml) — no Go required |
| **Content Creator** | Upload video, set NFT rules, go live | Use `make demo-quick` (no blockchain needed for testing) |
| **Node Operator** | Deploy and scale StreamGate | Use `make fullchain-deploy` or K8s manifests in `deploy/k8s/` |

### 📦 Product Roadmap

#### P0 — Core Flow (Stable)

Core authentication, NFT verification, and streaming pipeline. All items are implemented and tested.

- [x] **Wallet Sign-In** — EIP-191 / EIP-712 / SIWE (EIP-4361) + Solana ed25519
- [x] **NFT Ownership Verification** — ERC-721, ERC-1155, ERC-165 auto-detect
- [x] **NFTOwnershipsApproval (TOCTOU Protection)** — CheckApproval + CheckApprovalAutoDetect
- [x] **HLS Streaming** — Manifest generation + per-user playback tokens
- [x] **Video Upload** — Chunked resumable upload with integrity checks
- [x] **FFmpeg Transcoding** — Multi-profile HLS/DASH with worker pool
- [x] **Multi-chain EVM** — Ethereum, Polygon, BSC, Arbitrum, Optimism with RPC failover
- [x] **Solana Integration** — Multi-endpoint RPC failover, Metaplex NFT
- [x] **JWT Auth** — HS256/RS256 with key rotation support
- [x] **Rate Limiting** — Global + per-wallet (10 req/min)
- [x] **Configuration Management** — Viper YAML + env vars with hot reload
- [x] **Graceful Shutdown** — Context-aware with 30s timeout

#### P1 — Usability & Operations (Stable)

Production operations and developer experience.

- [x] **Dual-mode Deployment** — Monolith (dev) + 9 microservices (prod)
- [x] **Docker + Docker Compose** — Full stack with health checks
- [x] **Kubernetes Manifests** — Deployments, services, config maps, secrets
- [x] **Prometheus Metrics** — RED metrics (Rate/Errors/Duration) + custom
- [x] **gRPC API** — Full proto definitions + interceptors + health protocol
- [x] **OpenTelemetry Tracing** — Gin + gRPC integrated, export to Jaeger
- [x] **gRPC TLS** — Certificate-based server-side TLS
- [x] **Structured Logging** — Zap with request ID correlation
- [x] **Health Check Aggregation** — `/health`, `/ready`, `/health/live`
- [x] **Database Migrations** — 31 versioned migrations with rollback
- [x] **CI/CD Pipeline** — GitHub Actions: lint → test → build → security → docker
- [x] **Multi-stage Docker Build** — Builder + distroless runtime support
- [x] **Blues/Green + Canary Deployment** — K8s rollout strategies
- [x] **SLO Alerts** — Prometheus alerting rules with burn rate

#### P2 — Ecosystem & Advanced (Planned)

Features planned for future releases.

- [ ] **Web Admin UI** — Browser-based content and gating management
- [ ] **JS / Python SDK** — First-party client libraries
- [ ] **Account Abstraction (ERC-4337)** — Gasless wallet login
- [ ] **Social Login** — Email/OAuth → embedded wallet
- [ ] **NFT-gated Live Streaming** — WebRTC → HLS real-time
- [ ] **Analytics Dashboard** — Viewer metrics, revenue tracking
- [ ] **IPFS Pinning Service** — Decentralized content storage
- [ ] **Commercial License** — Enterprise support, SLA, SSO

## 📊 Performance Metrics

### Target Metrics

| Metric | Target | Status |
|--------|--------|--------|
| API response time (P95) | < 200ms | ✅ Designed |
| Video playback startup | < 2 seconds | ✅ Designed |
| Concurrent users | 10,000+ | ✅ Designed |
| Cache hit rate | > 80% | ✅ Designed |
| Service availability | > 99.9% | ✅ Designed |
| RPC uptime | > 99.5% | ✅ Designed |
| IPFS upload success | > 95% | ✅ Designed |
| Transaction confirmation | < 2 minutes | ✅ Designed |

### Monitoring & Observability

**Prometheus Metrics** (http://localhost:9090)
- HTTP request count and latency
- Cache hit/miss rate
- Transcoding task status and duration
- NFT verification request count
- System resource usage (CPU, memory, disk)
- Service health status

**Jaeger Tracing** (http://localhost:16686)
- Distributed request tracing
- Service dependency visualization
- Performance bottleneck identification
- Error tracking

**Consul UI** (http://localhost:8500)
- Service registration status
- Health check results
- Service discovery
- Key-value store

**Grafana Dashboards** (http://localhost:3000)
- Real-time metrics visualization
- Custom alerts
- Performance trends
- Resource utilization

## 🤝 Contributing

Contributions are welcome! Please see [Contributing Guide](CONTRIBUTING.md).

### Development Workflow

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Code Standards

- Follow Go conventions and best practices
- Write tests for new features
- Update documentation
- Run `make fmt` and `make lint` before committing

## 📄 License

This project is licensed under the MIT License - see [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [go-ethereum](https://github.com/ethereum/go-ethereum) - Ethereum Go client
- [solana-go](https://github.com/gagliardetto/solana-go) - Solana Go SDK
- [FFmpeg](https://ffmpeg.org/) - Video processing
- [NATS](https://nats.io/) - Message queue
- [Consul](https://www.consul.io/) - Service discovery
- [OpenTelemetry](https://opentelemetry.io/) - Observability

## 📞 Support

If you have questions or need help:

1. **Documentation**: Check [docs/](docs/) directory
2. **Examples**: See [examples/](examples/) directory
3. **Issues**: Submit an [Issue](https://github.com/rtcdance/streamgate/issues)
4. **Discussions**: Start a [Discussion](https://github.com/rtcdance/streamgate/discussions)

## 🚀 Roadmap

### Phase 1: Identity + Ownership
- Wallet sign-in
- Real NFT verification
- API wiring for `/api/v1/nft/verify`

### Phase 2: Protected Streaming
- Gate manifest access by NFT ownership
- Add Redis cache to the verification path
- Expose core metrics for auth and playback

### Phase 3: Media Pipeline
- Worker execution pipeline
- FFmpeg transcoding to HLS
- Retry, timeout, and queue visibility

### Phase 4: Interview Packaging
- Architecture cleanup
- Demo path and talking points
- README, architecture guide, and mock interview prep

See [docs/ARCHITECTURE_GUIDE.md](docs/ARCHITECTURE_GUIDE.md) for the current execution plan.

## 📈 Project Status

> Development in progress. The repository already contains architecture skeletons and several working components, but the interview-critical business flow is still being tightened into a real end-to-end path.

### Current Priorities

| Priority | Focus | Why it matters |
|----------|-------|----------------|
| **P0** | Wallet sign-in + NFT verification + protected HLS access | This is the core Web3 business loop |
| **P1** | Redis cache + RPC failover + metrics | This makes the project look like enterprise practice |
| **P1** | Worker + FFmpeg pipeline | This is where audio/video engineering experience stands out |
| **P2** | Further microservice split and polish | Useful later, not the current proof point |

### What Is Already Valuable

| Area | Current state |
|------|---------------|
| **Architecture** | Microkernel + dual deployment model are in place |
| **Media direction** | HLS/DASH, storage, and worker-related components exist |
| **Web3 direction** | Signature, NFT, and multichain modules exist, but some paths are still placeholders |
| **Deployment** | Docker/K8s assets exist for later expansion |

### What This README Assumes

- Some components are still skeletons or partially wired
- The goal is a credible interview project, not a fully productized platform
- The recommended path is monolith-first for the main flow, then selective service extraction

See [docs/ARCHITECTURE_GUIDE.md](docs/ARCHITECTURE_GUIDE.md) and [docs/web3-faq.md](docs/web3-faq.md) for the current development plan.

---

⭐ If this project helps you, please give it a Star!

**Repository**: https://github.com/rtcdance/streamgate
