---
name: "sepolia-wallet-round"
description: "Run one complete Sepolia wallet round (plan §10.4/§10.6 TN matrix) for an accepted candidate in a single owner session: fresh stack, six MetaMask transactions, media fixture, EOA and contract-wallet login/playback/revocation in both modes, oracle per phase, final read-only summary. Use when filling the W3-TN matrix or preparing a new round such as r8."
---

# Skill: sepolia-wallet-round

One round = one candidate C, one controller A, one recipient B, **exactly six
transactions** (`deploy-nft`, `deploy-wallet`, `mint-a`, `transfer-a`, `mint-c`,
`transfer-c`, enforced by `transaction_oracle.py`). Playback can only be observed
while A, then C, still holds its NFT, so every round redeploys. Hard limits per
round: ≤ 8 broadcasts, ≤ 0.02 Sepolia ETH (the H5 panel and preflight enforce them).

The owner signs and broadcasts; tooling never does. No private key in files,
arguments, logs or CI. A PASS flag in any file is a declaration, never evidence.

## 0. Preconditions (automatable, before the owner sits down)

| Check | How |
|---|---|
| C has a fully green same-SHA CI run with downloadable payload | `gh run view <run> --json conclusion` = success; artifacts within 30-day retention |
| Mac runner online, Docker Desktop running, sleep disabled | runners API; `docker info`; `pmset -g` (`sleep 0` on AC) |
| Two qualified RPCs | `make verify-web3-testnet-preflight` (exit 0) |
| A balance ≥ 0.02 ETH + gas headroom, code = `0x` | preflight report |
| Media tool available | checkout containing `scripts/wallet-testnet/media_fixture.py` (from `sg/w3-media-fixture-20261113`); ffmpeg with libx264 or h264_videotoolbox |

**Fresh state per round.** The H5 panel keeps its ledger in browser storage
keyed by candidate + controller, per origin. Never clear an earlier round's
storage: give the new round its own `W3_STACK_DIR`, its own evidence directory
`.verify-artifacts/w3-extension/<C>/<round>/`, and different H5 ports
(`W3_MONOLITH_H5_PORT`, `W3_GATEWAY_H5_PORT`), so the earlier ledger stays intact.

## 1. Stack (no wallet)

Run `prepare_stack.py`, then `launch_stack.py` with the plan §10.7 arguments
(candidate, controller, recipient, repository, CI run, producer, CI root, build
identity, the round's stack directory). Keep each tool's report and raw exit file.

## 2. Owner session — order and the exact wallet actions

Open the H5 test panel (`<monolith H5 origin>/testnet/`). After **every**
transaction: wait for the receipt, press **只读对账** (read-only reconcile), and
press **导出交易账本** (export the ledger) to a new file for the current phase.
UNKNOWN → copy the hash from MetaMask activity; never resend.

| # | Owner (MetaMask) | Automation right after |
|---|---|---|
| 1 | Panel 1: deploy N (1 tx) | — |
| 2 | Panel 2: deploy C, owner = A (1 tx) | `deployment_manifest.py` records N/C from canonical receipts → `W3_DEPLOYMENT_MANIFEST` |
| 3 | Panel 3: mint to A (1 tx) | oracle `--phase eoa-held`; if a provider has not reached safe yet, first run the TN-07 pending capture (`--wait-safe-seconds 0`, new output) |
| 4 | Monolith H5: SIWE login as A (1 signature) | export `sessionStorage.streamgate_token` to a 0600 file → `media_fixture.py` (once; both modes share the database) → its `contentID` is `W3_CONTENT_ID` |
| 5 | Both modes: `observe_controls.py --phase eoa` (one SIWE login per mode; video plays, Loop on) | wait for both `ready.json` |
| 6 | Panel 4: A → B (1 tx) | oracle `--phase eoa-transferred`; collectors record revocation ≤ 70 s on HTTP and gRPC |
| 7 | Panel 5: mint to C (1 tx) | oracle `--phase contract-held` |
| 8 | Both modes: `--phase contract`, connect A and choose C as subject (one SIWE login per mode) | wait for both `ready.json` |
| 9 | Panel 6: C.execute → B (1 tx) | oracle `--phase contract-transferred` |
| 10 | Both modes: negative phase: reject one signature, switch to a wrong chain once | `assert_sepolia_browser.mjs --phase negative` per mode |

The owner's total is six transactions plus seven SIWE signatures, two rejections
and two network switches. Contract-wallet renewals are re-verified server-side and
need no new signature. While a phase waits for safe confirmation, run the other
mode's collector instead of idling.

## 3. After the session (no wallet)

1. `observe_controls.py merge` per mode (eoa + contract).
2. Write `extension.json` that only **selects** original paths (plan §10.7 field
   table): no status/passed fields, no absolute paths, no `..`, no symlinks.
3. `scripts/cd/verify_web3_extension.py --candidate --controller --recipient --evidence`.
4. Record the result in `TRANSITION_PLAN.md` §C: PASS / FAIL / BLOCKED per TN cell
   with the original exits. Missing or skipped cells stay BLOCKED; never fill them by hand.

## Stop rules

- The same failure twice → stop and diagnose; do not retry blindly.
- A confirmation that does not reach safe within 30 min → PENDING with raw evidence; never switch to `latest`.
- Budget or balance short → stop; do not raise the limits.
- The stack must stay on the candidate's original images; a product defect found here starts a new candidate, not a patched stack.
