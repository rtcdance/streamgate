# 00 — Charter

> **Type:** identity · **Last verified:** 2026-10-21 @ `df253ef`

## What StreamGate is

A Go platform that gates video playback on on-chain NFT ownership:

```
wallet sign-in → ownership proven at a safe block → short-lived playback capability → HLS
```

Module `github.com/rtcdance/streamgate`, version `1.0.6`. One codebase, two
topologies: a monolith for development and demos, nine microservices for a
prod-like deployment.

## Why it exists

This is primarily a **portfolio and interview vehicle** for Web3 backend roles,
not a production service with an operating user base. That single fact drives
most trade-offs:

- The core flow must be **demonstrable end-to-end** — real wallet, real chain,
  real HLS — not merely unit-tested.
- Every security claim must survive questioning and be backed by a reproducible check.
- Depth beats breadth. A narrow, correct, well-evidenced path is worth more than
  many half-wired features.

## Audience

1. Interviewers and reviewers reading the repository and asking "why".
2. The owner, returning to the system after time away.
3. AI agents changing code under the rules in `CLAUDE.md`.

## Success criteria

- A reviewer can go from clone to a working gated stream using `DEPLOY.md`.
- The authorization path fails closed and is pinned by regression tests.
- Acceptance evidence is reproducible from Docker or CI, never from a host build.
- The owner can explain every major decision without opening the code.

## Non-goals

| Non-goal | Why |
|---|---|
| ERC-4337 / account abstraction | `pkg/web3/account_abstraction.go` is unused; it is not a proof point |
| IPFS / decentralized storage | the upload path is MinIO/S3; decentralization is not claimed |
| P2P networking or token economics | the platform is centralized by design |
| Production capacity claims | only fixed-budget local measurements exist |
| Speculative features, premature abstraction | forbidden by project rules |

## Constraints

- **Docker-first**: a host build is a fast compile check; acceptance comes from
  the Docker stack or CI.
- **Solo owner plus AI agents**: context must be written down, not remembered.
- **Single plan document**: `TRANSITION_PLAN.md`. No process documents.
- **Dependency gate**: a new module needs approval and a `DEPENDENCY_APPROVAL.md` entry.
