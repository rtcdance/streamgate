# StreamGate Product Roadmap

> **Vision**: The standard open-source platform for token-gated video delivery.

All dates are approximate and subject to change. Roadmap is driven by community feedback.

---

## Q3 2026 — 10-Minute Developer Experience

**Theme:** From `git clone` to first NFT-gated playback in under 10 minutes.

### Deliverables

| Feature | Description |
|---------|-------------|
| **Demo Mode** | `STREAMGATE_DEMO_MODE=true` — built-in mock NFT verification, no blockchain needed |
| **Quick Start CLI** | Interactive `streamgate init` command walks through setup |
| **Pre-built Demo Video** | `make demo` includes sample content ready to play |
| **Web Admin UI** | Browser-based content upload and gating rule management (MVP) |
| **Documentation Site** | Hosted docs with search and runnable examples |

### Success Metrics

- Time from clone to first playback: < 10 min (P95)
- Demo mode adoption: > 60% of first-time users start with demo mode
- Issue response time: < 24 hours

---

## Q4 2026 — Multi-Platform SDK

**Theme:** Integrate StreamGate from any platform without reading Go code.

### Deliverables

| Feature | Description |
|---------|-------------|
| **JavaScript SDK** | `npm install @streamgate/client` — browser + Node.js |
| **Python SDK** | `pip install streamgate-client` — ML/data workflows |
| **Unity SDK** | `com.streamgate.runtime` — game/metaverse integration |
| **Webhook Events** | Real-time notifications for upload complete, transcode done, verification failed |
| **Analytics API** | Viewer metrics, token usage, revenue tracking |

### Success Metrics

- SDK downloads: > 1000/week
- Third-party integrations: > 5 community-contributed
- API documentation coverage: 100% endpoint documented

---

## H1 2027 — Self-Service Creator

**Theme:** Non-technical content creators can launch token-gated content independently.

### Deliverables

| Feature | Description |
|---------|-------------|
| **Creator Dashboard** | Upload → set NFT rules → publish, no CLI needed |
| **Embeddable Player** | `<stream-gate>` web component, paste into any site |
| **Social Login** | Email/OAuth → embedded wallet (ERC-4337 account abstraction) |
| **Analytics Dashboard** | Viewers, geography, token holder demographics |
| **Pricing Tiers** | Community (free, self-hosted) vs Cloud (managed, SLA) |

### Success Metrics

- Creators publishing content: > 100
- Non-technical users: > 30% of total user base
- Paid conversions: > 5% of cloud trial users

---

## Current Status (v1.0.0)

All P0 features are complete and stable. See [Features section](../README.md#-features) for details.

### Legend

| Status | Meaning |
|--------|---------|
| ✅ Shipped | Available in current release |
| 🚧 In Progress | Active development |
| 📋 Planned | Specified, not yet started |
| 💡 Proposed | Under discussion |