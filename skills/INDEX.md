# StreamGate Skill Index

> Canonical location: `skills/`. All AI tools must read this file before a matching task. Tool-specific directories (`.claude/skills`, `.agents/skills`) are pointers only.

## Available Skills

0. `dev-machine-acceptance`
   - Verify a machine can run StreamGate's Docker-first acceptance (`make verify-docker` stages).
   - Check CPU/RAM/disk/toolchain/ports; stop SRS ABR when sharing the 118 dev box.

1. `remote-dev-machine`
   - Build, test and accept on the remote dev machine; the Mac is only an editor and SSH control plane.
   - Sync code, run remote gates, tunnel ports, fetch `.verify-artifacts` evidence.

2. `ethereum`
   - Solidity, OpenZeppelin and Hardhat guidance for the contracts/ tree.

3. `golang-pro`
   - Idiomatic Go, concurrency, microservices, profiling and production patterns.

4. `golang-testing`
   - Table-driven tests, subtests, benchmarks, fuzzing and coverage.

## How To Apply

- Pick the skills that match the task before coding.
- For acceptance, read `dev-machine-acceptance`; for remote execution, read `remote-dev-machine`.
- Do not add authoritative skills under `.claude/`, `.agents/` or any other tool directory.
