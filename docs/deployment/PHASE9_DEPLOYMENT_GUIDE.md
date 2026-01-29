# StreamGate Phase 9 - Deployment Guide

**Date**: 2025-01-28  
**Status**: Phase 9 Deployment Guide  
**Version**: 1.0.0

## Overview

This guide provides step-by-step instructions for deploying StreamGate Phase 9 features including blue-green deployments, canary deployments, and autoscaling infrastructure.

## Prerequisites

### Required Tools
- `kubectl` (v1.24+)
- `bash` (v4.0+)
- Docker (for building images)
- Helm (optional, for advanced deployments)

### Required Access
- Kubernetes cluster admin access
- Container registry access
- DNS management access

### Cluster Requirements
- Kubernetes 1.24+
- 3+ worker nodes
- 10+ GB total memory
- 10+ CPU cores
- Load balancer support

## Pre-Deployment Checklist

### Infrastructure
- [ ] Kubernetes cluster running
- [ ] Persistent storage configured
- [ ] Load balancer available
- [ ] DNS configured
- [ ] Network policies ready

### Configuration
- [ ] Environment variables set
- [ ] Secrets created
- [ ] ConfigMaps prepared
- [ ] RBAC roles defined
- [ ] Resource quotas set

### Monitoring
- [ ] Prometheus installed
- [ ] Grafana configured
- [ ] Alerting rules ready
- [ ] Logging configured
- [ ] Tracing enabled

### Testing
- [ ] Test cluster available
- [ ] Test data prepared
- [ ] Load testing tools ready
- [ ] Monitoring dashboards ready
- [ ] Rollback procedures tested

## Phase 1: Infrastructure Deployment

### Step 1: Create Namespace and Base Configuration

```bash
# Create namespace
kubectl apply -f deploy/k8s/namespace.yaml

# Verify namespace
kubectl get namespace streamgate

# Create base configuration
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/secret.yaml
kubectl apply -f deploy/k8s/rbac.yaml

# Verify configuration
kubectl get configmap -n streamgate
kubectl get secret -n streamgate
kubectl get serviceaccount -n streamgate
```

### Step 2: Deploy Blue-Green Infrastructure

```bash
# Deploy blue-green setup
kubectl apply -f deploy/k8s/blue-green-setup.yaml

# Verify deployments
kubectl get deployments -n streamgate
kubectl get services -n streamgate

# Check pod status
kubectl get pods -n streamgate -l app=streamgate

# Wait for pods to be ready
kubectl wait --for=condition=ready pod -l app=streamgate -n streamgate --timeout=300s
```

### Step 3: Deploy Canary Infrastructure

```bash
# Deploy canary setup
kubectl apply -f deploy/k8s/canary-setup.yaml

# Verify deployments
kubectl get deployments -n streamgate -l version=stable

# Check pod status
kubectl get pods -n streamgate -l version=stable
```

### Step 4: Configure Autoscaling

```bash
# Deploy HPA configuration
kubectl apply -f deploy/k8s/hpa-config.yaml

# Deploy VPA configuration
kubectl apply -f deploy/k8s/vpa-config.yaml

# Verify autoscaling
kubectl get hpa -n streamgate
kubectl get vpa -n streamgate
```

## Phase 2: Autoscaling Setup

### Step 1: Install Metrics Server

```bash
# Run HPA setup script
./scripts/setup-hpa.sh

# Verify metrics server
kubectl get deployment metrics-server -n kube-system

# Wait for metrics to be available
sleep 30
kubectl get --raw /apis/metrics.k8s.io/v1beta1/nodes
```

### Step 2: Install VPA (Optional)

```bash
# Run VPA setup script
./scripts/setup-vpa.sh

# Verify VPA installation
kubectl get deployment vpa-controller -n kube-system

# Check VPA recommendations
kubectl get vpa -n streamgate -o wide
```

### Step 3: Verify Autoscaling Configuration

```bash
# Check HPA status
kubectl describe hpa streamgate-hpa-cpu -n streamgate

# Check VPA status
kubectl describe vpa streamgate-vpa -n streamgate

# Monitor scaling events
kubectl get events -n streamgate --sort-by='.lastTimestamp'
```

## Phase 3: Testing Deployment

### Step 1: Run Infrastructure Tests

```bash
# Run blue-green tests
go test ./test/deployment -run TestBlueGreenDeployment -v

# Run canary tests
go test ./test/deployment -run TestCanaryDeployment -v

# Run HPA tests
go test ./test/scaling -run TestHPA -v
```

### Step 2: Verify Connectivity

```bash
# Get load balancer IP
kubectl get service streamgate-active -n streamgate

# Test API endpoint
curl http://<LOAD_BALANCER_IP>:80/health

# Test metrics endpoint
curl http://<LOAD_BALANCER_IP>:80/metrics
```

### Step 3: Verify Health Checks

```bash
# Check pod health
kubectl get pods -n streamgate -o wide

# Check pod logs
kubectl logs -n streamgate -l app=streamgate --tail=50

# Check pod events
kubectl describe pod -n streamgate -l app=streamgate
```

## Phase 4: Blue-Green Deployment

### Step 1: Prepare New Version

```bash
# Build new image
docker build -t streamgate:v1.2.0 .

# Push to registry
docker push streamgate:v1.2.0

# Verify image
docker inspect streamgate:v1.2.0
```

### Step 2: Deploy Using Blue-Green Script

```bash
# Run blue-green deployment
./scripts/blue-green-deploy.sh streamgate:v1.2.0 300

# Monitor deployment
kubectl get deployments -n streamgate -w

# Check pod status
kubectl get pods -n streamgate -l app=streamgate -w
```

### Step 3: Verify Deployment

```bash
# Check active version
kubectl get service streamgate-active -n streamgate -o jsonpath='{.spec.selector.version}'

# Test API
curl http://<LOAD_BALANCER_IP>:80/health

# Check metrics
curl http://<LOAD_BALANCER_IP>:80/metrics | grep streamgate_version
```

### Step 4: Rollback if Needed

```bash
# Rollback to previous version
./scripts/blue-green-rollback.sh

# Verify rollback
kubectl get service streamgate-active -n streamgate -o jsonpath='{.spec.selector.version}'

# Test API
curl http://<LOAD_BALANCER_IP>:80/health
```

## Phase 5: Canary Deployment

### Step 1: Prepare Canary Version

```bash
# Build canary image
docker build -t streamgate:v1.3.0 .

# Push to registry
docker push streamgate:v1.3.0

# Verify image
docker inspect streamgate:v1.3.0
```

### Step 2: Deploy Using Canary Script

```bash
# Run canary deployment
./scripts/canary-deploy.sh streamgate:v1.3.0 300 60

# Monitor canary deployment
kubectl get deployments -n streamgate -w

# Check canary pods
kubectl get pods -n streamgate -l version=canary -w
```

### Step 3: Monitor Canary Metrics

```bash
# Check error rate
kubectl logs -n streamgate -l version=canary --tail=100 | grep -i error

# Check latency
kubectl logs -n streamgate -l version=canary --tail=100 | grep -i latency

# Check traffic distribution
kubectl get endpoints streamgate-stable -n streamgate
kubectl get endpoints streamgate-canary -n streamgate
```

### Step 4: Promote or Rollback

```bash
# If successful, canary is promoted to stable automatically
# Verify promotion
kubectl get deployments -n streamgate -l version=stable

# If failed, canary is rolled back automatically
# Verify rollback
kubectl get deployments -n streamgate -l version=canary
```

## Phase 6: Autoscaling Validation

### Step 1: Load Testing

```bash
# Generate load
kubectl run -it --rm load-generator --image=busybox /bin/sh

# Inside pod, run load test
while sleep 0.01; do wget -q -O- http://streamgate-active:9090/api/v1/health; done
```

### Step 2: Monitor Scaling

```bash
# Watch HPA status
kubectl get hpa -n streamgate -w

# Watch pod count
kubectl get pods -n streamgate -l app=streamgate -w

# Check scaling events
kubectl get events -n streamgate --sort-by='.lastTimestamp' | grep -i scale
```

### Step 3: Verify Performance

```bash
# Check latency
kubectl top pods -n streamgate

# Check resource usage
kubectl top nodes

# Check metrics
kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/streamgate/pods
```

## Troubleshooting

### Deployment Issues

**Problem**: Pods not starting
```bash
# Check pod status
kubectl describe pod <pod-name> -n streamgate

# Check logs
kubectl logs <pod-name> -n streamgate

# Check events
kubectl get events -n streamgate --sort-by='.lastTimestamp'
```

**Problem**: Health checks failing
```bash
# Check health endpoint
kubectl exec -it <pod-name> -n streamgate -- curl localhost:9090/health

# Check readiness probe
kubectl describe pod <pod-name> -n streamgate | grep -A 5 "Readiness"

# Check liveness probe
kubectl describe pod <pod-name> -n streamgate | grep -A 5 "Liveness"
```

**Problem**: Traffic not switching
```bash
# Check service selector
kubectl get service streamgate-active -n streamgate -o yaml | grep -A 5 "selector"

# Check endpoints
kubectl get endpoints streamgate-active -n streamgate

# Check load balancer
kubectl describe service streamgate-active -n streamgate
```

### Scaling Issues

**Problem**: HPA not scaling
```bash
# Check HPA status
kubectl describe hpa streamgate-hpa-cpu -n streamgate

# Check metrics
kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/streamgate/pods

# Check resource requests
kubectl get pods -n streamgate -o yaml | grep -A 5 "resources"
```

**Problem**: Scaling too slow
```bash
# Check HPA behavior
kubectl get hpa streamgate-hpa-cpu -n streamgate -o yaml | grep -A 10 "behavior"

# Adjust scaling parameters
kubectl patch hpa streamgate-hpa-cpu -n streamgate -p '{"spec":{"behavior":{"scaleUp":{"stabilizationWindowSeconds":0}}}}'
```

## Monitoring and Observability

### Prometheus Queries

```promql
# Pod count
count(kube_pod_info{namespace="streamgate"})

# CPU usage
sum(rate(container_cpu_usage_seconds_total{namespace="streamgate"}[5m]))

# Memory usage
sum(container_memory_usage_bytes{namespace="streamgate"})

# Request rate
sum(rate(http_requests_total{namespace="streamgate"}[5m]))

# Error rate
sum(rate(http_requests_total{namespace="streamgate",status=~"5.."}[5m]))
```

### Grafana Dashboards

Create dashboards for:
- Deployment status
- Pod metrics
- Scaling events
- Traffic distribution
- Error rates
- Latency

### Alerting Rules

```yaml
- alert: DeploymentFailure
  expr: kube_deployment_status_replicas_unavailable > 0
  for: 5m

- alert: HighErrorRate
  expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.05
  for: 5m

- alert: HighLatency
  expr: histogram_quantile(0.95, http_request_duration_seconds) > 1
  for: 5m

- alert: ScalingFailure
  expr: kube_hpa_status_current_replicas != kube_hpa_status_desired_replicas
  for: 10m
```

## Rollback Procedures

### Quick Rollback

```bash
# Blue-green rollback
./scripts/blue-green-rollback.sh

# Verify rollback
kubectl get service streamgate-active -n streamgate -o jsonpath='{.spec.selector.version}'
```

### Manual Rollback

```bash
# Get previous image
kubectl get deployment streamgate-blue -n streamgate -o jsonpath='{.spec.template.spec.containers[0].image}'

# Rollback deployment
kubectl rollout undo deployment/streamgate-blue -n streamgate

# Verify rollback
kubectl rollout status deployment/streamgate-blue -n streamgate
```

### Data Rollback

```bash
# Restore from backup
kubectl exec -it <pod-name> -n streamgate -- /bin/bash

# Inside pod, restore database
psql -U streamgate -d streamgate < /backup/database.sql

# Verify data
psql -U streamgate -d streamgate -c "SELECT COUNT(*) FROM content;"
```

## Post-Deployment Verification

### Checklist

- [ ] All pods running
- [ ] All services accessible
- [ ] Health checks passing
- [ ] Metrics being collected
- [ ] Logs being generated
- [ ] Alerts configured
- [ ] Dashboards working
- [ ] Scaling working
- [ ] Rollback tested
- [ ] Documentation updated

### Performance Validation

```bash
# API response time
time curl http://<LOAD_BALANCER_IP>:80/api/v1/health

# Throughput
ab -n 1000 -c 100 http://<LOAD_BALANCER_IP>:80/api/v1/health

# Concurrent connections
wrk -t4 -c100 -d30s http://<LOAD_BALANCER_IP>:80/api/v1/health
```

## Maintenance

### Regular Tasks

- Monitor pod health
- Check resource usage
- Review logs
- Update metrics
- Verify backups
- Test rollback procedures
- Update documentation

### Scaling Optimization

```bash
# Review HPA metrics
kubectl get hpa -n streamgate -o wide

# Adjust thresholds if needed
kubectl patch hpa streamgate-hpa-cpu -n streamgate -p '{"spec":{"metrics":[{"type":"Resource","resource":{"name":"cpu","target":{"type":"Utilization","averageUtilization":75}}}]}}'

# Monitor scaling behavior
kubectl get events -n streamgate --sort-by='.lastTimestamp' | grep -i scale
```

## Conclusion

This guide provides comprehensive instructions for deploying and managing StreamGate Phase 9 features. Follow the steps carefully and verify each phase before proceeding to the next.

---

**Document Status**: Deployment Guide  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0

</content>
