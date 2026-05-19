# Contributing Guide for First-Time Contributors

Welcome! This guide helps you make your first contribution to StreamGate.

---

## Quick Start for Contributors

### 1. Set up your environment

```bash
git clone https://github.com/rtcdance/streamgate.git
cd streamgate
make build-monolith  # Builds the project, no external deps needed
```

### 2. Find something to work on

| Label | Description | Difficulty |
|-------|-------------|------------|
| `good first issue` | Well-scoped tasks with guidance | Beginner |
| `help wanted` | Open tasks needing contributors | Intermediate |
| `bug` | Defects that need fixing | Varies |

### 3. Make your change

- Read `CONTRIBUTING.md` for PR conventions
- One logical change per commit
- Run `make test` before submitting

### 4. Submit a PR

- Use the PR template (auto-filled when you create a PR)
- Link the issue you're fixing
- A maintainer will review within 48 hours

---

## Good First Issues Ideas

If no issues are tagged yet, here are tasks suitable for beginners:

### Documentation (no Go knowledge needed)

- Fix typos or broken links in README or docs/
- Add a missing API endpoint to the OpenAPI spec
- Translate a doc page (PR welcome for any language)

### Testing (basic Go knowledge)

- Add test cases for an existing function that lacks coverage
- Write a benchmark for a service endpoint
- Add edge case tests for wallet address validation

### Code (some Go experience)

- Add a new challenge to `examples/challenges/` (see existing ones as template)
- Add a Prometheus metric for a service that doesn't have one
- Improve error messages in `pkg/service/errors.go`

---

## Need Help?

- Open a GitHub Discussion with your question
- Tag your issue with `question` label
- Mention `@rtcdance` in your PR for faster review