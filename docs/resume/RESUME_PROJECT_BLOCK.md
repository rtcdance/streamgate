# StreamGate Resume Project Block

## Recommended project title

`StreamGate: NFT-Gated Streaming Backend in Go`

## Short version

Built a Go-based `NFT-gated streaming backend` that combines wallet challenge authentication, NFT ownership authorization, protected HLS access, playback tokens, and transcoding task APIs to model a realistic media access-control system.

## Resume bullet version

- Designed and implemented a Go-based `NFT-gated streaming backend` for protected media access, combining wallet challenge login, NFT authorization, HLS manifest protection, and short-lived playback tokens.
- Built chain-backed authorization flows with wallet signature verification and NFT ownership checks, exposing real backend APIs for `auth/challenge`, `auth/login`, `nft/verify`, and protected streaming access.
- Implemented transcoding task APIs and media backend foundations, including task submission, status query, task listing, worker scheduling, retry, cancellation, and health-check flows.
- Added targeted automated tests across service, API, gateway, worker, and transcoder layers to validate auth replay protection, NFT access behavior, cache hits, playback-token flow, and task lifecycle handling.

## Media-backend-leaning version

- Built a protected streaming backend in Go with NFT-based authorization, manifest gating, playback-token segment access, and media task orchestration.
- Implemented transcoding task submission, status tracking, worker scheduling, retry, cancellation, and queue/health behavior to model a realistic media processing backend.
- Used Web3 selectively as the authorization layer, integrating wallet challenge login and NFT ownership checks into a media delivery system rather than building a standalone blockchain demo.

## Go-backend-leaning version

- Built a Go backend system that integrates wallet authentication, NFT-based authorization, protected content access, and task-processing workflows.
- Implemented HTTP APIs, service-layer state transitions, queue-backed task flow, and focused tests across auth, streaming, and transcoding paths.
- Structured the system so the core path can run through a gateway-first flow while still mapping cleanly to microservice-style separation.

## Web3-backend-leaning version

- Built a backend integration project that turns wallet identity and NFT ownership into real product-side access control for protected media content.
- Implemented wallet challenge authentication, JWT issuance, NFT verification, manifest authorization, and short-lived playback tokens for efficient content delivery.
- Positioned Web3 as the authorization layer inside a real backend system rather than as a pure contract or token demo.

## What to avoid on the resume

- `Production-grade decentralized media platform`
- `Full DRM streaming network`
- `Complete multi-chain Web3 infrastructure`
- `End-to-end NFT metadata platform`

## Recommended one-line explanation in interview or resume summary

I used a media backend scenario to demonstrate how Go services and Web3 authorization can work together in a realistic content-access system.

## Contract-wallet extension — use only with its acceptance boundary

**Contract-wallet extension (W3, 2026-11-11): deterministic CI verified; public Sepolia path partially evidenced; full acceptance remains open.** Frozen code candidate `d267ad4439ba672fecf373cae22f07852a0f6ada` passed [default CI 42548083086](https://github.com/rtcdance/streamgate/actions/runs/42548083086) attempt 2 (55/55, no skips), the complete nine-stage Docker gate (original exit 0) and a read-only fixed-matrix closeout (CW-01–16 and TN-08 PASS, no FAIL items). Public Sepolia evidence recorded 2026-11-10/11: six real transactions (two deployments, two mints, two transfers; 0.009153 ETH total fees), an on-chain holdings oracle verified against two RPCs, a real MetaMask SIWE login and a rejected signature. Still open: media playback evidence, the contract-wallet and api-gateway browser phases and the remaining TN matrix cells — the fixed-matrix verdict stays BLOCKED (11 of 14 TN cells) and the pre-write network qualification original for this round is missing, so nothing here claims complete public acceptance. Historical `d0f05152` results below do not accept this extension. Scope: deployed ERC-1271 accounts with SIWE / EIP-191, demonstrated using the project-owned `Study1271Wallet` test fixture. No arbitrary Safe compatibility, ERC-4337, production-wallet or mainnet claims.

Suggested engineering description, subject to the candidate's own contribution review:

- Implemented a deployed ERC-1271 login path in Go that keeps the browser signer distinct from the authenticated contract and its NFT holdings. Signature verification uses one safe-block anchor and a shared total deadline; unsupported account types and failed contract checks are rejected.
- Bound contract-wallet refresh to server-held login proofs, Redis epoch/JTI/version CAS, a 60-second access lease and a fixed two-hour session limit. Playback capabilities are capped by the issuing access token rather than extended by a later refresh.
- Executed a bounded Sepolia demonstration (2026-11-10/11) with two independent RPCs and one shared six-transaction fixture across monolith and api-gateway: two deployments, two mints and two transfers confirmed on-chain (0.009153 ETH total fees) with an on-chain holdings oracle and a real MetaMask SIWE login / signature rejection recorded. Media playback evidence, the remaining browser phases and the full TN matrix remain pending.

中文对应口径：已实现“控制者 A 签名、合约 C 登录并持有资产”的身份模型及有界续验；已在 Sepolia 完成有界六笔真实交易与链上 oracle 验证，并留存真实 SIWE 登录/拒签记录。已具备同候选完整 Docker/CI 证据，当前不能写“已完成公开测试网闭环”。个人设计、实现和评审贡献仍需本人逐条确认。
