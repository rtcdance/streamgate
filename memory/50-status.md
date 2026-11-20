# 50 — Status Snapshot

> **Type:** status · **Snapshot:** 2026-11-18 @ master · **Base-path candidate:** `d0f05152` (closed) · **Extension candidate:** `d267ad44` (W3-07 exit reached, run 42548083086 all green)
> Documentation records candidates; a documentation commit does not inherit candidate CI. Recheck live state before new work.

## Current scope

SG-00 through SG-15 base-path engineering and M6 AI materials are complete with local Mac/Docker Desktop boundaries. The contract-wallet extension (W3) reached its deterministic exit (W3-07) and its public Sepolia path is **partially evidenced** (W3-08 = PASS WITH FIXTURE_BLOCKED): six real owner-signed Sepolia transactions, a dual-RPC holdings oracle and a real MetaMask SIWE login/rejection are recorded; media fixture missing leaves 11 of 14 TN cells BLOCKED, with no FAIL items. W3-09 documentation close-out is complete and the owner confirmed the plan goal is reached, but under plan §10.9 the extension as a whole stays `TESTNET_BLOCKED` and cannot be called PASS WITH BOUNDARIES.

## Authoritative evidence

### Base path — closed

- One complete same-candidate Docker all: original outer exit 0, nine stages, M0–M8 and R01–R12 index complete.
- CI run 41343759379, attempt 3: 55/55 jobs success, no skip; acceptance, evidence and Publishable succeeded.
- Independent regeneration of the CI summary is byte-identical to CI: SHA256 `516e0f3a…dce88`.
- Empty demo fixture volumes: warm startup 57.14s; real fullchain acceptance 77/77 per monolith and api-gateway mode.
- Plan §C.2 (former §8.86) owns commands, hashes, manifests, measurements and evidence locations.

### W3 extension — deterministic exit reached; public path partially evidenced

- Authoritative deterministic candidate `d267ad44`: default CI 42548083086 attempt 2, 55/55 success, no skip; complete nine-stage Docker all with original exit 0 (plan §C.3.1). Historical C4 `c38dc4bc` (run 41823377757) remains preserved. W3 code is on master.
- W3-08 r7 verdict (2026-11-11): PASS WITH FIXTURE_BLOCKED — six real Sepolia transactions (0.009153 ETH fees), dual-RPC holdings oracle (eoa-held PASS), real MetaMask SIWE login and one signature rejection; media fixture missing → 11/14 TN cells BLOCKED, no FAIL items; api-gateway mode and contract-wallet browser login not run on Sepolia (plan §C.3.2).
- W3-09 material (README, resumes, project block, talk script, 13-unit study guide) is on master and tag `v1.0.7` is pushed. On 2026-11-18 the docs from `sg/w3-09-transport-docs-20261113` (media-fixture procedure, r8 decision, `skills/sepolia-wallet-round`) were merged; that branch's CI code changes (`b92aedc`, `7c53907`) were not adopted because master already solves the same problems differently (plan §C.3.3).
- Candidates `160f1804`, `4f68c8bf` and object `f0025548` are not in the local repository; cite their CI run ids, not the SHAs.

## Operational rules learned

- Keep the Mac lid open. A task-scoped caffeinate assertion prevents idle sleep, not guaranteed lid-close sleep.
- Initialize every exact gitlink recursively before demo startup. A container being up does not prove contract or business readiness.
- A cancelled CI shell and its local controller can diverge; preserve original failures, inspect live state, and never relabel residual work as a successful CI job.
- One dispatcher owns each frozen candidate. Do not reset retry budgets by changing an error classifier.
- Timing-only failures with an empty `git diff --stat <green> <current> -- pkg/ cmd/ contracts/ test/ config/` are environment, not product.
- The Windows workstation (Win10 Pro 19045, 16 GB) has no WSL/Hyper-V features in its image, so no Docker; it can pre-check Python suites (user-scope Python 3.12) and Foundry tests (pinned v1.8.5 zip) only. Push goes through the SSH alias `github-streamgate`; GitHub CLI (winget, user scope) is logged in as `rtcdance`, so CI can be dispatched from here: `gh workflow run ci.yml -R rtcdance/streamgate --ref <branch>` (branch pushes do not trigger CI). Acceptance runs on the Mac Docker Desktop or CI.

## Boundaries and separately scoped work

- Mac fixed-budget/shared-host measurements do not establish native Linux or production capacity. Production deployment is unverified.
- Personal contribution claims, presentation, recording and applications remain human confirmation/work. No fabricated sign-off.
- SG-16/17 remain conditional and unstarted. ERC-4337, ERC-6492, EIP-7702 and Safe are out of scope; IPFS is not wired into upload.
- Every remaining Sepolia step (contract-wallet login, api-gateway mode, playback after fixture restore, transfer revocation timeline) needs the owner's wallet. Do not sign, broadcast or synthesise observations on the owner's behalf.

## Next action

Plan §10.10 owns the close-out. As of 2026-11-20: rounds r31/r32 passed public-network and public-chain on Sepolia; the remaining blocker is the controls checks, caused by an NFT-gate defect (a cached entry expiring during its reorg check returned 503 instead of a fresh read). The fix is on branch `sg/w3-c2-gate-freshness-20261120` (`9b0f294`), not yet compiled or merged. Order of work: verify it with `VERIFY_STAGE=unit make verify-docker` on the Mac, confirm the open stack questions in §10.10 (gateway RPC placeholder, transcode output naming, Docker egress), merge it as candidate C2, get the owner's authorization for one C2 CI run (the verifier's `ci` gate needs its capsule), then run r33 with the owner's wallet following `skills/sepolia-wallet-round`. A local MetaMask password was committed in `b429638` and is in public history; the owner rotated it on 2026-11-20. Never write credentials into tracked files. Do not sign for the owner, do not treat a port probe as acceptance, keep base candidate `d0f05152` frozen.
