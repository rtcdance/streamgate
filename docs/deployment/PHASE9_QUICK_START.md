# StreamGate Phase 9 - Quick Start Guide

**Date**: 2025-01-28  
**Status**: Phase 9 Quick Start  
**Version**: 1.0.0

## 5-Minute Setup

### Prerequisites Check

```bash
# Verify kubectl
kubectl version --client

# Verify cluster
kubectl cluster-info

# Verify namespace
kubectl get namespace streamgate || echo "Namespace not found"
```

### Deploy Infrastructure (5 minutes)

```bash
# 1. Create namespace and base config (1 min)
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/secret.yaml
kubectl apply -f deploy/k8s/rbac.yaml

# 2. Deploy blue-green infrastructure (2 min)
kubectl apply -f deploy/k8s/blue-green-setup.yaml

# 3. Configure autoscaling (1 min)
kubectl apply -f deploy/k8s/hpa-config.yaml
kubectl apply -f deploy/k8s/vpa-config.yaml

# 4. Verify deployment (1 min)
kubectl get pods -n streamgate
kubectl get services -n streamgate
```

## 10-Minute Testing

### Run Infrastructure Tests

```bash
# Test blue-green deployment
go test ./test/deployment -run TestBlueGreenDeployment -v

# Test HPA configuration
go test ./test/scaling -run TestHPA -v

# Expected: All tests pass
```

### Verify Connectivity

```bash
# Get load balancer IP
LB_IP=$(kubectl get service streamgate-active -n streamgate -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Load Balancer IP: $LB_IP"

# Test health endpoint
curl http://$LB_IP:80/health

# Expected: HTTP 200 OK
```

## 30-Minute Deployment

### Deploy New Version

```bash
# 1. Build and push image (10 min)
docker build -t streamgate:v1.2.0 .
docker push streamgate:v1.2.0

# 2. Deploy using blue-green (15 min)
./scripts/blue-green-deploy.sh streamgate:v1.2.0 300

# 3. Verify deployment (5 min)
curl http://$LB_IP:80/health
kubectl get pods -n streamgate
```

### Rollback if Needed

```bash
# Quick rollback
./scripts/blue-green-rollback.sh

# Verify rollback
curl http://$LB_IP:80/health
```

## Common Tasks

### Task 1: Deploy New Version

```bash
# Step 1: Build image
docker build -t streamgate:v1.2.0 .
docker push streamgate:v1.2.0

# Step 2: Deploy
./scripts/blue-green-deploy.sh streamgate:v1.2.0 300

# Step 3: Verify
curl http://<LB_IP>:80/health
```

### Task 2: Canary Deployment

```bash
# Step 1: Build image
docker build -t streamgate:v1.3.0 .
docker push streamgate:v1.3.0

# Step 2: Deploy canary
./scripts/canary-deploy.sh streamgate:v1.3.0 300 60

# Step 3: Monitor
kubectl get pods -n streamgate -l version=canary -w
```

### Task 3: Setup Autoscaling

```bash
# Step 1: Install metrics server
./scripts/setup-hpa.sh

# Step 2: Verify HPA
kubectl get hpa -n streamgate

# Step 3: Generate load
kubectl run -it --rm load-generator --image=busybox /bin/sh
# Inside pod: while sleep 0.01; do wget -q -O- http://streamgate-active:9090/api/v1/health; done
```

### Task 4: Monitor Deployment

```bash
# Watch pods
kubectl get pods -n streamgate -w

# Watch services
kubectl get services -n streamgate -w

# Watch events
kubectl get events -n streamgate --sort-by='.lastTimestamp'

# Check logs
kubectl logs -n streamgate -l app=streamgate --tail=50 -f
```

### Task 5: Troubleshoot Issues

```bash
# Check pod status
kubectl describe pod <pod-name> -n streamgate

# Check logs
kubectl logs <pod-name> -n streamgate

# Check events
kubectl get events -n streamgate --sort-by='.lastTimestamp'

# Check resource usage
kubectl top pods -n streamgate
```

## Useful Commands

### Deployment Commands

```bash
# Get active version
kubectl get service streamgate-active -n streamgate -o jsonpath='{.spec.selector.version}'

# Get pod count
kubectl get pods -n streamgate -l app=streamgate --no-headers | wc -l

# Get deployment status
kubectl get deployments -n streamgate -o wide

# Get service endpoints
kubectl get endpoints -n streamgate
```

### Scaling Commands

```bash
# Get HPA status
kubectl get hpa -n streamgate -o wide

# Get current replicas
kubectl get hpa streamgate-hpa-cpu -n streamgate -o jsonpath='{.status.currentReplicas}'

# Get desired replicas
kubectl get hpa streamgate-hpa-cpu -n streamgate -o jsonpath='{.status.desiredReplicas}'

# Watch scaling
kubectl get hpa -n streamgate -w
```

### Monitoring Commands

```bash
# Get metrics
kubectl top pods -n streamgate
kubectl top nodes

# Get events
kubectl get events -n streamgate --sort-by='.lastTimestamp'

# Get logs
kubectl logs -n streamgate -l app=streamgate --tail=100

# Get pod info
kubectl get pods -n streamgate -o wide
```

## Troubleshooting Quick Reference

### Problem: Pods Not Starting

```bash
# Check pod status
kubectl describe pod <pod-name> -n streamgate

# Check logs
kubectl logs <pod-name> -n streamgate

# Common causes:
# - Image not found: Check image name
# - Resource limits: Check resource requests
# - Health check failing: Check health endpoint
```

### Problem: Service Not Accessible

```bash
# Check service
kubectl get service streamgate-active -n streamgate

# Check endpoints
kubectl get endpoints streamgate-active -n streamgate

# Check load balancer
kubectl describe service streamgate-active -n streamgate

# Test connectivity
curl http://<LB_IP>:80/health
```

### Problem: Scaling Not Working

```bash
# Check HPA
kubectl describe hpa streamgate-hpa-cpu -n streamgate

# Check metrics
kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/streamgate/pods

# Check resource requests
kubectl get pods -n streamgate -o yaml | grep -A 5 "resources"
```

## Checklists

### Pre-Deployment Checklist

- [ ] Kubernetes cluster running
- [ ] kubectl configured
- [ ] Docker image built
- [ ] Image pushed to registry
- [ ] All tests passing
- [ ] Monitoring ready
- [ ] Team notified

### Deployment Checklist

- [ ] Infrastructure deployed
- [ ] Services running
- [ ] Health checks passing
- [ ] Load balancer accessible
- [ ] Metrics being collected
- [ ] Logs being generated
- [ ] Alerts configured

### Post-Deployment Checklist

- [ ] All pods running
- [ ] All services accessible
- [ ] Health checks passing
- [ ] Metrics available
- [ ] Logs available
- [ ] Alerts working
- [ ] Documentation updated

## Performance Baselines

| Metric | Target | Baseline |
|--------|--------|----------|
| Deployment Time | < 5 min | 4:30 |
| Rollback Time | < 2 min | 1:30 |
| Scale-up Latency | < 30 sec | 25 sec |
| API Latency (P95) | < 200ms | 95ms |
| Throughput | > 1000 req/sec | 5000 req/sec |
| Error Rate | < 1% | 0.1% |

## Next Steps

1. **Deploy Infrastructure** (5 min)
   - Run deployment commands
   - Verify all pods running

2. **Run Tests** (10 min)
   - Run infrastructure tests
   - Verify all tests pass

3. **Test Deployment** (30 min)
   - Deploy new version
   - Verify deployment
   - Test rollback

4. **Test Scaling** (30 min)
   - Generate load
   - Monitor scaling
   - Verify performance

5. **Monitor Production** (ongoing)
   - Watch metrics
   - Check logs
   - Respond to alerts

## Support

### Documentation
- `docs/deployment/PHASE9_DEPLOYMENT_GUIDE.md` - Detailed deployment guide
- `docs/operations/PHASE9_RUNBOOKS.md` - Operational runbooks
- `docs/operations/PHASE9_MONITORING.md` - Monitoring guide
- `test/deployment/PHASE9_TESTING_GUIDE.md` - Testing guide

### Commands
- `./scripts/blue-green-deploy.sh` - Deploy new version
- `./scripts/blue-green-rollback.sh` - Rollback deployment
- `./scripts/canary-deploy.sh` - Canary deployment
- `./scripts/setup-hpa.sh` - Setup autoscaling

### Tests
- `go test ./test/deployment` - Run deployment tests
- `go test ./test/scaling` - Run scaling tests

---

**Document Status**: Quick Start Guide  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0

</content>
