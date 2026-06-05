# Monitoring

> **Date**: 2026-06-05
> **Source**: Code analysis of `pkg/monitoring/`, `pkg/gateway/`, scripts, and compose files
> **Status**: Single source of truth for monitoring and observability
> **Last verified against**: `master` branch (commit `96beacf`)

This document describes what monitoring infrastructure is currently deployed, what is missing, and how to verify system health. Master context: [ARCHITECTURE.md](../ARCHITECTURE.md#observability).

---

## 1. Current State: What Is Deployed

The full Prometheus + Grafana + Jaeger stack was removed during a simplification pass. What remains is the instrumentation surface without the collection infrastructure.

```mermaid
flowchart LR
    subgraph DEPLOYED["Deployed Now"]
        H[/health endpoint<br/>200 if process up]
        R[/ready endpoint<br/>200 if deps healthy]
        KL[/health/live<br/>K8s liveness]
        M[/metrics endpoint<br/>Prometheus text format]
        NM[nginx /metrics proxy<br/>added 2026-06]
        DD[make deploy-status<br/>docker ps wrapper]
        VS[verify-deploy.sh<br/>8-point health check]
        FA[fullchain-acceptance.sh<br/>11-step API test]
        H5[h5-demo<br/>manual acceptance console]
    end

    subgraph MISSING["Missing"]
        PR[Prometheus server<br/>scraping /metrics]
        GF[Grafana dashboards]
        JR[Jaeger collector<br/>for OpenTelemetry traces]
        AL[Alertmanager<br/>with configured routes]
        TG[Telegraf or node_exporter<br/>for host metrics]
    end

    M -.->|NOT scraped| PR
    KL -.->|not wired| K8S[Kubernetes<br/>(not deployed)]
    NM -.->|proxies| M
```

Every HTTP-exposed service has 4 standard endpoints. None of them feed into a monitoring pipeline.

---

## 2. Health Endpoints

| Endpoint | Route location | Behavior |
|---|---|---|
| `/health` | `pkg/gateway/routes.go:91-183` | Returns 200 if the process is running. No dependency checks. |
| `/ready` | Same file | Returns 200 if all dependencies (DB, Redis, MinIO, NATS, chain RPC) are reachable. Returns 503 otherwise. |
| `/health/live` | Same file | Kubernetes liveness probe. Returns 200 if the Goroutine pool is healthy. |
| `/metrics` | Same file | Prometheus text format via `promhttp.Handler()`. Exposes Go runtime metrics + custom RED metrics. |

The nginx configuration at `deploy/nginx/` proxies `/metrics` to prevent direct exposure of the Go metrics endpoint and to add basic auth if configured.

---

## 3. What /metrics Exposes

`pkg/monitoring/` defines custom Prometheus metrics:

| Metric | Type | Labels |
|---|---|---|
| `http_requests_total` | Counter | method, route, status |
| `service_request_duration_seconds` | Histogram | service_name |
| `nft_verification_requests_total` | Counter | chain_id, result |
| `transcoding_tasks_total` | Counter | profile, status |
| `transcoding_task_duration_seconds` | Histogram | profile |
| `cache_hits_total` / `cache_misses_total` | Counter | cache_name |

Plus standard Go runtime metrics (`go_memstats_*`, `go_goroutines`, etc.) and process metrics (`process_cpu_seconds_total`, `process_resident_memory_bytes`).

There is no scrape target configuration. The endpoint exists but nothing collects from it.

---

## 4. Manual Monitoring Tools

In the absence of automated monitoring, 3 scripts and the h5-demo serve as operational tools:

### make deploy-status

Wraps `docker ps --format` to show container health, uptime, and port mappings for all 17 fullchain containers. Quick sanity check after `docker compose up`.

### verify-deploy.sh (8-point health check)

`./scripts/verify-deploy.sh` checks:

1. Postgres is accepting connections (`pg_isready`)
2. Redis is responding (`PING`)
3. MinIO is reachable (`mc ls`)
4. NATS is connected
5. Monolith `/health` returns 200
6. api-gateway `/health` returns 200
7. Consul service list shows all 8 expected services
8. Anvil RPC responds to `eth_blockNumber`

Each check has a 5s timeout. Exits non-zero on first failure.

### fullchain-acceptance.sh (11-step API acceptance)

`./scripts/fullchain-acceptance.sh` runs a complete E2E test via the REST API:

1. GET /health -> 200
2. POST /auth/challenge -> 200, gets nonce
3. POST /auth/login with wallet signature -> 200, gets JWT
4. POST /nft/verify with wallet -> 200
5. GET /streaming/demo/manifest.m3u8 with JWT -> 200, gets .m3u8
6. GET /streaming/demo/segment/001.ts with playback token -> 200
7. POST /transcode/submit -> 200
8. GET /transcode/status/:id -> 200 (poll 3x with backoff)
9. POST /upload/init -> 200
10. POST /upload/chunk -> 200
11. POST /upload/:id/complete -> 200

Uses the acceptance JWT secret: `fullchain-acceptance-secret-2026`.

### h5-demo as operational console

The h5-demo SPA at `h5-demo/index.html` is the primary manual acceptance tool. Its 7-step flow corresponds to the 11-step acceptance script:

| Step | Description | Endpoint |
|---|---|---|
| Backend | Verify /health | GET /health |
| Wallet | Connect MetaMask | browser wallet |
| Login | Challenge + sign | POST /auth/challenge, /auth/login |
| NFT | Verify ownership | POST /nft/verify |
| Playback | Load HLS player | GET /streaming/:id/manifest.m3u8 |
| RPC | Check chain status | GET /web3/rpc-status |
| Transcode | Submit + monitor | POST /transcode/submit, GET /transcode/status/:id |

---

## 5. Observability Gaps and Recommendations

### Gaps

| Capability | Status | Impact |
|---|---|---|
| Prometheus scraping | Endpoint exists, no collector | No historical metrics |
| Grafana dashboards | Not deployed | No visual monitoring |
| Alerting | No Alertmanager | No pager/notification on failure |
| Trace collection | Jaeger not deployed | No distributed tracing in microservice mode |
| Log aggregation | Docker logs only | No centralized log search |
| Uptime monitoring | None | No SLO tracking |

### How to re-add Prometheus + Grafana

The compose file at `docker-compose.fullchain.yml` previously had Prometheus and Grafana services. They were removed but the config can be restored:

```yaml
# Add to docker-compose.fullchain.yml services section:
prometheus:
  image: prom/prometheus:v2.53.0
  container_name: sg-fc-prometheus
  volumes:
    - ./deploy/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml
  ports:
    - "19090:9090"
  command:
    - "--config.file=/etc/prometheus/prometheus.yml"
    - "--storage.tsdb.path=/prometheus"
  networks:
    - streamgate-fullchain

grafana:
  image: grafana/grafana:11.0.0
  container_name: sg-fc-grafana
  environment:
    - GF_SECURITY_ADMIN_PASSWORD=streamgate
    - GF_INSTALL_DASHBOARDS=true
  volumes:
    - ./deploy/grafana/dashboards:/etc/grafana/provisioning/dashboards
    - ./deploy/grafana/datasources:/etc/grafana/provisioning/datasources
  ports:
    - "13000:3000"
  networks:
    - streamgate-fullchain
```

Then restore the prometheus scrape config at `deploy/prometheus/prometheus.yml`:

```yaml
scrape_configs:
  - job_name: "streamgate-gateway"
    static_configs:
      - targets: ["api-gateway:8080"]
  - job_name: "streamgate-monolith"
    static_configs:
      - targets: ["monolith:8080"]
  - job_name: "streamgate-services"
    consul_sd_configs:
      - server: "consul:8500"
```

The dashboard JSON files were previously in `deploy/grafana/dashboards/` and can be restored from git history if needed.

### Jaeger re-addition

OpenTelemetry tracing is still wired in code (`pkg/gateway/gateway.go:91` calls `provideOTelTracing()`). The Jaeger collector and agent were removed from compose but the exporter code expects `JAEGER_AGENT_HOST` and `JAEGER_AGENT_PORT` env vars to be set.

---

## Cross-References

- Master architecture: [ARCHITECTURE.md](../ARCHITECTURE.md#observability)
- Metrics definitions: `pkg/monitoring/`
- Gateway middleware: `pkg/gateway/gateway.go:124-154`
- Verify script: `./scripts/verify-deploy.sh`
- Acceptance script: `./scripts/fullchain-acceptance.sh`
- Full-chain compose: `docker-compose.fullchain.yml`
- H5 demo acceptance: `h5-demo/index.html`
