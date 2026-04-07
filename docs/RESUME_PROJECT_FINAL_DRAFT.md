# StreamGate Resume Final Draft

## Recommended title

`StreamGate: NFT-Gated Streaming Backend in Go`

## Version A: Best for media backend / streaming roles

Built a Go-based `NFT-gated streaming backend` for protected media access, using wallet challenge authentication, NFT ownership verification, HLS manifest gating, short-lived playback tokens, and transcoding task APIs to model a realistic media delivery and authorization system.

- Implemented wallet challenge login, JWT issuance, and NFT-based access control for protected media playback.
- Designed protected HLS access flow by verifying NFT ownership at manifest time and issuing short-lived playback tokens for segment retrieval.
- Built transcoding task submission, task status query, task listing, cancellation, and profile APIs to support media-processing workflows.
- Added worker and transcoder foundations including queueing, retry, cancellation, health-check behavior, and focused automated tests across service, API, gateway, and plugin layers.

## Version B: Best for Go backend roles

Built a Go backend project that integrates wallet authentication, NFT-based authorization, protected content access, and task-processing workflows in a realistic product scenario.

- Implemented backend APIs for wallet challenge login, NFT verification, streaming authorization, and transcoding task management.
- Designed service-layer state transitions for auth, playback-token flow, task lifecycle, and queue-backed processing.
- Added targeted tests for replay protection, NFT verification, cache-hit behavior, protected streaming access, task lifecycle, and worker/transcoder behavior.
- Structured the system so the core path can run cleanly through both monolith-style and microservice-oriented entry points.

## Version C: Best for Web3 backend roles

Built a backend integration project that turns wallet identity and NFT ownership into product-side authorization for protected media content.

- Implemented challenge-based wallet login, JWT issuance, and NFT ownership verification using real backend flows instead of mock authorization logic.
- Integrated chain-backed authorization into media access control through manifest gating and short-lived playback tokens.
- Positioned Web3 as the authorization layer inside a real backend/media system rather than as a standalone contract or token demo.

## Concise 3-bullet version

- Built a Go-based `NFT-gated streaming backend` combining wallet challenge login, NFT authorization, protected HLS manifest access, and playback tokens for segment delivery.
- Implemented transcoding task APIs, worker scheduling, retry/cancel flows, and task lifecycle handling to model a realistic media backend pipeline.
- Added focused automated tests across auth, NFT verification, streaming, gateway, worker, and transcoder layers to validate core backend behavior.

## Recommended skills / tags next to this project

- Go
- Web3
- JWT
- HLS
- REST API
- Worker Pool
- Task Queue
- Streaming Backend
- Backend Architecture

## Suggested one-line summary for profile or project section

Used a media backend scenario to demonstrate how Go services and Web3 authorization can work together in a realistic protected-content system.
