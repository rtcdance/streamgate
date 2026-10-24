# 50 — Status Snapshot

> **Type:** status · **Snapshot:** 2026-10-24 · **Verification revision:** `5ceb1f5d` (release), `7f2c208d` (recovery/media) on `sg/mainline-execution`
> **This file decays.** Re-verify with `git log` and `TRANSITION_PLAN.md` §8 before acting.

## Where the work stands

The core flow — wallet sign-in → NFT ownership at a safe block → playback
capability → protected HLS — is implemented, wired, and pinned by regression
tests. The current phase is closing the remaining plan items and keeping the
evidence reproducible, not building new surface area.

Volatile counts live in `CLAUDE.md`; measured results remain bound to their
source revision and raw artifacts.

## Environment (2026-10-24)

Acceptance runs on the **local Mac + Docker Desktop** (8 vCPU / 16 GiB VM).
The `prod-srs` Docker resources were purged (0 images/containers/volumes/cache)
and `remote-dev-machine` is a historical skill. Local functional stages are
valid evidence.

The owner confirmed Mac-only execution on 2026-10-24. The new `desktop-local`
profile allows complete all/perf/release under explicit local measurement
boundaries. Native Linux checks remain available as a separate strict profile.
Source freezing, single-run locking, raw exits, full workloads and business
assertions remain required. Profile regression and actual qualification results
belong to the plan, not an inferred PASS here.

## Evidenced (checkbox may lag)

| Item | Evidence |
|---|---|
| Historical baseline observations | `3e5785e0`: frozen unit plus six functional stages; separate runs, historical outer exits not recorded |
| M0 Shell gate and CI wiring | `a7791c1d`; fixed Docker tool regressions 84 PASS, real local gate refused before build |
| Proposed S | `3e5785e069b1126c4b7aa7b6301f8877edfcb0c7`, pinned by `39921df4`; protocol/tool regressions 101 PASS; not qualified |
| Recovery teardown correction | `9b8f0d14`; old stage regression red, corrected Docker regression 56 PASS |
| Cache wiring | `8a67064b` plus required fix `7f2c208d`; Go/browser mounts and forced test execution verified; initial failed controller run retained |
| Actual recovery → media | `7f2c208d`, `.verify-artifacts/sg-eff-recovery-media-20261024/`: outer exit 0, recovery 43/43 and media 77/77 ×2; identities and cleanup verified |
| Mac-only profile and controller integrity | Shell profile gate, dirty-source refusal, parser snapshot and created-state logging regressions; 101 distinct tool tests passed incrementally, old implementations fail negative controls |
| Empty-ABI mint correction | 5ceb1f5d; Docker pkg/service race plus vet exit 0 (694 top-level groups, two chain-environment skips); old EOA response test fails |
| Actual local release qualification | 5ceb1f5d; sg-eff-mac-release2-20261024 outer exit 0, candidate/S 77/77 each, schema unchanged, three failures and recoveries; logging observer boundary documented in plan |
| M6 candidate material preparation | Same frozen revision, four tracked documents snapshotted and hashed; material helper exit 0, full gate pending |

**Bookkeeping warning:** historical completion labels and checkboxes do not
prove current-candidate completion. Use the current table in plan §8.19 and
its SHA-bound evidence in §8.20; overall acceptance remains incomplete.

## Not closed

| Item | Priority | Note |
|---|---|---|
| Baseline compatibility qualification | — | Current local release qualifies old-S/new-schema business behavior and candidate recovery; terminal all/reference still pending |
| Full current candidate M0–M8 | P0 | No final nine-stage all, fresh same-runner reference, 48-scenario matrix or same-SHA CI |
| Environment boundary | — | Mac-only; native Linux/production capacity is outside local evidence, not a missing local prerequisite |
| Remote CI authorization | P0 | Push/dispatch requires explicit instruction; existing remote mainline history diverged, use a new acceptance branch |
| SG-16 HLS AES-128 and authorization keys | P2, conditional | only if the scope is extended |
| SG-17 one native player sample | P2, conditional | only if the scope is extended |

## Known limitations (accepted, not bugs to "fix" casually)

- ERC-4337 is out of scope; `pkg/web3/account_abstraction.go` is unused.
- `pkg/web3/contract/registry.go` is complete but never constructed.
- IPFS is not wired into upload.
- The Grafana/Jaeger stack was removed; `/metrics` remains.
- The platform is centralized; only ownership reads are on-chain.
- No production capacity or soak test; only fixed-budget local measurements.

## Next moves

1. Freeze final C and run complete local all with a fresh reference/matrix.
2. Set up the Mac as the acceptance runner only after installation/registration authorization; workflow supports a runner label and desktop-local profile variables.
3. Obtain push/dispatch authorization and run same-SHA CI with exact scanned archives.
4. Keep SG-16/17, production rollout and personal tasks outside the mainline gate.

## Refreshing this file

Replace the snapshot header, rewrite the evidenced and open tables from §8, and
delete anything that is no longer true. Do not append a history — the plan owns
history.
