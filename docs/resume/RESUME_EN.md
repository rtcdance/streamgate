# Resume (English)

> **Current delivery status (2026-10-30): local candidate PASS WITH BOUNDARIES; same-SHA CI attempt 10 failed; final fixes pending validation.** Candidate `4eae43d6abe94f21c7730c1dc27fb2b12b9f095a` passed the complete Docker `all` / M0–M8 with raw `command.exit=0`; artifacts `sg-final3-4eae43d6` were independently revalidated. CI run `40608718226` ended in failure at attempt 10; subsequent fixes need their own complete acceptance and CI. Figures tied to older SHAs remain historical. Production rollout and personal presentation/recording/application work remain pending. This material revision does not inherit the code candidate's CI status.

> Output discipline (TRANSITION_PLAN SG-09): only claims that can be demonstrated live; every figure cites its commit SHA / test evidence (§8). No line counts, no coverage numbers, no "microkernel architecture", no service counts, no version tags.
> 【待本人确认 / confirm with candidate】 marks claims to verify before sending; `待补 / pending` marks work that is NOT done and must not be presented as complete.
> `[...]` placeholders must be replaced before sending.

---

## Basic Information

- Name: [Your Name]
- Phone: [Your Phone] · Email: [Your Email]
- Location: [Your City] · GitHub: [github.com/your-handle](https://github.com/your-handle) (StreamGate pinned)

## Professional Summary

10+ years in C++, 6+ years in audio/video streaming (iOS/Android native players and streaming delivery), now focused on Go backends with Web3 identity and authorization. StreamGate's reproducible loop: wallet-signature login → server-side NFT rules → same-block ownership evidence → 60s playback capability → protected HLS with real decode → access denied within 70s after the safe view confirms an NFT transfer → fault injection and recovery. The project is AI-assisted; I own the requirements freeze, design trade-offs, review and failure-case diagnosis【待本人确认 / confirm with candidate】.

## Core Skills

- **Media**: FFmpeg ABR / HLS, real EXTINF playlists, Range/206, browser hls.js playback assertions; iOS/Android native player internals
- **Backend**: Go, PostgreSQL (single-statement transactions / advisory locks / outbox), Redis (atomic Lua consumption, bounded evidence caches, session epochs), NATS JetStream (redelivery / leases / explicit DLQ), MinIO / S3
- **Web3**: EIP-191 / EIP-712 / SIWE, EIP-2 low-s, EIP-1271, ERC-721/1155 quantity semantics with ERC-165 detection, same-block `safe`/`finalized` evidence, reorg invalidation, idempotent chain writes with durable nonces
- **Security engineering**: fail-closed authorization, derived revocation bound (60+5+5=70s), **one authorizer and one policy snapshot shared by the HTTP and gRPC entry points** (same `token_type` clause, quantity threshold and block anchor), A/B two-wallet negative acceptance tests, TDD security regressions

## Project Experience

### StreamGate — NFT-Gated Streaming Backend (Go, personal project, AI-assisted)

The main line: real wallet signature → one-time challenge → server-side NFT rules → same-block evidence → 60s capability → real HLS playback → access denied after transfer → RPC / Redis faults and recovery. **AI honesty**: most implementation code is AI-generated; I own the design decisions and acceptance criteria, review every change, and locate/fix failing cases — AI code generation is not "I designed and implemented this independently"【待本人确认 / confirm with candidate】.

Current status: candidate `4eae43d6` passed complete local Docker acceptance; same-SHA CI and final delivery review remain pending. Each media deployment leg passed 77 checks. HTTP/gRPC transfer denial took 62.989s/63.208s and 60.949s/61.149s after the safe view confirmed the transfer; chain finality waiting is outside those timings. Evidence: `sg-final3-4eae43d6/logs/media.log`. Other historical results retain their own SHA boundaries. **Pending**: production rollout, personal presentation, recording, and applications.

**Entry 1 — Signatures & resource authorization**

- **Problem**: the AI-generated authorization path had a systematic bypass surface — nonces double-allocated under concurrency, gating-rule writes without content-ownership checks, arbitrary server-side signing for any JWT user, and playback credentials spendable as access tokens on management APIs.
- **My decision**【待本人确认 / confirm with candidate】: authorization subjects come only from the server (rule table + authenticated session); signing is off by default behind a triple gate (development + chain 31337 + explicit flag; 404 when disabled); transaction nonces use a Postgres advisory lock with `UNIQUE(chain,signer,nonce)` as the single authority; one-time challenge consumption is a single atomic Redis Lua command.
- **Code entry**: `pkg/storage/redis_challenge.go`, `pkg/service/gating/rule.go` (`CreateRuleForOwner`, single-statement owner-conditional write), `pkg/gateway/routes.go` (`DemoSignerAllowed`), `pkg/middleware/` (token_type whitelist, iss/aud), `pkg/web3/tx/nonce_ledger.go`.
- **Measured under**: real-Postgres A/B two-wallet ownership negatives (7 cases, 0 skips); two instances allocating 20×20 concurrently on one database; fullchain acceptance in both deployment modes.
- **Result**: zero rows written by wallet B on wallet A's rules (stable 404/403 split); signing route 404 when disabled; `TestAcceptance_SharedNonceAuthority` — zero duplicates, a fix driven by a real acceptance FAIL (missing mint receipt), commit `0756ea4`; `TestAcceptance_RegistryIdempotency` — 20 concurrent creates yield 1 row, same key with a different payload returns 409, commit `e859d13`.

**Entry 1 (continued) — the authorizer is not duplicated (HTTP / gRPC)**

- **Problem**: gRPC `GetManifest` decided access with a boolean ownership call instead of the **quantity threshold**, and hardcoded the capability TTL at 2 minutes; HTTP and gRPC each carried their own authorization logic, so the threshold, the TTL and the credential rules could drift independently.
- **My decision**【待本人确认 / confirm with candidate】: both entry points share **one authorizer and one policy snapshot** (rules + rule version + authoritative read time); a capability can only be minted from that sealed authorization result; gRPC enforces the **real on-chain quantity** (ERC-721/1155, token_id, min_balance, OR rules) and uses the shared 60s capability policy (the hardcoded 2-minute TTL is gone); when a request carries both an access token and a capability, wallet, session and epoch must agree.
- **Code entry**: `pkg/service/` (`ResolvePolicySnapshot` / `AuthorizePlayback`), `pkg/gateway/grpc_server.go`, `test/grpcparity`, `scripts/fullchain-acceptance.sh` section 9b.
- **Measured under**: the same candidate image on **both deployment legs** (monolith and the standalone `api-gateway`), each running the gRPC parity section.
- **Result**: a real holder obtains the manifest over gRPC with a capability `exp` inside 60±5s, then fetches real media with that segment URL and **no `Authorization` header** — byte-for-byte identical to the protected HTTP media; a non-holder, a holder below the real quantity (1 held / 2 required, 2 held / 3 required) and conflicting dual credentials are each rejected with `PermissionDenied`; both legs report 71/71 PASS (`d0a9451`, `6178f11`). That section also caught a real defect: the standalone `api-gateway` entry never handed its authorizer to the gRPC server, so in that mode every gRPC call failed closed with `Internal` while HTTP worked (`d2f1d81` — invisible in monolith-only runs).

**Entry 2 — Chain state, caching & revocation**

- **Problem**: `latest` lies across reorgs; caches leaked evidence across chains and thresholds; issued playback credentials stayed valid long after the transfer.
- **My decision**【待本人确认 / confirm with candidate】: pin every ownership read to a single resolved block (number + hash), ERC-165 detection reading the same block; positive evidence has total age ≤5s from the original `observedAt`; caches store balance evidence, never a verdict; the revocation bound is derived (60s capability + 5s evidence + 5s clock skew = 70s) and its sum enforced by `ValidateProduction`; rule versions (Postgres) and session epochs (Redis) are checked at every media request.
- **Code entry**: `pkg/middleware/nft_gate.go` (`resolveAnchor`, `InvalidateNFTEvidence`), `pkg/web3/nft/verifier.go`, `pkg/core/config/config.go` (`MaxTransferRevocationBound`), migration 036, `storage.SessionStore`.
- **Measured under**: two real Anvils plus a recording RPC proxy (asserting every `eth_call`'s block parameter equals the anchor); snapshot/revert for a real reorg; transfer → deny → transfer-back → restore; RPC / Redis dependency-fault entries.
- **Result**: historical natural capability expiry was 67s (not a transfer-to-denial measurement); 5s evidence age; manifest 403 after the transfer, 200 after transferring back; logout revokes issued capabilities within 5s; with RPC fully down a new manifest answers 503 while issued capabilities keep working on their own validity and still expire on time; reorg invalidation drops only that chain's evidence (`796e096`, `b93b94f` / `396e833`, `99d4324`, dependency-fault entry 16/16 `767d506`).

**Entry 3 — Real media delivery & measured debugging**

- **Problem**: the old acceptance self-signed JWTs and passed 1KB of zero bytes off as video; hand-written playlists hardcoded 6s segments; a >2 GiB download was silently truncated and reported success; cancelling a transcode left orphaned FFmpeg processes; task state lived in an in-process map.
- **My decision**【待本人确认 / confirm with candidate】: acceptance must use real signatures, real media and real decode (full FFmpeg decode + browser playback); originals are SHA-256-hashed server-side and sealed under an unoverwritable key, re-sealing different bytes returns 409; an ffprobe whitelist gate plus `maxBytes+1` rejects oversized inputs; children are killed by process group; Postgres is the single task authority (lease / outbox / explicit DLQ); outputs go to a private generation prefix, published atomically after verification; untrusted media parsing runs in an isolated worker container (non-root, drop caps, no JWT/chain keys).
- **Code entry**: `scripts/fullchain-acceptance.sh`, `pkg/service/upload` (sealing), `pkg/plugins/transcoder/ffmpeg.go` (`ValidateMediaFile`), `pkg/service/transcoding` (lease / outbox / DLQ), `published_generation`, `cmd/microservices/transcoder/main.go`.
- **Measured under**: the same acceptance entry on monolith and api-gateway; a real fixture (10s / H.264+AAC / 640×360); a browser assertion that `currentTime` reaches ≥3s; execution attribution via DB `lease_owner` plus worker logs.
- **Result**: **ALL 66 PASSED in both modes** (59 + 7); EXTINF summing to 10.0s (`de4b83c`); sealed object's sha256 matching the DB digest (`f891091`); probe gate with 14 sub-cases (`313d414`); lease / DLQ cases (`a8784ad`); atomic generation publish, 6 sub-cases (`f7f0dd0`); isolated-worker attribution (`c67b222`).

**ChainPulse (independent project, evidence to be compiled separately)**: written as a separate proof task so the two projects do not duplicate each other's Go / Redis / microservice stack claims; no unreviewed numbers go into this resume【待本人确认 / confirm with candidate: keep this line?】.

**Trust boundary (non-negotiable)**: this is "on-chain NFT permission + a platform media service"; multi-RPC fallback is not light-client proof; holding an NFT is not media copyright; the platform registry is only the platform's attestation. No full-decentralization / trustlessness / verified production HA / copyright verification / DRM claims; downloaded media cannot be recalled.

## Work Experience

### [Company A] — [Title] ([Period])

- [Media backend / streaming systems: replace with real scope, e.g. "playback pipeline serving X DAU"]
- [Real outcomes in access control, task scheduling, transcoding, or performance]
- [Tech stack]

### [Company B] — [Title] ([Period])

- [Real experience]

## Education

- [University] · [Major] · [Degree] · [Years]

---

### Pre-send checklist (from TRANSITION_PLAN SG-09 — do not delete)

1. Every `[...]` placeholder replaced; every work bullet must survive 10 minutes of follow-up questions.
2. Every 【待本人确认】 reviewed; the three "my decision" entries were genuinely yours — otherwise reword as "reviewed and co-signed".
3. Every figure and test name points at evidence: commits `0756ea4 / e859d13 / f891091 / 313d414 / a8784ad / f7f0dd0 / c67b222 / de4b83c` and the matching §8 tables in TRANSITION_PLAN.md.
4. Follow the current status marker at the top of `TRANSITION_PLAN.md` and its latest §8 evidence. Old candidate `ab172c2` / run `39005594931` and prior performance figures are historical only, not current completion. `4eae43d6` has complete local acceptance and performance evidence; subsequent fixes still need full acceptance and same-SHA CI; production rollout is unperformed, and personal presentation/recording/application tasks await the candidate.
5. No DRM / full-decentralization / production-HA claims — boundary language is in `PROJECT_TALK_SCRIPT.md`.
