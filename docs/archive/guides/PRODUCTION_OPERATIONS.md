# StreamGate Production Operations Guide

**Version**: 1.0.0  
**Last Updated**: 2025-01-29  
**Status**: ✅ Complete

## Table of Contents

1. [Pre-Production Checklist](#pre-production-checklist)
2. [Deployment](#deployment)
3. [Monitoring](#monitoring)
4. [Incident Response](#incident-response)
5. [Maintenance](#maintenance)
6. [Scaling](#scaling)
7. [Disaster Recovery](#disaster-recovery)

## Pre-Production Checklist

### Infrastructure

- [ ] Kubernetes cluster configured (3+ nodes)
- [ ] PostgreSQL database setup (replicated)
- [ ] Redis cluster setup (replicated)
- [ ] MinIO/S3 storage configured
- [ ] NATS cluster setup (3+ nodes)
- [ ] Consul cluster setup (3+ nodes)
- [ ] Load balancer configured
- [ ] CDN configured
- [ ] DNS configured
- [ ] SSL/TLS certificates installed

### Security

- [ ] Firewall rules configured
- [ ] Network policies configured
- [ ] RBAC configured
- [ ] Secrets management setup
- [ ] Encryption at rest enabled
- [ ] Encryption in transit enabled
- [ ] API rate limiting configured
- [ ] DDoS protection enabled
- [ ] WAF configured
- [ ] Security audit completed

### Monitoring

- [ ] Prometheus configured
- [ ] Grafana dashboards created
- [ ] Alerting rules configured
- [ ] Log aggregation setup
- [ ] Distributed tracing setup
- [ ] Health checks configured
- [ ] Metrics collection verified
- [ ] Alert channels configured
- [ ] On-call rotation setup
- [ ] Runbooks created

### Testing

- [ ] Load testing completed
- [ ] Failover testing completed
- [ ] Backup/restore testing completed
- [ ] Security testing completed
- [ ] Performance testing completed
- [ ] Chaos engineering testing completed
- [ ] All tests passing
- [ ] Code review completed
- [ ] Security review completed
- [ ] Performance review completed

### Documentation

- [ ] Deployment guide completed
- [ ] Operations guide completed
- [ ] Troubleshooting guide completed
- [ ] Runbooks created
- [ ] Architecture documented
- [ ] API documented
- [ ] Configuration documented
- [ ] Backup procedures documented
- [ ] Disaster recovery plan documented
- [ ] Escalation procedures documented

## Deployment

### Pre-Deployment

```bash
# 1. Verify all checks passed
./scripts/pre-deployment-check.sh

# 2. Create backup
./scripts/backup-database.sh
./scripts/backup-storage.sh

# 3. Verify backup integrity
./scripts/verify-backup.sh

# 4. Notify team
# Send deployment notification to Slack/Teams

# 5. Start maintenance window
# Update status page
```

### Deployment Steps

```bash
# 1. Build Docker images
make docker-build

# 2. Push to registry
make docker-push

# 3. Update Kubernetes manifests
kubectl set image deployment/api-gateway \
  api-gateway=registry/streamgate:v1.0.0 \
  -n streamgate

# 4. Monitor rollout
kubectl rollout status deployment/api-gateway -n streamgate

# 5. Verify deployment
./scripts/verify-deployment.sh

# 6. Run smoke tests
./scripts/smoke-tests.sh

# 7. End maintenance window
# Update status page
```

### Rollback Procedure

```bash
# If deployment fails:

# 1. Immediate rollback
kubectl rollout undo deployment/api-gateway -n streamgate

# 2. Verify rollback
kubectl rollout status deployment/api-gateway -n streamgate

# 3. Investigate issue
kubectl logs deployment/api-gateway -n streamgate

# 4. Notify team
# Send incident notification

# 5. Post-mortem
# Schedule post-mortem meeting
```

## Monitoring

### Key Metrics

#### Application Metrics
- Request rate (req/s)
- Response time (p50, p95, p99)
- Error rate (%)
- Cache hit rate (%)
- Database query time (ms)

#### Infrastructure Metrics
- CPU usage (%)
- Memory usage (%)
- Disk usage (%)
- Network I/O (Mbps)
- Disk I/O (IOPS)

#### Business Metrics
- Active users
- Content uploads (per day)
- Streaming sessions
- Revenue (if applicable)

### Alerting Rules

```yaml
# Critical Alerts (page on-call)
- API error rate > 5%
- API response time p99 > 5s
- Database connection pool exhausted
- Redis memory usage > 90%
- Disk usage > 90%
- Service down (health check failed)

# Warning Alerts (notify team)
- API error rate > 1%
- API response time p99 > 1s
- Cache hit rate < 70%
- Database slow queries > 10
- Memory usage > 80%
- Disk usage > 80%
```

### Dashboard Setup

#### Overview Dashboard
- Service status
- Request rate
- Error rate
- Response time
- Active users

#### Performance Dashboard
- CPU usage
- Memory usage
- Disk I/O
- Network I/O
- Database performance

#### Business Dashboard
- Content uploads
- Streaming sessions
- User growth
- Revenue

## Incident Response

### Incident Severity

| Severity | Impact | Response Time | Example |
|----------|--------|---------------|---------|
| P1 | Critical | 15 minutes | Service down |
| P2 | High | 1 hour | Degraded performance |
| P3 | Medium | 4 hours | Minor feature broken |
| P4 | Low | 24 hours | Documentation issue |

### Incident Response Process

```
1. Detection
   └─> Alert triggered
       └─> On-call notified

2. Triage
   └─> Assess severity
       └─> Assign incident commander

3. Investigation
   └─> Gather information
       └─> Identify root cause

4. Mitigation
   └─> Implement fix
       └─> Verify resolution

5. Communication
   └─> Update status page
       └─> Notify stakeholders

6. Post-Mortem
   └─> Document incident
       └─> Identify improvements
```

### Common Issues and Solutions

#### High Error Rate

```bash
# 1. Check logs
kubectl logs -f deployment/api-gateway -n streamgate

# 2. Check metrics
curl http://prometheus:9090/api/v1/query?query=rate(http_requests_total{status=~"5.."}[5m])

# 3. Check database
psql streamgate -c "SELECT * FROM pg_stat_statements ORDER BY mean_time DESC LIMIT 10;"

# 4. Check cache
redis-cli INFO stats

# 5. Restart service if needed
kubectl rollout restart deployment/api-gateway -n streamgate
```

#### High Latency

```bash
# 1. Check database performance
psql streamgate -c "EXPLAIN ANALYZE SELECT ..."

# 2. Check cache hit rate
curl http://prometheus:9090/api/v1/query?query=cache_hit_rate

# 3. Check network latency
ping <service-host>

# 4. Check resource usage
kubectl top nodes
kubectl top pods -n streamgate

# 5. Scale up if needed
kubectl scale deployment api-gateway --replicas=5 -n streamgate
```

#### Database Connection Issues

```bash
# 1. Check connection pool
psql streamgate -c "SELECT count(*) FROM pg_stat_activity;"

# 2. Check for long-running queries
psql streamgate -c "SELECT * FROM pg_stat_activity WHERE state != 'idle';"

# 3. Kill long-running queries if needed
psql streamgate -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE duration > interval '1 hour';"

# 4. Restart database if needed
kubectl rollout restart statefulset/postgres -n streamgate
```

## Maintenance

### Regular Maintenance Tasks

#### Daily
- Monitor dashboards
- Check alerts
- Review logs
- Verify backups

#### Weekly
- Review performance metrics
- Check disk usage
- Verify security logs
- Update documentation

#### Monthly
- Database maintenance (VACUUM, ANALYZE)
- Log rotation
- Certificate renewal check
- Dependency updates

#### Quarterly
- Security audit
- Performance review
- Capacity planning
- Disaster recovery drill

### Database Maintenance

```bash
# Vacuum and analyze
psql streamgate -c "VACUUM ANALYZE;"

# Reindex tables
psql streamgate -c "REINDEX DATABASE streamgate;"

# Check table sizes
psql streamgate -c "SELECT schemaname, tablename, pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) FROM pg_tables ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;"

# Check index usage
psql streamgate -c "SELECT schemaname, tablename, indexname, idx_scan FROM pg_stat_user_indexes ORDER BY idx_scan DESC;"
```

### Log Rotation

```bash
# Configure logrotate
cat > /etc/logrotate.d/streamgate << EOF
/var/log/streamgate/*.log {
    daily
    rotate 30
    compress
    delaycompress
    notifempty
    create 0640 streamgate streamgate
    sharedscripts
    postrotate
        systemctl reload streamgate > /dev/null 2>&1 || true
    endscript
}
EOF
```

## Scaling

### Horizontal Scaling

```bash
# Scale API Gateway
kubectl scale deployment api-gateway --replicas=5 -n streamgate

# Scale Transcoder
kubectl scale deployment transcoder --replicas=3 -n streamgate

# Scale Streaming
kubectl scale deployment streaming --replicas=5 -n streamgate

# Verify scaling
kubectl get pods -n streamgate
```

### Auto-Scaling

```bash
# Create HPA for API Gateway
kubectl autoscale deployment api-gateway \
  --min=2 --max=10 \
  --cpu-percent=80 \
  -n streamgate

# Create HPA for Transcoder
kubectl autoscale deployment transcoder \
  --min=1 --max=5 \
  --cpu-percent=70 \
  -n streamgate

# Check HPA status
kubectl get hpa -n streamgate
```

### Vertical Scaling

```bash
# Update resource requests/limits
kubectl set resources deployment api-gateway \
  --limits=cpu=2,memory=2Gi \
  --requests=cpu=500m,memory=512Mi \
  -n streamgate

# Verify changes
kubectl get deployment api-gateway -o yaml -n streamgate
```

## Disaster Recovery

### Backup Strategy

```bash
# Daily backups
0 2 * * * /scripts/backup-database.sh
0 3 * * * /scripts/backup-storage.sh

# Weekly full backups
0 4 * * 0 /scripts/backup-full.sh

# Monthly archive
0 5 1 * * /scripts/backup-archive.sh
```

### Backup Verification

```bash
# Verify database backup
pg_restore --list /backups/streamgate-$(date +%Y%m%d).sql | head -20

# Verify storage backup
aws s3 ls s3://streamgate-backups/$(date +%Y%m%d)/

# Test restore (in test environment)
pg_restore -d streamgate_test /backups/streamgate-latest.sql
```

### Disaster Recovery Plan

#### RTO (Recovery Time Objective): 1 hour
#### RPO (Recovery Point Objective): 15 minutes

```
Disaster Event
  │
  ├─ Detect (5 min)
  │  └─> Alert triggered
  │
  ├─ Assess (5 min)
  │  └─> Determine scope
  │
  ├─ Recover (30 min)
  │  ├─> Restore database
  │  ├─> Restore storage
  │  └─> Restart services
  │
  ├─ Verify (10 min)
  │  ├─> Run smoke tests
  │  └─> Verify data integrity
  │
  └─ Communicate (5 min)
     └─> Notify stakeholders
```

### Failover Procedure

```bash
# 1. Detect primary failure
# Alert triggered

# 2. Promote secondary
kubectl patch statefulset postgres \
  -p '{"spec":{"template":{"spec":{"nodeSelector":{"role":"secondary"}}}}}' \
  -n streamgate

# 3. Update DNS
# Point to secondary

# 4. Verify failover
./scripts/verify-failover.sh

# 5. Restore primary
# Once primary is recovered

# 6. Resync data
# Sync secondary back to primary
```

## Runbooks

### Runbook: High Error Rate

**Severity**: P1  
**Response Time**: 15 minutes

1. Check error logs
   ```bash
   kubectl logs -f deployment/api-gateway -n streamgate | grep ERROR
   ```

2. Check error metrics
   ```bash
   curl http://prometheus:9090/api/v1/query?query=rate(http_requests_total{status=~"5.."}[5m])
   ```

3. Check database
   ```bash
   psql streamgate -c "SELECT * FROM pg_stat_statements ORDER BY mean_time DESC LIMIT 5;"
   ```

4. Restart service if needed
   ```bash
   kubectl rollout restart deployment/api-gateway -n streamgate
   ```

5. Monitor recovery
   ```bash
   kubectl logs -f deployment/api-gateway -n streamgate
   ```

### Runbook: Database Connection Pool Exhausted

**Severity**: P1  
**Response Time**: 15 minutes

1. Check connection count
   ```bash
   psql streamgate -c "SELECT count(*) FROM pg_stat_activity;"
   ```

2. Check long-running queries
   ```bash
   psql streamgate -c "SELECT * FROM pg_stat_activity WHERE state != 'idle' ORDER BY query_start;"
   ```

3. Kill long-running queries
   ```bash
   psql streamgate -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE duration > interval '30 minutes';"
   ```

4. Increase connection pool
   ```bash
   # Edit config and restart
   kubectl set env deployment/api-gateway DB_MAX_CONNECTIONS=100 -n streamgate
   ```

5. Monitor recovery
   ```bash
   psql streamgate -c "SELECT count(*) FROM pg_stat_activity;"
   ```

---

**Last Updated**: 2025-01-29  
**Version**: 1.0.0  
**Status**: ✅ Complete
