# English Resume Template

## Basic Information

- Name: Your Name
- Phone: Your Phone Number
- Email: Your Email
- Location: Your City / Region
- GitHub / Portfolio: Your Link

## Professional Summary

Backend engineer with 10+ years of C++ and media-backend experience, covering streaming delivery, access control, task orchestration, worker architecture, and high-concurrency backend systems. Currently focused on transitioning into Go backend and Web3 authorization scenarios, with hands-on work integrating wallet identity, NFT-based authorization, protected streaming access, and media-task workflows into realistic backend product flows.

## Core Skills

- Languages: C++, Go
- Backend: HTTP APIs, authentication, authorization, task lifecycle, service design
- Media / Streaming: HLS, protected media access, transcoding workflows, media backend systems
- Web3: wallet challenge login, signature verification, NFT ownership checks, chain-backed authorization
- Engineering: unit tests, route tests, worker/queue design, monolith-to-microservice evolution

## Project Experience

### StreamGate: NFT-Gated Streaming Backend in Go

Built a Go-based protected media-access backend that combines wallet challenge authentication, NFT ownership verification, HLS manifest gating, short-lived playback tokens, transcoding task APIs, and worker scheduling into a realistic content-access control system.

- Designed and implemented wallet challenge authentication, signature verification, JWT issuance, and NFT authorization flows for protected media access.
- Designed an HLS authorization flow that verifies NFT ownership at manifest time and uses short-lived playback tokens for segment access, keeping chain calls out of the hottest playback path.
- Implemented transcoding task submission, status query, task listing, cancellation, and profile APIs, along with worker scheduling, retry, cancellation, and health-check behavior.
- Added automated tests across auth, NFT verification, streaming, gateway, transcoding, and worker paths to validate replay protection, cache-hit behavior, protected playback, and task lifecycle handling.

## Work Experience

### Company A

- Title: Your Title
- Period: Start - End
- Highlights:
  - Designed and developed media-backend / streaming-related systems
  - Worked on access control, task scheduling, transcoding pipelines, or backend performance optimization
  - Replace this section with your real experience

### Company B

- Title: Your Title
- Period: Start - End
- Highlights:
  - Replace this section with your real experience

## Education

- University: Your School
- Major: Your Major
- Degree: Your Degree

## Notes

This template is best aligned with:

- media backend roles
- streaming backend roles
- Go backend roles

If a role is more Web3-heavy, move the wallet-auth and NFT-authorization points higher in the project bullets.
