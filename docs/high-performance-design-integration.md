# High-Performance Architecture Design Integration Guide

> How to integrate principles from `high-performance-architecture.md` into system design and implementation

## 📋 Integration Overview

The design document (`.kiro/specs/offchain-content-service/design.md`) should integrate high-performance design principles in the following sections:

### Recommended Design Document Structure

```
## 1. System Architecture Overview (existing)
## 2. Core Component Design (existing)
## 3. Core Plugin Design (existing)
## 4. Deployment Mode Design (existing)
## 5. High-Performance Architecture Design (NEW) ⭐
   5.1 High Concurrency Design
   5.2 High Availability Design  
   5.3 Easy Scalability Design
   5.4 High Performance Optimization
   5.5 Debuggability Design
## 6. Fault Handling and High Availability (original chapter 5)
## 7. Monitoring and Observability (original chapter 6)
## 8. Summary (original chapter 7)
```

## 🎯 Chapter 5: High-Performance Architecture Design (New Content)

### 5.1 High Concurrency Design

#### 5.1.1 Connection Pool Configuration

**Database Connection Pool** (implement in Storage Plugin):
```go
// pkg/plugins/storage/db_pool.go
type DBPool struct {
    db *sql.DB
}

func NewDBPool(dsn string) (*DBPool, error) {
    db, err := sql.Open("postgres", dsn)
    if err != nil {
        return nil, err
    }
    
    // High-performance configuration
    db.SetMaxOpenConns(100)              // Max open connections
    db.SetMaxIdleConns(10)               // Max idle connections
    db.SetConnMaxLifetime(time.Hour)     // Connection max lifetime
    db.SetConnMaxIdleTime(10*time.Minute) // Idle timeout
    
    return &DBPool{db: db}, nil
}
```

**Redis Connection Pool** (implement in Cache Plugin):
```go
// pkg/plugins/cache/redis_pool.go
func NewRedisPool(addr string) *redis.Client {
    return redis.NewClient(&redis.Options{
        Addr:         addr,
        PoolSize:     100,              // Connection pool size
        MinIdleConns: 10,               // Min idle connections
        MaxRetries:   3,                // Retry count
        DialTimeout:  5 * time.Second,  // Connection timeout
        ReadTimeout:  3 * time.Second,  // Read timeout
        WriteTimeout: 3 * time.Second,  // Write timeout
    })
}
```

**HTTP Client Connection Pool** (implement in Blockchain Plugin):
```go
// pkg/plugins/blockchain/http_client.go
var httpClient = &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        MaxConnsPerHost:     100,
        IdleConnTimeout:     90 * time.Second,
        DisableKeepAlives:   false,
    },
}
```

#### 5.1.2 Worker Pool Implementation

**Video Transcoding Worker Pool** (implement in Transcoder Plugin):
```go
// pkg/plugins/transcoder/worker_pool.go
type WorkerPool struct {
    workers   int
    taskQueue chan TranscodeTask
    wg        sync.WaitGroup
}

func NewWorkerPool(workers int, queueSize int) *WorkerPool {
    return &WorkerPool{
        workers:   workers,
        taskQueue: make(chan TranscodeTask, queueSize),
    }
}

func (p *WorkerPool) Start(ctx context.Context) {
    for i := 0; i < p.workers; i++ {
        p.wg.Add(1)
        go p.worker(ctx, i)
    }
}

func (p *WorkerPool) worker(ctx context.Context, id int) {
    defer p.wg.Done()
    for {
        select {
        case task := <-p.taskQueue:
            if err := task.Execute(); err != nil {
                log.Error("transcode failed", "worker", id, "error", err)
            }
        case <-ctx.Done():
            return
        }
    }
}
```

#### 5.1.3 Rate Limiter Design

**Token Bucket Rate Limiter** (implement in Rate Limiter Plugin):
```go
// pkg/plugins/ratelimiter/token_bucket.go
type TokenBucket struct {
    rate     float64
    capacity int
    tokens   float64
    lastTime time.Time
    mu       sync.Mutex
}

func (tb *TokenBucket) Allow() bool {
    tb.mu.Lock()
    defer tb.mu.Unlock()
    
    now := time.Now()
    elapsed := now.Sub(tb.lastTime).Seconds()
    
    // Replenish tokens
    tb.tokens = math.Min(
        float64(tb.capacity),
        tb.tokens + elapsed*tb.rate,
    )
    tb.lastTime = now
    
    // Consume token
    if tb.tokens >= 1 {
        tb.tokens--
        return true
    }
    return false
}
```

### 5.2 High Availability Design

#### 5.2.1 Health Check Implementation

**Multi-Layer Health Checks** (implement in Microkernel Core):
```go
// pkg/core/health/checker.go
type HealthChecker struct {
    db    *sql.DB
    redis *redis.Client
    s3    *minio.Client
}

// Liveness probe
func (h *HealthChecker) LivenessCheck(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}

// Readiness probe
func (h *HealthChecker) ReadinessCheck(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()
    
    checks := []struct {
        name string
        fn   func(context.Context) error
    }{
        {"database", h.checkDatabase},
        {"redis", h.checkRedis},
        {"s3", h.checkS3},
    }
    
    results := make(map[string]string)
    healthy := true
    
    for _, check := range checks {
        if err := check.fn(ctx); err != nil {
            results[check.name] = "unhealthy: " + err.Error()
            healthy = false
        } else {
            results[check.name] = "healthy"
        }
    }
    
    if healthy {
        w.WriteHeader(http.StatusOK)
    } else {
        w.WriteHeader(http.StatusServiceUnavailable)
    }
    json.NewEncoder(w).Encode(results)
}
```

#### 5.2.2 Circuit Breaker Pattern

**Circuit Breaker Implementation** (implement in Blockchain Plugin):
```go
// pkg/plugins/blockchain/circuit_breaker.go
type CircuitBreaker struct {
    maxFailures  int
    timeout      time.Duration
    state        State
    failures     int
    lastFailTime time.Time
    mu           sync.RWMutex
}

type State int

const (
    StateClosed State = iota  // Normal
    StateOpen                  // Broken
    StateHalfOpen             // Recovering
)

func (cb *CircuitBreaker) Call(fn func() error) error {
    cb.mu.Lock()
    state := cb.state
    
    switch state {
    case StateOpen:
        if time.Since(cb.lastFailTime) > cb.timeout {
            cb.state = StateHalfOpen
            cb.failures = 0
            cb.mu.Unlock()
        } else {
            cb.mu.Unlock()
            return errors.New("circuit breaker is open")
        }
    case StateHalfOpen, StateClosed:
        cb.mu.Unlock()
    }
    
    err := fn()
    
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    if err != nil {
        cb.failures++
        cb.lastFailTime = time.Now()
        if cb.failures >= cb.maxFailures {
            cb.state = StateOpen
        }
        return err
    }
    
    if cb.state == StateHalfOpen {
        cb.state = StateClosed
        cb.failures = 0
    }
    return nil
}
```

#### 5.2.3 Graceful Shutdown

**5-Step Graceful Shutdown Process** (implement in Microkernel Core):
```go
// pkg/core/server/graceful_shutdown.go
func (s *Server) GracefulShutdown(ctx context.Context) error {
    log.Info("starting graceful shutdown")
    
    // Step 1: Stop accepting new requests
    s.httpServer.SetKeepAlivesEnabled(false)
    
    // Step 2: Deregister from service registry
    if err := s.registry.Deregister(s.instanceID); err != nil {
        log.Error("failed to deregister", "error", err)
    }
    time.Sleep(5 * time.Second) // Wait for load balancer update
    
    // Step 3: Stop all workers
    for _, worker := range s.workers {
        worker.Stop()
    }
    
    // Step 4: Wait for existing requests to complete
    shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
        log.Error("http server shutdown error", "error", err)
    }
    
    // Step 5: Close database connections
    if err := s.db.Close(); err != nil {
        log.Error("failed to close database", "error", err)
    }
    
    log.Info("graceful shutdown completed")
    return nil
}
```

### 5.3 Easy Scalability Design

#### 5.3.1 Stateless Service

**State Externalization** (all plugins should follow):
```go
// ❌ Wrong: State stored in memory
type Server struct {
    sessions map[string]*Session  // Cannot scale horizontally!
}

// ✅ Correct: State stored in Redis
type Server struct {
    redis *redis.Client
}

func (s *Server) GetSession(sessionID string) (*Session, error) {
    data, err := s.redis.Get(context.Background(), "session:"+sessionID).Bytes()
    if err != nil {
        return nil, err
    }
    var session Session
    json.Unmarshal(data, &session)
    return &session, nil
}
```

#### 5.3.2 Distributed Lock

**Distributed Lock Implementation** (implement in Cache Plugin):
```go
// pkg/plugins/cache/distributed_lock.go
type DistributedLock struct {
    redis *redis.Client
    key   string
    value string
    ttl   time.Duration
}

func (l *DistributedLock) Acquire(ctx context.Context) (bool, error) {
    ok, err := l.redis.SetNX(ctx, l.key, l.value, l.ttl).Result()
    if err != nil {
        return false, err
    }
    if ok {
        go l.renew(ctx)
    }
    return ok, nil
}

func (l *DistributedLock) Release(ctx context.Context) error {
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("del", KEYS[1])
        else
            return 0
        end
    `
    return l.redis.Eval(ctx, script, []string{l.key}, l.value).Err()
}
```

#### 5.3.3 Consistent Hashing

**Consistent Hashing Implementation** (implement in Cache Plugin):
```go
// pkg/plugins/cache/consistent_hash.go
type ConsistentHash struct {
    circle map[uint32]string
    nodes  []string
    vnodes int
    mu     sync.RWMutex
}

func (ch *ConsistentHash) AddNode(node string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()
    
    ch.nodes = append(ch.nodes, node)
    for i := 0; i < ch.vnodes; i++ {
        hash := ch.hash(fmt.Sprintf("%s#%d", node, i))
        ch.circle[hash] = node
    }
}

func (ch *ConsistentHash) GetNode(key string) string {
    ch.mu.RLock()
    defer ch.mu.RUnlock()
    
    hash := ch.hash(key)
    // Find first node >= hash
    // ... implementation details
    return node
}
```

### 5.4 High Performance Optimization

#### 5.4.1 Object Pool

**Buffer Object Pool** (implement in Transcoder Plugin):
```go
// pkg/plugins/transcoder/buffer_pool.go
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func ProcessData(data []byte) ([]byte, error) {
    buf := bufferPool.Get().(*bytes.Buffer)
    defer func() {
        buf.Reset()
        bufferPool.Put(buf)
    }()
    
    buf.Write(data)
    // Process...
    return buf.Bytes(), nil
}
```

#### 5.4.2 Zero-Copy

**File Streaming** (implement in Streaming Plugin):
```go
// pkg/plugins/streaming/zero_copy.go
func StreamFile(w http.ResponseWriter, filePath string) error {
    file, err := os.Open(filePath)
    if err != nil {
        return err
    }
    defer file.Close()
    
    // Zero-copy: direct file to network
    _, err = io.Copy(w, file)
    return err
}
```

#### 5.4.3 Batch Query Optimization

**Multicall Batch Query** (implement in Blockchain Plugin):
```go
// pkg/plugins/blockchain/multicall.go
type Multicall struct {
    contract *Contract
}

func (m *Multicall) BatchCheckBalance(users []string, contract string) (map[string]int, error) {
    calls := make([]Call, len(users))
    for i, user := range users {
        calls[i] = Call{
            Target: contract,
            CallData: encodeBalanceOf(user),
        }
    }
    
    // Single RPC call for all results
    results, err := m.contract.Aggregate(calls)
    if err != nil {
        return nil, err
    }
    
    balances := make(map[string]int)
    for i, result := range results {
        balances[users[i]] = decodeBalance(result)
    }
    return balances, nil
}
```

### 5.5 Debuggability Design

#### 5.5.1 Structured Logging

**Unified Log Format** (implement in Microkernel Core):
```go
// pkg/core/logger/logger.go
import "go.uber.org/zap"

func InitLogger(env string) *zap.Logger {
    var config zap.Config
    if env == "production" {
        config = zap.NewProductionConfig()
    } else {
        config = zap.NewDevelopmentConfig()
    }
    
    config.EncoderConfig.TimeKey = "timestamp"
    config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
    config.OutputPaths = []string{"stdout", "/var/log/app.log"}
    
    logger, _ := config.Build()
    return logger
}

// Usage example
logger.Info("processing request",
    zap.String("request_id", requestID),
    zap.String("user_id", userID),
    zap.Duration("duration", duration),
)
```

#### 5.5.2 Distributed Tracing

**OpenTelemetry Integration** (implement in Microkernel Core):
```go
// pkg/core/tracing/tracer.go
import "go.opentelemetry.io/otel"

func InitTracer(serviceName string) (trace.TracerProvider, error) {
    exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(
        jaeger.WithEndpoint("http://jaeger:14268/api/traces"),
    ))
    if err != nil {
        return nil, err
    }
    
    tp := tracesdk.NewTracerProvider(
        tracesdk.WithBatcher(exporter),
        tracesdk.WithResource(resource.NewWithAttributes(
            semconv.ServiceNameKey.String(serviceName),
        )),
    )
    
    otel.SetTracerProvider(tp)
    return tp, nil
}

// HTTP middleware
func TracingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        tracer := otel.Tracer("http-server")
        ctx, span := tracer.Start(r.Context(), r.URL.Path)
        defer span.End()
        
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

#### 5.5.3 Performance Analysis

**pprof Integration** (implement in Microkernel Core):
```go
// pkg/core/debug/pprof.go
import _ "net/http/pprof"

func StartDebugServer() {
    go func() {
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()
}

// Usage:
// CPU profile: go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
// Memory profile: go tool pprof http://localhost:6060/debug/pprof/heap
// Goroutine profile: go tool pprof http://localhost:6060/debug/pprof/goroutine
```

## 📊 Performance Targets

| Metric | Target | Implementation |
|--------|--------|-----------------|
| Concurrent Connections | 10K+ | Goroutine + Connection Pools |
| API Latency P95 | < 200ms | Multi-level Cache + Async Processing |
| Availability | 99.9% | Circuit Breaker + Failover + Health Checks |
| Horizontal Scaling | Linear | Stateless + Consistent Hashing |
| Memory Usage | < 500MB | Object Pools + Zero-Copy |

## 🔧 Application During Coding

### Each Plugin Should Consider:

1. **High Concurrency**
   - Use connection pools instead of creating new connections each time
   - Use Worker Pool to control concurrency
   - Use lock-free data structures (atomic, sync.Map)

2. **High Availability**
   - Implement health check interface
   - Use circuit breaker for critical external calls
   - Implement graceful shutdown

3. **Easy Scalability**
   - Don't store state in memory
   - Use distributed locks instead of local locks
   - Support horizontal scaling

4. **High Performance**
   - Use object pools to reduce GC pressure
   - Use zero-copy for large file transfers
   - Batch queries instead of looping queries

5. **Debuggability**
   - Use structured logging
   - Add distributed tracing
   - Expose performance metrics

## 📝 Next Steps

1. **Update Design Document**: Insert this chapter content after chapter 4 in `.kiro/specs/offchain-content-service/design.md`
2. **Update Task List**: Add high-performance implementation tasks for each plugin in `tasks.md`
3. **Start Coding**: Implement each plugin following the high-performance principles in the design document

## 🎯 Key Principles

Remember: **Consider high-performance during design phase, not after!**

- ✅ Use connection pools during design
- ✅ Consider stateless design during design
- ✅ Add monitoring during design
- ❌ Don't wait for performance problems to optimize
