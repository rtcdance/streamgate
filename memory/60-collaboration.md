# 60 — Working Agreements

> **Type:** collaboration · **Last verified:** 2026-10-21 @ `df253ef`

## Session start

1. Read this file, then `50-status.md`, then the memory file matching the task.
2. Run `git log --oneline -10` and `git status --short`. Never assume the snapshot is current.
3. If the task is acceptance, read the `dev-machine-acceptance` skill; if remote,
   `remote-dev-machine`.

## Owner's standards

These are standing instructions, not suggestions:

- **Evidence over assertion.** Acceptance comes from Docker or CI. A host build is a
  pre-check. Quote a run id or commit hash.
- **Root cause, not bypass.** No `--no-verify`, no deleted or skipped tests, no
  loosened assertion to get green.
- **One logical change per commit**, conventional format
  `type(scope): description`. Do not push unless asked; each push cancels CI.
- **No new dependency** without asking and recording it in `DEPENDENCY_APPROVAL.md`.
- **No process documents.** No review reports, handovers, status files or dated
  scratch markdown. Durable conclusions go to `TRANSITION_PLAN.md`.
  `memory/` is the sanctioned exception: it holds recall, not status reports —
  never delete it, and never use it as a place to dump progress.
- **Clean up artifacts** before finishing.
- **Never weaken a security invariant** to make a test or demo pass. If an invariant
  blocks the work, surface the conflict instead of working around it.

## How to change the system

- Business logic in `pkg/service/`; plugins and handlers wire.
- Routes only in `pkg/gateway/routes.go`.
- Security-sensitive code gets a comment explaining the threat, and a regression
  test that fails if the check is removed.
- Prefer the smallest change that satisfies the request. No speculative features.

## Communication

- Lead with the answer or the action, then the minimum supporting detail.
- Use `path:line` when pointing at code.
- State uncertainty explicitly; do not fill gaps with plausible-sounding claims.
- When a fact comes from memory, verify it before the user acts on it.

## Memory duties

- Architectural choice made → append to `30-decisions.md`.
- New hard rule or threat → `20-security-invariants.md`.
- Environment or gate changed → `40-operations.md`.
- Milestone closed → rewrite `50-status.md`.
- Memory contradicted by code → fix the memory in the same change.

## Definition of done

```
[ ] behavior implemented in pkg/service/ (not in a plugin or handler)
[ ] route registered in pkg/gateway/routes.go, inside the right middleware chain
[ ] security check has a regression test that fails when removed
[ ] make fmt && make lint
[ ] make test (document pre-existing failures; do not chase them)
[ ] Docker/CI evidence captured with the commit hash
[ ] artifacts cleaned; memory updated if a durable fact changed
```
