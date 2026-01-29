# StreamGate Autoscaling Guide

**Date**: 2025-01-28  
**Status**: Autoscaling Implementation Guide  
**Version**: 1.0.0

## Table of Contents

1. [Horizontal Pod Autoscaling](#horizontal-pod-autoscaling)
2. [Vertical Pod Autoscaling](#vertical-pod-autoscaling)
3. [Cluster Autoscaling](#cluster-autoscaling)
4. [Custom Metrics Scaling](#custom-metrics-scaling)
5. [Scaling Policies](#scaling-policies)
6. [Monitoring & Optimization](#monitoring--optimization)

## Horizontal Pod Autoscaling

### Overview

Horizontal Pod Autoscaling (HPA) automatically scales the number of pods based on observed metrics.

```
Load ↑
  │     ┌─────────────────────────────────┐
  │     │ Scale Up (Add Pods)             │
  │     │ CPU > 70% or Memory > 75%       │
  │     └─────────────────────────────────┘
  │
  │     ┌─────────────────────────────────┐
  │     │ Scale Down (Remove Pods)        │
  │     │ CPU < 30% or Memory < 40%       │
  │     └─────────────────────────────────┘
  │
  └─────────────────────────────────────────→ Time
```

### Implementation

#### Step 1: Enable Metrics Server

```bash
# Install metrics-server for metric collection
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# Verify installation
kubectl get deployment metrics-server -n kube-system
```

#### Step 2: Configure HPA

```yaml
# kubernetes/hpa-config.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-gateway-hpa
  namespace: streamgate
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-gateway
  minReplicas: 3
  maxReplicas: 10
  metrics:
  # CPU-based scaling
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  # Memory-based scaling
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 75
  # Request rate-based scaling
  - type: Pods
    pods:
      metric:
        name: http_requests_per_second
      target:
        type: AverageValue
        averageValue: "1000"
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 50
        periodSeconds: 60
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
      - type: Percent
        value: 100
        periodSeconds: 30
      - type: Pods
        value: 2
        periodSeconds: 30
      selectPolicy: Max
```

#### Step 3: Configure Resource Requests

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
spec:
  replicas: 3
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
        image: streamgate:latest
        ports:
        - containerPort: 8080
        resources:
          requests:
            cpu: 500m           # Minimum CPU
            memory: 512Mi       # Minimum memory
          limits:
            cpu: 1000m          # Maximum CPU
            memory: 1Gi         # Maximum memory
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

#### Step 4: Monitor HPA

```bash
# Watch HPA status
kubectl get hpa -n streamgate -w

# Get detailed HPA status
kubectl describe hpa api-gateway-hpa -n streamgate

# View HPA events
kubectl get events -n streamgate --sort-by='.lastTimestamp' | grep HorizontalPodAutoscaler
```

### Scaling Behavior

```go
type ScalingBehavior struct {
    // Scale up behavior
    ScaleUpPercent      int           // 100% = double replicas
    ScaleUpPeriod       time.Duration // 30 seconds
    
    // Scale down behavior
    ScaleDownPercent    int           // 50% = half replicas
    ScaleDownPeriod     time.Duration // 60 seconds
    StabilizationWindow time.Duration // 300 seconds
}

// Example: Scale up aggressively, scale down conservatively
behavior := ScalingBehavior{
    ScaleUpPercent:      100,
    ScaleUpPeriod:       30 * time.Second,
    ScaleDownPercent:    50,
    ScaleDownPeriod:     60 * time.Second,
    StabilizationWindow: 5 * time.Minute,
}
```

## Vertical Pod Autoscaling

### Overview

Vertical Pod Autoscaling (VPA) automatically adjusts CPU and memory requests/limits based on usage.

```
Resource Usage Analysis
  ↓
Recommend optimal resources
  ↓
Update pod requests/limits
  ↓
Restart pods with new resources
```

### Implementation

#### Step 1: Install VPA

```bash
# Clone VPA repository
git clone https://github.com/kubernetes/autoscaler.git
cd autoscaler/vertical-pod-autoscaler

# Install VPA
./hack/vpa-up.sh
```

#### Step 2: Configure VPA

```yaml
# kubernetes/vpa-config.yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: api-gateway-vpa
  namespace: streamgate
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-gateway
  updatePolicy:
    updateMode: "Auto"  # Auto, Recreate, Initial, Off
  resourcePolicy:
    containerPolicies:
    - containerName: api-gateway
      minAllowed:
        cpu: 100m
        memory: 128Mi
      maxAllowed:
        cpu: 2000m
        memory: 2Gi
      controlledResources:
      - cpu
      - memory
      controlledValues: RequestsAndLimits
```

#### Step 3: Monitor VPA

```bash
# Get VPA recommendations
kubectl describe vpa api-gateway-vpa -n streamgate

# View VPA status
kubectl get vpa -n streamgate

# Check VPA recommender logs
kubectl logs -n kube-system -l app=vpa-recommender
```

### VPA Update Modes

| Mode | Behavior | Use Case |
|------|----------|----------|
| **Off** | No updates | Testing/validation |
| **Initial** | Only on pod creation | Conservative approach |
| **Recreate** | Update and restart pods | Non-critical services |
| **Auto** | Update and restart as needed | Production services |

## Cluster Autoscaling

### Overview

Cluster Autoscaling automatically scales the number of nodes in the cluster.

```
Pod Pending (insufficient resources)
  ↓
Cluster Autoscaler detects
  ↓
Add new node
  ↓
Pod scheduled on new node
```

### Implementation

```bash
# For AWS EKS
eksctl create cluster \
  --name streamgate \
  --region us-east-1 \
  --nodegroup-name standard-nodes \
  --nodes 3 \
  --nodes-min 3 \
  --nodes-max 10 \
  --node-type t3.medium

# Install Cluster Autoscaler
helm repo add autoscaler https://kubernetes.github.io/autoscaler
helm install cluster-autoscaler autoscaler/cluster-autoscaler \
  --namespace kube-system \
  --set autoDiscovery.clusterName=streamgate \
  --set awsRegion=us-east-1
```

## Custom Metrics Scaling

### Overview

Scale based on custom application metrics (e.g., request rate, queue length).

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-gateway-custom-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-gateway
  minReplicas: 3
  maxReplicas: 10
  metrics:
  - type: Pods
    pods:
      metric:
        name: http_requests_per_second
        selector:
          matchLabels:
            metric_type: request_rate
      target:
        type: AverageValue
        averageValue: "1000"
  - type: Pods
    pods:
      metric:
        name: queue_length
      target:
        type: AverageValue
        averageValue: "100"
```

### Implementing Custom Metrics

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "k8s.io/metrics/pkg/apis/custom_metrics/v1beta1"
)

type CustomMetricsProvider struct {
    requestsPerSecond prometheus.Gauge
    queueLength       prometheus.Gauge
}

func (cmp *CustomMetricsProvider) RecordMetrics(ctx context.Context) {
    // Record requests per second
    requestRate := cmp.calculateRequestRate()
    cmp.requestsPerSecond.Set(requestRate)
    
    // Record queue length
    queueLen := cmp.getQueueLength()
    cmp.queueLength.Set(float64(queueLen))
}
```

## Scaling Policies

### CPU-Based Scaling

```yaml
metrics:
- type: Resource
  resource:
    name: cpu
    target:
      type: Utilization
      averageUtilization: 70  # Scale up when CPU > 70%
```

**Configuration**:
- Scale up threshold: 70%
- Scale down threshold: 30%
- Scale up period: 30 seconds
- Scale down period: 60 seconds

### Memory-Based Scaling

```yaml
metrics:
- type: Resource
  resource:
    name: memory
    target:
      type: Utilization
      averageUtilization: 75  # Scale up when memory > 75%
```

**Configuration**:
- Scale up threshold: 75%
- Scale down threshold: 40%
- Scale up period: 30 seconds
- Scale down period: 60 seconds

### Request Rate-Based Scaling

```yaml
metrics:
- type: Pods
  pods:
    metric:
      name: http_requests_per_second
    target:
      type: AverageValue
      averageValue: "1000"  # Scale up when > 1000 req/sec per pod
```

**Configuration**:
- Target: 1000 requests/sec per pod
- Scale up period: 30 seconds
- Scale down period: 60 seconds

## Monitoring & Optimization

### Key Metrics to Monitor

```go
type ScalingMetrics struct {
    CurrentReplicas     int
    DesiredReplicas     int
    CPUUtilization      float64
    MemoryUtilization   float64
    RequestRate         float64
    ScalingEvents       int
    ScalingLatency      time.Duration
}
```

### Scaling Dashboard

```yaml
# Grafana dashboard queries
- Panel: Current vs Desired Replicas
  Query: |
    {
      current: kube_deployment_status_replicas,
      desired: kube_deployment_spec_replicas
    }

- Panel: CPU Utilization
  Query: |
    rate(container_cpu_usage_seconds_total[5m]) / 
    (container_spec_cpu_quota / container_spec_cpu_period)

- Panel: Memory Utilization
  Query: |
    container_memory_usage_bytes / container_spec_memory_limit_bytes

- Panel: Scaling Events
  Query: |
    rate(kube_hpa_status_current_replicas[5m])
```

### Optimization Tips

1. **Set Appropriate Thresholds**
   - CPU: 60-80% (higher = more aggressive)
   - Memory: 70-85% (higher = more aggressive)
   - Request rate: Based on service capacity

2. **Configure Stabilization Windows**
   - Prevent rapid scaling up/down
   - Typical: 300 seconds (5 minutes)

3. **Use Multiple Metrics**
   - Combine CPU, memory, and custom metrics
   - Prevents single metric from dominating

4. **Monitor Scaling Events**
   - Track frequency of scaling
   - Identify patterns
   - Adjust thresholds if needed

5. **Test Scaling Behavior**
   - Load test to verify scaling
   - Test scale-up and scale-down
   - Verify performance during scaling

### Scaling Troubleshooting

**Problem**: Pods not scaling up
- Check metrics server: `kubectl get deployment metrics-server -n kube-system`
- Check HPA status: `kubectl describe hpa <name>`
- Check resource requests: `kubectl describe pod <pod-name>`

**Problem**: Rapid scaling up/down
- Increase stabilization window
- Adjust thresholds
- Check for metric spikes

**Problem**: Scaling too slow
- Decrease scale-up period
- Increase scale-up percentage
- Check node availability

## Best Practices

1. **Always set resource requests and limits**
   - Required for HPA to work
   - Helps scheduler place pods

2. **Use multiple scaling metrics**
   - CPU + Memory + Custom metrics
   - Prevents single point of failure

3. **Monitor scaling behavior**
   - Track scaling events
   - Analyze patterns
   - Adjust as needed

4. **Test scaling procedures**
   - Load test before production
   - Verify scale-up and scale-down
   - Test edge cases

5. **Document scaling policies**
   - Explain thresholds
   - Document rationale
   - Keep updated

## Conclusion

Autoscaling enables StreamGate to:
- Handle variable load automatically
- Optimize resource utilization
- Reduce operational overhead
- Improve cost efficiency
- Maintain performance

---

**Document Status**: Complete  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0
