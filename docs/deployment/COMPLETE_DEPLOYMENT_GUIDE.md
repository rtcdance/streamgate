# StreamGate Complete Deployment Guide

**Version**: 1.0.0  
**Last Updated**: 2025-01-29  
**Status**: ✅ Complete

## Table of Contents

1. [Overview](#overview)
2. [Local Development](#local-development)
3. [Docker Compose](#docker-compose)
4. [Kubernetes](#kubernetes)
5. [Cloud Deployment](#cloud-deployment)
6. [Monitoring Setup](#monitoring-setup)
7. [Troubleshooting](#troubleshooting)

## Overview

StreamGate supports multiple deployment modes:

| Mode | Use Case | Complexity | Scalability |
|------|----------|-----------|------------|
| **Local Development** | Development & testing | Low | Single machine |
| **Docker Compose** | Testing & staging | Medium | Single host |
| **Kubernetes** | Production | High | Multi-node cluster |
| **Cloud (AWS/GCP/Azure)** | Enterprise | High | Global scale |

## Local Development

### Prerequisites

```bash
# macOS
brew install go postgresql redis ffmpeg

# Ubuntu/Debian
sudo apt-get install golang-go postgresql redis-server ffmpeg

# Verify installations
go version          # Go 1.21+
psql --version      # PostgreSQL 15+
redis-cli --version # Redis 7+
ffmpeg -version     # FFmpeg 4.4+
```

### Setup Steps

```bash
# 1. Clone repository
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# 2. Install Go dependencies
go mod download
go mod tidy

# 3. Create environment file
cp .env.example .env

# 4. Edit .env with local settings
cat > .env << EOF
# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=streamgate
DB_PASSWORD=streamgate
DB_NAME=streamgate

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379

# Storage
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin

# Web3
ETH_RPC_URL=https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY
POLYGON_RPC_URL=https://polygon-mainnet.g.alchemy.com/v2/YOUR_KEY

# Server
PORT=8080
ENV=development
EOF

# 5. Start PostgreSQL
# macOS
brew services start postgresql

# Ubuntu
sudo systemctl start postgresql

# 6. Create database
createdb streamgate
psql streamgate < migrations/001_init_schema.sql

# 7. Start Redis
# macOS
brew services start redis

# Ubuntu
sudo systemctl start redis-server

# 8. Build monolithic binary
make build-monolith

# 9. Run service
./bin/streamgate

# 10. Verify
curl http://localhost:8080/api/v1/health
```

### Development Commands

```bash
# Run tests
make test

# Run with hot reload (requires air)
go install github.com/cosmtrek/air@latest
air

# Format code
make fmt

# Lint code
make lint

# Generate coverage report
make coverage

# Build for different OS
GOOS=linux GOARCH=amd64 go build -o bin/streamgate-linux

# Debug with delve
go install github.com/go-delve/delve/cmd/dlv@latest
dlv debug ./cmd/monolith/streamgate
```

## Docker Compose

### Prerequisites

```bash
# Install Docker
# macOS: https://docs.docker.com/desktop/install/mac-install/
# Ubuntu: https://docs.docker.com/engine/install/ubuntu/

# Verify installation
docker --version
docker-compose --version
```

### Quick Start

```bash
# 1. Clone repository
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# 2. Create environment file
cp .env.example .env

# 3. Start all services
docker-compose up -d

# 4. Check service status
docker-compose ps

# 5. View logs
docker-compose logs -f

# 6. Access services
# API Gateway: http://localhost:8080
# Consul UI: http://localhost:8500
# Prometheus: http://localhost:9090
# Grafana: http://localhost:3000
# Jaeger: http://localhost:16686
```

### Service Ports

| Service | Port | URL |
|---------|------|-----|
| API Gateway | 8080 | http://localhost:8080 |
| PostgreSQL | 5432 | localhost:5432 |
| Redis | 6379 | localhost:6379 |
| MinIO | 9000 | http://localhost:9000 |
| NATS | 4222 | localhost:4222 |
| Consul | 8500 | http://localhost:8500 |
| Prometheus | 9090 | http://localhost:9090 |
| Grafana | 3000 | http://localhost:3000 |
| Jaeger | 16686 | http://localhost:16686 |

### Docker Compose Commands

```bash
# Start services
docker-compose up -d

# Stop services
docker-compose down

# View logs
docker-compose logs -f [service_name]

# Rebuild images
docker-compose build --no-cache

# Scale service
docker-compose up -d --scale transcoder=3

# Execute command in container
docker-compose exec api-gateway bash

# Remove volumes (WARNING: deletes data)
docker-compose down -v
```

### Custom Configuration

```bash
# Override environment variables
docker-compose -f docker-compose.yml \
  -e DB_HOST=custom-db \
  -e REDIS_HOST=custom-redis \
  up -d

# Use custom compose file
docker-compose -f docker-compose.prod.yml up -d

# Set resource limits
docker-compose up -d --memory 2g --cpus 2
```

## Kubernetes

### Prerequisites

```bash
# Install kubectl
# macOS
brew install kubectl

# Ubuntu
sudo apt-get install kubectl

# Install Helm
# macOS
brew install helm

# Ubuntu
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

# Verify installation
kubectl version --client
helm version

# Create Kubernetes cluster
# Option 1: Local (minikube)
brew install minikube
minikube start --cpus=4 --memory=8192

# Option 2: Cloud (GKE)
gcloud container clusters create streamgate \
  --num-nodes=3 \
  --machine-type=n1-standard-2 \
  --zone=us-central1-a

# Option 3: Cloud (EKS)
eksctl create cluster --name streamgate --region us-east-1
```

### Deployment Steps

```bash
# 1. Create namespace
kubectl create namespace streamgate

# 2. Create secrets
kubectl create secret generic streamgate-secrets \
  --from-literal=db-password=streamgate \
  --from-literal=redis-password=streamgate \
  --from-literal=jwt-secret=your-secret-key \
  -n streamgate

# 3. Create ConfigMap
kubectl create configmap streamgate-config \
  --from-file=config/config.yaml \
  -n streamgate

# 4. Deploy using Helm
helm install streamgate ./deploy/helm \
  -n streamgate \
  --values deploy/helm/values.yaml

# 5. Verify deployment
kubectl get pods -n streamgate
kubectl get svc -n streamgate

# 6. Check pod logs
kubectl logs -f deployment/api-gateway -n streamgate

# 7. Port forward for local access
kubectl port-forward svc/api-gateway 8080:8080 -n streamgate
```

### Kubernetes Manifests

```bash
# Deploy individual services
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/secret.yaml
kubectl apply -f deploy/k8s/microservices/

# Deploy monitoring
kubectl apply -f deploy/k8s/monitoring/

# Deploy ingress
kubectl apply -f deploy/k8s/ingress.yaml

# Deploy autoscaling
kubectl apply -f deploy/k8s/hpa-config.yaml
kubectl apply -f deploy/k8s/vpa-config.yaml
```

### Scaling

```bash
# Manual scaling
kubectl scale deployment api-gateway --replicas=5 -n streamgate

# Autoscaling (HPA)
kubectl autoscale deployment transcoder \
  --min=2 --max=10 \
  --cpu-percent=80 \
  -n streamgate

# Check HPA status
kubectl get hpa -n streamgate
kubectl describe hpa transcoder -n streamgate
```

### Monitoring

```bash
# Access Prometheus
kubectl port-forward svc/prometheus 9090:9090 -n streamgate

# Access Grafana
kubectl port-forward svc/grafana 3000:3000 -n streamgate

# Access Jaeger
kubectl port-forward svc/jaeger 16686:16686 -n streamgate

# View metrics
kubectl top nodes
kubectl top pods -n streamgate
```

### Troubleshooting

```bash
# Check pod status
kubectl describe pod <pod-name> -n streamgate

# View pod logs
kubectl logs <pod-name> -n streamgate

# Execute command in pod
kubectl exec -it <pod-name> -n streamgate -- bash

# Check events
kubectl get events -n streamgate

# Debug pod
kubectl debug <pod-name> -n streamgate -it --image=busybox
```

## Cloud Deployment

### AWS (ECS/EKS)

```bash
# 1. Create ECR repository
aws ecr create-repository --repository-name streamgate

# 2. Build and push images
docker build -t streamgate:latest .
docker tag streamgate:latest <account-id>.dkr.ecr.us-east-1.amazonaws.com/streamgate:latest
docker push <account-id>.dkr.ecr.us-east-1.amazonaws.com/streamgate:latest

# 3. Create EKS cluster
eksctl create cluster --name streamgate --region us-east-1

# 4. Deploy to EKS
kubectl apply -f deploy/k8s/

# 5. Setup RDS for PostgreSQL
aws rds create-db-instance \
  --db-instance-identifier streamgate-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --master-username admin \
  --master-user-password <password>

# 6. Setup ElastiCache for Redis
aws elasticache create-cache-cluster \
  --cache-cluster-id streamgate-cache \
  --cache-node-type cache.t3.micro \
  --engine redis

# 7. Setup S3 for storage
aws s3 mb s3://streamgate-content

# 8. Setup CloudFront for CDN
aws cloudfront create-distribution \
  --origin-domain-name streamgate-content.s3.amazonaws.com
```

### Google Cloud (GKE)

```bash
# 1. Create GKE cluster
gcloud container clusters create streamgate \
  --num-nodes=3 \
  --machine-type=n1-standard-2 \
  --zone=us-central1-a

# 2. Get credentials
gcloud container clusters get-credentials streamgate --zone=us-central1-a

# 3. Create Cloud SQL instance
gcloud sql instances create streamgate-db \
  --database-version=POSTGRES_15 \
  --tier=db-f1-micro

# 4. Create Cloud Memorystore (Redis)
gcloud redis instances create streamgate-cache \
  --size=1 \
  --region=us-central1

# 5. Create Cloud Storage bucket
gsutil mb gs://streamgate-content

# 6. Deploy to GKE
kubectl apply -f deploy/k8s/

# 7. Setup Cloud CDN
gcloud compute backend-buckets create streamgate-cdn \
  --gcs-bucket-name=streamgate-content \
  --enable-cdn
```

### Azure (AKS)

```bash
# 1. Create resource group
az group create --name streamgate --location eastus

# 2. Create AKS cluster
az aks create \
  --resource-group streamgate \
  --name streamgate-cluster \
  --node-count 3 \
  --vm-set-type VirtualMachineScaleSets

# 3. Get credentials
az aks get-credentials --resource-group streamgate --name streamgate-cluster

# 4. Create Azure Database for PostgreSQL
az postgres server create \
  --resource-group streamgate \
  --name streamgate-db \
  --location eastus \
  --admin-user admin \
  --admin-password <password>

# 5. Create Azure Cache for Redis
az redis create \
  --resource-group streamgate \
  --name streamgate-cache \
  --location eastus \
  --sku Basic \
  --vm-size c0

# 6. Create Storage Account
az storage account create \
  --resource-group streamgate \
  --name streamgatecontent

# 7. Deploy to AKS
kubectl apply -f deploy/k8s/
```

## Monitoring Setup

### Prometheus

```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'streamgate'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
```

### Grafana

```bash
# 1. Access Grafana
# http://localhost:3000
# Default: admin/admin

# 2. Add Prometheus data source
# URL: http://prometheus:9090

# 3. Import dashboards
# Dashboard ID: 1860 (Node Exporter)
# Dashboard ID: 3662 (Prometheus)
```

### Alerting

```yaml
# alert.rules.yml
groups:
  - name: streamgate
    rules:
      - alert: HighErrorRate
        expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.05
        for: 5m
        annotations:
          summary: "High error rate detected"

      - alert: HighLatency
        expr: histogram_quantile(0.95, http_request_duration_seconds) > 1
        for: 5m
        annotations:
          summary: "High latency detected"
```

## Troubleshooting

### Common Issues

#### Service won't start

```bash
# Check logs
docker-compose logs api-gateway

# Check port availability
lsof -i :8080

# Check environment variables
env | grep STREAMGATE

# Check database connection
psql -h localhost -U streamgate -d streamgate -c "SELECT 1"
```

#### High memory usage

```bash
# Check memory usage
docker stats

# Reduce cache size
# Edit config.yaml
cache:
  max_size: 1000000000  # 1GB

# Restart service
docker-compose restart api-gateway
```

#### Slow performance

```bash
# Check database queries
# Enable query logging in PostgreSQL
ALTER SYSTEM SET log_statement = 'all';
SELECT pg_reload_conf();

# Check Redis performance
redis-cli --stat

# Check network latency
ping <service-host>
```

#### Connection refused

```bash
# Check if service is running
docker-compose ps

# Check firewall rules
sudo ufw status

# Check port binding
netstat -tlnp | grep 8080

# Restart service
docker-compose restart api-gateway
```

---

**Last Updated**: 2025-01-29  
**Version**: 1.0.0  
**Status**: ✅ Complete
