# StreamGate Final Interview Script

## Opening

I built StreamGate as an `NFT-gated streaming backend` in Go.

The project is meant to show how I can take my audio/video backend background and apply it to a real product-shaped Web3 problem:

- wallet challenge login for identity
- NFT ownership for access control
- protected HLS manifest delivery
- short-lived playback tokens for segment access
- transcoding and worker scheduling for the media pipeline

So this is not a generic Web3 demo. It is a media backend with chain-backed authorization.

## Why this fits my background

My original strength is in media backend systems:

- content delivery
- access control
- task scheduling
- worker pools
- failure handling

That made it natural to frame Web3 as part of the authorization layer instead of turning the whole project into a token or contract demo.

## Core flow

### 1. Wallet identity

The backend issues a one-time challenge.
The client signs it.
The backend verifies the signature and consumes the challenge.

That gives us replay protection and avoids trusting raw wallet input.

### 2. NFT-backed authorization

I verify NFT ownership with real chain calls:

- `balanceOf` for collection-level checks
- `ownerOf` for token-level checks

Then the backend returns an authorization decision.

### 3. Protected streaming

The important media design choice is that I protect `manifest.m3u8`, not every segment with chain calls.

The flow is:

1. wallet login
2. bearer JWT
3. manifest request
4. NFT verification
5. short-lived `playback_token`
6. segment access using the token

That keeps playback cheap and matches how real streaming systems try to separate authorization from delivery.

### 4. Worker and transcoder side

The media backend side is not fake either.

I have:

- queued transcoding tasks
- worker pools
- priority scheduling
- retries
- cancellation
- health checks
- test coverage for the main state transitions

That part is where my audio/video experience maps most directly into Go concurrency and service design.

## What I would show live

1. `go test ./pkg/service -run 'Test(AuthService_AuthenticateWithWallet|MemoryChallengeStore_ChallengeLifecycle|AuthService_PlaybackTokenLifecycle)' -v`
2. `go test ./pkg/plugins/api -v`
3. `go test ./cmd/microservices/api-gateway -v`
4. `go test ./pkg/service -run 'TestTranscodingService_' -v`
5. `go test ./pkg/api/v1 -run 'TestTranscodingHandler_' -v`
6. `go test ./pkg/plugins/transcoder -v`
7. `go test ./pkg/plugins/worker -v`
8. `challenge -> login -> nft verify -> manifest -> segment -> transcode submit/status`

## What I would emphasize

- This project is about protected media access, not blockchain for its own sake.
- Web3 is used where it makes sense: identity and authorization.
- The streaming and worker pieces are still real media backend concerns.
- The transcoding API is also exposed, so the media pipeline is visible end to end.
- The design keeps expensive RPC work out of the hot path.

## What I would not overclaim

- No full DRM yet
- No production-grade RPC failover yet
- No end-to-end metadata completeness yet
- No fully productionized transcoding control plane yet

Those are good follow-up areas, but they are not required to make the current project credible.

## Closing

StreamGate shows that I can bridge two worlds:

- the media backend world I already know well
- the Go + Web3 stack I want to grow into

The interesting part is not just that the code runs. It is that the architecture tells a believable story about enterprise media access control with chain-backed identity.
