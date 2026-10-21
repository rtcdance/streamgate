# 10 — Architecture

> **Type:** architecture · **Last verified:** 2026-10-21 @ `df253ef`
> **Authority:** `docs/ARCHITECTURE.md` and the code. This file is the recall summary.

## Shape

Microkernel plus plugins. `pkg/core` owns the kernel, `GenericPlugin`, config, the
event bus and logging. `pkg/plugins/*` are thin wrappers; business logic lives in
`pkg/service/`.

```
cmd/monolith/streamgate        all 9 plugins in one process     :8080  (compose :18080)
cmd/microservices/<name>       9 standalone mains, own wiring    api-gateway :28080
pkg/plugins/<name>/plugin.go   lifecycle + route registration
pkg/service/<domain>/          business logic — the real layer
pkg/gateway/routes.go          THE route table; middleware order lives here
```

## Request path that matters

```
routes.go
  └─ streaming group: PlaybackTokenAuth → JWT → NFTGate
       ├─ segment / init / key : a valid capability is sufficient (no chain query)
       └─ manifest.m3u8        : re-resolve rules + re-verify holding (renewal point)
            └─ web3/nft verifier → RPC at safe/finalized
                 └─ streamingsvc delivers HLS
```

The asymmetry is deliberate: playback is cheap, renewal is strict. See D-007.

## Layering rules

1. Business logic never lives in a plugin or an HTTP handler. Plugins wire; services decide.
2. Every route registers in `pkg/gateway/routes.go`. A route added elsewhere is a bug.
3. The authorization subject (chain, contract, token id, minimum balance) comes from
   `content_gating_rules` in PostgreSQL — never from the request.
4. Microservice mains wire themselves. `core.RunMicroservice()` exists but has no callers.

## Topology

| Mode | Containers | API | Frontend |
|---|---|---|---|
| Monolith | 7 | `:18080` | `:18000` |
| Microservices | 9 services (api-gateway + 8 backends) + infra | `:28080` | `:18001` |
| Full chain | 17 (`docker-compose.fullchain.yml`, both side by side) | both | both |

Backing services: PostgreSQL 15, Redis 7, MinIO/S3, NATS, Consul (microservices only).
Async: `MemoryEventBus` in the monolith, `NATSEventBus` in microservices (built with
`-tags nats_integration`).

## Extension recipe

- **New domain**: `pkg/service/<domain>/` + `pkg/plugins/<name>/plugin.go` + a route in
  `routes.go` + a migration. Keep the plugin to wiring only.
- **New chain**: add it under `web3.chains` in `config/config.yaml` with an
  `rpc_urls` fallback list. A single endpoint is not acceptable.
- **New gate rule type**: extend `pkg/service/gating/` and `pkg/middleware/nft_gate.go`
  together. The gate must remain fail-closed.

## Deliberate absences

`pkg/web3/account_abstraction.go` and `pkg/web3/contract/registry.go` are complete
but never constructed. They are not dead code waiting to be wired up — they are out
of scope (see D-011).
