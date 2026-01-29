# StreamGate Deployment Architecture

## Overview

StreamGate supports two deployment modes from a single codebase:

1. **Monolithic Mode**: All plugins in a single process (development/testing)
2. **Microservice Mode**: Plugins distributed across independent services (production)

This document describes the architecture, deployment strategies, and operational considerations.

## Architecture Comparison

### Monolithic Mode

```
┌─────────────────────────────────────────┐
│         Single Process                  │
│  ┌─────────────────────────────────┐   │
│  │      Microkernel Core           │   │
│  │  - Plugin Manager               │   │
│  │  - Event Bus (In-Memory)        │   │
│  │  - Config Manager               │   │
│  │  - Logger                       │   │
│  └─────────────────────────────────┘   │
│                                         │
│  ┌──────────┐ ┌──────────┐ ┌────────┐ │
│  │   API    │ │ Storage  │ │ Cache  │ │
│  │ Gateway  │ │ Plugin   │ │ Plugin │ │
│  └──────────┘ └──────────┘ └────────┘ │
│                                         │
│  ┌──────────┐ ┌──────────┐ ┌────────┐ │
│  │Transcoder│ │Blockchain│ │ Rate   │ │
│  │ Plugin   │ │ Plugin   │ │Limiter │ │
│  └──────────┘ └──────────┘ └────────┘ │
│                                         │
│  ┌─────────────────────────────────┐   │
│  │  PostgreSQL, Redis, MinIO       │   │
│  │  (External Dependencies)        │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

**Characteristics**:
- Single binary: `streamgate-monolith`
- All plugins in one process
- In-memory event bus
- Direct function calls between plugins
- Shared memory and resources
- Easier debugging and profiling

**Use Cases**:
- Local development
- Integration testing
- Debugging
- Performance profiling
- Small-scale deployments

### Microservice Mode

```
┌──────────────────────────────────────────────────────────────────┐
│                    Microservice Architecture                      │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Service Registry (Consul)                  │   │
│  │  - Service Discovery                                   │   │
│  │  - Health Checks                                       │   │
│  │  - Configuration Management                           │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────┐  │
│  │  API Gateway     │  │  Upload Service  │  │ Transcoder   │  │
│  │  Service         │  │  Service         │  │ Service      │  │
│  │                  │  │                  │  │              │  │
│  │ - REST API       │  │ - File Upload    │  │ - Worker Pool│  │
│  │ - gRPC Gateway   │  │ - Chunking       │  │ - Task Queue │  │
│  │ - Auth           │  │ - Resume Support │  │ - Auto-scale │  │
│  │ - Rate Limiting  │  │ - Metadata       │  │ - Health Chk │  │
│  │ - Routing        │  │                  │  │              │  │
│  └──────────────────┘  └──────────────────┘  └──────────────┘  │
│         │                      │                      │          │
│         └──────────────────────┼──────────────────────┘          │
│                                │                                 │
│                    ┌───────────▼───────────┐                    │
│                    │   NATS Event Bus      │                    │
│                    │  - Pub/Sub            │                    │
│                    │  - Message Queue      │                    │
│                    │  - Event Routing      │                    │
│                    └───────────┬───────────┘                    │
│                                │                                 │
│  ┌──────────────────┐  ┌──────▼──────────┐  ┌──────────────┐  │
│  │ Streaming Service│  │ Metadata Service│  │ Monitor      │  │
│  │ Service          │  │ Service         │  │ Service      │  │
│  │                  │  │                 │  │              │  │
│  │ - HLS Streaming  │  │ - NFT Verify    │  │ - Prometheus │  │
│  │ - DASH Streaming │  │ - Sig Verify    │  │ - Jaeger     │  │
│  │ - Segment Serve  │  │ - Metadata Mgmt │  │ - Alerting   │  │
│  │ - Cache Mgmt     │  │                 │  │              │  │
│  └──────────────────┘  └─────────────────┘  └──────────────┘  │
│         │                      │                      │          │
│         └──────────────────────┼──────────────────────┘          │
│                                │                                 │
│                    ┌───────────▼───────────┐                    │
│                    │  Shared Infrastructure│                    │
│                    │  - PostgreSQL         │                    │
│                    │  - Redis              │                    │
│                    │  - MinIO              │                    │
│                    │  - Prometheus         │                    │
│                    │  - Jaeger             │                    │
│                    └───────────────────────┘                    │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

**Characteristics**:
- Multiple independent binaries
- Each service runs in separate process
- gRPC for inter-service communication
- NATS for event bus
- Consul for service discovery
- Independent scaling
- Fault isolation

**Use Cases**:
- Production deployments
- High-traffic scenarios
- Independent service scaling
- Fault tolerance
- Multi-region deployments

## Service Breakdown

### API Gateway Service

**Binary**: `streamgate-api-gateway`

**Responsibilities**:
- REST API endpoints (`/api/v1/*`)
- gRPC gateway for mobile clients
- Web3 authentication (signature verification)
- Request routing to backend services
- Rate limiting and throttling
- Request/response transformation
- Load balancing

**Scaling**:
- Stateless design
- Horizontal scaling behind load balancer
- Recommended: 3-5 replicas in production

**Configuration**:
```yaml
api_gateway:
  port: 8080
  grpc_port: 9090
  rate_limit:
    requests_per_second: 1000
    burst: 100
  timeout: 30s
```

### Transcoder Service

**Binary**: `streamgate-transcoder`

**Responsibilities**:
- Video transcoding (FFmpeg)
- HLS/DASH generation
- Quality profile management
- Task scheduling and execution
- Progress tracking
- Error handling and retry

**Key Features**:
- **Worker Pool**: Configurable number of concurrent workers
- **Task Queue**: Priority-based task queue with persistence
- **Auto-Scaling**: Automatic scaling based on queue length
- **Health Monitoring**: Worker health checks and recovery
- **Graceful Shutdown**: Completes in-flight tasks before shutdown

**Scaling Strategy**:

```
Queue Length vs Workers Ratio:
- Ratio > 2.5: Scale up (add workers)
- Ratio < 0.5: Scale down (remove workers)
- Min workers: 2
- Max workers: 16
```

**Configuration**:
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

**Monitoring**:
```bash
# Get transcoder metrics
grpcurl -plaintext localhost:9092 streamgate.Transcoder/GetMetrics

# Expected output:
{
  "total_workers": 4,
  "active_workers": 2,
  "idle_workers": 2,
  "unhealthy_workers": 0,
  "total_tasks_processed": 1234,
  "total_tasks_failed": 5,
  "average_task_time": "45.5s"
}
```

### Upload Service

**Binary**: `streamgate-upload`

**Responsibilities**:
- Chunked file uploads
- Resumable upload support
- Storage backend management (S3/MinIO)
- Metadata persistence
- Upload progress tracking

**Scaling**:
- Stateless design
- Horizontal scaling
- Recommended: 2-3 replicas

### Streaming Service

**Binary**: `streamgate-streaming`

**Responsibilities**:
- HLS playlist serving
- DASH manifest serving
- Video segment delivery
- Adaptive bitrate selection
- Cache management
- CDN integration

**Scaling**:
- Stateless design
- Horizontal scaling
- Recommended: 3-5 replicas

## Deployment Modes

### Development: Monolithic

```bash
# Build
make build-monolith

# Run
./bin/streamgate-monolith

# Configuration: config.yaml
deployment:
  mode: monolith
```

**Advantages**:
- Single process to manage
- Easier debugging
- Lower resource usage
- Faster startup
- Simpler configuration

**Disadvantages**:
- Cannot scale individual components
- One failure affects entire system
- Limited concurrency

### Testing: Monolithic with Docker

```bash
# Build Docker image
docker build -f Dockerfile.monolith -t streamgate:monolith .

# Run
docker run -p 8080:8080 streamgate:monolith
```

### Production: Microservices with Docker Compose

```bash
# Start all services
docker-compose up -d

# Scale transcoder
docker-compose up -d --scale streamgate-transcoder=3

# View logs
docker-compose logs -f streamgate-transcoder
```

### Production: Kubernetes

```bash
# Deploy
kubectl apply -f k8s/

# Check status
kubectl get deployments
kubectl get pods

# Scale transcoder
kubectl scale deployment streamgate-transcoder --replicas=5

# Monitor
kubectl logs -f deployment/streamgate-transcoder
```

## High-Concurrency Transcoder Design

### Architecture

```
┌─────────────────────────────────────────────────────┐
│         Transcoder Service                          │
├─────────────────────────────────────────────────────┤
│                                                     │
│  ┌──────────────────────────────────────────────┐  │
│  │         Task Queue (Priority-based)          │  │
│  │  - Pending tasks                             │  │
│  │  - Priority ordering                         │  │
│  │  - Persistence (Redis/DB)                    │  │
│  └──────────────────────────────────────────────┘  │
│                      │                              │
│                      ▼                              │
│  ┌──────────────────────────────────────────────┐  │
│  │         Worker Pool (Auto-scaling)           │  │
│  │  ┌────────┐ ┌────────┐ ┌────────┐           │  │
│  │  │Worker 1│ │Worker 2│ │Worker 3│ ...       │  │
│  │  │        │ │        │ │        │           │  │
│  │  │ Idle   │ │ Busy   │ │ Busy   │           │  │
│  │  └────────┘ └────────┘ └────────┘           │  │
│  │                                              │  │
│  │  Auto-scaling Monitor:                       │  │
│  │  - Monitors queue length                     │  │
│  │  - Scales up when queue > threshold          │  │
│  │  - Scales down when queue empty              │  │
│  └──────────────────────────────────────────────┘  │
│                      │                              │
│                      ▼                              │
│  ┌──────────────────────────────────────────────┐  │
│  │      Task Execution & Monitoring             │  │
│  │  - FFmpeg transcoding                        │  │
│  │  - Progress tracking                         │  │
│  │  - Error handling & retry                    │  │
│  │  - Health monitoring                         │  │
│  └──────────────────────────────────────────────┘  │
│                      │                              │
│                      ▼                              │
│  ┌──────────────────────────────────────────────┐  │
│  │      Event Publishing                        │  │
│  │  - transcode.task.submitted                  │  │
│  │  - transcode.task.started                    │  │
│  │  - transcode.task.progress                   │  │
│  │  - transcode.task.completed                  │  │
│  │  - transcode.task.failed                     │  │
│  └──────────────────────────────────────────────┘  │
│                      │                              │
│                      ▼                              │
│              NATS Event Bus
│
```

### Task Lifecycle

```
1. Submit Task
   ├─ Validate input
   ├─ Create task record
   ├─ Enqueue to task queue
   └─ Publish "task.submitted" event

2. Dequeue Task
   ├─ Worker picks task from queue
   ├─ Update task status to "processing"
   └─ Publish "task.started" event

3. Execute Task
   ├─ Download source file
   ├─ Transcode to multiple profiles
   ├─ Generate HLS/DASH manifests
   ├─ Upload results
   └─ Track progress (0-100%)

4. Complete Task
   ├─ Update task status
   ├─ Record completion time
   ├─ Publish "task.completed" event
   └─ Update metrics

5. Handle Failure
   ├─ Catch error
   ├─ Increment retry count
   ├─ If retries < max: re-enqueue
   ├─ If retries >= max: mark failed
   └─ Publish "task.failed" event
```

### Auto-Scaling Algorithm

```go
// Pseudo-code for auto-scaling
func monitorAutoScaling() {
    for {
        queueLen := taskQueue.Length()
        activeWorkers := workerPool.ActiveCount()
        
        ratio := float64(queueLen) / float64(activeWorkers)
        
        // Scale up
        if ratio > scaleUpThreshold && activeWorkers < maxWorkers {
            workerPool.AddWorker()
        }
        
        // Scale down
        if ratio < scaleDownThreshold && activeWorkers > minWorkers && queueLen == 0 {
            workerPool.RemoveWorker()
        }
        
        sleep(checkInterval)
    }
}
```

### Performance Characteristics

**Throughput**:
- Single worker: ~1-2 videos/hour (depends on video size/quality)
- 4 workers: ~4-8 videos/hour
- 8 workers: ~8-16 videos/hour
- 16 workers: ~16-32 videos/hour

**Latency**:
- Queue wait time: 0-60 seconds (depends on load)
- Processing time: 5-60 minutes (depends on video size)
- Total time: 5-120 minutes

**Resource Usage**:
- Per worker: ~1-2 CPU cores, 1-2 GB RAM
- 4 workers: ~4-8 CPU cores, 4-8 GB RAM
- 8 workers: ~8-16 CPU cores, 8-16 GB RAM

## Operational Considerations

### Monitoring

**Key Metrics**:
- Queue length
- Active workers
- Task completion rate
- Task failure rate
- Average task time
- Worker health status

**Prometheus Queries**:
```promql
# Queue length
streamgate_transcoder_queue_length

# Active workers
streamgate_transcoder_active_workers

# Task completion rate
rate(streamgate_transcoder_tasks_completed[5m])

# Task failure rate
rate(streamgate_transcoder_tasks_failed[5m])

# Average task time
streamgate_transcoder_average_task_time
```

### Alerting

**Critical Alerts**:
- Queue length > 100 for 5 minutes
- Worker failure rate > 10%
- All workers unhealthy
- Service unavailable

**Warning Alerts**:
- Queue length > 50 for 5 minutes
- Worker failure rate > 5%
- Single worker unhealthy
- High task latency (> 10 minutes)

### Scaling Decisions

**Scale Up When**:
- Queue length > 50
- Average task wait time > 5 minutes
- CPU utilization > 80%
- Memory utilization > 80%

**Scale Down When**:
- Queue length < 10
- CPU utilization < 30%
- Memory utilization < 30%
- No tasks for 5 minutes

### Graceful Shutdown

```bash
# Send SIGTERM to service
kill -TERM <pid>

# Service will:
# 1. Stop accepting new tasks
# 2. Wait for in-flight tasks to complete (max 60 seconds)
# 3. Deregister from service registry
# 4. Close connections
# 5. Exit
```

## Migration Path

### Phase 1: Development (Monolithic)
- Use monolithic mode for development
- Test all features locally
- Profile and optimize

### Phase 2: Testing (Monolithic + Docker)
- Containerize monolithic service
- Test with Docker Compose
- Verify all integrations

### Phase 3: Production (Microservices)
- Deploy microservices to Kubernetes
- Start with 1 replica per service
- Monitor and optimize
- Scale based on load

### Phase 4: Optimization
- Fine-tune resource allocation
- Optimize auto-scaling policies
- Add caching layers
- Implement CDN

## Troubleshooting

### High Queue Length

**Symptoms**:
- Queue length continuously increasing
- Task wait time > 10 minutes

**Solutions**:
1. Check worker health: `grpcurl -plaintext localhost:9092 streamgate.Transcoder/GetMetrics`
2. Scale up workers: `kubectl scale deployment streamgate-transcoder --replicas=8`
3. Check resource availability: `kubectl top nodes`
4. Review task complexity: Are tasks taking longer than expected?

### Worker Failures

**Symptoms**:
- High task failure rate
- Worker marked unhealthy

**Solutions**:
1. Check logs: `kubectl logs deployment/streamgate-transcoder`
2. Verify dependencies: PostgreSQL, Redis, MinIO availability
3. Check disk space: `df -h`
4. Restart workers: `kubectl rollout restart deployment/streamgate-transcoder`

### Memory Leaks

**Symptoms**:
- Memory usage continuously increasing
- OOM kills

**Solutions**:
1. Profile memory: `go tool pprof http://localhost:6060/debug/pprof/heap`
2. Check for goroutine leaks: `curl http://localhost:6060/debug/pprof/goroutine`
3. Review recent code changes
4. Restart service if necessary

## Summary

StreamGate's dual-mode deployment architecture provides:

- **Flexibility**: Monolithic for development, microservices for production
- **Scalability**: Independent scaling of each service
- **Reliability**: Fault isolation and graceful degradation
- **Observability**: Comprehensive monitoring and logging
- **Efficiency**: Resource optimization through auto-scaling

The transcoder service is specifically designed for high-concurrency scenarios with:
- Worker pool for parallel processing
- Priority-based task queue
- Automatic scaling based on load
- Health monitoring and recovery
- Comprehensive metrics and logging
