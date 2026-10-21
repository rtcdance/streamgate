# StreamGate Interview Self-Intro

## 30-second version

I built StreamGate as an `NFT-gated streaming backend` in Go.

The project combines my media backend background with Web3 authorization:

- wallet challenge login for identity
- NFT ownership checks for access control
- protected HLS manifest delivery
- short-lived playback tokens for segment access
- transcoding and worker scheduling for the media pipeline

It is designed to look like a realistic enterprise media system, not a token demo.

## 1-minute version

My goal was to use Web3 in a way that matches real product requirements.

Instead of treating blockchain as the whole application, I used it as the authorization layer for protected media access:

1. The backend issues a one-time wallet challenge.
2. The user signs the challenge.
3. The backend verifies the signature and issues JWT.
4. NFT ownership is checked at manifest access time.
5. If access is allowed, the backend issues a short-lived playback token.
6. Segment requests use the playback token instead of repeating chain calls.

This lets the system stay efficient while still keeping authorization chain-backed.

## 2-minute version

What makes this project a good transition story is that it sits exactly between my old strengths and the new stack I want to work on.

My prior background is audio/video backend work:

- content delivery
- access control
- task orchestration
- worker pools
- failure handling

StreamGate keeps that media backbone, then adds Go and Web3 in a business-relevant way:

- `pkg/service/auth_wallet.go` handles challenge-based wallet login
- `pkg/web3/chain.go` does real on-chain NFT checks
- `pkg/plugins/api/streaming_auth.go` protects HLS manifest and segment access
- `pkg/plugins/transcoder/transcoder.go` and `pkg/plugins/worker/scheduler.go` model the media processing side

The important design choice is that I do not verify NFTs on every segment request.
I verify once at manifest access, then issue a short-lived playback token.
That keeps the playback path cheap and makes the design closer to a real media product.

## What to emphasize

- This is not a generic Web3 demo.
- The business problem is protected media access.
- Web3 is used for identity and authorization.
- The media pipeline is still a real media backend, not just a wrapper around contracts.
- The code is test-driven on the main path, so the demo is reproducible.

## What to avoid overclaiming

- Do not say metadata is fully complete end-to-end.
- Do not claim full production RPC failover yet.
- Do not describe the current state as a full DRM system.
- Do not say the worker/transcoder pipeline is feature-complete; say it is a strong foundation with queueing, health, retry, and scheduling in place.

## Suggested closing line

This project shows that I can take my media backend experience, translate it into Go, and add Web3 authorization in a way that solves an actual product-shaped problem.
