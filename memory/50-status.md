# 50 — Status Snapshot

> **Type:** status · **Snapshot:** 2026-11-07 · **Base-path candidate:** `d0f05152` (closed) · **Extension candidate:** `685b6b97` (frozen C12, acceptance pending)
> Documentation records candidates; a documentation commit does not inherit candidate CI. Recheck live state before new work.

## Current scope

SG-00 through SG-15 base-path engineering and M6 AI materials are complete with local Mac/Docker Desktop boundaries. The contract-wallet extension (W3) is implemented and deterministically verified but its public Sepolia path is **not** accepted: no real MetaMask signature, no public transaction, no final W3-TN summary.

## Authoritative evidence

### Base path — closed

- One complete same-candidate Docker all: original outer exit 0, nine stages, M0–M8 and R01–R12 index complete.
- CI run 41343759379, attempt 3: 55/55 jobs success, no skip; acceptance, evidence and Publishable succeeded.
- Independent regeneration of the CI summary is byte-identical to CI: SHA256 `516e0f3a…dce88`.
- Empty demo fixture volumes: warm startup 57.14s; real fullchain acceptance 77/77 per monolith and api-gateway mode.
- Plan §8.86 owns commands, hashes, manifests, measurements and evidence locations.

### W3 extension — historical deterministic evidence; C12 pending

- Frozen candidate `c38dc4bc` (C4): default CI 41823377757 attempt 4, 55/55 success, no skip; complete nine-stage Docker all with original exit 0; originals independently preserved.
- C4 replaced 9054cecf/run41758926448 attempt 3 (media and upload failures preserved, not reused).
- Post-C4 development line `sg/w3-local-profile-20261105`: local Sepolia startup fixes at `37027d10`, page-scoped CUA observation transport at `80638738` (13 Node + 9 Python suites and workflow syntax, all original exit 0).
- D-013 is the historical C4 transport exclusion. Frozen C12 independently validates authenticated CUA traces, but observation still does not sign off for the owner — see D-016. C12 requires its own successful all/CI and the complete public matrix.
- W3-09 material (README, resumes, project block, talk script, 13-unit study guide) is committed on `sg/w3-delivery-docs-20261104` (`0e9cb338`), not on master: its README source entry points do not exist on master.

## Operational rules learned

- Keep the Mac lid open. A task-scoped caffeinate assertion prevents idle sleep, not guaranteed lid-close sleep.
- Initialize every exact gitlink recursively before demo startup. A container being up does not prove contract or business readiness.
- A cancelled CI shell and its local controller can diverge; preserve original failures, inspect live state, and never relabel residual work as a successful CI job.
- One dispatcher owns each frozen candidate. Do not reset retry budgets by changing an error classifier.
- A red→green pair with the original exit kept is worth more than a single green run; the red file must be preserved.

## Boundaries and separately scoped work

- Mac fixed-budget/shared-host measurements do not establish native Linux or production capacity. Testnet and production deployment are unverified.
- Personal contribution claims, presentation, recording and applications remain human confirmation/work. No fabricated sign-off.
- SG-16/17 remain conditional and unstarted. ERC-4337 is out of scope; IPFS is not wired into upload.
- W3 public path is blocked on the owner's wallet action and on tool-side browser availability (Chrome extension `Allow access to file URLs` off, CUA `apps/browsers` empty). Do not sign, broadcast or synthesise observations on the owner's behalf.

## Next action

Use the current candidate and evidence gates in `TRANSITION_PLAN.md` §10. C12 is `685b6b9780daf03c4f3272728d08d6b1b169c16c`, CI run `42319609406` attempt 1; its final acceptance has not yet been verified. Preserve the existing run and verify its original outer exit, upload receipt, complete producer tree and final CI summary before preparing the public stack. Reuse only the private RPC inputs, never historical candidate artifacts. Real owner-controlled signatures/transactions, both public modes and the full CW/TN matrix remain required. Source or CI success alone does not accept the extension. Keep base candidate `d0f05152` frozen.
