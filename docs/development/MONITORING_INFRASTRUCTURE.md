# StreamGate Monitoring & Observability (Current State)

> **Updated 2026-06-05** to reflect the simplified observability stack. The previous version (2025-01-28) described Prometheus + Grafana + Jaeger + OpenTelemetry as if fully deployed. **They were removed during the 2026 simplification.** This file documents what's actually running now.
>
> **Last verified against**: `master` branch (commit `96beacf`)
> **Cross-references**: [operations/monitoring.md](../operations/monitoring.md), [ARCHITECTURE.md](../ARCHITECTURE.md), [DEPLOY.md](../../DEPLOY.md)

---

## What changed

Between January 2025 and June 2026, the observability stack was simplified:

| Component | Jan 2025 | Jun 2026 |
|-----------|----------|----------|
| Prometheus | Deployed (compose service) | **REMOVED from compose** |
| Grafana | Deployed (compose service) | **REMOVED from compose** |
| Jaeger | Deployed (compose service) | **REMOVED from compose** |
| OpenTelemetry collector | Deployed | **REMOVED** |
| `/metrics` endpoint (Prometheus text) | Exposed, scraped | Exposed, **NOT scraped** |
| `/health`, `/ready`, `/health/live` | Exposed | Exposed, still used |
| h5-demo as manual health check | Existed | Expanded (see [DASHBOARD_GUIDE.md](DASHBOARD_GUIDE.md)) |
| `verify-deploy.sh` 8-point check | Did not exist | Added 2026-06 |
| `fullchain-acceptance.sh` 11-step API check | Existed (different impl) | Rewritten 2026-06 |

The simplification was driven by two factors: (1) the full Prometheus + Grafana + Jaeger stack added 3+ containers of operational overhead for a project that is mostly used for dev/demo/interview, and (2) the `/metrics` endpoint was being scraped by nothing in CI/dev, so the data was going to `/dev/null` anyway.

---

## What exists right now

### 4 standard endpoints (every HTTP-exposed service)

| Endpoint | Returns | Use |
|----------|---------|-----|
| `GET /health` | 200 `{status: "healthy", uptime: "..."}` if process is up | Process liveness |
| `GET /ready` | 200 if all critical deps (PostgreSQL, Redis, MinIO, NATS) reachable; 503 otherwise | Dependency readiness |
| `GET /health/live` | 200 if process is up; matches `/health` semantics | Kubernetes liveness probe |
| `GET /metrics` | Prometheus text format (~290 lines of metrics) | **Defined but not scraped** |

**Code**: `pkg/gateway/routes.go:91-183` (infrastructure routes registration)

### What's in the `/metrics` output

The endpoint exposes standard Prometheus metrics for:

- HTTP request count and latency (per route, per status code)
- Cache hit/miss rate
- Transcoding task count by status
- NFT verification count and duration
- Web3 RPC call count and error rate
- Event bus publish/subscribe counters
- Process metrics (CPU, memory, goroutines)

But because no Prometheus instance is running, these metrics are read-only when you `curl :18080/metrics` — they're not aggregated, not stored, not visualized.

### Manual monitoring tools

The current observability workflow is:

1. **`make deploy-status`** — `docker ps --filter "name=sg-fc"` shows all 17 containers with their health status
2. **`./scripts/verify-deploy.sh [monolith|microservices]`** — 8 automated checks:
   - Docker daemon running
   - All containers healthy
   - `/health` returns 200
   - `/api/v1/health` returns 200 (detailed health)
   - `/metrics` returns 200 (Prometheus text)
   - h5-demo frontend returns 200 HTML
   - Anvil RPC reachable
   - Auth challenge roundtrip works
3. **`./scripts/fullchain-acceptance.sh [BASE_URL]`** — 11-step API acceptance:
   - Infrastructure (PostgreSQL, Redis, MinIO, NATS, Anvil)
   - Backend health
   - Auth challenge
   - NFT verify
   - Streaming manifest with auth
   - Transcode submit/status/tasks/profiles
   - Metrics endpoint
4. **h5-demo 7-step manual flow** — see [DASHBOARD_GUIDE.md](DASHBOARD_GUIDE.md)

---

## What should exist (recommended for production)

A real production deployment should add back the Prometheus + Grafana stack. Here's the minimum viable configuration:

```yaml
# Add to docker-compose.fullchain.yml
prometheus:
  image: prom/prometheus:v2.50.0
  container_name: sg-fc-prometheus
  ports:
    - "19090:9090"
  volumes:
    - ./deploy/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    - prometheus-data:/prometheus
  command:
    - '--config.file=/etc/prometheus/prometheus.yml'
    - '--storage.tsdb.path=/prometheus'
  healthcheck:
    test: ["CMD", "wget", "-qO-", "http://localhost:9090/-/healthy"]
    interval: 30s
    timeout: 5s
    retries: 3
  depends_on:
    - consul

grafana:
  image: grafana/grafana:10.4.0
  container_name: sg-fc-grafana
  ports:
    - "13000:3000"
  environment:
    - GF_SECURITY_ADMIN_PASSWORD=admin
    - GF_USERS_ALLOW_SIGN_UP=false
  volumes:
    - grafana-data:/var/lib/grafana
    - ./deploy/grafana/dashboards:/etc/grafana/provisioning/dashboards:ro
    - ./deploy/grafana/datasources:/etc/grafana/provisioning/datasources:ro
  depends_on:
    - prometheus

volumes:
  prometheus-data:
```

And a minimal `prometheus.yml`:

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'streamgate-monolith'
    static_configs:
      - targets: ['monolith:8080']
    metrics_path: /metrics

  - job_name: 'streamgate-api-gateway'
    static_configs:
      - targets: ['api-gateway:8080']
    metrics_path: /metrics

  - job_name: 'streamgate-microservices'
    static_configs:
      - targets: ['auth:8080', 'cache:8080', 'metadata:8080', 'monitor:8080',
                  'streaming:8080', 'transcoder:8080', 'upload:8080', 'worker:8080']
    metrics_path: /metrics
```

(Note: the 8 microservices would each need to be configured to expose `/metrics` on a port — currently only the gateway does.)

---

## Re-adding tracing (Jaeger)

For distributed tracing, add to compose:

```yaml
jaeger:
  image: jaegertracing/all-in-one:1.55
  container_name: sg-fc-jaeger
  ports:
    - "16686:16686"  # Jaeger UI
    - "14268:14268"  # Jaeger collector (HTTP)
  environment:
    - COLLECTOR_OTLP_ENABLED=true
```

And set OpenTelemetry environment variables on every service:

```yaml
environment:
  - OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4317
  - OTEL_SERVICE_NAME=streamgate-monolith
```

---

## Current observability gaps

| Need | Current state | Gap |
|------|---------------|-----|
| Process health | 4 endpoints | ✓ OK |
| Dependency health | `/ready` endpoint | ✓ OK |
| Time-series metrics | `/metrics` exists, not scraped | **Gap** — no aggregation |
| Dashboards | h5-demo (manual), no charts | **Gap** |
| Alerting | None | **Gap** |
| Distributed tracing | Not implemented | **Gap** |
| Log aggregation | `docker logs <container>` | Manual, no central view |
| Error tracking | None | **Gap** |

For interview/demo purposes, the current state is sufficient. For production, items in **Gap** should be addressed.

---

## See also

- [operations/monitoring.md](../operations/monitoring.md) — detailed current monitoring endpoints
- [ARCHITECTURE.md](../ARCHITECTURE.md) — system overview
- [advanced/OPERATIONAL_EXCELLENCE.md](../advanced/OPERATIONAL_EXCELLENCE.md) — incident response, runbooks
- [DEPLOY.md](../../DEPLOY.md) — deployment guide
- [scripts/verify-deploy.sh](../../scripts/verify-deploy.sh) — 8-point automated check
- [scripts/fullchain-acceptance.sh](../../scripts/fullchain-acceptance.sh) — 11-step API acceptance
