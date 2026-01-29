# StreamGate Phase 9 - Operational Runbooks

**Date**: 2025-01-28  
**Status**: Phase 9 Runbooks  
**Version**: 1.0.0

## Overview

This document provides operational runbooks for common Phase 9 tasks including deployments, scaling, monitoring, and incident response.

## Table of Contents

1. [Blue-Green Deployment Runbook](#blue-green-deployment-runbook)
2. [Canary Deployment Runbook](#canary-deployment-runbook)
3. [Autoscaling Runbook](#autoscaling-runbook)
4. [Incident Response Runbook](#incident-response-runbook)
5. [Monitoring Runbook](#monitoring-runbook)
6. [Troubleshooting Runbook](#troubleshooting-runbook)

---

## Blue-Green Deployment Runbook

### Purpose
Deploy new versions with zero downtime using blue-green deployment strategy.

### Prerequisites
- New image built and pushed to registry
- All tests passing
- Monitoring dashboards ready
- Team notified

### Procedure

#### Step 1: Pre-Deployment Checks
```bash
# Verify current state
kubectl get deployments -n streamgate
kubectl get services -n streamgate
kubectl get pods -n streamgate

# Check metrics
kubectl top nodes
kubectl top pods -n streamgate

# Verify backups
kubectl exec -it <pod-name> -n streamgate -- pg_dump -U streamgate streamgate > /backup/pre-deployment.sql
```

#### Step 2: Execute Deployment
```bash
# Run deployment script
./scripts/blue-green-deploy.sh streamgate:v1.2.0 300

# Monitor deployment
kubectl get deployments -n streamgate -w
kubectl get pods -n streamgate -w

# Check logs
kubectl logs -n streamgate -l app=streamgate --tail=50 -f
```

#### Step 3: Verify Deployment
```bash
# Check active version
ACTIVE=$(kubectl get service streamgate-active -n streamgate -o jsonpath='{.spec.selector.version}')
echo "Active version: $ACTIVE"

# Test API
curl http://<LOAD_BALANCER_IP>:80/health
curl http://<LOAD_BALANCER_IP>:80/api/v1/content

# Check metrics
curl http://<LOAD_BALANCER_IP>:80/metrics | grep streamgate_version

# Monitor error rate
kubectl logs -n streamgate -l app=streamgate --tail=100 | grep -i error | wc -l
```

#### Step 4: Post-Deployment
```bash
# Update documentation
echo "Deployed version: v1.2.0" >> DEPLOYMENT_LOG.md

# Notify team
echo "Deployment completed successfully"

# Schedule monitoring
# Continue monitoring for 1 hour
```

### Rollback Procedure

If issues detected:
```bash
# Immediate rollback
./scripts/blue-green-rollback.sh

# Verify rollback
ACTIVE=$(kubectl get service streamgate-active -n streamgate -o jsonpath='{.spec.selector.version}')
echo "Active version after rollback: $ACTIVE"

# Test API
curl http://<LOAD_BALANCER_IP>:80/health

# Investigate issue
kubectl logs -n streamgate -l app=streamgate --tail=200 > /tmp/deployment-issue.log
```

### Success Criteria
- ✅ Deployment time < 5 minutes
- ✅ Zero downtime
- ✅ All health checks passing
- ✅ Error rate < 0.5%
- ✅ Latency < 200ms (P95)

---

## Canary Deployment Runbook

### Purpose
Deploy new versions gradually with automatic rollback on errors.

### Prerequisites
- New image built and pushed to registry
- All tests passing
- Monitoring dashboards ready
- Team notified

### Procedure

#### Step 1: Pre-Deployment Checks
```bash
# Verify current state
kubectl get deployments -n streamgate -l version=stable
kubectl get services -n streamgate

# Check metrics
kubectl top pods -n streamgate -l version=stable

# Verify monitoring
kubectl get pods -n streamgate -l app=prometheus
```

#### Step 2: Execute Canary Deployment
```bash
# Run canary deployment script
./scripts/canary-deploy.sh streamgate:v1.3.0 300 60

# Monitor canary deployment
kubectl get deployments -n streamgate -w
kubectl get pods -n streamgate -l version=canary -w

# Check logs
kubectl logs -n streamgate -l version=canary --tail=50 -f
```

#### Step 3: Monitor Canary Metrics
```bash
# Check error rate
ERROR_RATE=$(kubectl logs -n streamgate -l version=canary --tail=100 | grep -i error | wc -l)
echo "Error rate: $ERROR_RATE"

# Check latency
kubectl logs -n streamgate -l version=canary --tail=100 | grep -i latency

# Check traffic distribution
kubectl get endpoints streamgate-stable -n streamgate
kubectl get endpoints streamgate-canary -n streamgate

# Monitor for 60 seconds per step
sleep 60
```

#### Step 4: Verify Promotion
```bash
# Check if canary was promoted
STABLE_IMAGE=$(kubectl get deployment streamgate-stable -n streamgate -o jsonpath='{.spec.template.spec.containers[0].image}')
echo "Stable image: $STABLE_IMAGE"

# Verify all pods running
kubectl get pods -n streamgate -l version=stable

# Test API
curl http://<LOAD_BALANCER_IP>:80/health
```

#### Step 5: Post-Deployment
```bash
# Update documentation
echo "Canary deployment completed: v1.3.0" >> DEPLOYMENT_LOG.md

# Notify team
echo "Canary deployment successful"

# Continue monitoring
```

### Automatic Rollback

If errors detected during canary:
```bash
# Automatic rollback triggered
# Verify rollback
CANARY_REPLICAS=$(kubectl get deployment streamgate-canary -n streamgate -o jsonpath='{.spec.replicas}')
echo "Canary replicas after rollback: $CANARY_REPLICAS"

# Verify stable unchanged
kubectl get deployment streamgate-stable -n streamgate

# Investigate issue
kubectl logs -n streamgate -l version=canary --tail=200 > /tmp/canary-issue.log
```

### Success Criteria
- ✅ Gradual traffic shift (5% → 100%)
- ✅ Error rate < 1%
- ✅ Latency < 500ms
- ✅ Automatic promotion on success
- ✅ Automatic rollback on failure

---

## Autoscaling Runbook

### Purpose
Monitor and manage automatic scaling of pods based on metrics.

### Prerequisites
- HPA configured
- Metrics server running
- Monitoring dashboards ready

### Procedure

#### Step 1: Monitor Scaling Events
```bash
# Watch HPA status
kubectl get hpa -n streamgate -w

# Check current replicas
kubectl get hpa -n streamgate -o wide

# View scaling history
kubectl get events -n streamgate --sort-by='.lastTimestamp' | grep -i scale
```

#### Step 2: Generate Load
```bash
# Create load generator pod
kubectl run -it --rm load-generator --image=busybox /bin/sh

# Inside pod, generate load
while sleep 0.01; do wget -q -O- http://streamgate-active:9090/api/v1/health; done
```

#### Step 3: Monitor Scaling
```bash
# Watch pod count increase
kubectl get pods -n streamgate -l app=streamgate -w

# Monitor resource usage
kubectl top pods -n streamgate -l app=streamgate

# Check HPA metrics
kubectl get hpa streamgate-hpa-cpu -n streamgate -o yaml | grep -A 10 "currentMetrics"
```

#### Step 4: Stop Load
```bash
# Stop load generator (Ctrl+C)

# Monitor scaling down
kubectl get pods -n streamgate -l app=streamgate -w

# Verify scale-down
kubectl get hpa -n streamgate -o wide
```

#### Step 5: Verify Performance
```bash
# Check latency
kubectl top pods -n streamgate

# Check error rate
kubectl logs -n streamgate -l app=streamgate --tail=100 | grep -i error | wc -l

# Check resource usage
kubectl top nodes
```

### Scaling Optimization

If scaling not working as expected:
```bash
# Check HPA status
kubectl describe hpa streamgate-hpa-cpu -n streamgate

# Check metrics availability
kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/streamgate/pods

# Check resource requests
kubectl get pods -n streamgate -o yaml | grep -A 5 "resources"

# Adjust HPA if needed
kubectl patch hpa streamgate-hpa-cpu -n streamgate -p '{"spec":{"metrics":[{"type":"Resource","resource":{"name":"cpu","target":{"type":"Utilization","averageUtilization":70}}}]}}'
```

### Success Criteria
- ✅ Scale-up latency < 30 seconds
- ✅ Scale-down latency < 5 minutes
- ✅ Scaling accuracy > 95%
- ✅ Performance maintained during scaling
- ✅ No errors during scaling

---

## Incident Response Runbook

### Purpose
Respond to incidents and restore service quickly.

### Incident Types

#### Type 1: Pod Crash

**Symptoms**: Pods restarting, error rate high

**Response**:
```bash
# Check pod status
kubectl get pods -n streamgate -l app=streamgate

# Check pod logs
kubectl logs <pod-name> -n streamgate --previous

# Check events
kubectl describe pod <pod-name> -n streamgate

# Check resource limits
kubectl get pods -n streamgate -o yaml | grep -A 5 "resources"

# If resource issue, scale up
kubectl scale deployment streamgate-blue -n streamgate --replicas=5

# If code issue, rollback
./scripts/blue-green-rollback.sh
```

#### Type 2: High Error Rate

**Symptoms**: Error rate > 1%, alerts firing

**Response**:
```bash
# Check error logs
kubectl logs -n streamgate -l app=streamgate --tail=200 | grep -i error

# Check metrics
curl http://<LOAD_BALANCER_IP>:80/metrics | grep http_requests_total

# Check dependencies
kubectl get pods -n streamgate

# If deployment issue, rollback
./scripts/blue-green-rollback.sh

# If scaling issue, increase replicas
kubectl scale deployment streamgate-blue -n streamgate --replicas=5
```

#### Type 3: High Latency

**Symptoms**: Latency > 500ms, P95 > 1s

**Response**:
```bash
# Check resource usage
kubectl top pods -n streamgate
kubectl top nodes

# Check scaling status
kubectl get hpa -n streamgate -o wide

# If resource constrained, scale up
kubectl scale deployment streamgate-blue -n streamgate --replicas=5

# If database slow, check connections
kubectl exec -it <pod-name> -n streamgate -- psql -U streamgate -d streamgate -c "SELECT count(*) FROM pg_stat_activity;"

# If cache issue, clear cache
kubectl exec -it <pod-name> -n streamgate -- redis-cli FLUSHALL
```

#### Type 4: Deployment Failure

**Symptoms**: Deployment stuck, pods not starting

**Response**:
```bash
# Check deployment status
kubectl describe deployment streamgate-blue -n streamgate

# Check pod events
kubectl get events -n streamgate --sort-by='.lastTimestamp'

# Check image availability
docker inspect streamgate:v1.2.0

# Rollback to previous version
./scripts/blue-green-rollback.sh

# Investigate issue
kubectl logs -n streamgate -l app=streamgate --tail=200 > /tmp/deployment-issue.log
```

### Escalation

If issue not resolved:
1. Notify team lead
2. Create incident ticket
3. Gather logs and metrics
4. Escalate to infrastructure team
5. Document incident

---

## Monitoring Runbook

### Purpose
Monitor system health and performance.

### Daily Checks

```bash
# Check pod health
kubectl get pods -n streamgate -l app=streamgate

# Check service health
kubectl get services -n streamgate

# Check resource usage
kubectl top pods -n streamgate
kubectl top nodes

# Check error rate
kubectl logs -n streamgate -l app=streamgate --tail=100 | grep -i error | wc -l

# Check scaling events
kubectl get events -n streamgate --sort-by='.lastTimestamp' | grep -i scale
```

### Weekly Checks

```bash
# Review metrics
# - CPU usage trends
# - Memory usage trends
# - Request rate trends
# - Error rate trends

# Review scaling behavior
# - Scale-up frequency
# - Scale-down frequency
# - Scaling accuracy

# Review performance
# - Latency trends
# - Throughput trends
# - Error rate trends

# Review costs
# - Pod count trends
# - Resource usage trends
# - Cost optimization opportunities
```

### Monthly Checks

```bash
# Review deployment history
# - Number of deployments
# - Deployment success rate
# - Rollback frequency

# Review incident history
# - Number of incidents
# - Incident severity
# - Resolution time

# Review performance trends
# - Performance improvements
# - Performance regressions
# - Optimization opportunities

# Review cost trends
# - Cost per request
# - Cost per user
# - Cost optimization opportunities
```

### Alerting

Configure alerts for:
- Pod crash (restart count > 3)
- High error rate (> 1%)
- High latency (P95 > 500ms)
- Scaling failure (desired != current)
- Resource exhaustion (> 90%)
- Deployment failure (replicas unavailable)

---

## Troubleshooting Runbook

### Problem: Pods Not Starting

**Diagnosis**:
```bash
kubectl describe pod <pod-name> -n streamgate
kubectl logs <pod-name> -n streamgate
```

**Common Causes**:
- Image not found
- Resource limits too low
- Health check failing
- Configuration missing

**Solution**:
```bash
# Check image
docker inspect streamgate:v1.2.0

# Check resource limits
kubectl get pods -n streamgate -o yaml | grep -A 5 "resources"

# Check health endpoint
kubectl exec -it <pod-name> -n streamgate -- curl localhost:9090/health

# Check configuration
kubectl get configmap -n streamgate
kubectl get secret -n streamgate
```

### Problem: High Error Rate

**Diagnosis**:
```bash
kubectl logs -n streamgate -l app=streamgate --tail=200 | grep -i error
```

**Common Causes**:
- Code bug
- Configuration error
- Dependency failure
- Resource exhaustion

**Solution**:
```bash
# Check logs for errors
kubectl logs -n streamgate -l app=streamgate --tail=200

# Check dependencies
kubectl get pods -n streamgate

# Check resource usage
kubectl top pods -n streamgate

# Rollback if needed
./scripts/blue-green-rollback.sh
```

### Problem: Scaling Not Working

**Diagnosis**:
```bash
kubectl describe hpa streamgate-hpa-cpu -n streamgate
kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/streamgate/pods
```

**Common Causes**:
- Metrics not available
- Resource requests not set
- HPA misconfigured
- Metrics server not running

**Solution**:
```bash
# Check metrics server
kubectl get deployment metrics-server -n kube-system

# Check resource requests
kubectl get pods -n streamgate -o yaml | grep -A 5 "resources"

# Check HPA configuration
kubectl get hpa -n streamgate -o yaml

# Restart metrics server if needed
kubectl rollout restart deployment metrics-server -n kube-system
```

---

## Conclusion

These runbooks provide step-by-step procedures for common Phase 9 operations. Follow them carefully and adapt as needed for your environment.

---

**Document Status**: Operational Runbooks  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0

</content>
