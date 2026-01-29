# Phase 4 Integration Guide

## Quick Start: Using Inter-Service Communication

### 1. Service Registration (Automatic)

When a microservice starts, it automatically registers with Consul:

```go
// In cmd/microservices/upload/main.go
cfg.Mode = "microservice"
cfg.ServiceName = "upload"
cfg.Server.Port = 9091

kernel, _ := core.NewMicrokernel(cfg, log)
kernel.Start(ctx)  // Automatically registers with Consul
```

### 2. Service Discovery

Discover other services from within a plugin:

```go
// In a plugin handler
kernel := m.kernel  // Available in plugin context
registry := kernel.GetRegistry()

// Discover upload service
services, err := registry.Discover(ctx, "upload")
if err != nil {
    return err
}

// Use first available service
service := services[0]
address := fmt.Sprintf("%s:%d", service.Address, service.Port)
```

### 3. Service-to-Service gRPC Calls

Call another service via gRPC:

```go
// Get client pool
clientPool := kernel.GetClientPool()

// Get connection to upload service
conn, err := clientPool.GetConnection(ctx, "upload")
if err != nil {
    return err
}

// Create gRPC client
client := pb.NewUploadServiceClient(conn)

// Call service
resp, err := client.UploadFile(ctx, &pb.UploadFileRequest{
    FileName:    "video.mp4",
    FileSize:    1024000,
    ContentType: "video/mp4",
})
```

### 4. Event Publishing

Publish events from a service:

```go
// Get event bus
eventBus := kernel.GetEventBus()

// Publish file uploaded event
err := event.PublishFileUploaded(ctx, eventBus, fileID, fileName, fileSize)
if err != nil {
    return err
}
```

### 5. Event Subscription

Subscribe to events in a plugin:

```go
// In plugin Init method
eventBus := kernel.GetEventBus()

// Subscribe to file uploaded events
err := eventBus.Subscribe(ctx, event.EventFileUploaded, func(ctx context.Context, e *event.Event) error {
    fileID := e.Data["file_id"].(string)
    fileName := e.Data["file_name"].(string)
    
    // Handle file uploaded event
    log.Info("File uploaded", "file_id", fileID, "file_name", fileName)
    return nil
})
```

## Service Ports

| Service | Port | Name |
|---------|------|------|
| API Gateway | 9090 | api-gateway |
| Upload | 9091 | upload |
| Transcoder | 9092 | transcoder |
| Streaming | 9093 | streaming |
| Metadata | 9005 | metadata |
| Cache | 9006 | cache |
| Auth | 9007 | auth |
| Worker | 9008 | worker |
| Monitor | 9009 | monitor |

## Event Types

### File Events
- `EventFileUploaded` - File uploaded to storage

### Transcoding Events
- `EventTranscodingStarted` - Transcoding job started
- `EventTranscodingCompleted` - Transcoding job completed
- `EventTranscodingFailed` - Transcoding job failed

### Streaming Events
- `EventStreamingStarted` - Streaming session started
- `EventStreamingStopped` - Streaming session stopped

### Metadata Events
- `EventMetadataCreated` - Metadata created
- `EventMetadataUpdated` - Metadata updated
- `EventMetadataDeleted` - Metadata deleted

### Job Events
- `EventJobSubmitted` - Job submitted to worker
- `EventJobCompleted` - Job completed
- `EventJobFailed` - Job failed

### Alert Events
- `EventAlertTriggered` - Alert triggered
- `EventAlertResolved` - Alert resolved

## Configuration

### Consul Configuration

Set in `config/config.yaml`:

```yaml
consul:
  address: localhost
  port: 8500
```

Or via environment variables:

```bash
export CONSUL_ADDRESS=localhost
export CONSUL_PORT=8500
```

### NATS Configuration

Set in `config/config.yaml`:

```yaml
nats:
  url: nats://localhost:4222
```

Or via environment variables:

```bash
export NATS_URL=nats://localhost:4222
```

## Running Services Locally

### 1. Start Consul

```bash
docker run -d \
  -p 8500:8500 \
  -p 8600:8600/udp \
  --name=consul \
  consul agent -server -ui -bootstrap-expect=1 -client=0.0.0.0
```

### 2. Start NATS

```bash
docker run -d \
  -p 4222:4222 \
  --name=nats \
  nats
```

### 3. Start Services

In separate terminals:

```bash
# Terminal 1: Upload Service
go run cmd/microservices/upload/main.go

# Terminal 2: Streaming Service
go run cmd/microservices/streaming/main.go

# Terminal 3: Metadata Service
go run cmd/microservices/metadata/main.go

# Terminal 4: API Gateway
go run cmd/microservices/api-gateway/main.go
```

### 4. Verify Services

Check Consul UI:
```
http://localhost:8500/ui/
```

## Debugging

### Check Service Registration

```bash
# List all services
curl http://localhost:8500/v1/catalog/services

# Get specific service
curl http://localhost:8500/v1/catalog/service/upload

# Get service health
curl http://localhost:8500/v1/health/service/upload
```

### Check NATS Connections

```bash
# Connect to NATS
nats sub "streamgate.>"

# In another terminal, publish an event
nats pub "streamgate.file.uploaded" '{"type":"file.uploaded","source":"upload-service"}'
```

### Enable Debug Logging

Set in `config/config.yaml`:

```yaml
app:
  debug: true

monitoring:
  log_level: debug
```

## Common Patterns

### Pattern 1: Service-to-Service Call with Error Handling

```go
func (h *Handler) CallUploadService(ctx context.Context, kernel *core.Microkernel) error {
    clientPool := kernel.GetClientPool()
    
    conn, err := clientPool.GetConnection(ctx, "upload")
    if err != nil {
        h.logger.Error("Failed to get upload service", "error", err)
        return fmt.Errorf("upload service unavailable: %w", err)
    }
    
    client := pb.NewUploadServiceClient(conn)
    
    resp, err := client.UploadFile(ctx, &pb.UploadFileRequest{
        FileName:    "video.mp4",
        FileSize:    1024000,
        ContentType: "video/mp4",
    })
    
    if err != nil {
        h.logger.Error("Upload service call failed", "error", err)
        return fmt.Errorf("upload failed: %w", err)
    }
    
    h.logger.Info("File uploaded", "file_id", resp.FileId)
    return nil
}
```

### Pattern 2: Event Publishing and Subscription

```go
// In plugin Init
func (p *MyPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
    eventBus := kernel.GetEventBus()
    
    // Subscribe to events
    err := eventBus.Subscribe(ctx, event.EventFileUploaded, p.handleFileUploaded)
    if err != nil {
        return err
    }
    
    return nil
}

// Event handler
func (p *MyPlugin) handleFileUploaded(ctx context.Context, e *event.Event) error {
    fileID := e.Data["file_id"].(string)
    fileName := e.Data["file_name"].(string)
    fileSize := e.Data["file_size"].(int64)
    
    p.logger.Info("Processing uploaded file", 
        "file_id", fileID, 
        "file_name", fileName, 
        "file_size", fileSize)
    
    // Process file
    return nil
}

// Publish event
func (p *MyPlugin) publishEvent(ctx context.Context, kernel *core.Microkernel) error {
    eventBus := kernel.GetEventBus()
    
    return event.PublishFileUploaded(ctx, eventBus, 
        "file-123", 
        "video.mp4", 
        1024000)
}
```

### Pattern 3: Service Discovery with Fallback

```go
func (h *Handler) GetServiceAddress(ctx context.Context, kernel *core.Microkernel, serviceName string) (string, error) {
    registry := kernel.GetRegistry()
    
    services, err := registry.Discover(ctx, serviceName)
    if err != nil {
        h.logger.Error("Service discovery failed", "service", serviceName, "error", err)
        return "", err
    }
    
    if len(services) == 0 {
        h.logger.Error("No services found", "service", serviceName)
        return "", fmt.Errorf("service not found: %s", serviceName)
    }
    
    // Use first available service
    service := services[0]
    address := fmt.Sprintf("%s:%d", service.Address, service.Port)
    
    h.logger.Info("Service discovered", "service", serviceName, "address", address)
    return address, nil
}
```

## Troubleshooting

### Service Not Registering

1. Check Consul is running:
   ```bash
   curl http://localhost:8500/v1/status/leader
   ```

2. Check service logs for registration errors:
   ```bash
   go run cmd/microservices/upload/main.go 2>&1 | grep -i "register"
   ```

3. Verify Consul configuration in config.yaml

### Service Discovery Failing

1. Check service is registered:
   ```bash
   curl http://localhost:8500/v1/catalog/service/upload
   ```

2. Check health check is passing:
   ```bash
   curl http://localhost:8500/v1/health/service/upload
   ```

3. Verify health check endpoint is responding:
   ```bash
   curl http://localhost:9091/health
   ```

### Events Not Publishing

1. Check NATS is running:
   ```bash
   nats-cli server info
   ```

2. Check NATS URL in config.yaml

3. Enable debug logging to see event publishing

### gRPC Connection Errors

1. Verify target service is running
2. Check port is correct
3. Verify firewall allows connection
4. Check gRPC service is listening

## Performance Tips

1. **Connection Pooling**: ClientPool automatically caches connections
2. **Event Batching**: Batch events when possible to reduce NATS overhead
3. **Service Watching**: Use Watch() for efficient service discovery updates
4. **Circuit Breaker**: Use CircuitBreaker to prevent cascading failures
5. **Timeouts**: Set appropriate timeouts for service calls

## Security Considerations

1. **Service-to-Service Auth**: Implement in middleware (framework ready)
2. **TLS**: Enable TLS for gRPC connections in production
3. **NATS Auth**: Enable NATS authentication in production
4. **Consul ACLs**: Enable Consul ACLs in production
5. **Network Policies**: Use network policies to restrict service communication

## Next Steps

- Implement service-to-service authentication
- Add distributed tracing
- Implement metrics collection
- Add rate limiting
- Implement retry logic with exponential backoff

---

For more information, see:
- `docs/project-planning/implementation/CODE_IMPLEMENTATION_PHASE4.md`
- `proto/v1/service.proto` - gRPC service definitions
- `pkg/service/registry.go` - Service registry
- `pkg/core/event/nats.go` - Event bus
