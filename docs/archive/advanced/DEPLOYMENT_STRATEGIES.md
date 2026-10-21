# StreamGate Deployment Strategies Guide

**Date**: 2025-01-28  
**Status**: Deployment Implementation Guide  
**Version**: 1.0.0

## Table of Contents

1. [Blue-Green Deployment](#blue-green-deployment)
2. [Canary Deployment](#canary-deployment)
3. [Rolling Deployment](#rolling-deployment)
4. [Deployment Automation](#deployment-automation)
5. [Rollback Procedures](#rollback-procedures)
6. [Monitoring During Deployment](#monitoring-during-deployment)

## Blue-Green Deployment

### Overview

Blue-Green deployment maintains two identical production environments. At any time, only one is live (receiving traffic). This enables zero-downtime deployments and instant rollbacks.

```
┌─────────────────────────────────────────────────────────┐
│                    Load Balancer                        │
└────────────────────┬────────────────────────────────────┘
                     │
        ┌────────────┴────────────┐
        │                         │
    ┌───▼────┐              ┌────▼───┐
    │  BLUE  │              │ GREEN  │
    │ (Live) │              │(Standby)
    └────────┘              └────────┘
```

### Implementation Steps

#### Step 1: Infrastructure Setup

```yaml
# kubernetes/blue-green-setup.yaml
apiVersion: v1
kind: Service
metadata:
  name: api-gateway
spec:
  selector:
    app: api-gateway
    version: blue  # Initially points to blue
  ports:
  - port: 80
    targetPort: 8080
  type: LoadBalancer
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway-blue
spec:
  replicas: 3
  selector:
    matchLabels:
      app: api-gateway
      version: blue
  template:
    metadata:
      labels:
        app: api-gateway
        version: blue
    spec:
      containers:
      - name: api-gateway
        image: streamgate:v1.0.0
        ports:
        - containerPort: 8080
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway-green
spec:
  replicas: 0  # Initially scaled to 0
  selector:
    matchLabels:
      app: api-gateway
      version: green
  template:
    metadata:
      labels:
        app: api-gateway
        version: green
    spec:
      containers:
      - name: api-gateway
        image: streamgate:v1.0.0
        ports:
        - containerPort: 8080
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
```

#### Step 2: Deployment Script

```bash
#!/bin/bash
# scripts/blue-green-deploy.sh

set -e

NAMESPACE="streamgate"
SERVICE="api-gateway"
NEW_VERSION="v1.1.0"
CURRENT_VERSION=$(kubectl get deployment -n $NAMESPACE -o jsonpath='{.items[0].spec.template.spec.containers[0].image}' | cut -d: -f2)

# Determine which is blue and which is green
if kubectl get deployment -n $NAMESPACE ${SERVICE}-blue > /dev/null 2>&1; then
    ACTIVE="blue"
    STANDBY="green"
else
    ACTIVE="green"
    STANDBY="blue"
fi

echo "Current active: $ACTIVE"
echo "Deploying to: $STANDBY"

# 1. Scale up standby environment
echo "Scaling up $STANDBY environment..."
kubectl scale deployment ${SERVICE}-${STANDBY} -n $NAMESPACE --replicas=3

# 2. Wait for pods to be ready
echo "Waiting for pods to be ready..."
kubectl wait --for=condition=ready pod -l app=$SERVICE,version=$STANDBY -n $NAMESPACE --timeout=300s

# 3. Run health checks
echo "Running health checks..."
PODS=$(kubectl get pods -n $NAMESPACE -l app=$SERVICE,version=$STANDBY -o jsonpath='{.items[*].metadata.name}')
for POD in $PODS; do
    echo "Checking pod: $POD"
    kubectl exec -it $POD -n $NAMESPACE -- curl -f http://localhost:8080/health || exit 1
done

# 4. Run smoke tests
echo "Running smoke tests..."
ENDPOINT=$(kubectl get service $SERVICE -n $NAMESPACE -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl -f http://$ENDPOINT/api/v1/health || exit 1

# 5. Switch traffic
echo "Switching traffic to $STANDBY..."
kubectl patch service $SERVICE -n $NAMESPACE -p '{"spec":{"selector":{"version":"'$STANDBY'"}}}'

# 6. Verify traffic switch
echo "Verifying traffic switch..."
sleep 10
curl -f http://$ENDPOINT/api/v1/health || exit 1

# 7. Scale down old environment
echo "Scaling down $ACTIVE environment..."
kubectl scale deployment ${SERVICE}-${ACTIVE} -n $NAMESPACE --replicas=0

echo "Deployment successful!"
echo "Active: $STANDBY"
echo "Standby: $ACTIVE"
```

#### Step 3: Rollback Script

```bash
#!/bin/bash
# scripts/blue-green-rollback.sh

set -e

NAMESPACE="streamgate"
SERVICE="api-gateway"

# Get current active version
CURRENT_ACTIVE=$(kubectl get service $SERVICE -n $NAMESPACE -o jsonpath='{.spec.selector.version}')
PREVIOUS_ACTIVE=$([ "$CURRENT_ACTIVE" = "blue" ] && echo "green" || echo "blue")

echo "Rolling back from $CURRENT_ACTIVE to $PREVIOUS_ACTIVE..."

# 1. Scale up previous environment
echo "Scaling up $PREVIOUS_ACTIVE environment..."
kubectl scale deployment ${SERVICE}-${PREVIOUS_ACTIVE} -n $NAMESPACE --replicas=3

# 2. Wait for pods to be ready
echo "Waiting for pods to be ready..."
kubectl wait --for=condition=ready pod -l app=$SERVICE,version=$PREVIOUS_ACTIVE -n $NAMESPACE --timeout=300s

# 3. Switch traffic back
echo "Switching traffic back to $PREVIOUS_ACTIVE..."
kubectl patch service $SERVICE -n $NAMESPACE -p '{"spec":{"selector":{"version":"'$PREVIOUS_ACTIVE'"}}}'

# 4. Verify rollback
echo "Verifying rollback..."
sleep 10
ENDPOINT=$(kubectl get service $SERVICE -n $NAMESPACE -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl -f http://$ENDPOINT/api/v1/health || exit 1

# 5. Scale down failed environment
echo "Scaling down $CURRENT_ACTIVE environment..."
kubectl scale deployment ${SERVICE}-${CURRENT_ACTIVE} -n $NAMESPACE --replicas=0

echo "Rollback successful!"
```

### Advantages

- ✅ Zero-downtime deployments
- ✅ Instant rollback capability
- ✅ Easy to test before switching
- ✅ No gradual traffic shift needed
- ✅ Simple to understand and implement

### Disadvantages

- ❌ Requires double resources
- ❌ Database migrations need careful handling
- ❌ No gradual rollout

## Canary Deployment

### Overview

Canary deployment gradually shifts traffic to a new version, monitoring for errors. If errors are detected, traffic is automatically rolled back.

```
┌─────────────────────────────────────────────────────────┐
│                    Load Balancer                        │
│              (Traffic Splitting: 95/5)                  │
└────────────────────┬────────────────────────────────────┘
                     │
        ┌────────────┴────────────┐
        │                         │
    ┌───▼────┐              ┌────▼───┐
    │ STABLE │              │ CANARY │
    │ (95%)  │              │ (5%)   │
    └────────┘              └────────┘
```

### Implementation Steps

#### Step 1: Canary Infrastructure

```yaml
# kubernetes/canary-setup.yaml
apiVersion: v1
kind: Service
metadata:
  name: api-gateway
spec:
  selector:
    app: api-gateway
  ports:
  - port: 80
    targetPort: 8080
  type: LoadBalancer
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway-stable
spec:
  replicas: 19  # 95% of traffic
  selector:
    matchLabels:
      app: api-gateway
      version: stable
  template:
    metadata:
      labels:
        app: api-gateway
        version: stable
    spec:
      containers:
      - name: api-gateway
        image: streamgate:v1.0.0
        ports:
        - containerPort: 8080
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway-canary
spec:
  replicas: 1  # 5% of traffic
  selector:
    matchLabels:
      app: api-gateway
      version: canary
  template:
    metadata:
      labels:
        app: api-gateway
        version: canary
    spec:
      containers:
      - name: api-gateway
        image: streamgate:v1.1.0  # New version
        ports:
        - containerPort: 8080
```

#### Step 2: Canary Deployment Script

```bash
#!/bin/bash
# scripts/canary-deploy.sh

set -e

NAMESPACE="streamgate"
SERVICE="api-gateway"
NEW_VERSION="v1.1.0"
TOTAL_REPLICAS=20

# Traffic percentages for gradual rollout
PERCENTAGES=(5 10 25 50 100)
MONITORING_INTERVAL=300  # 5 minutes

echo "Starting canary deployment..."

# 1. Deploy canary with new version
echo "Deploying canary version..."
kubectl set image deployment/api-gateway-canary \
  api-gateway=streamgate:$NEW_VERSION \
  -n $NAMESPACE

# 2. Wait for canary pods to be ready
echo "Waiting for canary pods to be ready..."
kubectl wait --for=condition=ready pod -l app=$SERVICE,version=canary -n $NAMESPACE --timeout=300s

# 3. Gradual traffic shift
for PERCENTAGE in "${PERCENTAGES[@]}"; do
    CANARY_REPLICAS=$((TOTAL_REPLICAS * PERCENTAGE / 100))
    STABLE_REPLICAS=$((TOTAL_REPLICAS - CANARY_REPLICAS))
    
    echo "Shifting traffic: $PERCENTAGE% to canary ($CANARY_REPLICAS replicas)"
    
    # Scale deployments
    kubectl scale deployment api-gateway-canary -n $NAMESPACE --replicas=$CANARY_REPLICAS
    kubectl scale deployment api-gateway-stable -n $NAMESPACE --replicas=$STABLE_REPLICAS
    
    # Wait for scaling
    sleep 30
    
    # Monitor metrics
    echo "Monitoring metrics for $MONITORING_INTERVAL seconds..."
    ERROR_RATE=$(kubectl exec -it $(kubectl get pod -n $NAMESPACE -l app=$SERVICE,version=canary -o jsonpath='{.items[0].metadata.name}') \
      -n $NAMESPACE -- curl -s http://localhost:8080/metrics | grep http_requests_total | grep 5 | awk '{sum+=$NF} END {print sum}')
    
    if [ "$ERROR_RATE" -gt 10 ]; then
        echo "High error rate detected: $ERROR_RATE"
        echo "Rolling back..."
        kubectl scale deployment api-gateway-canary -n $NAMESPACE --replicas=0
        kubectl scale deployment api-gateway-stable -n $NAMESPACE --replicas=$TOTAL_REPLICAS
        exit 1
    fi
    
    sleep $MONITORING_INTERVAL
done

# 4. Promote canary to stable
echo "Promoting canary to stable..."
kubectl set image deployment/api-gateway-stable \
  api-gateway=streamgate:$NEW_VERSION \
  -n $NAMESPACE

# 5. Scale down canary
echo "Scaling down canary..."
kubectl scale deployment api-gateway-canary -n $NAMESPACE --replicas=0
kubectl scale deployment api-gateway-stable -n $NAMESPACE --replicas=$TOTAL_REPLICAS

echo "Canary deployment successful!"
```

### Advantages

- ✅ Gradual rollout reduces risk
- ✅ Early error detection
- ✅ Automatic rollback on errors
- ✅ Minimal resource overhead
- ✅ Real user traffic testing

### Disadvantages

- ❌ More complex to implement
- ❌ Longer deployment time
- ❌ Requires good monitoring
- ❌ Database migrations still risky

## Rolling Deployment

### Overview

Rolling deployment gradually replaces old pods with new ones, maintaining service availability.

```
Time →
Pod 1: [Old] → [New]
Pod 2: [Old] → [New]
Pod 3: [Old] → [New]
```

### Implementation

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1        # One extra pod during update
      maxUnavailable: 0  # No pods unavailable
  selector:
    matchLabels:
      app: api-gateway
  template:
    metadata:
      labels:
        app: api-gateway
    spec:
      containers:
      - name: api-gateway
        image: streamgate:v1.1.0
        ports:
        - containerPort: 8080
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
```

## Deployment Automation

### CI/CD Pipeline

```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      
      - name: Build Docker image
        run: docker build -t streamgate:${{ github.sha }} .
      
      - name: Push to registry
        run: docker push streamgate:${{ github.sha }}
      
      - name: Deploy to staging
        run: |
          kubectl set image deployment/api-gateway-staging \
            api-gateway=streamgate:${{ github.sha }} \
            -n staging
      
      - name: Run smoke tests
        run: ./scripts/smoke-tests.sh staging
      
      - name: Deploy to production (blue-green)
        run: ./scripts/blue-green-deploy.sh ${{ github.sha }}
      
      - name: Verify deployment
        run: ./scripts/verify-deployment.sh
```

## Rollback Procedures

### Automatic Rollback

```go
type DeploymentMonitor struct {
    errorThreshold float64
    latencyThreshold time.Duration
}

func (dm *DeploymentMonitor) Monitor(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            metrics := dm.getMetrics()
            
            if metrics.ErrorRate > dm.errorThreshold {
                log.Error("High error rate detected, rolling back")
                dm.rollback()
                return
            }
            
            if metrics.P95Latency > dm.latencyThreshold {
                log.Error("High latency detected, rolling back")
                dm.rollback()
                return
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### Manual Rollback

```bash
#!/bin/bash
# scripts/manual-rollback.sh

NAMESPACE="streamgate"
SERVICE="api-gateway"
PREVIOUS_VERSION=$(kubectl rollout history deployment/$SERVICE -n $NAMESPACE | tail -2 | head -1 | awk '{print $1}')

echo "Rolling back to revision $PREVIOUS_VERSION..."
kubectl rollout undo deployment/$SERVICE -n $NAMESPACE --to-revision=$PREVIOUS_VERSION

echo "Waiting for rollback to complete..."
kubectl rollout status deployment/$SERVICE -n $NAMESPACE

echo "Rollback complete!"
```

## Monitoring During Deployment

### Key Metrics to Monitor

```go
type DeploymentMetrics struct {
    RequestCount    prometheus.Counter
    ErrorCount      prometheus.Counter
    LatencyHistogram prometheus.Histogram
    ActiveConnections prometheus.Gauge
}

func (dm *DeploymentMetrics) RecordRequest(duration time.Duration, err error) {
    dm.RequestCount.Inc()
    dm.LatencyHistogram.Observe(duration.Seconds())
    
    if err != nil {
        dm.ErrorCount.Inc()
    }
}
```

### Alerting Rules

```yaml
groups:
- name: deployment
  rules:
  - alert: HighErrorRateDuringDeployment
    expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.05
    for: 2m
    annotations:
      summary: "High error rate during deployment"
      
  - alert: HighLatencyDuringDeployment
    expr: histogram_quantile(0.95, http_request_duration_seconds) > 1
    for: 2m
    annotations:
      summary: "High latency during deployment"
```

## Conclusion

Choose deployment strategy based on requirements:
- **Blue-Green**: Zero-downtime, instant rollback, but requires double resources
- **Canary**: Gradual rollout, early error detection, but more complex
- **Rolling**: Simple, minimal resources, but slower rollback

---

**Document Status**: Complete  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0
