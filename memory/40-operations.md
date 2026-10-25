# 40 — Operations

> **Type:** operations · **Last verified:** 2026-10-21 @ `df253ef`
> Full command list: `CLAUDE.md`. This is the recall layer: what matters and what bites.

## Environment

`docker-compose.fullchain.yml` — 17 containers, all healthchecked. The monolith and
api-gateway quick images `COPY` prebuilt Linux binaries; the deploy script does not
build them.

```bash
GOOS=linux GOARCH=$(go env GOARCH) CGO_ENABLED=0 go build -o deploy/docker/streamgate-linux ./cmd/monolith/streamgate
GOOS=linux GOARCH=$(go env GOARCH) CGO_ENABLED=0 go build -o deploy/docker/api-gateway-linux ./cmd/microservices/api-gateway
```

| Host port | Service |
|---|---|
| 18080 / 19090 | monolith HTTP / gRPC |
| 28080 | api-gateway |
| 18000 / 18001 | h5-demo (monolith view / gateway view) |
| 18545 | Anvil — chain 31337, DemoNFT + ContentRegistry |
| 15432 / 16379 / 19000-19001 / 14222 / 28500 | PostgreSQL / Redis / MinIO / NATS / Consul |

## Verification ladder

| Rung | Command | What it proves |
|---|---|---|
| 0 | `go build ./cmd/monolith/streamgate` | it compiles; nothing more |
| 1 | `make test` | unit and integration behavior on the host |
| 2 | `VERIFY_STAGE=unit make verify-docker` | the unit gate inside the real image |
| 3 | `make verify-docker` | all nine stages; `all` requires candidate mode and a clean tree |
| 4 | `make fullchain-test` | the full acceptance script against the running stack |
| 5 | CI (`ci.yml`, `build.yml`) | authoritative acceptance |

Rungs 0–1 are pre-checks. Evidence is a CI run id or a commit hash, never a host run.

## Traps

| Symptom | Cause | Fix |
|---|---|---|
| `fullchain-deploy` says `deploy/docker/streamgate-linux: not found` | quick Dockerfiles `COPY` prebuilt binaries | build the two Linux binaries first |
| The gRPC checks fail with `exit status 1` | default `GRPC_TARGET=monolith:9090` is compose-internal | `GRPC_TARGET=localhost:19090 make fullchain-test` |
| Image build fails fetching github.com | Dockerfile defaults `GOPROXY=direct` | `VERIFY_GOPROXY=https://goproxy.cn,direct make verify-docker` |
| Build stalls or OOMs on an 8GB Docker VM | six or more parallel `go build -a` | build services serially, one `docker compose build` at a time |
| Startup: `runtime code hash mismatch` | solc embedded absolute remappings | keep `bytecode_hash = "none"`; regenerate the manifest |
| Microservice events error outside Docker | host `make build-all` lacks `-tags nats_integration` | run the service from its image |
| NFT verification returns a wrong answer | `latest` block tag | use `safe` / `finalized` |

## Evidence discipline

- A green host test is a pre-check. Do not present it as acceptance.
- Capture the commit hash alongside any run. History was rewritten twice with
  `git filter-repo` on 2026-10-21, so SHAs quoted in older docs may not resolve.
  Check with `git cat-file -e <sha>^{commit}`; prefer plan section numbers or tree
  hashes when citing evidence.
- A full `make verify-docker` run (`VERIFY_STAGE=all`) requires a clean tree;
  commit first, then verify. A single stage may run on a dirty tree.
- Delete generated artifacts when done: `bin/`, `out/`, `cache/`, `target/`,
  `coverage.out`, `deploy/docker/*-linux`, `scripts/h5-demo-acceptance/reports/`.
  Never delete `.workbuddy/`.

## Acceptance environment (2026-10-24)

The owner has only a Mac, no Linux server. Local Docker Desktop is the default
acceptance environment, including the full nine-stage DAG. The controller selects
`VERIFY_ENVIRONMENT_PROFILE=desktop-local` on macOS: >=8 VM CPUs, >=15 GiB usable
RAM (16 GiB configured), >=100 GiB free measured inside the Docker filesystem.
The native-linux profile keeps its amd64 / 32 GiB / dedicated-daemon checks.

- Keep linux/amd64 until the pinned ARM solc/toolchain is independently ported.
- Run stages and builds sequentially; shared Go cache is opt-in, tests use -count=1.
- Other acceptance runs are forbidden; background business containers are reported,
  never stopped without authorization. Performance manifests bind their IDs, the
  execution profile/platform and VM identity, and include a load snapshot.
- Equal background IDs do not prove equal load. Mac performance is a local,
  potentially emulated/shared-host observation, never production capacity proof.
- All/perf/release automatically freeze the source; same-SHA CI remains separate.
- No remote prod-srs acceptance; that environment was purged on 2026-10-23.

For same-SHA CI, the acceptance job can use this Mac as a self-hosted runner.
STREAMGATE_ACCEPTANCE_RUNNER selects its dedicated label;
STREAMGATE_ACCEPTANCE_PROFILE=desktop-local selects the preflight profile.
Installation/registration, repository settings, push and dispatch require explicit
authorization. Local results do not stand in for that job or registry receipts.

## Self-hosted runner uplink (2026-10-25)

The Mac's upload path measured **~111 KiB/s** (5 MiB to a public speed-test
endpoint; the runner's own `/proc/net/dev` agreed at ~113 KiB/s during a cache
save). Downloads are an order of magnitude faster. This asymmetry, not quota,
sets the cost of every CI design here:

- Never let a job upload a multi-hundred-MB cache. `actions/setup-go` defaults
  to caching a 1.2 GiB Go cache, so `cache: false` is deliberate; `cache-to` on
  the buildx image builds is likewise removed (a `mode=min` export took 5,823s
  and then failed on the one-hour cache token).
- Candidate images still have to travel to the Mac, so the transport stages are
  the slow part of a full DAG. Do not add uploads that nothing consumes.
- Expect a failed large upload to surface as a bare `EOF` from the registry; the
  transport helper retries a bounded number of times before it gives up.

## Same-SHA retry after a transient third-party flake (2026-10-25)

A failed job that never reached the candidate's own assertions is retried on the
same SHA, not by dispatching a new commit: `rerun-failed-jobs` re-runs the failed
jobs **and their dependents**, so the image/acceptance stages that were skipped
still execute, and the run keeps one SHA and one evidence chain. The transport
consumer resolves `g<run>-a<attempt>-<name>` from `max-attempt` downwards, so an
attempt-2 consumer still accepts attempt-1 artifacts.

Two things that waste time here:

- `gh run rerun <id> --failed` exits **0** even when the API refuses with
  `403 This workflow is already running`. Confirm with
  `gh api -X POST .../rerun-failed-jobs` (non-zero on refusal) or by checking
  that `run_attempt` incremented.
- `scripts/cd/verify_environment.sh` defaults to `profile=native-linux`, which
  demands a native amd64 Linux host with 32 GiB RAM. On this Mac it must be run
  as `VERIFY_ENVIRONMENT_PROFILE=desktop-local`; the CI job gets that from the
  `STREAMGATE_ACCEPTANCE_PROFILE` repository variable.

The runner container shares the DinD's network namespace
(`--network container:<dind>`), which is why job steps reach stack ports
published inside the DinD at `localhost` and why `services:` port mappings work.
