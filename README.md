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
[![Status](https://img.shields.io/badge/Status-v1.0.0--stable-2ea44f)](VERSION)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord)](https://discord.gg/streamgate)

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

### 🚫 What StreamGate Is Not

| Not this | Because |
|----------|---------|
| **Decentralized video network** | StreamGate is self-hosted infrastructure, not a peer-to-peer network. For decentralized transcoding, see Livepeer. |
| **Traditional DRM (Widevine/FairPlay)** | DRM requires proprietary licensing. StreamGate uses on-chain ownership as an alternative. |
| **Managed SaaS / Cloud** | Community edition is self-hosted. A hosted version is on the [roadmap](docs/product-roadmap.md). |
| **No-code platform** | StreamGate is API-first. A Web UI is planned but not yet available. |

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

| Document | Description |
|----------|-------------|
| [Product Roadmap](docs/product-roadmap.md) | Quarterly roadmap with deliverables and success metrics |
| [Product OKRs](docs/product-okrs.md) | North star metric + Q3 objectives and key results |
| [Competitive Analysis](docs/competitive-analysis.md) | StreamGate vs Livepeer vs DIY vs DRM |

### Phase 1: Identity + Ownership (Completed ✅)
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

## 📬 Contact & Community

| Channel | Purpose |
|---------|---------|
| **GitHub Issues** | Bug reports, feature requests |
| **GitHub Discussions** | Q&A, ideas, show and tell |
| **Discord** | Real-time discussion: [Join](https://discord.gg/streamgate) |
| **Security** | [Private vulnerability report](https://github.com/rtcdance/streamgate/security/advisories) |
| **Commercial** | [COMMERCIAL.md](COMMERCIAL.md) — licensing, SLA, custom deployment |

---

⭐ If this project helps you, please give it a Star!

**Repository**: https://github.com/rtcdance/streamgate
