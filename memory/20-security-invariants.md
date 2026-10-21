# 20 — Security Invariants

> **Type:** invariants · **Last verified:** 2026-10-21 @ `df253ef`
> These are not preferences. Breaking one is a release blocker.

## The invariants

| # | Invariant | Enforced in | Why |
|---|---|---|---|
| S1 | Never deliver a manifest or segment without an authorization decision — fresh ownership at the playlist, a valid capability for media | `pkg/middleware/nft_gate.go`, `pkg/gateway/routes.go` | it is the product's only reason to exist |
| S2 | The authorization subject comes from server-side rules, never the request | `resolveGatingRules` | otherwise the caller chooses what to prove |
| S3 | The gate fails closed | `NFTGateMiddleware` | an outage must not become a bypass |
| S4 | Ownership is read at `safe` or `finalized`, never `latest` | `pkg/web3/nft/verifier.go` | a reorg must not grant access |
| S5 | Evidence is same-chain and age-bounded (default 5s) | `resolveAnchor`, `evidenceAgeBound` | a stale read ignores transfers and rule changes |
| S6 | Challenges are consumed atomically | Redis Lua in `pkg/storage/redis_challenge.go`, called by the auth service | check-then-use would allow replay |
| S7 | A playback token is a signed, wallet-bound capability — presence alone never authorizes | `GeneratePlaybackToken`, `ValidatePlaybackToken` | HLS.js cannot set `Authorization` on media requests |
| S8 | `token_type` and audience are checked, not just the signature | `ParsePlaybackToken` | an access JWT must not work as a playback token |
| S9 | An empty wallet never receives a playback token | `GeneratePlaybackToken` | it would be an unauthenticated success |
| S10 | Every chain read has an RPC fallback chain | `pkg/web3/`, config `rpc_urls` | a single endpoint is a single point of failure |

## Gate decision matrix

| Condition | Result |
|---|---|
| `manifest.m3u8`, rules exist, holding sufficient | allow; mint or renew capability |
| `manifest.m3u8`, holding insufficient | `403` |
| segment / init / key, valid capability | allow, no chain query |
| segment / init / key, missing or invalid capability | `401` |
| no rules, mode `closed` (default) | `403 GATING_REQUIRED` |
| no rules, mode `open` (explicit opt-in) | allow |
| rule resolution error | `5xx` |
| gate disabled | `503 GATING_DISABLED` |
| no authenticated wallet | `401` |

## Threat model

- **Replay** — challenge reuse, token reuse across content, reuse across credential
  kinds. Defenses: atomic nonce consumption, `content_id` binding, `token_type`
  plus audience, `jti` revocation.
- **Reorg / stale read** — granting on a block that later disappears. Defenses:
  safe/finalized, anchor re-check, 5s evidence bound, reorg invalidation.
- **Confused deputy** — the client supplies contract or token id. Defense: rules
  come from the database.
- **Transfer or rule change mid-playback** — a capability would outlive the right.
  Defense: the playlist is the renewal point and always re-queries, and the
  capability itself is capped at 60s.
- **"Disabled" as a bypass** — treating the off switch as allow. Defense: `503`.

## Review checklist

- [ ] Did I add a route outside `routes.go`, or outside the streaming middleware chain?
- [ ] Does any new path return media bytes before `nft_verified` is set?
- [ ] Did I introduce `latest`, an untagged `eth_call`, or a single RPC URL?
- [ ] Does any failure branch return `2xx`?
- [ ] Is an authorization input taken from a param, query or body instead of the rules table?
- [ ] Is there a regression test that fails if the check is removed?
