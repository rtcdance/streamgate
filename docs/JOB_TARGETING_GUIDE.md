# StreamGate Job Targeting Guide

This guide maps the current StreamGate project to the kinds of roles it supports best.

## Best-fit roles

### 1. Go Backend Engineer

Best fit level:

- mid
- senior

Why it fits:

- HTTP API design
- auth flow
- service boundaries
- worker and queue logic
- tests around core service behavior

How to position it:

- emphasize backend architecture
- emphasize concurrency and state flow
- emphasize service evolution from monolith to microservice

### 2. Media Backend / Streaming Backend Engineer

Best fit level:

- mid
- senior

Why it fits:

- protected HLS access
- playback-token design
- transcoding task flow
- worker scheduling
- queue, retry, cancellation, health check

How to position it:

- lead with media delivery and access control
- treat Web3 as the new authorization mechanism
- show that the media path remains the core business path

### 3. Web3 Backend Engineer

Best fit level:

- junior to mid transition
- mid if the company values product-shaped backend work over pure protocol depth

Why it fits:

- wallet challenge login
- signature verification
- NFT ownership checks
- chain-backed authorization

How to position it:

- do not pitch it as deep protocol or smart contract engineering
- pitch it as backend integration of chain identity and content authorization

### 4. Platform / Infrastructure-adjacent Backend Roles

Best fit level:

- selective

Why it partly fits:

- worker lifecycle
- queueing behavior
- health monitoring
- service split thinking

Limits:

- observability is not fully productionized
- RPC failover is not complete
- deployment and runtime hardening are still evolving

## Roles this project supports less well right now

### Smart Contract Engineer

Why not ideal:

- the main depth is backend integration, not contract design
- the project does not center on Solidity architecture or contract security

### Pure SRE / DevOps

Why not ideal:

- there is some system thinking, but not enough production operations depth yet
- infra automation and runtime hardening are not the strongest story here

### Full-time Frontend Roles

Why not ideal:

- the strongest parts of the project are backend, media, and service architecture

## Best narrative by role

### If applying to Go backend roles

Say:

- I used a product-shaped media problem to demonstrate Go backend design.
- The interesting part is the service flow, auth path, queueing, and testing.

### If applying to streaming/media backend roles

Say:

- This project is fundamentally about protected media access.
- Web3 is the authorization layer; streaming remains the core product path.

### If applying to Web3 backend roles

Say:

- I focused on how wallets and NFTs integrate into a real backend system.
- My strength is turning chain primitives into actual product-side access control.

## Recommended title lines for resume or project list

Good options:

- `StreamGate: NFT-Gated Streaming Backend in Go`
- `NFT-Gated Media Access Backend (Go, Web3, HLS)`
- `Protected Streaming Backend with Wallet Auth and NFT Authorization`

Avoid titles that overclaim:

- `Web3 Streaming Platform`
- `Production-Grade Decentralized Media Network`
- `Full DRM + Multi-Chain Media Infrastructure`

## What to emphasize on a resume

- wallet challenge authentication
- NFT-based authorization
- protected HLS manifest flow
- playback token design
- transcoding task APIs
- worker pool and scheduler testing
- Go backend service architecture

## What to describe carefully

- RPC failover: say planned or partial, not complete
- metadata: say partial, not end-to-end complete
- transcoding: say strong foundation, not full production pipeline
- observability: say basic path exists, not fully productionized platform

## Best interview angle for your background

Your strongest angle is not:

- “I learned some Web3”

Your strongest angle is:

- “I took my existing media backend experience and applied Web3 where it actually makes sense: identity and authorization for protected content.”

That is the most credible and differentiated version of the story.
