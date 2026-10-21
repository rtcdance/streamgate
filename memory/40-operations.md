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

## Remote execution

When the Mac is only an editor, use the `remote-dev-machine` skill: sync, run the
gates remotely, tunnel ports, fetch `.verify-artifacts`. Local Docker is the
fallback, not the default, on constrained machines.
