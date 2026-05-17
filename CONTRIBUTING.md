# Contributing to StreamGate

## Getting Started

1. Read `CLAUDE.md` for project conventions and architecture
2. Read `AGENTS.md` for package-level guidance
3. Check `DEPENDENCY_APPROVAL.md` before adding new dependencies

## Development Workflow

```bash
# 1. Start infrastructure
docker-compose up -d postgres redis

# 2. Copy and configure environment
cp .env.example .env
# Edit .env with your RPC endpoints

# 3. Build and run
make build-monolith
make run-monolith

# 4. Verify
curl http://localhost:8080/health
```

## Pull Request Process

1. Rebase onto the latest master
2. Run `make fmt && make lint && make test` — all must pass
3. One logical change per commit (squash WIP commits)
4. PR title follows `type(scope): description` (e.g. `feat(auth): add SIWE sign-in`)
5. PR description includes:
   - **What** changed
   - **Why** (problem being solved)
   - **Testing** done

## Before Merging

- [ ] Builds clean (`make build-monolith` or `make build-all`)
- [ ] Lint clean (`make lint`)
- [ ] Tests pass (`make test`)
- [ ] No unnecessary comments added
- [ ] Dependencies approved (if new)

## Code Review Principles

- **Plugin as thin wrapper**: Logic lives in `pkg/service/`, plugins only register routes
- **No comments unless necessary**: Code should be self-documenting
- **Monolith-first**: Develop in monolith mode, split to microservices only when scaling requires
- **Security first**: Never skip NFT ownership check, never expose segments without auth
