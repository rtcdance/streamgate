# P1 Media + Web3 Interview Guide

This guide helps present StreamGate as a realistic transition project for:

- an audio/video backend engineer
- moving into Go backend engineering
- adding Web3 identity and authorization in a business-relevant way

It is intentionally framed for interviews, demos, and technical discussion.

## One-sentence project definition

StreamGate is an `NFT-gated streaming backend` in Go:

- wallets are used for identity
- NFT ownership is used for content authorization
- HLS manifest access is protected
- segment delivery uses short-lived playback tokens
- transcoding and worker scheduling provide the media backend foundation

## Why this project is a strong transition story

This is not a generic Web3 toy project.

It combines two things:

1. Existing strength:
   - media backend
   - access control
   - task scheduling
   - worker pools
   - performance and failure handling

2. New capability:
   - wallet challenge login
   - signature verification
   - chain-backed NFT authorization
   - RPC-aware backend design

That makes the project much easier to defend in interviews than a pure token or contract demo.

## Core story to tell

### Phase 1: Wallet identity

- The user does not just submit a wallet address.
- The backend issues a one-time challenge.
- The client signs the challenge.
- The backend verifies the signature and marks the challenge as consumed.

Why it matters:

- avoids trusting user input
- prevents replay
- looks like a real authentication flow

Relevant code:

- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/service/auth_wallet.go`
- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/service/auth_test.go`

### Phase 2: NFT-backed authorization

- The backend verifies collection ownership with `balanceOf`
- or token ownership with `ownerOf`
- then returns an authorization decision

Why it matters:

- the business rule is chain-backed, not hardcoded
- NFT ownership becomes access control, not just metadata

Relevant code:

- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/web3/chain.go`
- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/service/web3.go`
- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/plugins/api/auth_nft.go`

### Phase 3: Protected media delivery

- The backend protects `manifest.m3u8`
- NFT verification happens at manifest access time
- after authorization, the backend issues a short-lived `playback_token`
- segment access only validates the playback token

Why it matters:

- avoids chain lookup on every segment request
- matches real media delivery patterns
- separates expensive authorization from cheap delivery

Relevant code:

- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/plugins/api/streaming_auth.go`
- `/Users/mingo/Applications/workspace/web3/project/streamgate/cmd/microservices/api-gateway/main.go`

### Phase 4: Media backend execution

- transcoding tasks are queued
- worker pools process tasks
- workers have health state
- jobs support priority, retry, and cancellation

Why it matters:

- this is where audio/video engineering depth shows up
- it turns the project into a real media backend foundation

Relevant code:

- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/plugins/transcoder/transcoder.go`
- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/plugins/transcoder/transcoder_test.go`
- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/plugins/worker/scheduler.go`
- `/Users/mingo/Applications/workspace/web3/project/streamgate/pkg/plugins/worker/scheduler_test.go`

## Suggested interview demo flow

### Step 1: show automated validation

Run:

```bash
go test ./pkg/service -run 'Test(AuthService_AuthenticateWithWallet|MemoryChallengeStore_ChallengeLifecycle|AuthService_PlaybackTokenLifecycle)' -v
go test ./pkg/plugins/api -v
go test ./cmd/microservices/api-gateway -v
go test ./pkg/plugins/transcoder -v
go test ./pkg/plugins/worker -v
```

Talking point:

- I validate security flow, API behavior, worker logic, and media task orchestration with targeted tests before broadening scope.

### Step 2: show wallet challenge and login

Use the flow in:

- `/Users/mingo/Applications/workspace/web3/project/streamgate/docs/P0_DEMO_CHECKLIST.md`

Talking point:

- I intentionally used a challenge-based wallet login instead of trusting a wallet address directly.

### Step 3: show NFT verification and cache behavior

- call `/api/v1/nft/verify`
- repeat the same request
- explain `cache_hit`

Talking point:

- I cache short-lived NFT access decisions to reduce repeated RPC pressure while keeping authorization reasonably fresh.

### Step 4: show protected HLS access

- request `manifest.m3u8` with bearer token
- explain why manifest is protected
- extract `playback_token`
- request the segment

Talking point:

- chain-backed authorization is done once at manifest time; segment delivery uses a short-lived token for efficiency.

### Step 5: show worker/transcoder capability

- point to queue, worker pool, retry, cancel, health check tests
- explain that this is the media systems backbone

Talking point:

- this is where my media backend experience translates most directly into Go service design.

## Best architecture decisions to highlight

### 1. Challenge-based wallet auth

Why:

- prevents replay
- maps well to backend security thinking
- easier to reason about in a multi-instance setup

### 2. Manifest-first authorization

Why:

- content access is checked before playback starts
- avoids RPC work on every segment
- aligns with HLS and CDN behavior

### 3. Short-lived playback token

Why:

- decouples media delivery from chain calls
- makes segment access cheap
- narrows token blast radius

### 4. Short TTL NFT cache

Why:

- reduces repeated NFT checks
- keeps chain dependency manageable
- still lets authorization refresh within a bounded window

### 5. Worker pool with retry/cancel/health

Why:

- realistic for transcoding and asynchronous media pipelines
- demonstrates operational thinking beyond CRUD APIs

## Strong interview answers

### Q: Why use Web3 here at all?

Suggested answer:

Because the goal is not “put blockchain everywhere.”  
The goal is to model content membership or ownership with a chain-backed asset, then enforce that rule in a normal media backend.  
That makes Web3 part of the authorization layer, not the whole product.

### Q: Why not verify NFT ownership on every segment request?

Suggested answer:

That would be too expensive and would hurt playback latency.  
I verify at manifest access, then issue a short-lived playback token for segments.  
That keeps authorization strong enough for the use case without making media delivery depend on repeated chain RPC.

### Q: Why is this a good Go project for you?

Suggested answer:

Go fits the backend concurrency model well:

- HTTP APIs
- worker pools
- task orchestration
- service boundaries
- instrumentation

My previous audio/video backend experience transfers naturally to these parts, while Web3 adds a new identity and authorization layer.

### Q: What parts are intentionally not production-complete yet?

Suggested answer:

- no DRM yet
- no distributed worker persistence yet
- no production-grade RPC failover strategy yet
- no full transcoding pipeline with actual FFmpeg profiles exposed through API yet

I intentionally kept the scope focused so the core access-control and media-delivery decisions are clear and testable.

## What to emphasize from your background

Say this clearly:

- I did not abandon my media backend strengths to learn Web3.
- I used Web3 to solve a media access-control problem.
- The strongest part of the project is the combination of:
  - security-oriented access control
  - media delivery behavior
  - worker and transcoding orchestration

That framing makes your transition story coherent.

## Current evidence you can point to

### Security/auth evidence

- one-time challenge consumption
- replay rejection
- expired challenge rejection
- playback token content binding

### Authorization evidence

- NFT verify by balance
- NFT verify by token ownership
- `cache_hit` behavior

### Media/backend evidence

- manifest/segment split
- worker queue
- retry
- cancel
- health check
- deadlock fixes in queue/scheduler paths

## Honest limitations to mention

- some media execution paths still use placeholder processing instead of full FFmpeg production wiring
- Redis-backed challenge storage exists, but the most complete automated coverage today is still memory-backed
- end-to-end chain behavior depends on real RPC and contract state

These are acceptable limitations if you present them as deliberate scope control.

## Recommended presentation order

1. Project definition
2. Why it matches your background
3. Wallet auth flow
4. NFT authorization flow
5. Protected streaming flow
6. Worker/transcoder architecture
7. What is tested
8. What you would build next in production

This order keeps the story coherent and easy to defend.
