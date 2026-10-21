# StreamGate Documentation

Entry point for project documentation. `TRANSITION_PLAN.md` (repo root) is the **only** plan document; this tree holds reference material.

| Area | Location | Contents |
|------|----------|----------|
| Architecture | [ARCHITECTURE.md](ARCHITECTURE.md) · [architecture/](architecture/) | C4 overview, microkernel, microservices, data flow, communication |
| API | [api/openapi.yaml](api/openapi.yaml) | OpenAPI 3.0 spec — single source of truth, served at `/docs/openapi.yaml` |
| Deployment | [../DEPLOY.md](../DEPLOY.md) | 5-minute stack, port map, teardown, deploy FAQ |
| Operations | [operations/](operations/) | Incident runbook, SLOs, monitoring, troubleshooting, operational excellence |
| Development | [development/](development/) | CI/CD, coverage, lint, testing, dashboard, monitoring, implementation, Web3 integration |
| Web3 | [web3/](web3/) | Setup, patterns, best practices, testing, troubleshooting, FAQ |
| Job material | [resume/](resume/) | Resumes, interview scripts, project narrative |
| Archive | [archive/](archive/) | Frozen / superseded / speculative docs — not maintained |

## Rules

- New documents must fit one folder above; do not add files at `docs/` root (only `README.md` and `ARCHITECTURE.md` live there).
- No process documents — no status reports, handovers, review notes or dated scratch files. Durable conclusions go to `TRANSITION_PLAN.md`.
- When moving or renaming a document, update inbound links: `grep -rn "<path>" --include="*.md" .`.
