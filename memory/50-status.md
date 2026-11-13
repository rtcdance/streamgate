# 50 — Status Snapshot

> **Type:** status · **Snapshot:** 2026-11-13 · **Base-path candidate:** `d0f05152` (closed) · **Extension candidate:** `d267ad44` (W3-07 exit reached, run 42548083086 all green)
> Documentation records candidates; a documentation commit does not inherit candidate CI. Recheck live state before new work.

## Current scope

SG-00 through SG-15 base-path engineering and M6 AI materials are complete with local Mac/Docker Desktop boundaries. The contract-wallet extension (W3) reached its deterministic exit (W3-07) and its public Sepolia path is **partially evidenced** (W3-08 = PASS WITH FIXTURE_BLOCKED): six real owner-signed Sepolia transactions, a dual-RPC holdings oracle and a real MetaMask SIWE login/rejection are recorded; media fixture missing leaves 11 of 14 TN cells BLOCKED, with no FAIL items. W3-09 documentation close-out is in progress on the delivery line.

## Authoritative evidence

### Base path — closed

- One complete same-candidate Docker all: original outer exit 0, nine stages, M0–M8 and R01–R12 index complete.
- CI run 41343759379, attempt 3: 55/55 jobs success, no skip; acceptance, evidence and Publishable succeeded.
- Independent regeneration of the CI summary is byte-identical to CI: SHA256 `516e0f3a…dce88`.
- Empty demo fixture volumes: warm startup 57.14s; real fullchain acceptance 77/77 per monolith and api-gateway mode.
- Plan §8.86 owns commands, hashes, manifests, measurements and evidence locations.

### W3 extension — deterministic exit reached; public path partially evidenced

- Authoritative deterministic candidate `d267ad44`: default CI 42548083086 attempt 2, 55/55 success, no skip; complete nine-stage Docker all with original exit 0 (plan §C.3.1). Historical C4 `c38dc4bc` (run 41823377757) remains preserved.
- W3-08 r7 verdict (2026-11-11): PASS WITH FIXTURE_BLOCKED — six real Sepolia transactions (0.009153 ETH fees), dual-RPC holdings oracle (eoa-held PASS), real MetaMask SIWE login and one signature rejection; media fixture missing → 11/14 TN cells BLOCKED, no FAIL items (plan §C.3.2).
- W3-09 material (README, resumes, project block, talk script, 13-unit study guide) is merged onto local master via `sg/w3-09-closeout-local-20261113` (fast-forward to `50df5775`, tagged `v1.0.7` locally on 2026-11-13). Not pushed; remote CI paused by owner decision.

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

W3-09 close-out is complete and merged to master (pushed `35230311`, tag `v1.0.7`); owner reconfirmed on 2026-11-13 that the plan's overall goal is reached and GitHub CI stays paused — acceptance remains the local `make verify-docker` route. Remaining owner-only items: media-fixture candidate CI and the owner's wallet run (`media_fixture.py`, recovers the 11 BLOCKED TN cells), same-environment re-diagnosis of the public-network replay divergence. Do not re-run green CI/all to refresh this snapshot, do not sign for the owner, and do not treat a port probe as acceptance. Keep base candidate `d0f05152` frozen.
