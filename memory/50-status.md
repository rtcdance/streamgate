# 50 — Status Snapshot

> **Type:** status · **Snapshot:** 2026-10-23 (EFF pass) · **HEAD:** `4abcb3c5` on `sg/mainline-execution`
> **This file decays.** Re-verify with `git log` and `TRANSITION_PLAN.md` §8 before acting.

## Where the work stands

The core flow — wallet sign-in → NFT ownership at a safe block → playback
capability → protected HLS — is implemented, wired, and pinned by regression
tests. The current phase is closing the remaining plan items and keeping the
evidence reproducible, not building new surface area.

Baseline facts at snapshot time: version `1.0.6`, 11 build targets, 338 test
files, ~71% coverage under the Docker unit gate, 60% CI floor, 42 up / 26 down
migrations.

## Environment (2026-10-23)

Acceptance runs on the **local Mac + Docker Desktop** (8 vCPU / 16 GiB VM).
The `prod-srs` Docker resources were purged (0 images/containers/volumes/cache)
and `remote-dev-machine` is a historical skill. Local functional stages are
valid evidence.

Since the EFF pass, `all` / `perf` / `release` are **refused before the tool
image is built** unless the daemon is native linux/amd64 with >=8 CPU, >=32 GiB
and >=100 GiB free (`scripts/cd/verify_environment.py`). This machine is
aarch64 / 15.6 GiB, so the terminal run is explicitly BLOCKED here — by design,
not as a flake to retry. Formal runs also use a frozen clone of the approved
HEAD and a daemon-wide lock; the shared Go cache is opt-in and off by default.

## Evidenced (checkbox may lag)

| Item | Evidence |
|---|---|
| SG-00 unified Docker acceptance entry | plan heading (2026-10-17 final run) |
| Local `unit` stage | `.verify-artifacts/sg-local-unit3` (2026-10-23, `6b613ec0`) |
| Frozen-clone `unit` stage | `.verify-artifacts/sg-unit-frozen5` PASS 20m47s, GOCACHE=off (2026-10-23, `4abcb3c5`) |
| Local `media` stage | `.verify-artifacts/sg-local-media4` 77/77 ×2 (2026-10-23, `6823b56d`) |
| SG-04 same-chain, same-block holding and reorg | plan §8.9, checked |
| SG-05 real wallet → real HLS loop | plan §8.10, checked |
| SG-09 job material (AI portion) | plan SG-09 heading, checked (its SHA is a pre-rewrite label) |
| SG-10 trusted architecture and wiring | plan §8.1, checked |
| SG-11 contract standards, state machine, ABI | plan §8.3, checked |
| SG-06 renewal, transfer, rule/session revocation | plan §8.11, marked complete |
| SG-07 media performance and observability | plan §8.16 H, marked complete |
| SG-08 same-artifact CI/CD and recovery | plan §8.16 K.2, marked complete |
| SG-12 media originals, idempotent tasks, upload safety | plan §8.12, nine steps complete |
| SG-13 off-chain consistency, tx idempotency, event recovery | plan §8.13 A/B both PASS; box unchecked |
| SG-14 runtime governance, budget, recovery | plan §8.15 |
| SG-15 closeout and traceability | plan §8.16–8.17 |

**Bookkeeping warning:** SG-00/06/07/08/12/13/14/15 have completion evidence in
§8 but their checkboxes are still `[ ]`. Treat the checkbox as unreliable; the
evidence section is the source of truth.

## Not closed

| Item | Priority | Note |
|---|---|---|
| SG-01 NFT quantity, rules, cache isolation | P0 | §8.4 is all PASS, but its Docker unit stage was never run — re-evidence on a current candidate |
| SG-02 CI must really fail; one gate for every entry | P0 | §8.5 passes locally; the new CI jobs only run on GitHub — later SG-08 runs may satisfy it, confirm |
| SG-03 wallet identity, resource ownership, server signing authority | P0 | §8.6/8.7 are partial; §8.8 covers only resource ownership, so the task stays open |
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

1. Reconcile the plan's checkbox state with §8 so status is not ambiguous.
2. Close or re-evidence SG-01/02/03, the only remaining P0 items.
3. Decide explicitly whether SG-16/17 are in scope; do not drift into them.
4. Re-run the full Docker gate on the resulting clean candidate and record the hash.

## Refreshing this file

Replace the snapshot header, rewrite the evidenced and open tables from §8, and
delete anything that is no longer true. Do not append a history — the plan owns
history.
