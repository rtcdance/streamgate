# StreamGate Competitive Analysis

## Overview

StreamGate competes in the **token-gated video delivery** space — where blockchain-based
access control meets traditional streaming infrastructure. Below is a comparison with
the most relevant alternatives.

---

## Comparison Matrix

| Dimension | StreamGate | Livepeer | HLS.js + NFT Check | Widevine/FairPlay |
|-----------|-----------|----------|-------------------|-------------------|
| **License** | MIT (free) | MIT + Commercial | MIT | Proprietary ($50K+/yr) |
| **NFT Gating** | ✅ Native, built-in | ❌ Requires custom integration | ⚠️ Must implement from scratch | ❌ Not supported |
| **Deployment** | Single binary + Docker | Multi-node orchestrator + broadcaster network | Zero (runs in browser) | Requires license negotiation |
| **Web3 Depth** | EIP-191/712, SIWE, ERC-721/1155, ERC-165, Solana, EIP-1271 | Generic L1 support via oracles | Only at frontend layer | None |
| **Video Pipeline** | Upload → Transcode (FFmpeg) → HLS/DASH → Cache | Decentralized transcoding network | Requires separate backend | Content preparation SDK |
| **RPC Reliability** | Multi-endpoint failover (EVM + Solana) | N/A (own network) | User's own RPC | N/A |
| **Operational Maturity** | Prometheus, Grafana, Jaeger, K8s, CI/CD, SLO alerts | Built-in network dashboard | None | Enterprise SLAs available |
| **Target User** | Platform devs integrating NFT gating | Decentralized video infrastructure | Frontend devs building DIY | Large media companies |

---

## When to Choose StreamGate

- **You are a Web3 platform** (gaming, metaverse, NFT marketplace) that needs to gate video content behind token ownership
- **You want a single binary**, not a multi-node network or multiple SaaS services
- **You need wallet sign-in + NFT verification** without managing your own blockchain infrastructure
- **You value open source** with MIT licensing and no vendor lock-in

## When Not to Choose StreamGate

- **You need a fully decentralized video network** — Livepeer's distributed transcoding is a better fit
- **You only need frontend-only token checking** with no backend — HLS.js + ethers.js is sufficient
- **You require Hollywood-grade DRM** (Widevine L1, PlayReady SL3000) — StreamGate currently has no DRM layer
- **You need a managed SaaS** with no self-hosting — consider Livepeer Studio or build on StreamGate yourself