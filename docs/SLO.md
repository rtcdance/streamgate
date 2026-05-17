# Service Level Objectives

This document defines the formal SLOs, error budget policy, and burn-rate alerting
for the StreamGate platform. It follows the Google SRE Workbook methodology.

## SLO Table

| Service | Indicator | Target | Window | Measurement |
|---------|-----------|--------|--------|-------------|
| API Gateway | Request Availability | **99.9%** | 30d rolling | `(1 - 5xx / total) * 100` |
| API Gateway | P99 Latency | **< 2000ms** | 5m sliding | `histogram_quantile(0.99)` |
| API Gateway | P95 Latency | **< 500ms** | 5m sliding | `histogram_quantile(0.95)` |
| NFT Verification | Request Success | **99.5%** | 30d rolling | `(1 - error / total) * 100` |
| RPC Providers | Aggregate Availability | **99.5%** | 30d rolling | failover rate monitoring |
| Cache Hit Rate | NFT Access Cache | **> 85%** | 5m sliding | cache hit / total queries |

## Error Budgets

| SLO | Monthly Budget | Budget Details |
|-----|---------------|----------------|
| API 99.9% | **0.1%** of requests | e.g., 43min downtime/month at 1000 rpm |
| NFT 99.5% | **0.5%** of requests | e.g., 3.6h of degraded service/month |

### Budget consumption triggers

| Burn Rate | Window | Action | Example |
|-----------|--------|--------|---------|
| >= 10x | 1h | **Page** oncall (critical) | Budget exhausted in ~3 days |
| >= 3x | 6h | **Ticket** (warning) | Budget exhausted in ~10 days |
| >= 1x | 24h | **Monitor** (notice) | On-track to exhaust in 30d |

### Deployment freeze

When error budget is exhausted:

```
Condition: slo:api:error_budget_remaining < 0
Action:    All production deploys frozen
Exception: Hotfixes for the issue consuming the budget
Reset:     Budget resets at the start of each calendar month
```

## Prometheus Rules

- `config/alerts/slo_recording_rules.yml` — precomputed SLO metrics
- `config/alerts/slo_burnrate_alerts.yml` — multi-window burn-rate alerts

## Monthly SLO Review

At the end of each month, the oncall team should:

1. Check SLO compliance for the past 30 days:
   ```promql
   slo:api:compliance_30d
   ```
2. Review the top 3 sources of error budget consumption (by route, by status code)
3. Decide if SLO targets need adjustment for the next month
4. Document the review in the team's monthly post-mortem

## Excluded from SLO

The following are intentionally excluded from availability calculations:

- `/health` and `/metrics` endpoints (internal monitoring — can return 503 during
  dependency outages without affecting user-facing availability)
- Rate-limited requests (429 responses are client errors, not service failures)
- Requests to undefined routes (404s from path scanning / bots)
- Pre-production environments (dev, test, staging)

## Related Documents

- `config/alerts/streamgate_alerts.yml` — existing threshold-based alerts
- `docs/advanced/OPERATIONAL_EXCELLENCE.md` — incident severity definitions
- `test/README.md` — performance targets as tested in CI
