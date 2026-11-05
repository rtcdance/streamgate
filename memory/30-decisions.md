# 30 — Decision Log

> **Type:** decisions · **Last verified:** 2026-10-21 @ `df253ef`
> Append-only. A changed mind is a new entry that supersedes the old one.

## D-001 — Monolith first, microservices only when scale demands

**Status:** accepted · **Evidence:** `cmd/monolith`, `cmd/microservices`

**Context:** Nine services is a lot of wiring for a system whose value is the
authorization path.

**Decision:** Ship one binary that loads every plugin; keep standalone mains as a
second topology over the same code.

**Why:** A demo must start in one command, and the split is only justified by
independent scaling — which this project cannot claim.

**Consequences:** Two wiring paths to keep in sync. Plugins must stay free of
cross-plugin assumptions. The monolith is the default acceptance target.

---

## D-002 — Plugins are thin wrappers; `pkg/service/` holds the logic

**Status:** accepted · **Evidence:** `pkg/plugins/*/plugin.go`, `pkg/service/`

**Context:** The microkernel makes it easy to put logic in a plugin, which would
make it unreachable from another topology.

**Decision:** A plugin registers lifecycle and routes; every decision lives in a service.

**Why:** One logic layer, two topologies, no duplication.

**Consequences:** New features start in `pkg/service/`. A plugin over ~20 lines is a smell.

---

## D-003 — The NFT gate fails closed

**Status:** accepted · **Evidence:** `pkg/middleware/nft_gate.go`

**Context:** A gate that opens on error is worse than no gate, because it looks safe.

**Decision:** No rules → `403`; resolver error → `5xx`; gate disabled → `503`.
`GatingMode` defaults to `closed`; `open` is an explicit operator opt-in.

**Why:** Availability must never be traded for authorization.

**Consequences:** Misconfiguration breaks playback loudly. That is intended.

---

## D-004 — Read ownership at `safe` / `finalized`, never `latest`

**Status:** accepted · **Evidence:** `pkg/web3/nft/verifier.go`

**Context:** A balance at `latest` can be undone by a reorg after access is granted.

**Decision:** Default to a finalized-class block tag; re-check the anchor before
renewing a playlist.

**Why:** Access must be granted on state that will not disappear.

**Consequences:** Slightly older state, slightly higher latency. Accepted.

---

## D-005 — Consume challenges with Redis Lua

**Status:** accepted · **Evidence:** `pkg/service/auth_wallet.go`

**Context:** Read-then-delete is a TOCTOU window; two concurrent requests could
both consume one challenge.

**Decision:** Consume atomically inside a Lua script.

**Why:** Replay protection must be atomic to be real.

**Consequences:** Redis is on the critical path for sign-in. It already was.

---

## D-006 — Playback authorization is a capability carried in the query string

**Status:** accepted · **Evidence:** `pkg/middleware/playback_token_auth.go`

**Context:** HLS.js cannot attach an `Authorization` header to segment requests.

**Decision:** Mint a short-lived, wallet-bound token with an explicit `token_type`
and audience; validate it cryptographically in middleware.

**Why:** The transport constraint is fixed by the browser, not by us.

**Consequences:** Tokens appear in URLs and logs — hence short TTL, wallet binding,
content binding, revocation and audience checks. Presence alone never authorizes.

---

## D-007 — The playlist is the renewal point; segments are cheap

**Status:** accepted · **Evidence:** `pkg/middleware/nft_gate.go`

**Context:** Querying the chain on every segment would make playback expensive and
fragile; never re-querying would let a transferred NFT keep playing.

**Decision:** A valid capability serves segments without a chain read. Every
`manifest.m3u8` request re-resolves rules and re-verifies the current holding
against fresh evidence.

**Why:** It puts the expensive check at the natural renewal boundary.

**Consequences:** Worst-case staleness is the capability TTL — 60s default and
hard ceiling — not one segment. The playlist refresh is the earliest point a
revoked right is noticed. Documented and accepted.

---

## D-008 — Authorization subject comes from the database, not the request

**Status:** accepted · **Evidence:** `resolveGatingRules`

**Context:** Accepting `contract` or `token_id` from the caller lets them prove
ownership of something cheaper than what they are watching.

**Decision:** Rules live in `content_gating_rules`; the request supplies only the
content id and the credential.

**Why:** It removes an entire class of confused-deputy bugs.

**Consequences:** Every gated content needs a rule row. The demo seed exists for this.

---

## D-009 — Docker and CI are the acceptance authority

**Status:** accepted · **Evidence:** `Makefile`, `.github/workflows/`

**Context:** Host builds hide tag differences, missing services and image wiring.

**Decision:** `make verify-docker` and `make fullchain-test` produce evidence; CI is
the final gate. A host build is a pre-check only.

**Why:** "Works on my machine" is not evidence for a reviewer.

**Consequences:** Local acceptance needs the two prebuilt Linux binaries and a
17-container stack. Slower, but defensible.

---

## D-010 — Contract bytecode must be reproducible

**Status:** accepted · **Evidence:** `contracts/foundry.toml`, `scripts/registry-manifest.sh`

**Context:** solc embeds absolute remappings in the metadata hash, so a host build
and a `/contracts` build produced different bytecode and the services refused to start.

**Decision:** `bytecode_hash = "none"`, and regenerate the manifest record after any
contract change.

**Why:** Deployment must not depend on the builder's filesystem path.

**Consequences:** Contract changes require a manifest refresh — a known startup
failure if forgotten.

---

## D-011 — Account abstraction stays out of scope

**Status:** accepted · **Evidence:** `pkg/web3/account_abstraction.go`, plan appendix D

**Context:** ERC-4337 types and entry-point validation exist but nothing constructs them.

**Decision:** Do not wire them up, and do not delete them to look tidy.

**Why:** It is not a proof point for this project and would dilute the security story.

**Consequences:** Reviewers may ask. The answer is "deliberately out of scope", not "unfinished".

---

## D-012 — One plan document, no process documents

**Status:** accepted · **Evidence:** `CLAUDE.md` → Documentation

**Context:** Status reports and handovers rot, multiply, and contradict the plan.

**Decision:** `TRANSITION_PLAN.md` is the only plan and evidence log. Reference
material lives under `docs/`. This `memory/` tree holds recall, not status.

**Why:** A single timeline is auditable; many are not.

**Consequences:** Finished work is recorded as evidence in the plan, not as a new file.

---

## D-013 — A page-scoped CUA transport observes, but never signs off

**Status:** accepted · **Evidence:** `scripts/h5-demo-acceptance/lib/wallet_cua_*.mjs`,
`scripts/cd/test_sepolia_browser_evidence.cjs`

**Context:** The Sepolia phase collectors need a browser, and the desktop tool
exposes page-scoped CUA instead of browser-wide CDP. Making the collectors accept
any transport would also make it easy to launder a synthetic observation into
public acceptance.

**Decision:** Add `W3_BROWSER_TRANSPORT=cua` as an explicit second transport behind
a single-use loopback relay with fixed operations and target-origin pinning. Both
transports feed one `WalletPhaseNetwork`. CUA reports are labelled
`CUA_SCOPED_READ_ONLY` and the phase validator rejects that adapter, so only the
existing MetaMask CDP adapter can close TN.

**Why:** Evidence must come from the owner's own browser. A transport that cannot
be distinguished from a scripted page must not be able to produce a pass.

**Consequences:** CUA runs are development observations. If they are ever to count,
they need a new frozen candidate and the same CI/all cycle; the frozen C4 result
stays valid and unchanged.

---

## Adding a decision

Copy the shape above. Keep **Context / Decision / Why / Consequences** — the *why*
is the part the code cannot tell you. Give it the next `D-xxx` id and link the
files that enforce it.
