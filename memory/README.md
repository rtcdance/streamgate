# StreamGate Long-Term Memory

> **Status:** active · **Created:** 2026-10-21 · **Last verified:** 2026-10-21 @ `df253ef` · **Owner:** project owner
> **Scope:** durable recall for humans and AI agents working on this repository.

## What this is

`memory/` answers one question: *what should I already know before I touch this code?*
It records judgments that are expensive to re-derive — the project's purpose, the
non-negotiable invariants, decisions and their rationale, the operating contract,
and a dated snapshot of where the work stands.

Together with `skills/`, it is a mandatory entry point for every AI tool: memory
holds judgments, skills hold procedures. It is **not** documentation and **not** a
plan. Those already have owners:

| Question | Authoritative source |
|---|---|
| How do I build, run, test? | `CLAUDE.md` |
| What is the architecture? | `docs/ARCHITECTURE.md` + `docs/architecture/` |
| What is planned, and what is the evidence? | `TRANSITION_PLAN.md` |
| What is the API? | `docs/api/openapi.yaml` |
| What does the code actually do? | the code |
| Which procedure should I follow? | `skills/INDEX.md` |

If a memory file disagrees with the code, `CLAUDE.md`, or `TRANSITION_PLAN.md` §8,
those win. Fix the memory in the same change.

## Index

| File | Holds | Read before |
|---|---|---|
| [00-charter.md](00-charter.md) | purpose, audience, success criteria, non-goals | scoping any feature |
| [10-architecture.md](10-architecture.md) | system shape, request path, layering, extension points | adding a route, service or plugin |
| [20-security-invariants.md](20-security-invariants.md) | hard security rules, gate matrix, threat model | touching auth, NFT, gating or streaming |
| [30-decisions.md](30-decisions.md) | decision log with rationale | proposing an alternative design |
| [40-operations.md](40-operations.md) | environments, ports, verification ladder, traps | running or accepting anything |
| [50-status.md](50-status.md) | dated snapshot of done / open work | planning the next task |
| [60-collaboration.md](60-collaboration.md) | working agreements for agents and humans | starting a session |
| [70-glossary.md](70-glossary.md) | project IDs, terms, core file map | reading the plan or the code |

## Protocol

1. **Read** `60-collaboration.md`, `skills/INDEX.md`, plus the files matching your task before acting.
2. **Verify** any fact you are about to rely on. Each file carries `Last verified`
   and a commit. Status and line numbers decay fastest.
3. **Write** only durable conclusions:
   - a new architectural choice → append to `30-decisions.md`;
   - a new hard rule → `20-security-invariants.md`;
   - a changed environment or gate → `40-operations.md`;
   - a completed milestone → refresh `50-status.md` (replace, do not append history).
4. **Never** put secrets, private keys, RPC credentials or per-run addresses here.
5. **Never** turn this into a work log. No daily notes, handovers or review reports.
   Durable plan evidence still belongs in `TRANSITION_PLAN.md`.
6. **Superseding** a decision: change its `Status` to `superseded by D-xxx`; do not
   delete it. The trail is the value.

## Freshness

`50-status.md` is a snapshot, not a live feed. Before acting on it:

```bash
git log --oneline -10
git status --short
```

Then re-read the relevant `TRANSITION_PLAN.md` §8 subsection. The plan's checkbox
state has historically lagged its own evidence sections — trust §8, not the box.
