# StreamGate Operational Excellence Guide

> **Updated 2026-06-05** to reflect the simplified observability stack. The previous version (2025-01-28) was written when Prometheus + Grafana + Jaeger were deployed. **They have since been removed.** This file documents the current operational reality.
>
> **Last verified against**: `master` branch (commit `96beacf`)
> **Cross-references**: [operations/monitoring.md](../operations/monitoring.md), [MONITORING_INFRASTRUCTURE.md](../development/MONITORING_INFRASTRUCTURE.md), [DEPLOY.md](../../DEPLOY.md)

---

## What changed

The Jan 2025 version of this file described:

- SLOs measured by Prometheus queries
- Alerting via Grafana
- Incident response triggered by Prometheus alerts
- Capacity planning based on Prometheus time-series data

The Jun 2026 version reflects:

- SLOs verified manually via `./scripts/verify-deploy.sh` (8 checks)
- No automated alerting — operators check dashboards manually
- Incident response is mostly reactive (someone notices a problem)
- Capacity planning is rule-of-thumb (see [architecture/microservices.md](../architecture/microservices.md) for per-service resource limits)

---

## Current SLOs

| SLO | Target | How measured | Status |
|-----|--------|--------------|--------|
| **Availability** | 99.9% | `curl /health` returns 200 | Manually verified |
| **API latency P95** | < 200ms | `time curl /api/v1/auth/challenge` | Sample only |
| **Error rate** | < 1% | Auth challenge/login success ratio in logs | Manual log inspection |
| **Time to recovery (TTR)** | < 5 min | `make deploy-teardown && make deploy-monolith` | ✓ Achievable |
| **Time to detect (TTD)** | < 10 min | User complaint or h5-demo step failure | Reactive |

For production, these SLOs should be measured continuously via Prometheus + alerting. The recommendations section below has the minimum setup.

---

## Monitoring reality

### What we have

| Component | URL | Purpose |
|-----------|-----|---------|
| `/health` | `:18080/health`, `:28080/health` | Process liveness |
| `/ready` | `:18080/ready` | Dependency readiness |
| `/metrics` | `:18080/metrics` | Prometheus text (not scraped) |
| `make deploy-status` | (CLI) | `docker ps` filter |
| `verify-deploy.sh` | (CLI) | 8 automated checks |
| `fullchain-acceptance.sh` | (CLI) | 11 API checks |
| h5-demo | `:18000`/`:18001` | Manual 7-step flow |

### What we want (production)

| Component | Status | Implementation |
|-----------|--------|----------------|
| Prometheus | Not deployed | See [MONITORING_INFRASTRUCTURE.md](../development/MONITORING_INFRASTRUCTURE.md) for setup |
| Grafana | Not deployed | Same as above |
| Alertmanager | Not deployed | Same as above |
| Jaeger tracing | Not deployed | Same as above |
| Log aggregation | Not deployed | ELK or Loki + Promtail |
| Error tracking (Sentry) | Not deployed | SDK integration needed |

For the current scale (dev/demo/interview), the manual tools are sufficient. For production, the items above should be prioritized.

---

## Incident response

### Detection

Symptoms come from 4 sources:

1. **h5-demo failure** — one of the 7 steps turns red
2. **CLI check failure** — `make deploy-status` shows unhealthy containers, or `verify-deploy.sh` returns non-zero
3. **User complaint** — manual report of broken behavior
4. **Container restart loop** — visible in `docker logs`

### Triage

| Symptom | First check | Most likely cause |
|---------|-------------|-------------------|
| h5-demo step 1 (Backend) red | `curl /health` directly | Monolith or api-gateway not running |
| h5-demo step 4 (NFT) red | `curl /api/v1/web3/rpc-status` | Anvil down or RPC chain mismatch |
| h5-demo step 7 (Transcoding) stuck | `docker logs sg-fc-transcoder` | FFmpeg failure or NATS queue blocked |
| All 7 steps red | `make deploy-status` | Infrastructure (postgres/redis/minio/nats) down |
| `verify-deploy.sh` fails 1/8 | (specific check) | See below |
| Container restart loop | `docker logs <container>` | Crash on startup (config error, port conflict) |

### Mitigation

Most issues are resolved with one of these commands:

```bash
# Restart everything cleanly
make deploy-teardown && make deploy-monolith

# Restart just infrastructure
make deploy-teardown
docker compose -f docker-compose.fullchain.yml up -d postgres redis minio nats anvil
make deploy-monolith

# Restart a specific service
docker restart sg-fc-transcoder

# Check service logs
docker logs --tail=100 sg-fc-transcoder
```

### Recovery verification

After mitigation:

```bash
./scripts/verify-deploy.sh monolith        # Should be 8/8
./scripts/fullchain-acceptance.sh          # Should be 11/11
```

If both pass, the stack is healthy.

---

## Runbooks

### Anvil out of sync

**Symptom**: NFT verify fails with "chain mismatch" or returns wrong balance

**Diagnosis**:
```bash
curl -s -X POST http://localhost:18545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","id":1}'
# Should return {"result":"0x7a69"} (31337)
```

**Fix**:
```bash
docker restart sg-fc-anvil
# Wait 5s, then redeploy contracts:
make contracts-deploy-anvil
```

### PostgreSQL full

**Symptom**: Slow API responses, eventually 500 errors

**Diagnosis**:
```bash
docker exec sg-fc-postgres psql -U postgres -c "SELECT pg_database_size('streamgate');"
```

**Fix**:
```bash
docker exec sg-fc-postgres vacuumdb -U postgres --all --analyze
# Or full reset (loses data):
make deploy-teardown && make deploy-monolith
```

### MinIO bucket missing

**Symptom**: Upload fails with "bucket not found"

**Diagnosis**:
```bash
docker exec sg-fc-minio mc ls local/
```

**Fix**:
```bash
docker exec sg-fc-minio mc mb local/streamgate
docker exec sg-fc-minio mc anonymous set download local/streamgate
```

### NATS stream stuck

**Symptom**: Transcoding submit succeeds but no progress, tasks list empty

**Diagnosis**:
```bash
docker exec sg-fc-nats nats stream info TRANSCODING
```

**Fix**:
```bash
docker restart sg-fc-nats
# Stream is recreated automatically by the transcoder service on next start
```

### Microservice won't start

**Symptom**: `make deploy-microservices` shows unhealthy services

**Diagnosis**:
```bash
docker logs sg-fc-<service-name>
```

Common causes:
- Consul not ready (wait 30s, retry)
- Port conflict with monolith (run `make deploy-teardown` first)
- Missing image (run with `--build` flag)

**Fix**:
```bash
make deploy-teardown
make deploy-microservices  # if --build is set in Makefile, otherwise manually
```

### Both modes running, port conflict

**Symptom**: `verify-deploy.sh` says ports are taken

**Fix**:
```bash
make deploy-teardown
# Pick one mode:
make deploy-monolith
# OR
make deploy-microservices
```

---

## Capacity planning

### Monolith (1 process)

- **CPU**: 1-2 cores
- **Memory**: 2GB
- **Suitable for**: < 100 RPS, dev/demo/interview
- **Limit**: Single process can't scale beyond ~100 RPS sustained

### Microservices (9 binaries + 6 infra)

Per-service resources (recommended for ~500 RPS total):

| Service | CPU | Memory |
|---------|-----|--------|
| api-gateway | 1.0 | 512MB |
| auth | 0.3 | 256MB |
| cache | 0.2 | 512MB |
| metadata | 0.3 | 256MB |
| monitor | 0.2 | 256MB |
| streaming | 0.5 | 512MB |
| transcoder | 2.0 | 2GB |
| upload | 0.5 | 512MB |
| worker | 1.0 | 1GB |
| **Total** | **6.0** | **6.0GB** |

For higher RPS, scale api-gateway and streaming horizontally (HPA), keep transcoder and worker at 1 replica each (FFmpeg is CPU-bound, not parallelizable across replicas for a single task).

---

## Disaster recovery

### Volume backups

The fullchain compose uses 5 named volumes:

| Volume | Used by | Backup command |
|--------|---------|----------------|
| `pg_data` | postgres | `docker exec sg-fc-postgres pg_dump -U postgres streamgate > backup.sql` |
| `redis_data` | redis | `docker exec sg-fc-redis redis-cli BGSAVE` (snapshot) |
| `minio_data` | minio | `docker exec sg-fc-minio mc mirror local/ /backup/` |
| `nats_data` | nats | `docker exec sg-fc-nats nats stream backup TRANSCODING /backup/transcoding.tar` |
| `consul_data` | consul | `docker exec sg-fc-consul consul snapshot save /backup/snap.snap` |

### Full restore

```bash
make deploy-teardown

# Restore volumes
docker volume create streamgate_pg_data
docker run --rm -v streamgate_pg_data:/data -v $(pwd):/backup alpine \
  sh -c "cp /backup/pg_data/* /data/"

# Same for redis, minio, nats, consul

make deploy-monolith
./scripts/verify-deploy.sh monolith
```

### RPO / RTO targets

| Tier | RPO | RTO | Notes |
|------|-----|-----|-------|
| Critical (PostgreSQL) | 1 hour | 30 min | Hourly pg_dump, 30min to redeploy |
| Important (MinIO) | 24 hours | 1 hour | Daily mirror, 1hr to restore |
| Standard (Redis, NATS, Consul) | None (recreatable) | 5 min | Ephemeral state |

---

## Production readiness checklist

Before deploying to production, complete this list:

- [ ] Replace `STREAMGATE_JWT_SECRET` with a 32+ byte secret from a secret manager
- [ ] Set `APP_DEBUG=false` in environment
- [ ] Configure CORS to only allow your frontend domain
- [ ] Set up PostgreSQL backups (cron + pg_dump)
- [ ] Add Prometheus + Grafana (see [MONITORING_INFRASTRUCTURE.md](../development/MONITORING_INFRASTRUCTURE.md))
- [ ] Configure alert rules (P95 latency, error rate, disk usage)
- [ ] Set up log aggregation (Loki or ELK)
- [ ] Run `make validate-config` to check production config
- [ ] Load test with k6 or vegeta
- [ ] Document your incident response in a runbook specific to your environment
- [ ] Test the disaster recovery procedure (do a real restore drill)

---

## See also

- [ARCHITECTURE.md](../ARCHITECTURE.md) — system overview
- [operations/monitoring.md](../operations/monitoring.md) — current monitoring endpoints
- [development/MONITORING_INFRASTRUCTURE.md](../development/MONITORING_INFRASTRUCTURE.md) — full Prometheus + Grafana setup
- [DEPLOY.md](../../DEPLOY.md) — deployment guide
- [scripts/verify-deploy.sh](../../scripts/verify-deploy.sh) — 8-point automated check
- [scripts/fullchain-acceptance.sh](../../scripts/fullchain-acceptance.sh) — 11-step API acceptance
