# 50 — Status Snapshot

> **Type:** status · **Snapshot:** 2026-10-24 · **Verification revisions:** `caeb1a47` (old all3 outer FAIL), `aa948e78` (failed all, perf repro), `5d87fe8b` (tooling) on `sg/mainline-execution`
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
| Historical baseline observations | 3e5785e0: unit plus six functional stages; historical outer exits not recorded |
| Mac-only execution | desktop-local, frozen source, daemon mutex, explicit compile/browser cache mounts; native-linux remains a separate profile |
| Schema / release qualification | 5ceb1f5d release exit 0; candidate/S 77 each, identical schema fingerprint and three failures/recoveries |
| all3 raw checks | caeb1a47: 48 matrix cases, three comparison rounds per side, media 77 x2, recovery 43, release checks; outer exit 2 because perf marker missing, not complete acceptance |
| Independent all3 verification | Original caeb verifier in Docker reread matrix/resources/faults/fixture/archives/images; exit 0; p95 ratio 0.9072, local measurement only |
| Integrated strict fuzz retry | a3a7df3d; affected real Docker ops2 command.exit=0 |
| Marker/delegated release correction | 5d87fe8b; independent Docker shell49 + fuzz10 + gates16 + evidence12 = 87 PASS |
| Collaboration ownership | CodeBuddy owns Docker runs and integration; Codex owns independent review and plan; accepted through the local ACP session |

**Bookkeeping warning:** historical completion labels and checkboxes do not
prove current-candidate completion. Use the current table in plan §8.19 and
its SHA-bound evidence in §8.20–8.30; overall acceptance remains incomplete.

## Not closed

| Item | Priority | Note |
|---|---|---|
| Baseline compatibility qualification | — | Old-S/new-schema behavior, restore and fresh caeb reference measured; new terminal candidate still pending |
| Full current candidate M0–M8 | P0 | aa948e78 all exited 2 after comparison round 2 had 26x503 (95.31%); perf-only repro exit 0 does not close all. Second all is in flight; CI portability fix and final same-SHA evidence remain required |
| Environment boundary | — | Mac-only; native Linux/production capacity is outside local evidence, not a missing local prerequisite |
| Same-SHA CI execution | P0 | Owner delegated recommended decisions on 2026-10-24: necessary Mac runner setup, variables, new-branch push and dispatch may proceed under plan §8.30; portability fix still required; no force push |
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

1. Finish the in-flight run, preserve its raw result, and close the confirmed cross-runner archive-path defect with targeted checks.
2. Under the delegated authorization, prepare this Mac as the acceptance runner with a dedicated label and desktop-local profile.
3. Freeze final C and run its complete Docker all inside the existing same-SHA CI acceptance job, using exact scanned archives and a fresh reference/matrix; avoid another duplicate standalone full run.
4. Keep SG-16/17, production rollout and personal tasks outside the mainline gate.

## Refreshing this file

Replace the snapshot header, rewrite the evidenced and open tables from §8, and
delete anything that is no longer true. Do not append a history — the plan owns
history.
