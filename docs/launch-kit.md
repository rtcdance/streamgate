# StreamGate Launch Kit

Use this kit to prepare announcements for StreamGate releases.

---

## Announcement Template

```
🚀 StreamGate v1.0.0 — Token-Gated Video Streaming, Now Open Source

StreamGate is an open-source (MIT) platform for NFT-gated video delivery.
Users prove NFT ownership to watch — no password, no piracy, no middleware.

What's included:
• Wallet sign-in (EIP-191/712/SIWE + Solana)
• NFT verification (ERC-721/1155 + ERC-165 auto-detect)
• HLS streaming with per-user playback tokens
• FFmpeg transcoding (multi-profile HLS/DASH)
• Multi-chain failover (Ethereum, Polygon, BSC, Solana)
• One-command demo: `make demo`
• OpenAPI 3.0 spec + gRPC + Prometheus + Jaeger

Try it: https://github.com/rtcdance/streamgate
Docs: docs/API_DOCUMENTATION.md
License: MIT
```

---

## Where to Post

| Platform | Format | Best for |
|----------|--------|----------|
| **Twitter/X** | Short thread (3-5 tweets) with demo GIF | Broad reach |
| **Hacker News** | Text post with technical depth | Developer audience |
| **Reddit r/ethdev** | Text post with technical details | Web3 developers |
| **Reddit r/golang** | Text post with Go architecture highlights | Go developers |
| **Dev.to / Medium** | Long-form article with screenshots | Tutorial audience |

---

## Screenshot Guide

For the announcement, prepare these screenshots:

1. **Terminal**: `make demo` output showing the one-command experience
2. **API call**: `curl` to `/api/v1/auth/login` with response
3. **H5 Demo**: Browser showing wallet connect → NFT verify → video play
4. **Metrics**: Grafana dashboard showing RED metrics

## Tagline Options

- "StreamGate: Token-gated video for Web3. Own the NFT, watch the video."
- "The missing open-source platform for NFT-gated streaming."
- "No passwords. No DRM. Just a wallet and an NFT."