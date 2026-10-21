# 70 — Glossary and Map

> **Type:** reference · **Last verified:** 2026-10-21 @ `df253ef`

## Project IDs

| ID | Meaning |
|---|---|
| `SG-00` … `SG-17` | Work items in `TRANSITION_PLAN.md` §5. P0 = blocking, P1 = required, P2 = conditional |
| `§8.x` | Evidence sections of the plan. More trustworthy than the checkboxes |
| `R01` … `R12` | Requirement traceability ids used by SG-15 |

## Domain terms

| Term | Meaning here |
|---|---|
| **Gating rule** | A row in `content_gating_rules`: chain, contract, token id, standard, minimum balance, active flag |
| **Rule version** | Monotonic version of a content's rule set; bound into capabilities and read through the rule-version authority so a rule edit can invalidate authorization |
| **Anchor** | The block (number + hash) at which ownership was proven; re-checked before renewal |
| **Evidence age** | Maximum age of an ownership read before it must be re-fetched; default 5s |
| **Playback token / capability** | Short-lived JWT (`token_type=playback`, 60s default and ceiling) bound to wallet, content, rule version and optionally a session |
| **Access token** | Ordinary login JWT with `token_type=access`; must never pass playback validation |
| **Fail closed** | Errors and missing configuration deny access rather than allow it |
| **Safe / finalized** | Ethereum block tags used for ownership reads; `latest` is forbidden |
| **Reorg** | Chain reorganization that can invalidate a previously observed holding |
| **sigchain** | `pkg/service/sigchain` — routes signature verification to EVM (secp256k1/EIP-191) or Solana (ed25519) |
| **Anvil** | Local Foundry chain, id 31337, hosting DemoNFT, DemoERC1155 and ContentRegistry |
| **HLS** | HTTP Live Streaming; manifests (`.m3u8`) plus segments, served by `streamingsvc` |

## Core file map

```
cmd/monolith/streamgate/main.go         entry, plugin loading
pkg/plugins/auth/plugin.go              auth plugin, route registration
pkg/service/auth_wallet.go              wallet sign-in, playback tokens
pkg/web3/signature/                     EIP-191 / EIP-712 / SIWE verification
pkg/middleware/nft_gate.go              gate, rule resolution, fail-closed policy
pkg/middleware/playback_token_auth.go   capability validation ahead of JWT
pkg/web3/nft/verifier.go                ownership, ERC-165 detection
pkg/gateway/routes.go                   the route table and middleware order
pkg/service/streamingsvc/streaming.go   manifest delivery
pkg/service/transcoding/transcoding.go  FFmpeg pipeline and worker pool
```

## Where to look

| Need | Location |
|---|---|
| Build, test, pitfalls | `CLAUDE.md` |
| Architecture deep dives | `docs/ARCHITECTURE.md`, `docs/architecture/` |
| Plan and evidence | `TRANSITION_PLAN.md` §5 and §8 |
| API contract | `docs/api/openapi.yaml` |
| Deployment | `DEPLOY.md` |
| Operations | `docs/operations/` |
| Web3 guidance | `docs/web3/` |
| Skills | `skills/INDEX.md` |
| Contracts | `contracts/src/` — `DemoNFT.sol`, `DemoERC1155.sol`, `ContentRegistry.sol` |
