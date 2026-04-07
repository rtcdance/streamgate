# StreamGate Resume Experience Final

## Recommended final project entry

### StreamGate: NFT-Gated Streaming Backend in Go

Built a Go-based `NFT-gated streaming backend` for protected media access, using wallet challenge authentication, NFT ownership verification, HLS manifest gating, short-lived playback tokens, and transcoding task APIs to model a realistic media delivery and authorization system.

- Designed and implemented wallet challenge authentication, JWT issuance, NFT authorization, and protected streaming access flows for gated media playback.
- Built protected HLS delivery by verifying NFT ownership at manifest time and issuing short-lived playback tokens for segment access, keeping chain-backed authorization out of the hottest playback path.
- Implemented transcoding task submission, status query, task listing, cancellation, and profile APIs, along with worker scheduling, retry, health-check, and task-lifecycle behavior.
- Added focused automated tests across service, API, gateway, worker, and transcoder layers to validate replay protection, NFT verification, cache-hit behavior, protected playback, and media task flow.

## Shorter version if space is tight

### StreamGate: NFT-Gated Streaming Backend in Go

Built a Go-based protected media backend integrating wallet challenge login, NFT authorization, HLS manifest gating, playback tokens, and transcoding task workflows.

- Implemented wallet auth, NFT verification, protected streaming access, and transcoding task APIs.
- Added worker scheduling, retry/cancel flow, and focused tests across auth, streaming, gateway, and transcoder paths.

## One-line version for highly compressed resumes

Built an `NFT-gated streaming backend` in Go combining wallet auth, NFT-based authorization, protected HLS access, playback tokens, and transcoding task workflows.

## Best fit for this version

Use this as the default version for:

- media backend roles
- streaming backend roles
- Go backend roles in media, video, or content platforms

## Why this is the best default

- It leads with your strongest background: media backend systems.
- It still shows clear Go backend service design.
- It uses Web3 as a meaningful differentiator without making the whole story sound niche.
