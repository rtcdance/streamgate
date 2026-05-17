# StreamGate Production Runbook

> For incident severity definitions (SEV-1~4), see `docs/advanced/OPERATIONAL_EXCELLENCE.md`.

## Index

1. [PostgreSQL Recovery](#1-postgresql-recovery)
2. [RPC Provider Failover](#2-rpc-provider-failover)
3. [NFT Verification False Positive](#3-nft-verification-false-positive)
4. [Cache Stampede / Redis Down](#4-cache-stampede--redis-down)
5. [Pod Crash Loop / OOM](#5-pod-crash-loop--oom)
6. [High Latency Degradation](#6-high-latency-degradation)
7. [Post-mortem Template](#7-post-mortem-template)

---

## 1. PostgreSQL Recovery

### Symptoms
- `/health` returns 503 (database dependency unhealthy)
- Error logs: `pq: connection refused`, `pq: SSL not enabled`
- Prometheus alert: `ServiceUnhealthy` with "database" in description

### Sev
SEV-1 if both read and write paths fail. SEV-2 if only writes fail.

### Check

```bash
# 1. Check pod status and restarts
kubectl get pods -n production -l app=postgresql
kubectl describe pod -n production -l app=postgresql

# 2. Check logs
kubectl logs -n production -l app=postgresql --tail=100

# 3. Check disk space
kubectl exec -n production deploy/postgresql -- df -h /var/lib/postgresql/data

# 4. Check replication lag (if replica exists)
kubectl exec -n production deploy/postgresql -- psql -c "SELECT * FROM pg_stat_replication;"
```

### Recovery

**Case A: PostgreSQL process crashed**

```bash
# Restart the pod
kubectl rollout restart deploy/postgresql -n production

# Wait for readiness
kubectl rollout status deploy/postgresql -n production --timeout=120s

# Verify connectivity
kubectl exec -n production deploy/api-gateway -- wget -qO- http://localhost:8080/health
```

**Case B: Disk full**

```bash
# Find large tables
kubectl exec -n production deploy/postgresql -- psql streamgate -c "
  SELECT relname, pg_size_pretty(pg_total_relation_size(relid))
  FROM pg_catalog.pg_statio_user_tables
  ORDER BY pg_total_relation_size(relid) DESC LIMIT 10;"

# Clear old migration logs or temp data. If urgent:
kubectl exec -n production deploy/postgresql -- psql streamgate -c "VACUUM FULL;"
```

**Case C: Connection pool exhausted**

```bash
# Check active connections
kubectl exec -n production deploy/postgresql -- psql -c "
  SELECT state, count(*) FROM pg_stat_activity GROUP BY state;"

# Kill idle transactions older than 5 minutes
kubectl exec -n production deploy/postgresql -- psql -c "
  SELECT pg_terminate_backend(pid)
  FROM pg_stat_activity
  WHERE state = 'idle in transaction'
  AND age(now(), query_start) > interval '5 minutes';"
```

### Escalation
If disk corruption is suspected → restore from WAL archive backup (see `docs/operations/backup.md`). Estimated RTO: 30min with WAL, 4h from full backup.

---

## 2. RPC Provider Failover

### Symptoms
- Prometheus `SLORPCFailoverCritical` alert
- Error logs: `rpc timeout`, `connection refused` for RPC calls
- Users report: NFT verification fails, wallet login times out
- Grafana panel "RPC Failover Rate" shows spikes

### Sev
SEV-1 if all RPC providers are down (no NFT verification possible). SEV-2 if primary degrades but failover works.

### Check

```bash
# 1. Check current RPC scores (if debug endpoint is enabled)
curl -s http://localhost:8080/debug/web3-state | jq '.rpc_scores'

# 2. Check failover metrics
curl -s http://localhost:8080/metrics | grep streamgate_rpc_failover

# 3. Direct connectivity test
curl -s -X POST https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

### Recovery

**Case A: Primary RPC degraded (failover is working)**

```bash
# 1. Verify failover is operational — check metrics for successful fallback
#    The system auto-scores RPC endpoints. No immediate action needed.

# 2. If auto-failover is too slow, force next preferred endpoint in config:
#    Edit config.prod.yaml or K8s ConfigMap:
#      web3:
#        chains:
#          ethereum:
#            rpc_urls:
#              - https://eth-mainnet.g.alchemy.com/v2/${ALCHEMY_KEY}
#    Then: kubectl rollout restart deploy/api-gateway -n production
```

**Case B: All RPC providers down**

```bash
# 1. Check RPC provider status pages:
#    - Infura:   https://status.infura.io
#    - Alchemy:  https://status.alchemy.com
#    - QuickNode: https://status.quicknode.com

# 2. Switch to backup RPC provider (update config):
#    Edit config.prod.yaml:
#      web3:
#        chains:
#          ethereum:
#            rpc_urls:
#              - https://eth-mainnet.public.blastapi.io
#    Then restart API gateway.

# 3. If no RPC available, consider:
#    - Running a local node (geth --syncmode=snap)
#    - Accepting degraded mode (read NFT verification from cache only)
```

### Special notes (Web3)

- The system caches `NFTAccessEntry` for 60 seconds. During total RPC outage, the cache
  continues serving verification results for cached tokens without RPC calls.
- Cache entries are bound to `BlockNumber + BlockHash`. If the underlying chain advances
  while RPC is down, cached entries become stale.
- **Do not lower the block tag from `safe` to `latest`** to improve RPC responsiveness.
  This trades correctness for latency — see `pkg/web3/nft.go` for details.
- Cost: Each `eth_call` costs ~0.00001 ETH at current gas prices. A spike in failover
  alerts may indicate an expensive loop, not just a connectivity issue.

---

## 3. NFT Verification False Positive

### Symptoms
- User reports: "Someone accessed content they shouldn't have"
- Reorg detected log: `reorg detected at block N, hash changed`
- Prometheus: spike in NFT verification successes from a known suspicious address

### Sev
SEV-1 (security incident). Escalate immediately.

### Check

```bash
# 1. Check reorg detector logs
kubectl logs -n production deploy/api-gateway --tail=200 | grep reorg

# 2. Check NFT verification audit trail
kubectl logs -n production deploy/api-gateway | grep nft_verify

# 3. Verify user's current NFT ownership
curl -s http://localhost:8080/api/v1/nft/<user-address>/<contract-address>/<token-id>
```

### Investigation

```bash
# 4. Query the transaction that the reorg affected
#    Note the block number and hash from reorg detector logs
#    Compare with current canonical chain

# 5. Check if the affected blocks were "safe" or "latest" reads
#    If BlockTagSafe was used: nearly impossible to exploit via reorg
#    If latest was used: possible vector
```

### Mitigation

| Cause | Action |
|-------|--------|
| `latest` block tag in reading code | Fix to `BlockTagSafe` + deploy hotfix |
| Correct `safe` tag but RPC lag | Verify RPC supports finalized tags |
| Reorg deeper than 1 epoch | Extremely rare on Ethereum mainnet (~1/year) |

### Prevention

The multi-layer protection in place (see `pkg/middleware/nft_gate.go`):
1. `BlockTagSafe` reading — prevents 99% of reorg bypass
2. `BlockHash` cache binding — detects reorgs even within safe window
3. JWT session binding — verification result tied to session
4. Short TTL (60s) — minimizes stale cache window

---

## 4. Cache Stampede / Redis Down

### Symptoms
- Latency spikes on NFT verification endpoints
- DB connection pool exhaustion
- Prometheus: cache hit rate drops below 50%

### Sev
SEV-2 (degraded, no data loss)

### Check

```bash
# 1. Check Redis pod status
kubectl get pods -n production -l app=redis
kubectl logs -n production deploy/redis --tail=50

# 2. Check Redis memory and hit rate
kubectl exec -n production deploy/redis -- redis-cli INFO stats | grep hits
kubectl exec -n production deploy/redis -- redis-cli INFO memory

# 3. Check if cache keyspace is under pressure
kubectl exec -n production deploy/redis -- redis-cli INFO keyspace
```

### Recovery

```bash
# 1. Restart Redis (clears cache, may cause brief load spike)
kubectl rollout restart deploy/redis -n production

# 2. If restart is not possible, flush expired keys
kubectl exec -n production deploy/redis -- redis-cli MEMORY PURGE

# 3. If CPU/memory is maxed, scale Redis
kubectl scale deploy/redis -n production --replicas=3
```

### Application-level fallback

The cache module (`pkg/storage/cache.go`) uses singleflight pattern:
- Concurrent cache misses coalesce into one DB/RPC call
- If Redis is down, falls back to in-process LRU cache (limited by memory)
- If both caches fail, falls through to direct RPC/DB call with rate limiting

---

## 5. Pod Crash Loop / OOM

### Symptoms
- `kubectl get pods` shows CrashLoopBackOff
- Prometheus alert: `ServiceUnhealthy` + `kube_pod_container_status_restarts_total` rising

### Sev
SEV-2 (unless all replicas crash)

### Check

```bash
# 1. View crash reason
kubectl describe pod -n production <pod-name> | tail -20

# 2. Check previous pod logs
kubectl logs -n production <pod-name> --previous --tail=50

# 3. Check OOM
kubectl describe pod -n production <pod-name> | grep -A5 OOMKilled

# 4. Check resource usage trend
kubectl top pods -n production --sort-by=memory
```

### Recovery

**Case A: OOMKilled**

```bash
# Increase memory limit
kubectl set resources deploy/<name> -n production \
  --limits=memory=2Gi \
  --requests=memory=1Gi

# Or trigger GC earlier in Go:
# Set GOGC=50 as environment variable
```

**Case B: Panic in startup**

```bash
# 1. Read the panic trace from previous logs
kubectl logs -n production <pod-name> --previous | grep panic

# 2. Rollback to previous working version
kubectl rollout undo deploy/<name> -n production

# 3. Pin the working version
kubectl rollout history deploy/<name> -n production
```

**Case C: Liveness probe failing**

```bash
# Check probe configuration
kubectl describe deploy/<name> -n production | grep -A10 Liveness

# If too aggressive, update probe:
kubectl patch deploy/<name> -n production -p '{
  "spec":{"template":{"spec":{"containers":[{
    "name":"<container>",
    "livenessProbe":{"httpGet":{"path":"/health","port":8080},"initialDelaySeconds":15,"periodSeconds":30}
  }]}}}}'
```

---

## 6. High Latency Degradation

### Symptoms
- `SLOP99LatencyWarning` or `SLOP99LatencyCritical` alerts
- User reports slow page loads or video buffering
- Grafana: P99 latencies above 2000ms

### Sev
SEV-2 if P95 > 2s. SEV-3 if only P99 affected.

### Check

```bash
# 1. Check latency by endpoint
curl -s http://localhost:8080/metrics | grep streamgate_service_request_duration_ms

# 2. Check goroutine count (leak indicator)
curl -s http://localhost:8080/metrics | grep go_goroutines

# 3. Check GC pressure
curl -s http://localhost:8080/metrics | grep go_gc_duration_seconds

# 4. Check RPC call latency (most common cause in Web3)
curl -s http://localhost:8080/metrics | grep streamgate_rpc_latency_seconds
```

### Recovery

| Cause | Check | Fix |
|-------|-------|-----|
| RPC latency spike | `streamgate_rpc_latency_seconds` | failover to next RPC |
| DB query slowdown | `pg_stat_activity` | add index / tune query |
| GC thrashing | GC duration > 10% of CPU | reduce allocation rate, increase GOGC |
| Goroutine leak | goroutine count > 100k | fix missing channel close / context cancel |
| Connection pool full | active connections = max | increase pool or add replicas |

---

## 7. Post-mortem Template

```markdown
# Post-mortem: [Title]

**Date**: YYYY-MM-DD
**Incident ID**: INC-XXX
**Severity**: SEV-[1-4]
**Duration**: [start] – [end] ([duration])
**Detected by**: [alert / user report / monitoring]
**Responders**: [names]

## Summary

[1-2 sentence summary of what happened]

## Timeline

| Time (UTC) | Event |
|------------|-------|
| HH:MM | Alert fired: [alert name] |
| HH:MM | Responder acknowledged |
| HH:MM | Initial assessment: [finding] |
| HH:MM | Mitigation applied: [action] |
| HH:MM | Service restored |
| HH:MM | Monitoring confirmed healthy |

## Root Cause

[What was the underlying cause? 3-5 sentences max.]

## Impact

- **Users affected**: [count or percentage]
- **Requests failed**: [count]
- **Error budget consumed**: [% — see docs/SLO.md]
- **RPC costs incurred**: [estimated cost]

## Action Items

| Priority | Action | Owner | Ticket |
|----------|--------|-------|--------|
| P0 | [immediate fix] | @name | #123 |
| P1 | [prevention] | @name | #124 |
| P2 | [monitoring improvement] | @name | #125 |

## Lessons Learned

### What went well
- [ ]

### What went wrong
- [ ]

### What to improve
- [ ]

## Follow-up

- [ ] Post-mortem reviewed by team
- [ ] Action items triaged in sprint planning
- [ ] SLO adjustment considered (if recurring)
```
