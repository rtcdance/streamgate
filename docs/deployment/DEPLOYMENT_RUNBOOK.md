# StreamGate Deployment Runbook

**Version**: 1.0.0  
**Last Updated**: 2025-01-29  
**Status**: ✅ Production Ready

## Table of Contents

1. [Pre-Deployment Checklist](#pre-deployment-checklist)
2. [Local Development Deployment](#local-development-deployment)
3. [Docker Compose Deployment](#docker-compose-deployment)
4. [Kubernetes Deployment](#kubernetes-deployment)
5. [Helm Deployment](#helm-deployment)
6. [Verification Steps](#verification-steps)
7. [Rollback Procedures](#rollback-procedures)
8. [Troubleshooting](#troubleshooting)
9. [Monitoring](#monitoring)
10. [Incident Response](#incident-response)

## Pre-Deployment Checklist

### Code Verification

```bash
# 1. Verify code compiles
make build-all
# Expected: All binaries built successfully

# 2. Verify tests pass
make test
# Expected: All tests pass with 100% coverage

# 3. Verify linting passes
make lint
# Expected: No linting errors

# 4. Verify no security issues
golangci-lint run ./... --no-config
# Expected: No security warnings
```

### Environment Verification

```bash
# 1. Check Go version
go version
# Expected: go1.21 or higher

# 2. Check Docker version
docker --version
# Expected: Docker 20.10 or higher

# 3. Check Docker Compose version
docker-compose --version
# Expected: Docker Compose 1.29 or higher

# 4. Check kubectl version (for Kubernetes)
kubectl version --client
# Expected: kubectl 1.24 or higher

# 5. Check Helm version (for Helm)
helm version
# Expected: Helm 3.10 or higher
```

### Configuration Verification

```bash
# 1. Check configuration files exist
ls -la config/config.*.yaml
# Expected: config.dev.yaml, config.prod.yaml, config.test.yaml

# 2. Check environment variables
cat .env.example
# Expected: All required variables documented

# 3. Check Docker files exist
ls -la deploy/docker/Dockerfile.*
# Expected: All Dockerfiles present

# 4. Check Kubernetes manifests exist
ls -la deploy/k8s/*.yaml
# Expected: All manifests present

# 5. Check Helm charts exist
ls -la deploy/helm/
# Expected: Chart.yaml, values.yaml, templates/
```

## Local Development Deployment

### Monolithic Deployment

```bash
# 1. Build monolithic binary
make build-monolith
# Expected: bin/streamgate created

# 2. Set environment variables
export CONFIG_FILE=config/config.dev.yaml
export LOG_LEVEL=debug
export PORT=8080

# 3. Run monolithic service
make run-monolith
# Expected: Service starts on port 8080

# 4. Verify service is running
curl http://localhost:8080/health
# Expected: {"status":"healthy"}

# 5. Stop service
Ctrl+C
```

### Microservices Deployment

```bash
# Terminal 1: API Gateway
make run-api-gateway
# Expected: API Gateway starts on port 8080

# Terminal 2: Auth Service
make run-auth
# Expected: Auth Service starts on port 9001

# Terminal 3: Upload Service
make run-upload
# Expected: Upload Service starts on port 9002

# Terminal 4: Streaming Service
make run-streaming
# Expected: Streaming Service starts on port 9003

# Terminal 5: Transcoder Service
make run-transcoder
# Expected: Transcoder Service starts on port 9004

# Verify all services
curl http://localhost:8080/health
curl http://localhost:9001/health
curl http://localhost:9002/health
curl http://localhost:9003/health
curl http://localhost:9004/health
```

## Docker Compose Deployment

### Build Docker Images

```bash
# 1. Build all Docker images
make docker-build
# Expected: All images built successfully

# 2. Verify images
docker images | grep streamgate
# Expected: 10 images (1 monolith + 9 microservices)

# 3. Check image sizes
docker images --format "table {{.Repository}}\t{{.Size}}" | grep streamgate
# Expected: Images are reasonable size (< 500MB each)
```

### Start Services

```bash
# 1. Start all services
make docker-up
# Expected: All services start successfully

# 2. Verify services are running
docker-compose ps
# Expected: All services show "Up"

# 3. Check service logs
docker-compose logs -f
# Expected: No error messages

# 4. Verify health endpoints
curl http://localhost:8080/health
# Expected: {"status":"healthy"}
```

### Verify Deployment

```bash
# 1. Check API Gateway
curl http://localhost:8080/api/v1/health
# Expected: 200 OK

# 2. Check database connection
docker-compose exec postgres psql -U streamgate -d streamgate -c "SELECT 1"
# Expected: 1

# 3. Check Redis connection
docker-compose exec redis redis-cli ping
# Expected: PONG

# 4. Check MinIO connection
docker-compose exec minio mc ls minio/streamgate
# Expected: Bucket listing

# 5. Check Prometheus metrics
curl http://localhost:9090/api/v1/query?query=up
# Expected: Metrics available
```

### Stop Services

```bash
# 1. Stop all services
make docker-down
# Expected: All services stopped

# 2. Verify services are stopped
docker-compose ps
# Expected: No running services

# 3. Clean up volumes (optional)
docker-compose down -v
# Expected: All volumes removed
```

## Kubernetes Deployment

### Prerequisites

```bash
# 1. Kubernetes cluster running
kubectl cluster-info
# Expected: Cluster info displayed

# 2. kubectl configured
kubectl config current-context
# Expected: Current context displayed

# 3. Sufficient resources
kubectl top nodes
# Expected: Nodes have available resources

# 4. Storage class available
kubectl get storageclass
# Expected: At least one storage class available
```

### Create Namespace

```bash
# 1. Create namespace
kubectl create namespace streamgate
# Expected: namespace/streamgate created

# 2. Verify namespace
kubectl get namespace streamgate
# Expected: Namespace listed

# 3. Set default namespace
kubectl config set-context --current --namespace=streamgate
# Expected: Context updated
```

### Create Secrets

```bash
# 1. Create database secret
kubectl create secret generic db-secret \
  --from-literal=username=streamgate \
  --from-literal=password=<password> \
  -n streamgate
# Expected: secret/db-secret created

# 2. Create storage secret
kubectl create secret generic storage-secret \
  --from-literal=access-key=<key> \
  --from-literal=secret-key=<secret> \
  -n streamgate
# Expected: secret/storage-secret created

# 3. Verify secrets
kubectl get secrets -n streamgate
# Expected: Secrets listed
```

### Deploy Services

```bash
# 1. Apply ConfigMap
kubectl apply -f deploy/k8s/configmap.yaml
# Expected: configmap/streamgate-config created

# 2. Apply RBAC
kubectl apply -f deploy/k8s/rbac.yaml
# Expected: serviceaccount, role, rolebinding created

# 3. Deploy microservices
kubectl apply -f deploy/k8s/microservices/
# Expected: All deployments created

# 4. Verify deployments
kubectl get deployments -n streamgate
# Expected: All deployments listed

# 5. Verify pods
kubectl get pods -n streamgate
# Expected: All pods running

# 6. Verify services
kubectl get services -n streamgate
# Expected: All services listed
```

### Verify Deployment

```bash
# 1. Check pod status
kubectl get pods -n streamgate -o wide
# Expected: All pods in Running state

# 2. Check pod logs
kubectl logs deployment/api-gateway -n streamgate
# Expected: No error messages

# 3. Check service endpoints
kubectl get endpoints -n streamgate
# Expected: All endpoints have addresses

# 4. Port forward to API Gateway
kubectl port-forward svc/api-gateway 8080:8080 -n streamgate
# Expected: Forwarding established

# 5. Test API (in another terminal)
curl http://localhost:8080/health
# Expected: {"status":"healthy"}
```

## Helm Deployment

### Prerequisites

```bash
# 1. Helm installed
helm version
# Expected: Helm version displayed

# 2. Helm repository configured
helm repo list
# Expected: Repositories listed

# 3. Chart values reviewed
cat deploy/helm/values.yaml
# Expected: Configuration values displayed
```

### Install Release

```bash
# 1. Create namespace
kubectl create namespace streamgate
# Expected: namespace/streamgate created

# 2. Install Helm release
helm install streamgate deploy/helm/ \
  -n streamgate \
  --values deploy/helm/values.yaml
# Expected: release "streamgate" installed

# 3. Verify installation
helm list -n streamgate
# Expected: Release listed

# 4. Check release status
helm status streamgate -n streamgate
# Expected: Release status displayed
```

### Verify Deployment

```bash
# 1. Check resources
kubectl get all -n streamgate
# Expected: All resources listed

# 2. Check pods
kubectl get pods -n streamgate
# Expected: All pods running

# 3. Check services
kubectl get services -n streamgate
# Expected: All services listed

# 4. Check ingress
kubectl get ingress -n streamgate
# Expected: Ingress configured (if applicable)
```

### Upgrade Release

```bash
# 1. Update values
vim deploy/helm/values.yaml

# 2. Upgrade release
helm upgrade streamgate deploy/helm/ \
  -n streamgate \
  --values deploy/helm/values.yaml
# Expected: release "streamgate" upgraded

# 3. Verify upgrade
helm status streamgate -n streamgate
# Expected: Release status updated

# 4. Check rollout status
kubectl rollout status deployment/api-gateway -n streamgate
# Expected: Rollout complete
```

## Verification Steps

### Health Checks

```bash
# 1. API Gateway health
curl http://localhost:8080/health

# 2. Auth service health
curl http://localhost:9001/health

# 3. Database health
curl http://localhost:8080/api/v1/health/db

# 4. Cache health
curl http://localhost:8080/api/v1/health/cache

# 5. Storage health
curl http://localhost:8080/api/v1/health/storage
```

### Smoke Tests

```bash
# 1. Create user
curl -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{"username":"test","email":"test@example.com"}'

# 2. Upload content
curl -X POST http://localhost:8080/api/v1/content \
  -F "file=@test.mp4"

# 3. Stream content
curl http://localhost:8080/api/v1/content/{id}/stream

# 4. Create NFT
curl -X POST http://localhost:8080/api/v1/nft \
  -H "Content-Type: application/json" \
  -d '{"content_id":"{id}","chain":"ethereum"}'

# 5. Verify metrics
curl http://localhost:9090/api/v1/query?query=up
```

## Rollback Procedures

### Docker Compose Rollback

```bash
# 1. Stop current services
docker-compose down

# 2. Checkout previous version
git checkout <previous-tag>

# 3. Rebuild images
make docker-build

# 4. Start services
docker-compose up -d

# 5. Verify services
docker-compose ps
```

### Kubernetes Rollback

```bash
# 1. Check rollout history
kubectl rollout history deployment/api-gateway -n streamgate

# 2. Rollback to previous version
kubectl rollout undo deployment/api-gateway -n streamgate

# 3. Verify rollback
kubectl rollout status deployment/api-gateway -n streamgate

# 4. Check pod status
kubectl get pods -n streamgate
```

### Helm Rollback

```bash
# 1. Check release history
helm history streamgate -n streamgate

# 2. Rollback to previous release
helm rollback streamgate 1 -n streamgate

# 3. Verify rollback
helm status streamgate -n streamgate

# 4. Check pod status
kubectl get pods -n streamgate
```

## Troubleshooting

### Service Won't Start

```bash
# 1. Check logs
docker-compose logs <service>
# or
kubectl logs deployment/<service> -n streamgate

# 2. Check configuration
cat config/config.yaml

# 3. Check dependencies
docker-compose ps
# or
kubectl get pods -n streamgate

# 4. Check resource limits
docker stats
# or
kubectl top pods -n streamgate

# 5. Restart service
docker-compose restart <service>
# or
kubectl rollout restart deployment/<service> -n streamgate
```

### Database Connection Failed

```bash
# 1. Check database status
docker-compose ps postgres
# or
kubectl get pod postgres -n streamgate

# 2. Check connection string
grep DATABASE config/config.yaml

# 3. Test connection
psql -h localhost -U streamgate -d streamgate

# 4. Check database logs
docker-compose logs postgres
# or
kubectl logs pod/postgres -n streamgate

# 5. Restart database
docker-compose restart postgres
# or
kubectl rollout restart deployment/postgres -n streamgate
```

### High Memory Usage

```bash
# 1. Check memory usage
docker stats
# or
kubectl top pods -n streamgate

# 2. Check for memory leaks
go tool pprof http://localhost:6060/debug/pprof/heap

# 3. Check garbage collection
curl http://localhost:6060/debug/pprof/gc

# 4. Restart service
docker-compose restart <service>
# or
kubectl rollout restart deployment/<service> -n streamgate

# 5. Increase memory limit
# Edit docker-compose.yml or Kubernetes manifest
```

### High Latency

```bash
# 1. Check metrics
curl http://localhost:9090/metrics

# 2. Check traces
http://localhost:16686

# 3. Check database performance
EXPLAIN ANALYZE <query>

# 4. Check cache hit rate
redis-cli INFO stats

# 5. Check network latency
ping <service-host>
```

## Monitoring

### Prometheus

```bash
# Access Prometheus
http://localhost:9090

# Query metrics
- streamgate_requests_total
- streamgate_request_duration_seconds
- streamgate_errors_total
- streamgate_database_connections
- streamgate_cache_hits
- streamgate_cache_misses
```

### Grafana

```bash
# Access Grafana
http://localhost:3000

# Default credentials
Username: admin
Password: admin

# Available dashboards
- System Overview
- API Gateway
- Database Performance
- Cache Performance
- Error Tracking
```

### Jaeger

```bash
# Access Jaeger
http://localhost:16686

# Trace services
- api-gateway
- auth
- content
- streaming
- transcoding
- upload
```

## Incident Response

### Critical Issue Response

```bash
# 1. Assess severity
# - Is service down?
# - Are users affected?
# - Is data at risk?

# 2. Immediate action
# - Rollback if necessary
# - Scale up resources
# - Enable maintenance mode

# 3. Investigation
# - Check logs
# - Check metrics
# - Check traces

# 4. Resolution
# - Fix root cause
# - Deploy fix
# - Verify resolution

# 5. Post-incident
# - Document issue
# - Update runbooks
# - Schedule retrospective
```

### Escalation Path

```
Level 1: On-call engineer
  - Assess issue
  - Check runbooks
  - Attempt resolution

Level 2: Team lead
  - Escalate if Level 1 cannot resolve
  - Coordinate response
  - Communicate status

Level 3: Engineering manager
  - Escalate if Level 2 cannot resolve
  - Coordinate across teams
  - Executive communication

Level 4: Director
  - Escalate if Level 3 cannot resolve
  - Executive decision making
  - Customer communication
```

## Conclusion

This runbook provides step-by-step procedures for deploying and managing StreamGate in various environments. Always follow the pre-deployment checklist and verification steps before considering deployment complete.

---

**Version**: 1.0.0  
**Last Updated**: 2025-01-29  
**Status**: ✅ Production Ready
