# 50 — Status Snapshot

> **Type:** status · **Snapshot:** 2026-11-13 @ master `d151ae62` · **Base-path candidate:** `d0f05152` (closed) · **Extension candidate:** `d267ad44` (W3-07 exit, run 42548083086 green)
> Documentation records candidates; a documentation commit does not inherit candidate CI. Recheck live state before new work.

## Current scope

SG-00 through SG-15 base-path engineering and M6 AI materials are complete with local Mac/Docker Desktop boundaries. The contract-wallet extension (W3) is deterministically verified (W3-00–03/05–07). Its public Sepolia path is **partial**: W3-08 = PASS WITH FIXTURE_BLOCKED, and the extension as a whole is `TESTNET_BLOCKED` under plan §10.8 rule 5 — it cannot be called PASS WITH BOUNDARIES.

## Authoritative evidence

### Base path — closed

- One complete same-candidate Docker all: original outer exit 0, nine stages, M0–M8 and R01–R12 index complete.
- CI run 41343759379, attempt 3: 55/55 jobs success, no skip; acceptance, evidence and Publishable succeeded.
- Empty demo fixture volumes: warm startup 57.14s; real fullchain acceptance 77/77 per monolith and api-gateway mode.
- Plan §C.2 (former §8.86) owns commands, hashes, manifests, measurements and evidence locations.

### W3 extension

- Deterministic exit: candidate `d267ad44`, CI run 42548083086 attempt 2, 55/55, complete Docker all exit 0, media 77/77 (plan §C.3.1).
- Sepolia r7 (2026-11-11): 6 confirmed transactions (2 deploys, 2 mints, 2 transfers), holdings oracle, personal MetaMask SIWE login and signature rejection in monolith. Media fixture missing → playback BLOCKED; api-gateway mode and contract-wallet browser login not run; TN matrix 2/14 PASS, 12/14 BLOCKED, 0 FAIL (plan §C.3.2).
- **W3 code is not on master.** It lives on `d267ad44` = remote `sg/w3-freshness-20261107` (34 commits ahead). The local clone is shallow; fetch that branch before reading W3 code. Merging needs the owner's decision.
- Candidates `160f1804`, `4f68c8bf`, and the earlier W3-09 docs branch `sg/w3-delivery-docs-20261104` / `0e9cb338` are **not in the local repository**; cite their CI run ids, not the SHAs.

## Operational rules learned

- Keep the Mac lid open. A task-scoped caffeinate assertion prevents idle sleep, not guaranteed lid-close sleep.
- Initialize every exact gitlink recursively before demo startup. A container being up does not prove contract or business readiness.
- A cancelled CI shell and its local controller can diverge; preserve original failures, inspect live state, and never relabel residual work as a successful CI job.
- One dispatcher owns each frozen candidate. Do not reset retry budgets by changing an error classifier.
- Timing-only failures with an empty `git diff --stat <green> <current> -- pkg/ cmd/ contracts/ test/ config/` are environment, not product.
- A Windows workstation without Python/Docker can edit but cannot produce evidence; acceptance runs on the Mac Docker Desktop or CI.

## Boundaries and separately scoped work

- Mac fixed-budget/shared-host measurements do not establish native Linux or production capacity. Production deployment is unverified.
- Personal contribution claims, presentation, recording and applications remain human confirmation/work. No fabricated sign-off.
- SG-16/17 remain conditional and unstarted. ERC-4337, ERC-6492, EIP-7702 and Safe are out of scope; IPFS is not wired into upload.
- Every remaining Sepolia step (contract-wallet login, api-gateway mode, playback after fixture restore, transfer revocation timeline) needs the owner's wallet. Do not sign, broadcast or synthesise observations on the owner's behalf.

## Next action

1. Verify the evidence-transport completion (plan §C.3.3 ②③, in the working tree, not yet run) with `VERIFY_STAGE=unit make verify-docker` or CI, then form a candidate under the §10.5 authorization boundary.
2. Restore the Sepolia media fixture and run the remaining TN phases with the owner (plan §10.4 steps 3–5, both modes).
3. Finish W3-09: run `scripts/cd/verify_web3_extension.py` over real TN originals, clean round-specific resources, tag only after the extension closes.
