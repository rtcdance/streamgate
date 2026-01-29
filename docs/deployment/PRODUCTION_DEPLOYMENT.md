# StreamGate Production Deployment Guide

**Date**: 2025-01-28  
**Status**: Production-Ready  
**Version**: 1.0.0

## Executive Summary

This guide provides step-by-step instructions for deploying StreamGate to production. The system is production-ready with comprehensive monitoring, security hardening, and performance optimization.

## Pre-Deployment Checklist

### Infrastructure Requirements

- [ ] Kubernetes cluster (1.24+) with 3+ nodes
- [ ] PostgreSQL 15+ database
- [ ] Redis 7+ cache
- [ ] MinIO or S3 object storage
- [ ] NATS message broker
- [ ] Consul service registry
- [ ] Prometheus monitoring
- [ ] Grafana dashboards
- [ ] Jaeger tracing
- [ ] Load balancer (Nginx/Envoy)

### Security Requirements

- [ ] TLS certificates (Let's Encrypt or CA)
- [ ] API keys and secrets configured
- [ ] Database credentials secured
- [ ] Private keys for blockchain operations
- [ ] Rate limiting configured
- [ ] Firewall rules configured
- [ ] DDoS protection enabled
- [ ] WAF rules configured

### Monitoring Requirements

- [ ] Prometheus scrape targets configured
- [ ] Grafana dashboards created
- [ ] Alert rules configured
- [ ] Log aggregation setup
- [ ] Distributed tracing enabled
- [ ] Health check endpoints verified
- [ ] Backup and recovery tested

### Performance Requirements

- [ ] Load testing completed
- [ ] Performance targets verified
- [ ] Cache hit rate > 80%
- [ ] API response time < 200ms (P95)
- [ ] Error rate < 1%
- [ ] Throughput > 1000 req/sec

## Deployment Steps

### Step 1: Prepare Infrastructure

#### 1.1 Create Kubernetes Namespace

```bash
kubectl create namespace streamgate
kubectl label namespace streamgate environment=production
```

#### 1.2 Create Secrets

```bash
# Database credentials
kubectl create secret generic db-credentials \
  --from-literal=username=streamgate \
  --from-literal=password=$(openssl rand -base64 32) \
  -n streamgate

# API keys
kubectl create secret generic api-keys \
  --from-literal=jwt-secret=$(openssl rand -base64 32) \
  --from-literal=api-key=$(openssl rand -base64 32) \
  -n streamgate

# Blockchain keys
kubectl create secret generic blockchain-keys \
  --from-literal=private-key=$(cat private-key.txt) \
  --from-literal=mnemonic=$(cat mnemonic.txt) \
  -n streamgate

# TLS certificates
kubectl create secret tls streamgate-tls \
  --cert=cert.pem \
  --key=key.pem \
  -n streamgate
```

#### 1.3 Create ConfigMaps

```bash
kubectl create configmap streamgate-config \
  --from-file=config/config.prod.yaml \
  -n streamgate

kubectl create configmap prometheus-config \
  --from-file=config/prometheus.yml \
  -n streamgate
```

### Step 2: Deploy Infrastructure Services

#### 2.1 Deploy PostgreSQL

```bash
# Using Helm
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install postgres bitnami/postgresql \
  --set auth.username=streamgate \
  --set auth.password=$(openssl rand -base64 32) \
  --set primary.persistence.size=100Gi \
  -n streamgate
```

#### 2.2 Deploy Redis

```bash
helm install redis bitnami/redis \
  --set auth.enabled=true \
  --set auth.password=$(openssl rand -base64 32) \
  --set master.persistence.size=50Gi \
  -n streamgate
```

#### 2.3 Deploy NATS

```bash
helm install nats nats/nats \
  --set nats.jetstream.enabled=true \
  --set persistence.enabled=true \
  --set persistence.size=50Gi \
  -n streamgate
```

#### 2.4 Deploy Consul

```bash
helm install consul hashicorp/consul \
  --set server.replicas=3 \
  --set client.enabled=true \
  --set ui.enabled=true \
  -n streamgate
```

### Step 3: Deploy StreamGate Services

#### 3.1 Build and Push Docker Images

```bash
# Build all images
make docker-build

# Tag images
docker tag streamgate:latest registry.example.com/streamgate:latest
docker tag streamgate-api-gateway:latest registry.example.com/streamgate-api-gateway:latest
# ... tag other services

# Push to registry
docker push registry.example.com/streamgate:latest
docker push registry.example.com/streamgate-api-gateway:latest
# ... push other services
```

#### 3.2 Deploy Services

```bash
# Apply Kubernetes manifests
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/secret.yaml
kubectl apply -f deploy/k8s/microservices/

# Verify deployments
kubectl get deployments -n streamgate
kubectl get pods -n streamgate
```

#### 3.3 Deploy Ingress

```bash
kubectl apply -f deploy/k8s/monolith/ingress.yaml

# Verify ingress
kubectl get ingress -n streamgate
```

### Step 4: Deploy Monitoring Stack

#### 4.1 Deploy Prometheus

```bash
helm install prometheus prometheus-community/kube-prometheus-stack \
  --set prometheus.prometheusSpec.retention=30d \
  --set prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage=100Gi \
  -n streamgate
```

#### 4.2 Deploy Grafana

```bash
helm install grafana grafana/grafana \
  --set adminPassword=$(openssl rand -base64 32) \
  --set persistence.enabled=true \
  --set persistence.size=10Gi \
  -n streamgate
```

#### 4.3 Deploy Jaeger

```bash
helm install jaeger jaegertracing/jaeger \
  --set storage.type=elasticsearch \
  --set elasticsearch.enabled=true \
  -n streamgate
```

### Step 5: Configure Monitoring

#### 5.1 Add Prometheus Scrape Targets

```yaml
# config/prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'streamgate'
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names:
            - streamgate
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
        action: replace
        target_label: __metrics_path__
        regex: (.+)
      - source_labels: [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        regex: ([^:]+)(?::\d+)?;(\d+)
        replacement: $1:$2
        target_label: __address__
```

#### 5.2 Create Grafana Dashboards

```bash
# Import dashboards
kubectl exec -it grafana-0 -n streamgate -- \
  grafana-cli admin reset-admin-password $(openssl rand -base64 32)

# Access Grafana
kubectl port-forward svc/grafana 3000:80 -n streamgate
# Open http://localhost:3000
```

#### 5.3 Configure Alert Rules

```yaml
# config/alert-rules.yaml
groups:
  - name: streamgate
    interval: 30s
    rules:
      - alert: HighErrorRate
        expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.05
        for: 5m
        annotations:
          summary: "High error rate detected"

      - alert: HighLatency
        expr: histogram_quantile(0.95, http_request_duration_seconds) > 0.5
        for: 5m
        annotations:
          summary: "High latency detected"

      - alert: ServiceDown
        expr: up{job="streamgate"} == 0
        for: 1m
        annotations:
          summary: "Service is down"
```

### Step 6: Database Setup

#### 6.1 Run Migrations

```bash
# Connect to PostgreSQL
kubectl exec -it postgres-0 -n streamgate -- psql -U streamgate

# Run migrations
psql -U streamgate -d streamgate -f migrations/001_init_schema.sql
psql -U streamgate -d streamgate -f migrations/002_add_content_table.sql
psql -U streamgate -d streamgate -f migrations/003_add_user_table.sql
psql -U streamgate -d streamgate -f migrations/004_add_nft_table.sql
psql -U streamgate -d streamgate -f migrations/005_add_transaction_table.sql
```

#### 6.2 Verify Database

```bash
# Check tables
kubectl exec -it postgres-0 -n streamgate -- \
  psql -U streamgate -d streamgate -c "\dt"

# Check indexes
kubectl exec -it postgres-0 -n streamgate -- \
  psql -U streamgate -d streamgate -c "\di"
```

### Step 7: Verify Deployment

#### 7.1 Check Service Status

```bash
# Check all pods
kubectl get pods -n streamgate

# Check services
kubectl get svc -n streamgate

# Check ingress
kubectl get ingress -n streamgate
```

#### 7.2 Test API Endpoints

```bash
# Get API Gateway IP
API_IP=$(kubectl get svc api-gateway -n streamgate -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Test health endpoint
curl http://$API_IP:9090/health

# Test API endpoint
curl http://$API_IP:9090/api/v1/content

# Test metrics endpoint
curl http://$API_IP:9090/metrics
```

#### 7.3 Verify Monitoring

```bash
# Check Prometheus targets
kubectl port-forward svc/prometheus 9090:9090 -n streamgate
# Open http://localhost:9090/targets

# Check Grafana dashboards
kubectl port-forward svc/grafana 3000:80 -n streamgate
# Open http://localhost:3000

# Check Jaeger traces
kubectl port-forward svc/jaeger-query 16686:16686 -n streamgate
# Open http://localhost:16686
```

### Step 8: Performance Validation

#### 8.1 Run Load Tests

```bash
# Run performance tests
go test -v ./test/performance/...

# Run load tests
go test -v ./test/load/...

# Run security audit
go test -v ./test/security/...
```

#### 8.2 Verify Performance Targets

```bash
# Check metrics
kubectl exec -it prometheus-0 -n streamgate -- \
  promtool query instant 'rate(http_requests_total[5m])'

# Check latency
kubectl exec -it prometheus-0 -n streamgate -- \
  promtool query instant 'histogram_quantile(0.95, http_request_duration_seconds)'

# Check error rate
kubectl exec -it prometheus-0 -n streamgate -- \
  promtool query instant 'rate(http_requests_total{status=~"5.."}[5m])'
```

### Step 9: Backup and Recovery

#### 9.1 Configure Database Backups

```bash
# Create backup script
cat > backup-db.sh << 'EOF'
#!/bin/bash
BACKUP_DIR="/backups/postgres"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

kubectl exec -it postgres-0 -n streamgate -- \
  pg_dump -U streamgate streamgate > $BACKUP_DIR/backup_$TIMESTAMP.sql

# Keep only last 30 days
find $BACKUP_DIR -name "backup_*.sql" -mtime +30 -delete
EOF

chmod +x backup-db.sh

# Schedule backup (cron)
0 2 * * * /path/to/backup-db.sh
```

#### 9.2 Configure Redis Backups

```bash
# Enable Redis persistence
kubectl exec -it redis-master-0 -n streamgate -- \
  redis-cli CONFIG SET save "900 1 300 10 60 10000"
```

#### 9.3 Test Recovery

```bash
# Backup current state
kubectl get all -n streamgate -o yaml > backup.yaml

# Simulate failure
kubectl delete pod api-gateway-0 -n streamgate

# Verify recovery
kubectl get pods -n streamgate
```

### Step 10: Security Hardening

#### 10.1 Enable Network Policies

```yaml
# deploy/k8s/network-policy.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: streamgate-network-policy
  namespace: streamgate
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              name: streamgate
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              name: streamgate
    - to:
        - podSelector: {}
      ports:
        - protocol: TCP
          port: 53
        - protocol: UDP
          port: 53
```

#### 10.2 Enable Pod Security Policies

```yaml
# deploy/k8s/pod-security-policy.yaml
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: streamgate-psp
spec:
  privileged: false
  allowPrivilegeEscalation: false
  requiredDropCapabilities:
    - ALL
  volumes:
    - 'configMap'
    - 'emptyDir'
    - 'projected'
    - 'secret'
    - 'downwardAPI'
    - 'persistentVolumeClaim'
  runAsUser:
    rule: 'MustRunAsNonRoot'
  seLinux:
    rule: 'MustRunAs'
    seLinuxOptions:
      level: "s0:c123,c456"
  fsGroup:
    rule: 'MustRunAs'
    ranges:
      - min: 1000
        max: 65535
  readOnlyRootFilesystem: false
```

#### 10.3 Enable RBAC

```yaml
# deploy/k8s/rbac.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: streamgate-role
  namespace: streamgate
rules:
  - apiGroups: [""]
    resources: ["pods", "services"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: streamgate-rolebinding
  namespace: streamgate
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: streamgate-role
subjects:
  - kind: ServiceAccount
    name: streamgate
    namespace: streamgate
```

## Post-Deployment Verification

### Checklist

- [ ] All pods are running
- [ ] All services are accessible
- [ ] Database migrations completed
- [ ] Monitoring is collecting metrics
- [ ] Alerts are configured
- [ ] Backups are running
- [ ] Performance targets met
- [ ] Security audit passed
- [ ] Load tests passed
- [ ] Documentation updated

### Monitoring Dashboard

Access Grafana dashboard:
```bash
kubectl port-forward svc/grafana 3000:80 -n streamgate
# Open http://localhost:3000
# Username: admin
# Password: (from deployment)
```

### Health Checks

```bash
# API Gateway health
curl http://api-gateway:9090/health

# Service health
curl http://upload:9091/health
curl http://streaming:9093/health
curl http://metadata:9005/health

# Database health
kubectl exec -it postgres-0 -n streamgate -- \
  psql -U streamgate -d streamgate -c "SELECT 1"

# Redis health
kubectl exec -it redis-master-0 -n streamgate -- \
  redis-cli ping
```

## Troubleshooting

### Pod Not Starting

```bash
# Check pod logs
kubectl logs <pod-name> -n streamgate

# Check pod events
kubectl describe pod <pod-name> -n streamgate

# Check resource usage
kubectl top pod <pod-name> -n streamgate
```

### Service Not Accessible

```bash
# Check service endpoints
kubectl get endpoints <service-name> -n streamgate

# Check ingress
kubectl describe ingress <ingress-name> -n streamgate

# Test connectivity
kubectl exec -it <pod-name> -n streamgate -- \
  curl http://<service-name>:9090/health
```

### High Latency

```bash
# Check metrics
kubectl exec -it prometheus-0 -n streamgate -- \
  promtool query instant 'histogram_quantile(0.95, http_request_duration_seconds)'

# Check resource usage
kubectl top nodes
kubectl top pods -n streamgate

# Check logs for errors
kubectl logs -f <pod-name> -n streamgate
```

## Rollback Procedure

### If Deployment Fails

```bash
# Rollback deployment
kubectl rollout undo deployment/<service-name> -n streamgate

# Check rollout status
kubectl rollout status deployment/<service-name> -n streamgate

# Verify services are running
kubectl get pods -n streamgate
```

## Maintenance

### Regular Tasks

- [ ] Monitor disk usage
- [ ] Review logs for errors
- [ ] Update dependencies
- [ ] Run security scans
- [ ] Test backup/recovery
- [ ] Review performance metrics
- [ ] Update documentation

### Scaling

```bash
# Scale deployment
kubectl scale deployment api-gateway --replicas=5 -n streamgate

# Check scaling
kubectl get deployment api-gateway -n streamgate
```

### Updates

```bash
# Update image
kubectl set image deployment/api-gateway \
  api-gateway=registry.example.com/streamgate-api-gateway:v1.1.0 \
  -n streamgate

# Check rollout
kubectl rollout status deployment/api-gateway -n streamgate
```

## Support

For issues or questions:
1. Check logs: `kubectl logs <pod-name> -n streamgate`
2. Check metrics: Access Grafana dashboard
3. Check traces: Access Jaeger UI
4. Review documentation: See `docs/` directory

---

**Deployment Status**: ✅ PRODUCTION-READY  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0
