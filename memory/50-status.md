# 50 — Status Snapshot

> **Type:** status · **Snapshot:** 2026-11-05 · **Base-path candidate:** `d0f05152` (closed) · **Extension candidate:** `c38dc4bc` (deterministic, public path open)
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

### W3 extension — deterministic only

- Frozen candidate `c38dc4bc` (C4): default CI 41823377757 attempt 4, 55/55 success, no skip; complete nine-stage Docker all with original exit 0; originals independently preserved.
- C4 replaced 9054cecf/run41758926448 attempt 3 (media and upload failures preserved, not reused).
- Post-C4 development line `sg/w3-local-profile-20261105`: local Sepolia startup fixes at `37027d10`, page-scoped CUA observation transport at `80638738` (13 Node + 9 Python suites and workflow syntax, all original exit 0).
- CUA is a development observation channel only — see D-013. Adopting it needs a new candidate C5 with its own all/CI.
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

Candidate C5 is frozen at `80638738` and its default CI run 41966091151 is in flight; the next step is to prepare the Sepolia stack from that run's same-SHA producer payload once it succeeds. Dual-RPC qualification was re-verified read-only on 2026-11-05T04:01Z (exit 0, `NETWORK_READS_PASS`, controller still an EOA with 0.19176 ETH), so the network side is ready. What remains after the stack is up is the owner's part: enable `Allow access to file URLs` for the MetaMask Chrome extension, keep that page open, then sign the six-transaction fixture. Do not re-run green CI/all to refresh this snapshot, do not sign for the owner, and do not treat a port probe as acceptance. For the base path: keep `d0f05152` frozen.
