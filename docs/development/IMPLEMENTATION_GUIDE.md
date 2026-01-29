# StreamGate Implementation Guide

## Quick Start

### Prerequisites

- Go 1.21+
- Docker & Docker Compose
- PostgreSQL 15+
- Redis 7+
- MinIO / S3

### Setup

```bash
# Clone repository
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# Install dependencies
go mod download

# Build all services
make build-all

# Or build individual services
make build-monolith
make build-api-gateway
```

### Run Monolithic (Development)

```bash
# Terminal 1: Start infrastructure
docker-compose up

# Terminal 2: Run monolithic service
./bin/streamgate

# Terminal 3: Test
curl http://localhost:8080/health
```

### Run Microservices (Production)

```bash
# Start all services with Docker Compose
docker-compose up

# Or run individually
./bin/api-gateway &
./bin/upload &
./bin/transcoder &
./bin/streaming &
./bin/metadata &
./bin/cache &
./bin/auth &
./bin/worker &
./bin/monitor &
```

## Architecture Overview

### Microkernel Plugin System

Every service is built as a plugin that implements the `Plugin` interface:

```go
type Plugin interface {
    Name() string
    Version() string
    Init(ctx context.Context, kernel *Microkernel) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Health(ctx context.Context) error
}
```

### Service Structure

```
cmd/microservices/[service-name]/
├── main.go                 # Entry point
└── ...

pkg/plugins/[service-name]/
├── handler.go              # HTTP/gRPC handlers
├── plugin.go               # Plugin implementation
├── service.go              # Business logic
└── ...
```

## Creating a New Service Plugin

### Step 1: Create Plugin Package

```bash
mkdir -p pkg/plugins/myservice
```

### Step 2: Implement Plugin Interface

Create `pkg/plugins/myservice/plugin.go`:

```go
package myservice

import (
    "context"
    "github.com/yourusername/streamgate/pkg/core"
    "go.uber.org/zap"
)

type MyServicePlugin struct {
    name   string
    kernel *core.Microkernel
    logger *zap.Logger
}

func NewMyServicePlugin(logger *zap.Logger) *MyServicePlugin {
    return &MyServicePlugin{
        name:   "my-service",
        logger: logger,
    }
}

func (p *MyServicePlugin) Name() string {
    return p.name
}

func (p *MyServicePlugin) Version() string {
    return "1.0.0"
}

func (p *MyServicePlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
    p.kernel = kernel
    p.logger.Info("Initializing my-service plugin")
    return nil
}

func (p *MyServicePlugin) Start(ctx context.Context) error {
    p.logger.Info("Starting my-service plugin")
    // Start your service here
    return nil
}

func (p *MyServicePlugin) Stop(ctx context.Context) error {
    p.logger.Info("Stopping my-service plugin")
    // Stop your service here
    return nil
}

func (p *MyServicePlugin) Health(ctx context.Context) error {
    // Check health here
    return nil
}
```

### Step 3: Create Entry Point

Create `cmd/microservices/myservice/main.go`:

```go
package main

import (
    "context"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/yourusername/streamgate/pkg/core"
    "github.com/yourusername/streamgate/pkg/core/config"
    "github.com/yourusername/streamgate/pkg/core/logger"
    "github.com/yourusername/streamgate/pkg/plugins/myservice"
)

func main() {
    log := logger.NewDevelopmentLogger("streamgate-myservice")
    defer log.Sync()

    log.Info("Starting StreamGate My Service...")

    cfg, err := config.LoadConfig()
    if err != nil {
        log.Fatal("Failed to load configuration", "error", err)
    }

    cfg.Mode = "microservice"
    cfg.ServiceName = "my-service"

    kernel, err := core.NewMicrokernel(cfg, log)
    if err != nil {
        log.Fatal("Failed to initialize microkernel", "error", err)
    }

    if err := kernel.RegisterPlugin(myservice.NewMyServicePlugin(log)); err != nil {
        log.Fatal("Failed to register plugin", "error", err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := kernel.Start(ctx); err != nil {
        log.Fatal("Failed to start microkernel", "error", err)
    }

    log.Info("StreamGate My Service started successfully")

    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    sig := <-sigChan
    log.Info("Received shutdown signal", "signal", sig)

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := kernel.Shutdown(shutdownCtx); err != nil {
        log.Error("Error during shutdown", "error", err)
        os.Exit(1)
    }

    log.Info("StreamGate My Service stopped gracefully")
}
```

### Step 4: Register in Monolithic Mode

Update `cmd/monolith/streamgate/main.go`:

```go
// Add import
import "github.com/yourusername/streamgate/pkg/plugins/myservice"

// In main(), after registering API Gateway:
if err := kernel.RegisterPlugin(myservice.NewMyServicePlugin(log)); err != nil {
    log.Fatal("Failed to register my-service plugin", "error", err)
}
```

### Step 5: Add Build Target

Update `Makefile`:

```makefile
build-myservice:
    go build -o bin/myservice ./cmd/microservices/myservice
```

## Configuration

### Environment Variables

Override config with environment variables:

```bash
export APP_MODE=microservice
export APP_SERVICE_NAME=my-service
export SERVER_PORT=8080
export DATABASE_HOST=localhost
export REDIS_HOST=localhost
export NATS_URL=nats://localhost:4222
```

### Config File

Create `config.yaml`:

```yaml
app:
  name: streamgate
  mode: monolith
  port: 8080

server:
  port: 8080
  read_timeout: 30
  write_timeout: 30

database:
  host: localhost
  port: 5432
  user: postgres
  password: postgres
  database: streamgate

redis:
  host: localhost
  port: 6379

storage:
  type: minio
  endpoint: localhost:9000
  accesskey: minioadmin
  secretkey: minioadmin
  bucket: streamgate
```

## Logging

### Structured Logging with Zap

```go
// Info level
logger.Info("User created", "user_id", 123, "email", "user@example.com")

// Error level
logger.Error("Failed to create user", "error", err, "email", "user@example.com")

// Debug level
logger.Debug("Processing request", "request_id", "abc123")

// With fields
logger.With(zap.String("request_id", "abc123")).Info("Request started")
```

## Event Bus

### Publishing Events

```go
eventBus := kernel.GetEventBus()

event := &event.Event{
    Type:      "file.uploaded",
    Source:    "upload-service",
    Timestamp: time.Now().Unix(),
    Data: map[string]interface{}{
        "file_id": "123",
        "size":    1024,
    },
}

if err := eventBus.Publish(ctx, event); err != nil {
    logger.Error("Failed to publish event", "error", err)
}
```

### Subscribing to Events

```go
eventBus := kernel.GetEventBus()

handler := func(ctx context.Context, event *event.Event) error {
    logger.Info("Received event", "type", event.Type)
    // Handle event
    return nil
}

if err := eventBus.Subscribe(ctx, "file.uploaded", handler); err != nil {
    logger.Error("Failed to subscribe", "error", err)
}
```

## Testing

### Unit Tests

```bash
go test ./pkg/...
```

### Integration Tests

```bash
go test ./test/integration/...
```

### E2E Tests

```bash
go test ./test/e2e/...
```

## Deployment

### Docker

```bash
# Build image
docker build -f deploy/docker/Dockerfile.api-gateway -t streamgate:api-gateway .

# Run container
docker run -p 8080:8080 streamgate:api-gateway
```

### Kubernetes

```bash
# Deploy
kubectl apply -f deploy/k8s/

# Check status
kubectl get pods
kubectl get services

# View logs
kubectl logs deployment/streamgate-api-gateway
```

### Docker Compose

```bash
# Start all services
docker-compose up

# Stop all services
docker-compose down

# View logs
docker-compose logs -f api-gateway
```

## Monitoring

### Health Checks

```bash
# Monolithic
curl http://localhost:8080/health

# API Gateway
curl http://localhost:8080/health

# Readiness
curl http://localhost:8080/ready
```

### Metrics

```bash
# Prometheus
curl http://localhost:9090/metrics

# Jaeger UI
open http://localhost:16686

# Consul UI
open http://localhost:8500
```

## Troubleshooting

### Service won't start

1. Check logs: `docker-compose logs [service-name]`
2. Verify configuration: `cat config.yaml`
3. Check ports: `lsof -i :8080`
4. Check dependencies: `docker-compose ps`

### Connection errors

1. Verify NATS: `docker-compose logs nats`
2. Verify Consul: `docker-compose logs consul`
3. Verify PostgreSQL: `docker-compose logs postgres`
4. Verify Redis: `docker-compose logs redis`

### Performance issues

1. Check CPU: `docker stats`
2. Check memory: `docker stats`
3. Check logs for errors
4. Check Prometheus metrics

## Best Practices

### Error Handling

```go
if err != nil {
    logger.Error("Operation failed", "error", err, "context", "relevant_info")
    return fmt.Errorf("operation failed: %w", err)
}
```

### Context Usage

```go
// Always use context for cancellation
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

result, err := service.DoSomething(ctx)
```

### Graceful Shutdown

```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

sig := <-sigChan
logger.Info("Received signal", "signal", sig)

shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := kernel.Shutdown(shutdownCtx); err != nil {
    logger.Error("Shutdown error", "error", err)
}
```

### Configuration Management

```go
// Load configuration
cfg, err := config.LoadConfig()
if err != nil {
    logger.Fatal("Failed to load config", "error", err)
}

// Use configuration
logger.Info("Starting service", "port", cfg.Server.Port)
```

## Resources

- [Go Documentation](https://golang.org/doc)
- [Zap Logger](https://github.com/uber-go/zap)
- [Docker Documentation](https://docs.docker.com)
- [Kubernetes Documentation](https://kubernetes.io/docs)
- [NATS Documentation](https://docs.nats.io)
- [Consul Documentation](https://www.consul.io/docs)

---

**Last Updated**: 2025-01-28
**Version**: 1.0

