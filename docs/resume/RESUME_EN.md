# Resume (English)

> Output discipline (TRANSITION_PLAN C0/C2): only claims that can be demonstrated live; no line counts, no coverage numbers, no "microkernel architecture", no service counts, no version tags.
> `[...]` placeholders must be replaced before sending.

---

## Basic Information

- Name: [Your Name]
- Phone: [Your Phone] · Email: [Your Email]
- Location: [Your City] · GitHub: [github.com/your-handle](https://github.com/your-handle) (StreamGate pinned)

## Professional Summary

Backend engineer with 6 years in audio/video systems — streaming delivery, native player development, and access control — now focused on Go backends with Web3 identity and authorization. Built StreamGate end to end: wallet signature login, on-chain NFT gating, and protected HLS delivery, including a full security audit of my own authorization path.

## Core Skills

- **Media**: HLS/ABR transcoding pipelines (FFmpeg), manifest/segment delivery, player internals and native player development
- **Backend**: Go, HTTP/gRPC, task queues (NATS JetStream + DLQ), retry/timeout/backpressure design
- **Web3**: EIP-191 / EIP-712 / SIWE signature verification, EIP-2 low-s, EIP-1271 contract-signature checks, NFT ownership verification (ERC-721/1155, ERC-165 detection), reorg handling (`safe` block tag + cache invalidation)
- **Security engineering**: challenge-response replay protection (atomic Redis Lua), fail-closed authorization design, TDD security regressions

## Project Experience

### StreamGate — NFT-Gated Streaming Backend (Go, personal project)

A complete backend for wallet-signed login, on-chain NFT ownership verification, and protected HLS delivery. I found and fixed a full authorization bypass chain in my own code.

- **Wallet identity & replay protection**: EIP-191 / EIP-712 / SIWE verification paths with constant-time comparison and enforced EIP-2 low-s; one-time challenge consumption implemented as an atomic Redis Lua script (no TOCTOU replay window).
- **NFT-gated authorization**: ownership verified at manifest access time (ownerOf/balanceOf per server-side rules, ERC-165 standard auto-detection); reads pinned to the `safe` block tag with automatic cache invalidation on reorg detection; positive-result cache TTL capped at 15s to balance tamper-resistance against RPC load.
- **Protected delivery**: authorization happens at the manifest; segments are served with content-bound, short-TTL playback tokens so no chain RPC sits in the playback hot path. Tokens are wallet- and fingerprint-bound; cross-user reuse is rejected.
- **ABR transcoding pipeline**: FFmpeg 4-rung resolution ladder + HLS segmenting with live stderr progress parsing; tasks distributed via NATS JetStream with retries, timeouts, stuck-worker reclaim, and a DLQ for poison tasks.
- **Security audit (project highlight)**: wrote regression tests first, then discovered a complete unauthorized-access chain — segment routes registered without any auth, playback tokens accepted without validation, gating that failed open, and caller-selectable verification contracts (6 defects). Fixed all of them TDD-style and promoted every test to a permanent CI regression; gate semantics are now fail-closed.
- **Engineering**: golangci-lint + CI gates (coverage floor, govulncheck, container image scanning), one-command docker compose stack, Playwright end-to-end acceptance probes — including a security probe asserting unauthenticated requests get 401.

## Work Experience

### [Company A] — [Title] ([Period])

- [Media backend / streaming systems: replace with real scope, e.g. "playback pipeline serving X DAU"]
- [Real outcomes in access control, task scheduling, transcoding, or performance]
- [Tech stack]

### [Company B] — [Title] ([Period])

- [Real experience]

## Education

- [University] · [Major] · [Degree] · [Years]

---

### Pre-send checklist (from TRANSITION_PLAN — do not delete)

1. Every `[...]` placeholder replaced with real information; every work bullet must survive 10 minutes of follow-up questions.
2. The "6 years" figure verified against your real experience.
3. Every StreamGate bullet is pointable at code and tests: `pkg/web3/signature/`, `pkg/web3/nft/verifier.go`, `pkg/middleware/nft_gate.go`, `pkg/service/transcoding/`, `pkg/gateway/authz_regression_test.go`.
4. No DRM / full-decentralization / production-HA claims — boundary language is in `PROJECT_TALK_SCRIPT.md`.
