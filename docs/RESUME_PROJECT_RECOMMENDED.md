# StreamGate Recommended Resume Version

## Recommended title

`StreamGate: NFT-Gated Streaming Backend in Go`

## Recommended summary

Built a Go-based `NFT-gated streaming backend` for protected media access, using wallet challenge authentication, NFT ownership verification, HLS manifest gating, short-lived playback tokens, and transcoding task APIs to model a realistic media delivery and authorization system.

## Recommended bullet block

- Designed and implemented a Go-based `NFT-gated streaming backend` for protected media access, combining wallet challenge login, NFT authorization, protected HLS manifest delivery, and short-lived playback tokens.
- Built chain-backed authorization flows with wallet signature verification and NFT ownership checks, exposing real backend APIs for `auth/challenge`, `auth/login`, `nft/verify`, protected streaming access, and transcoding task submission/status flows.
- Implemented media backend task capabilities including transcoding task submission, task status query, task listing, worker scheduling, retry, cancellation, and health-check behavior.
- Added focused automated tests across service, API, gateway, worker, and transcoder layers to validate replay protection, NFT access behavior, cache-hit flow, protected playback, and task lifecycle handling.

## Why this is the recommended version

- It matches your strongest direction: media backend / streaming backend.
- It still reads well for Go backend roles.
- It keeps Web3 as a differentiator, not the entire identity.
- It avoids overclaiming protocol depth or production hardening.

## Best use cases

Use this version for:

- media backend roles
- streaming backend roles
- Go backend roles in media / video / content companies

Use a more Web3-heavy version only when the target role clearly prioritizes chain integration over media/backend depth.
