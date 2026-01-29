# StreamGate Phase 9 - Monitoring and Alerting Guide

**Date**: 2025-01-28  
**Status**: Phase 9 Monitoring Guide  
**Version**: 1.0.0

## Overview

This guide provides comprehensive monitoring and alerting configuration for Phase 9 features including deployments, scaling, and performance metrics.

## Monitoring Architecture

```
┌─────────────────────────────────────────────────────────┐
│                  Prometheus                             │
│  (Metrics Collection & Storage)                         │
└────────────────────┬────────────────────────────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
    ┌───▼──┐    ┌───▼──┐    ┌───▼──┐
    │Grafana│   │Alerting│  │Logging│
    │(Viz) │   │(Rules) │  │(ELK)  │
    └──────┘    └────────┘  └───────┘
```

## Key Metrics

### Deployment Metrics

```promql
# Deployment status
kube_deployment_status_replicas_available{namespace="streamgate"}
kube_deployment_status_replicas_unavailable{namespace="streamgate"}

# Pod status
kube_pod_status_phase{namespace="streamgate",phase="Running"}
kube_pod_status_phase{namespace="streamgate",phase="Failed"}

# Container restarts
increase(kube_pod_container_status_restarts_total{namespace="streamgate"}[1h])
```

### Performance Metrics

```promql
# Request rate
sum(rate(http_requests_total{namespace="streamgate"}[5m]))

# Error rate
sum(rate(http_requests_total{namespace="streamgate",status=~"5.."}[5m]))

# Latency (P95)
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{namespace="streamgate"}[5m]))

# Latency (P99)
histogram_quantile(0.99, rate(http_request_duration_seconds_bucket{namespace="streamgate"}[5m]))
```

### Resource Metrics

```promql
# CPU usage
sum(rate(container_cpu_usage_seconds_total{namespace="streamgate"}[5m]))

# Memory usage
sum(container_memory_usage_bytes{namespace="streamgate"})

# Disk usage
sum(container_fs_usage_bytes{namespace="streamgate"})

# Network I/O
sum(rate(container_network_receive_bytes_total{namespace="streamgate"}[5m]))
```

### Scaling Metrics

```promql
# Current replicas
kube_hpa_status_current_replicas{namespace="streamgate"}

# Desired replicas
kube_hpa_status_desired_replicas{namespace="streamgate"}

# Scaling events
increase(kube_hpa_status_current_replicas{namespace="streamgate"}[5m])

# HPA status
kube_hpa_status_condition{namespace="streamgate",condition="ScalingActive"}
```

## Alerting Rules

### Critical Alerts

#### Pod Crash Alert
```yaml
- alert: PodCrashing
  expr: rate(kube_pod_container_status_restarts_total{namespace="streamgate"}[15m]) > 0.1
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "Pod {{ $labels.pod }} is crashing"
    description: "Pod {{ $labels.pod }} has restarted {{ $value }} times in 15 minutes"
```

#### High Error Rate Alert
```yaml
- alert: HighErrorRate
  expr: sum(rate(http_requests_total{namespace="streamgate",status=~"5.."}[5m])) / sum(rate(http_requests_total{namespace="streamgate"}[5m])) > 0.05
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "High error rate detected"
    description: "Error rate is {{ $value | humanizePercentage }}"
```

#### Deployment Failure Alert
```yaml
- alert: DeploymentFailure
  expr: kube_deployment_status_replicas_unavailable{namespace="streamgate"} > 0
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "Deployment {{ $labels.deployment }} has unavailable replicas"
    description: "{{ $value }} replicas are unavailable"
```

### Warning Alerts

#### High Latency Alert
```yaml
- alert: HighLatency
  expr: histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{namespace="streamgate"}[5m])) > 0.5
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High latency detected"
    description: "P95 latency is {{ $value }}s"
```

#### High CPU Usage Alert
```yaml
- alert: HighCPUUsage
  expr: sum(rate(container_cpu_usage_seconds_total{namespace="streamgate"}[5m])) > 0.8
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High CPU usage detected"
    description: "CPU usage is {{ $value | humanizePercentage }}"
```

#### High Memory Usage Alert
```yaml
- alert: HighMemoryUsage
  expr: sum(container_memory_usage_bytes{namespace="streamgate"}) / sum(kube_pod_container_resource_limits_memory_bytes{namespace="streamgate"}) > 0.8
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High memory usage detected"
    description: "Memory usage is {{ $value | humanizePercentage }}"
```

#### Scaling Failure Alert
```yaml
- alert: ScalingFailure
  expr: kube_hpa_status_current_replicas{namespace="streamgate"} != kube_hpa_status_desired_replicas{namespace="streamgate"}
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "HPA {{ $labels.hpa }} scaling failure"
    description: "Current replicas ({{ $value }}) != desired replicas"
```

## Grafana Dashboards

### Dashboard 1: Deployment Status

**Panels**:
- Pod count (gauge)
- Pod status (pie chart)
- Deployment replicas (graph)
- Pod restart count (graph)
- Deployment events (table)

### Dashboard 2: Performance Metrics

**Panels**:
- Request rate (graph)
- Error rate (graph)
- Latency P95 (graph)
- Latency P99 (graph)
- Error breakdown by endpoint (pie chart)

### Dashboard 3: Resource Usage

**Panels**:
- CPU usage (graph)
- Memory usage (graph)
- Disk usage (graph)
- Network I/O (graph)
- Resource limits (gauge)

### Dashboard 4: Scaling Metrics

**Panels**:
- Current replicas (gauge)
- Desired replicas (gauge)
- Scaling events (graph)
- HPA status (table)
- Scaling latency (graph)

### Dashboard 5: Blue-Green Deployment

**Panels**:
- Active version (stat)
- Blue replicas (gauge)
- Green replicas (gauge)
- Traffic distribution (pie chart)
- Deployment history (table)

### Dashboard 6: Canary Deployment

**Panels**:
- Stable replicas (gauge)
- Canary replicas (gauge)
- Traffic to canary (gauge)
- Canary error rate (graph)
- Canary latency (graph)

## Logging Configuration

### Log Levels

```yaml
# Application logs
LOG_LEVEL: info

# Deployment logs
kubectl logs -n streamgate -l app=streamgate --tail=100

# Event logs
kubectl get events -n streamgate --sort-by='.lastTimestamp'

# Audit logs
kubectl get events -n streamgate --field-selector type=Warning
```

### Log Aggregation

```yaml
# ELK Stack configuration
elasticsearch:
  hosts: ["elasticsearch:9200"]
  
logstash:
  inputs:
    - type: kubernetes
      namespace: streamgate
      
kibana:
  dashboards:
    - deployment-logs
    - error-logs
    - performance-logs
```

### Log Queries

```
# Error logs
level:ERROR AND namespace:streamgate

# Deployment logs
type:deployment AND namespace:streamgate

# Performance logs
latency:>500 AND namespace:streamgate

# Scaling logs
type:scaling AND namespace:streamgate
```

## Tracing Configuration

### Distributed Tracing

```yaml
# Jaeger configuration
jaeger:
  endpoint: http://jaeger:14268/api/traces
  sampler:
    type: probabilistic
    param: 0.1
```

### Trace Queries

```
# Deployment traces
service:streamgate AND operation:deploy

# Performance traces
service:streamgate AND duration:>500ms

# Error traces
service:streamgate AND error:true
```

## Health Checks

### Liveness Probe

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 9090
  initialDelaySeconds: 30
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3
```

### Readiness Probe

```yaml
readinessProbe:
  httpGet:
    path: /ready
    port: 9090
  initialDelaySeconds: 10
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 2
```

### Health Endpoint Response

```json
{
  "status": "healthy",
  "timestamp": "2025-01-28T12:00:00Z",
  "version": "v1.2.0",
  "uptime": 3600,
  "checks": {
    "database": "ok",
    "cache": "ok",
    "storage": "ok"
  }
}
```

## SLO and SLI

### Service Level Objectives

| Metric | Target | SLI |
|--------|--------|-----|
| Availability | 99.9% | Uptime / Total Time |
| Latency (P95) | < 200ms | Requests < 200ms / Total |
| Error Rate | < 0.5% | Errors / Total Requests |
| Deployment Success | > 99% | Successful / Total |

### SLI Queries

```promql
# Availability
sum(rate(http_requests_total{namespace="streamgate",status!~"5.."}[5m])) / sum(rate(http_requests_total{namespace="streamgate"}[5m]))

# Latency (P95)
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{namespace="streamgate"}[5m]))

# Error Rate
sum(rate(http_requests_total{namespace="streamgate",status=~"5.."}[5m])) / sum(rate(http_requests_total{namespace="streamgate"}[5m]))

# Deployment Success
sum(rate(deployment_success_total{namespace="streamgate"}[1h])) / sum(rate(deployment_total{namespace="streamgate"}[1h]))
```

## Monitoring Best Practices

### 1. Metric Naming
- Use descriptive names
- Include unit in name (e.g., `_seconds`, `_bytes`)
- Use consistent prefixes (e.g., `http_`, `db_`)

### 2. Alert Tuning
- Set appropriate thresholds
- Use meaningful alert names
- Include actionable descriptions
- Set appropriate severity levels

### 3. Dashboard Design
- Group related metrics
- Use appropriate visualizations
- Include context and annotations
- Keep dashboards simple

### 4. Log Management
- Centralize logs
- Use structured logging
- Set appropriate retention
- Index important fields

### 5. Tracing
- Sample appropriately
- Include context
- Track latency
- Identify bottlenecks

## Monitoring Checklist

### Daily
- [ ] Check pod health
- [ ] Check error rate
- [ ] Check latency
- [ ] Check resource usage
- [ ] Check scaling events

### Weekly
- [ ] Review metrics trends
- [ ] Review alert frequency
- [ ] Review log volume
- [ ] Review performance trends
- [ ] Review cost trends

### Monthly
- [ ] Review SLO compliance
- [ ] Review alert effectiveness
- [ ] Review dashboard usage
- [ ] Review log retention
- [ ] Review monitoring costs

## Troubleshooting Monitoring

### Problem: Metrics Not Available

**Diagnosis**:
```bash
kubectl get deployment metrics-server -n kube-system
kubectl get --raw /apis/metrics.k8s.io/v1beta1/nodes
```

**Solution**:
```bash
# Restart metrics server
kubectl rollout restart deployment metrics-server -n kube-system

# Verify metrics
sleep 30
kubectl get --raw /apis/metrics.k8s.io/v1beta1/nodes
```

### Problem: Alerts Not Firing

**Diagnosis**:
```bash
kubectl get pods -n monitoring -l app=prometheus
kubectl logs -n monitoring -l app=prometheus --tail=100
```

**Solution**:
```bash
# Check alert rules
kubectl get prometheusrule -n monitoring

# Reload Prometheus
kubectl rollout restart deployment prometheus -n monitoring

# Verify alerts
kubectl port-forward -n monitoring svc/prometheus 9090:9090
# Visit http://localhost:9090/alerts
```

### Problem: High Cardinality

**Diagnosis**:
```bash
# Check metric cardinality
curl http://prometheus:9090/api/v1/label/__name__/values | jq 'length'
```

**Solution**:
```bash
# Reduce cardinality
# - Remove unnecessary labels
# - Use label relabeling
# - Aggregate metrics
```

## Conclusion

This guide provides comprehensive monitoring and alerting configuration for Phase 9. Implement these practices to ensure system reliability and performance.

---

**Document Status**: Monitoring Guide  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0

</content>
