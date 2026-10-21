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
