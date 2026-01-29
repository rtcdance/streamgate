# StreamGate Command Line Executables

This directory contains separate entry points for different deployment modes.

## Directory Structure

```
cmd/
├── monolith/                    # Monolithic deployment
│   └── streamgate/              # Single binary (all plugins in one process)
│       └── main.go
│
├── microservices/               # Microservice deployment (9 services)
│   ├── api-gateway/             # API Gateway service (port 9090)
│   │   └── main.go
│   ├── upload/                  # Upload/Storage service (port 9091)
│   │   └── main.go
│   ├── transcoder/              # Transcoder service (port 9092, high-concurrency)
│   │   └── main.go
│   ├── streaming/               # Streaming/Playback service (port 9093)
│   │   └── main.go
│   ├── metadata/                # Metadata service (port 9005)
│   │   └── main.go
│   ├── cache/                   # Cache service (port 9006)
│   │   └── main.go
│   ├── auth/                    # Auth service (port 9007)
│   │   └── main.go
│   ├── worker/                  # Worker service (port 9008)
│   │   └── main.go
│   └── monitor/                 # Monitor service (port 9009)
│       └── main.go
│
└── README.md                    # This file
```

## Deployment Modes

### 1. Monolithic Mode (Development/Testing)

**Location**: `cmd/monolith/streamgate/`

All plugins run in a single process with in-memory communication.

```bash
# Build
go build -o bin/streamgate ./cmd/monolith/streamgate

# Or use Makefile
make build-monolith

# Run
./bin/streamgate

# Configuration
# Uses config.yaml with mode: monolith
```

**Characteristics**:
- Single process, all plugins loaded
- In-memory event bus
- No network overhead
- Ideal for development and debugging
- Easier to profile and debug

**Use Cases**:
- Local development
- Integration testing
- Debugging
- Performance profiling

### 2. Microservice Mode (Production)

**Location**: `cmd/microservices/`

Each service runs independently with gRPC communication and NATS event bus.

#### API Gateway Service

**Location**: `cmd/microservices/api-gateway/`

Entry point for all client requests. Handles authentication, routing, and rate limiting.

```bash
# Build
go build -o bin/api-gateway ./cmd/microservices/api-gateway

# Or use Makefile
make build-api-gateway

# Run
./bin/api-gateway

# Configuration
# Uses config.yaml with mode: microservice, service_name: api-gateway
```

**Responsibilities**:
- REST API endpoints
- gRPC gateway
- Web3 authentication (signature verification)
- Request routing
- Rate limiting
- Load balancing

**Scaling**:
```bash
# Run multiple instances behind a load balancer
./bin/api-gateway &
./bin/api-gateway &
./bin/api-gateway &
```

#### Transcoder Service

**Location**: `cmd/microservices/transcoder/`

High-concurrency video transcoding with automatic scaling and task management.

```bash
# Build
go build -o bin/transcoder ./cmd/microservices/transcoder

# Or use Makefile
make build-transcoder

# Run
./bin/transcoder

# Configuration
# Uses config.yaml with mode: microservice, service_name: transcoder-service
```

**Features**:
- Worker pool with configurable size
- Priority-based task queue
- Automatic scaling based on queue length
- Health monitoring
- Task retry mechanism
- Progress tracking
- Graceful shutdown

**Configuration** (in config.yaml):
```yaml
transcoder:
  worker_pool_size: 4
  max_concurrent_tasks: 16
  max_queue_size: 1000
  task_timeout: 3600s
  health_check_interval: 30s
  scaling_policy:
    min_workers: 2
    max_workers: 16
    target_queue_len: 10
    scale_up_threshold: 2.5
    scale_down_threshold: 0.5
    check_interval: 10s
```

**Scaling**:
```bash
# Run multiple transcoder instances
./bin/transcoder &
./bin/transcoder &
./bin/transcoder &

# Each instance:
# - Registers with service registry (Consul)
# - Connects to shared NATS event bus
# - Pulls tasks from distributed queue
# - Auto-scales based on load
```

**Monitoring**:
```bash
# Check transcoder metrics via gRPC
grpcurl -plaintext localhost:9092 streamgate.Transcoder/GetMetrics

# Expected response:
# {
#   "total_workers": 4,
#   "active_workers": 2,
#   "idle_workers": 2,
#   "unhealthy_workers": 0,
#   "total_tasks_processed": 1234,
#   "total_tasks_failed": 5,
#   "average_task_time": "45.5s"
# }
```

#### Upload Service

**Location**: `cmd/microservices/upload/`

Handles file uploads with chunking and resumable uploads.

```bash
# Build
go build -o bin/upload ./cmd/microservices/upload

# Or use Makefile
make build-upload

# Run
./bin/upload

# Configuration
# Uses config.yaml with mode: microservice, service_name: upload-service
```

**Responsibilities**:
- Chunked file uploads
- Resumable uploads
- Storage backend (S3/MinIO)
- Metadata management
- Upload progress tracking

**Scaling**:
```bash
# Run multiple instances
./bin/upload &
./bin/upload &
```

#### Streaming Service

**Location**: `cmd/microservices/streaming/`

Handles video streaming with HLS and DASH support.

```bash
# Build
go build -o bin/streaming ./cmd/microservices/streaming

# Or use Makefile
make build-streaming

# Run
./bin/streaming

# Configuration
# Uses config.yaml with mode: microservice, service_name: streaming-service
```

**Responsibilities**:
- HLS playlist generation
- DASH manifest generation
- Segment delivery
- Adaptive bitrate streaming
- Cache management

**Scaling**:
```bash
# Run multiple instances
./bin/streaming &
./bin/streaming &
```

## Building All Binaries

```bash
# Build all binaries
make build-all

# Or manually:
go build -o bin/streamgate ./cmd/monolith/streamgate
go build -o bin/api-gateway ./cmd/microservices/api-gateway
go build -o bin/transcoder ./cmd/microservices/transcoder
go build -o bin/upload ./cmd/microservices/upload
go build -o bin/streaming ./cmd/microservices/streaming
```

## Docker Deployment

### Monolithic Mode

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o streamgate ./cmd/monolith/streamgate

FROM alpine:latest
COPY --from=builder /app/streamgate /app/
ENTRYPOINT ["/app/streamgate"]
```

```bash
docker build -f Dockerfile.monolith -t streamgate:monolith .
docker run -p 8080:8080 streamgate:monolith
```

### Microservice Mode

```bash
# Build individual service images
docker build -f Dockerfile.api-gateway -t streamgate:api-gateway .
docker build -f Dockerfile.transcoder -t streamgate:transcoder .
docker build -f Dockerfile.upload -t streamgate:upload .
docker build -f Dockerfile.streaming -t streamgate:streaming .

# Run with docker-compose
docker-compose -f docker-compose.microservices.yml up
```

## Kubernetes Deployment

### Monolithic Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: streamgate-monolith
spec:
  replicas: 1
  selector:
    matchLabels:
      app: streamgate-monolith
  template:
    metadata:
      labels:
        app: streamgate-monolith
    spec:
      containers:
      - name: streamgate
        image: streamgate:monolith
        ports:
        - containerPort: 8080
```

### Microservice Deployment

```yaml
# API Gateway
apiVersion: apps/v1
kind: Deployment
metadata:
  name: streamgate-api-gateway
spec:
  replicas: 3
  selector:
    matchLabels:
      app: streamgate-api-gateway
  template:
    metadata:
      labels:
        app: streamgate-api-gateway
    spec:
      containers:
      - name: api-gateway
        image: streamgate:api-gateway
        ports:
        - containerPort: 8080
        - containerPort: 9090

---
# Transcoder
apiVersion: apps/v1
kind: Deployment
metadata:
  name: streamgate-transcoder
spec:
  replicas: 3
  selector:
    matchLabels:
      app: streamgate-transcoder
  template:
    metadata:
      labels:
        app: streamgate-transcoder
    spec:
      containers:
      - name: transcoder
        image: streamgate:transcoder
        ports:
        - containerPort: 9092
        resources:
          requests:
            cpu: 4000m
            memory: 4Gi
          limits:
            cpu: 8000m
            memory: 8Gi

---
# Horizontal Pod Autoscaler for Transcoder
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: streamgate-transcoder-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: streamgate-transcoder
  minReplicas: 2
  maxReplicas: 16
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

## Configuration

Each service reads from `config.yaml`:

```yaml
# Deployment mode
deployment:
  mode: monolith  # or microservice
  service_name: api-gateway  # for microservice mode

# Server configuration
server:
  port: 8080
  read_timeout: 30s
  write_timeout: 30s

# gRPC configuration (microservice mode)
grpc:
  port: 9090

# Event bus configuration
eventbus:
  type: memory  # monolith mode
  # type: nats  # microservice mode
  nats:
    url: nats://localhost:4222

# Service registry (microservice mode)
registry:
  type: consul
  consul:
    address: localhost:8500

# Transcoder configuration
transcoder:
  worker_pool_size: 4
  max_concurrent_tasks: 16
  max_queue_size: 1000
  task_timeout: 3600s
  health_check_interval: 30s
  scaling_policy:
    min_workers: 2
    max_workers: 16
    target_queue_len: 10
    scale_up_threshold: 2.5
    scale_down_threshold: 0.5
    check_interval: 10s
```

## Environment Variables

Override configuration with environment variables:

```bash
# Deployment mode
export DEPLOYMENT_MODE=monolith

# Service name (microservice mode)
export SERVICE_NAME=api-gateway

# Server port
export SERVER_PORT=8080

# gRPC port
export GRPC_PORT=9090

# Event bus
export EVENTBUS_TYPE=nats
export NATS_URL=nats://localhost:4222

# Service registry
export REGISTRY_TYPE=consul
export CONSUL_ADDRESS=localhost:8500

# Transcoder
export TRANSCODER_WORKERS=4
export TRANSCODER_MAX_TASKS=16
```

## Development Workflow

### Local Development (Monolithic)

```bash
# Terminal 1: Start monolithic service
go run ./cmd/monolith/streamgate/main.go

# Terminal 2: Test API
curl http://localhost:8080/api/v1/health
```

### Local Microservices (Docker Compose)

```bash
# Start all services
docker-compose up

# Check logs
docker-compose logs -f transcoder

# Scale transcoder
docker-compose up -d --scale transcoder=3
```

### Production Deployment (Kubernetes)

```bash
# Deploy monolithic
kubectl apply -f k8s/monolith-deployment.yaml

# Deploy microservices
kubectl apply -f k8s/microservices/

# Check status
kubectl get deployments
kubectl get pods

# Scale transcoder
kubectl scale deployment streamgate-transcoder --replicas=5

# Monitor
kubectl logs -f deployment/streamgate-transcoder
```

## Performance Tuning

### Transcoder Scaling

Adjust based on workload:

```yaml
# Light workload
transcoder:
  worker_pool_size: 2
  max_concurrent_tasks: 8
  scaling_policy:
    min_workers: 1
    max_workers: 4

# Medium workload
transcoder:
  worker_pool_size: 4
  max_concurrent_tasks: 16
  scaling_policy:
    min_workers: 2
    max_workers: 8

# Heavy workload
transcoder:
  worker_pool_size: 8
  max_concurrent_tasks: 32
  scaling_policy:
    min_workers: 4
    max_workers: 16
```

### Resource Allocation

```yaml
# Kubernetes resource requests/limits
resources:
  requests:
    cpu: 4000m      # 4 CPU cores
    memory: 4Gi     # 4GB RAM
  limits:
    cpu: 8000m      # 8 CPU cores
    memory: 8Gi     # 8GB RAM
```

## Troubleshooting

### Check Service Health

```bash
# Monolithic
curl http://localhost:8080/health

# Microservice (gRPC)
grpcurl -plaintext localhost:9090 grpc.health.v1.Health/Check
```

### View Metrics

```bash
# Prometheus metrics
curl http://localhost:8080/metrics

# Transcoder metrics
grpcurl -plaintext localhost:9092 streamgate.Transcoder/GetMetrics
```

### View Logs

```bash
# Monolithic
./bin/streamgate 2>&1 | grep -i error

# Docker
docker logs transcoder

# Kubernetes
kubectl logs deployment/streamgate-transcoder
```

## Summary

- **Monolithic** (`cmd/monolith/`): Single binary for development, all plugins in one process
- **Microservices** (`cmd/microservices/`): Separate binaries for each service, independent scaling
- **Transcoder**: Specialized high-concurrency service with auto-scaling
- **Configuration**: Unified config.yaml for all services
- **Deployment**: Docker, Docker Compose, or Kubernetes

## Clear Separation

The new directory structure provides clear separation:

1. **`cmd/monolith/`**: Contains only monolithic deployment
   - Single entry point: `streamgate`
   - All-in-one binary

2. **`cmd/microservices/`**: Contains all microservice deployments
   - `api-gateway/`: API Gateway service
   - `transcoder/`: Transcoder service
   - `upload/`: Upload service
   - `streaming/`: Streaming service

This structure makes it immediately clear which deployment mode you're working with.
