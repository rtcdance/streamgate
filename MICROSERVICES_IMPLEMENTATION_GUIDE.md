# StreamGate Microservices Implementation Guide

**Date**: 2025-01-28  
**Status**: Implementation Guide  
**Priority**: High  
**Version**: 1.0.0

## Overview

This guide documents the implementation of StreamGate microservices. The system uses a plugin architecture where each microservice is implemented as a plugin with its own HTTP server.

## Architecture

### Plugin Architecture
Each microservice follows this pattern:

```
cmd/microservices/{service}/main.go
  ↓
pkg/plugins/{service}/plugin.go (Plugin interface)
  ↓
pkg/plugins/{service}/server.go (HTTP Server)
  ↓
pkg/plugins/{service}/handler.go (Request handlers)
  ↓
pkg/plugins/{service}/*.go (Business logic)
```

### Service Ports

| Service | Port | Purpose |
|---------|------|---------|
| API Gateway | 9090 | HTTP/REST API entry point |
| gRPC Gateway | 9091 | gRPC entry point |
| Transcoder | 9092 | Video transcoding |
| Streaming | 9093 | Video streaming (HLS/DASH) |
| Metadata | 9005 | Content metadata management |
| Cache | 9006 | Distributed caching |
| Auth | 9007 | Authentication & authorization |
| Worker | 9008 | Background job processing |
| Monitor | 9009 | Health monitoring & metrics |

## Service Implementations

### 1. API Gateway (Port 9090)
**Status**: ✅ Implemented

**Features**:
- REST API routing
- gRPC Gateway
- Request authentication
- Rate limiting
- Request routing to microservices
- Response aggregation

**Key Files**:
- `cmd/microservices/api-gateway/main.go` - Entry point with Gin framework
- `pkg/plugins/api/gateway.go` - Gateway logic
- `pkg/plugins/api/handler.go` - Request handlers

**Implementation Details**:
- Uses Gin framework for HTTP routing
- Implements gRPC server on port 9091
- Middleware stack: logging, recovery, CORS, rate limiting
- Routes requests to downstream services

### 2. Upload Service (Port 9091)
**Status**: ✅ Plugin Implemented

**Features**:
- Single file upload
- Chunked upload support
- Upload progress tracking
- S3/MinIO integration
- Database storage

**Key Files**:
- `cmd/microservices/upload/main.go` - Entry point
- `pkg/plugins/upload/plugin.go` - Plugin interface
- `pkg/plugins/upload/server.go` - HTTP server
- `pkg/plugins/upload/handler.go` - Request handlers
- `pkg/plugins/upload/store.go` - File storage

**Endpoints**:
- `POST /api/v1/upload` - Upload single file
- `POST /api/v1/upload/chunk` - Upload file chunk
- `POST /api/v1/upload/complete` - Complete chunked upload
- `GET /api/v1/upload/status` - Get upload status

### 3. Streaming Service (Port 9093)
**Status**: ✅ Plugin Implemented

**Features**:
- HLS playlist generation
- DASH manifest generation
- Adaptive bitrate streaming
- Segment delivery
- Stream caching

**Key Files**:
- `cmd/microservices/streaming/main.go` - Entry point
- `pkg/plugins/streaming/plugin.go` - Plugin interface
- `pkg/plugins/streaming/server.go` - HTTP server
- `pkg/plugins/streaming/handler.go` - Request handlers

**Endpoints**:
- `GET /api/v1/stream/hls` - Get HLS playlist
- `GET /api/v1/stream/dash` - Get DASH manifest
- `GET /api/v1/stream/segment` - Get video segment
- `GET /api/v1/stream/info` - Get stream info

### 4. Metadata Service (Port 9005)
**Status**: ✅ Plugin Implemented

**Features**:
- Content metadata management
- Search indexing
- Query optimization
- Database operations

**Key Files**:
- `cmd/microservices/metadata/main.go` - Entry point
- `pkg/plugins/metadata/plugin.go` - Plugin interface
- `pkg/plugins/metadata/server.go` - HTTP server
- `pkg/plugins/metadata/handler.go` - Request handlers

**Endpoints**:
- `GET /api/v1/metadata` - Get metadata
- `POST /api/v1/metadata/create` - Create metadata
- `PUT /api/v1/metadata/update` - Update metadata
- `DELETE /api/v1/metadata/delete` - Delete metadata
- `GET /api/v1/metadata/search` - Search metadata

### 5. Cache Service (Port 9006)
**Status**: ✅ Plugin Implemented

**Features**:
- Distributed caching
- Redis integration
- TTL management
- Cache invalidation

**Key Files**:
- `cmd/microservices/cache/main.go` - Entry point
- `pkg/plugins/cache/plugin.go` - Plugin interface
- `pkg/plugins/cache/server.go` - HTTP server
- `pkg/plugins/cache/handler.go` - Request handlers

**Endpoints**:
- `GET /api/v1/cache/get` - Get cached value
- `POST /api/v1/cache/set` - Set cached value
- `DELETE /api/v1/cache/delete` - Delete cached value
- `POST /api/v1/cache/clear` - Clear all cache
- `GET /api/v1/cache/stats` - Get cache statistics

### 6. Auth Service (Port 9007)
**Status**: ✅ Plugin Implemented

**Features**:
- Signature verification (EIP-191, EIP-712, Solana)
- NFT verification (ERC-721, ERC-1155, Metaplex)
- Token verification
- Challenge generation

**Key Files**:
- `cmd/microservices/auth/main.go` - Entry point
- `pkg/plugins/auth/plugin.go` - Plugin interface
- `pkg/plugins/auth/server.go` - HTTP server
- `pkg/plugins/auth/handler.go` - Request handlers

**Endpoints**:
- `POST /api/v1/auth/verify-signature` - Verify wallet signature
- `POST /api/v1/auth/verify-nft` - Verify NFT ownership
- `POST /api/v1/auth/verify-token` - Verify authentication token
- `GET /api/v1/auth/challenge` - Get signing challenge

### 7. Worker Service (Port 9008)
**Status**: ✅ Plugin Implemented

**Features**:
- Background job processing
- Job scheduling
- Retry logic
- Job queue management

**Key Files**:
- `cmd/microservices/worker/main.go` - Entry point
- `pkg/plugins/worker/plugin.go` - Plugin interface
- `pkg/plugins/worker/server.go` - HTTP server
- `pkg/plugins/worker/handler.go` - Request handlers

**Endpoints**:
- `POST /api/v1/jobs/submit` - Submit job
- `GET /api/v1/jobs/status` - Get job status
- `POST /api/v1/jobs/cancel` - Cancel job
- `GET /api/v1/jobs/list` - List jobs
- `POST /api/v1/jobs/schedule` - Schedule job

### 8. Monitor Service (Port 9009)
**Status**: ✅ Plugin Implemented

**Features**:
- Health monitoring
- Metrics collection
- Alert generation
- Log aggregation

**Key Files**:
- `cmd/microservices/monitor/main.go` - Entry point
- `pkg/plugins/monitor/plugin.go` - Plugin interface
- `pkg/plugins/monitor/server.go` - HTTP server
- `pkg/plugins/monitor/handler.go` - Request handlers

**Endpoints**:
- `GET /api/v1/monitor/health` - Get system health
- `GET /api/v1/monitor/metrics` - Get metrics
- `GET /api/v1/monitor/alerts` - Get alerts
- `GET /api/v1/monitor/logs` - Get logs
- `GET /metrics` - Prometheus metrics

### 9. Transcoder Service (Port 9092)
**Status**: ✅ Plugin Implemented

**Features**:
- Video transcoding
- Task queue management
- Worker pool management
- Auto-scaling
- Progress tracking

**Key Files**:
- `cmd/microservices/transcoder/main.go` - Entry point
- `pkg/plugins/transcoder/plugin.go` - Plugin interface
- `pkg/plugins/transcoder/server.go` - HTTP server
- `pkg/plugins/transcoder/handler.go` - Request handlers
- `pkg/plugins/transcoder/transcoder.go` - Transcoding logic

**Endpoints**:
- `POST /api/v1/transcode/submit` - Submit transcoding task
- `GET /api/v1/transcode/status` - Get task status
- `POST /api/v1/transcode/cancel` - Cancel task
- `GET /api/v1/transcode/list` - List tasks
- `GET /api/v1/transcode/metrics` - Get transcoder metrics

## Running Services

### Single Service
```bash
# Start Upload Service
go run cmd/microservices/upload/main.go

# Start Streaming Service
go run cmd/microservices/streaming/main.go

# Start API Gateway
go run cmd/microservices/api-gateway/main.go
```

### All Services (Docker Compose)
```bash
docker-compose up
```

### Kubernetes
```bash
kubectl apply -f deploy/k8s/
```

## Service Communication

### Inter-Service Communication
Services communicate via:
1. **HTTP/REST** - For synchronous calls
2. **gRPC** - For high-performance calls
3. **NATS** - For asynchronous events

### Service Discovery
- **Local**: Direct HTTP calls with hardcoded addresses
- **Kubernetes**: DNS-based service discovery
- **Consul**: Consul-based service discovery (optional)

## Configuration

### Environment Variables
```bash
# Server
SERVER_PORT=9090
SERVER_READ_TIMEOUT=15
SERVER_WRITE_TIMEOUT=15

# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=streamgate
DB_PASSWORD=password
DB_DATABASE=streamgate

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=

# NATS
NATS_URL=nats://localhost:4222

# Storage
S3_ENDPOINT=http://localhost:9000
S3_ACCESS_KEY=minioadmin
S3_SECRET_KEY=minioadmin
S3_BUCKET=streamgate

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
```

### Configuration Files
- `config/config.yaml` - Default configuration
- `config/config.dev.yaml` - Development configuration
- `config/config.prod.yaml` - Production configuration
- `config/config.test.yaml` - Test configuration

## Testing

### Unit Tests
```bash
go test ./pkg/plugins/upload/...
go test ./pkg/plugins/streaming/...
```

### Integration Tests
```bash
go test ./test/integration/...
```

### E2E Tests
```bash
go test ./test/e2e/...
```

### Load Tests
```bash
go test ./test/load/...
```

## Monitoring

### Health Checks
```bash
curl http://localhost:9090/health
curl http://localhost:9090/ready
```

### Metrics
```bash
curl http://localhost:9009/metrics
```

### Logs
```bash
docker logs streamgate-api-gateway
docker logs streamgate-upload
docker logs streamgate-streaming
```

## Troubleshooting

### Service Won't Start
1. Check port availability: `lsof -i :9090`
2. Check configuration: `cat config/config.yaml`
3. Check logs: `docker logs <service-name>`

### Service Unhealthy
1. Check health endpoint: `curl http://localhost:9090/health`
2. Check database connectivity
3. Check Redis connectivity
4. Check NATS connectivity

### High Latency
1. Check metrics: `curl http://localhost:9009/metrics`
2. Check database query performance
3. Check cache hit rate
4. Check network connectivity

## Performance Tuning

### Database
- Enable connection pooling
- Add indexes on frequently queried columns
- Use prepared statements
- Monitor slow queries

### Cache
- Increase Redis memory
- Adjust TTL values
- Monitor cache hit rate
- Use cache warming

### Streaming
- Enable CDN
- Use adaptive bitrate
- Optimize segment size
- Monitor bandwidth usage

## Security

### Authentication
- Use JWT tokens
- Verify signatures
- Implement rate limiting
- Use HTTPS/TLS

### Authorization
- Implement RBAC
- Check permissions
- Audit access
- Log security events

### Data Protection
- Encrypt sensitive data
- Use secure storage
- Implement key rotation
- Monitor access logs

## Deployment

### Docker
```bash
docker build -f deploy/docker/Dockerfile.api-gateway -t streamgate-api-gateway .
docker run -p 9090:9090 streamgate-api-gateway
```

### Kubernetes
```bash
kubectl apply -f deploy/k8s/
kubectl get pods -n streamgate
kubectl logs -n streamgate <pod-name>
```

### Helm
```bash
helm install streamgate deploy/helm/
helm upgrade streamgate deploy/helm/
helm uninstall streamgate
```

## Next Steps

1. ✅ Implement all plugin servers
2. ✅ Implement all handlers
3. ⏳ Implement monolithic mode
4. ⏳ Add comprehensive tests
5. ⏳ Add service mesh (Istio)
6. ⏳ Add observability (Prometheus, Grafana, Jaeger)
7. ⏳ Add security hardening
8. ⏳ Performance optimization

---

**Status**: Implementation Guide  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0
