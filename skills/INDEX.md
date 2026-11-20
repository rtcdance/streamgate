# StreamGate Skill Index

> Canonical location: `skills/`. All AI tools must read this file before a matching task. Tool-specific directories (`.claude/skills`, `.agents/skills`) are pointers only.
>
> **Companion memory:** every task also starts at `memory/README.md`. Skills hold
> procedures; memory holds the project's durable judgments. Both are canonical at
> the repository root.

## Available Skills

0. `dev-machine-acceptance`
   - **Default environment:** the local Mac + Docker Desktop.
   - Set up and verify the Docker dev/test stack (`docker-compose.fullchain.yml`, 17 containers).
   - Run StreamGate's Docker-first acceptance (`make verify-docker` stages, `fullchain-test`).
   - Check CPU/RAM/disk/toolchain/ports; Mac-only full acceptance uses `desktop-local`; native Linux remains a separate strict profile.

1. `remote-dev-machine` — **deprecated 2026-10-23**
   - The `prod-srs` Docker environment was purged; this is a historical record only.
   - Do not run builds, tests or acceptance on the remote host.

2. `ethereum`
   - Solidity, OpenZeppelin and Hardhat guidance for the contracts/ tree.

3. `golang-pro`
   - Idiomatic Go, concurrency, microservices, profiling and production patterns.

4. `golang-testing`
   - Table-driven tests, subtests, benchmarks, fuzzing and coverage.

5. `sepolia-wallet-round`
   - One complete Sepolia wallet round for an accepted candidate in a single owner session: fresh stack, six MetaMask transactions, media fixture, both modes, oracle per phase, final summary.
   - Tooling never signs or broadcasts; per-round limits ≤ 8 tx / 0.02 ETH.

## How To Apply

- Read `memory/README.md` and this file before a matching task; then pick the skills that match the task.
- For acceptance, read `dev-machine-acceptance`; for remote execution, read `remote-dev-machine`.
- Do not add authoritative skills under `.claude/`, `.agents/` or any other tool directory.
