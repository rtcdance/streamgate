# High PerformanceArchitecture Design Guide

> Showcase 10+ years of C++ experience in Go projects

## �� Architecture Goals

- **High Concurrency**: Support 10K+ concurrent connections
- **High Availability**: 99.9% availability (< 8.76 hours downtime per year)
- **Easy Scalability**: Horizontal scaling, stateless design
- **High Performance**: P95 latency < 200ms
- **Debuggability**: Complete logging, tracing, monitoring

## 1. High Concurrency Design

### 1.1 Go Concurrency Model (vs C++ Threads)

#### C++ Thread Model
```cpp
// C++ traditional way
std::vector<std::thread> threads;
for (int i = 0; i < 1000; i++) {
    threads.emplace_back([i]() {
        processRequest(i);
    });
}
// Problem: 1000 threads = 1000MB+ memory
```

#### Go Goroutine Model
```go
// Go way (lightweight)
for i := 0; i < 10000; i++ {
    go func(id int) {
        processRequest(id)
    }(i)
}
// Advantage: 10000 goroutines = ~20MB memory
```

**Key Differences**:
- C++ thread: ~1MB stack space
- Go goroutine: ~2KB initial stack space, dynamic growth
- Go scheduler: M:N model, automatic load balancing

### 1.2 连接池设计

#### 数据库连接池
```go
type DBPool struct {
    db          *sql.DB
    maxOpen     int
    maxIdle     int
    maxLifetime time.Duration
}

func NewDBPool(dsn string) (*DBPool, error) {
    db, err := sql.Open("postgres", dsn)
    if err != nil {
        return nil, err
    }
    
    // Connection pool configuration (critical!)
    db.SetMaxOpenConns(100)        // Max open connections
    db.SetMaxIdleConns(10)         // Max idle connections
    db.SetConnMaxLifetime(time.Hour) // Connection max lifetime
    db.SetConnMaxIdleTime(10 * time.Minute) // Idle connection timeout
    
    return &DBPool{
        db:          db,
        maxOpen:     100,
        maxIdle:     10,
        maxLifetime: time.Hour,
    }, nil
}

// Connection pool monitoring
func (p *DBPool) Stats() sql.DBStats {
    stats := p.db.Stats()
    
    // Key metrics
    log.Info("db pool stats",
        "open_connections", stats.OpenConnections,
        "in_use", stats.InUse,
        "idle", stats.Idle,
        "wait_count", stats.WaitCount,
        "wait_duration", stats.WaitDuration,
    )
    
    // Alert: wait time too long
    if stats.WaitDuration > time.Second {
        log.Warn("db pool congestion detected")
    }
    
    return stats
}
```

#### Redis Connection Pool
```go
func NewRedisPool(addr string) *redis.Client {
    return redis.NewClient(&redis.Options{
        Addr:         addr,
        PoolSize:     100,              // 连接池大小
        MinIdleConns: 10,               // 最小空闲连接
        MaxRetries:   3,                // 重试次数
        DialTimeout:  5 * time.Second,  // Connection timeout
        ReadTimeout:  3 * time.Second,  // 读超时
        WriteTimeout: 3 * time.Second,  // 写超时
        PoolTimeout:  4 * time.Second,  // 获取Connection timeout
        
        // 连接健康检查
        OnConnect: func(ctx context.Context, cn *redis.Conn) error {
            return cn.Ping(ctx).Err()
        },
    })
}
```

#### HTTP Client Connection Pool
```go
// Global HTTP client (connection reuse)
var httpClient = &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,              // Max idle connections
        MaxIdleConnsPerHost: 10,               // 每个 host Max idle connections
        MaxConnsPerHost:     100,              // Max connections per host
        IdleConnTimeout:     90 * time.Second, // Idle connection timeout
        DisableKeepAlives:   false,            // Enable Keep-Alive
        
        // Connection timeout
        DialContext: (&net.Dialer{
            Timeout:   5 * time.Second,
            KeepAlive: 30 * time.Second,
        }).DialContext,
        
        // TLS handshake timeout
        TLSHandshakeTimeout: 10 * time.Second,
    },
}
```

### 1.3 并发控制

#### Worker Pool Pattern
```go
type WorkerPool struct {
    workers   int
    taskQueue chan Task
    wg        sync.WaitGroup
}

func NewWorkerPool(workers int, queueSize int) *WorkerPool {
    return &WorkerPool{
        workers:   workers,
        taskQueue: make(chan Task, queueSize),
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
            // Process task
            if err := task.Execute(); err != nil {
                log.Error("task failed", "worker", id, "error", err)
            }
            
        case <-ctx.Done():
            log.Info("worker stopping", "id", id)
            return
        }
    }
}

func (p *WorkerPool) Submit(task Task) error {
    select {
    case p.taskQueue <- task:
        return nil
    default:
        return errors.New("queue full")
    }
}

// 使用示例: 视频转码
transcoderPool := NewWorkerPool(
    runtime.NumCPU(),  // worker count = CPU cores
    1000,              // queue size
)
transcoderPool.Start(ctx)
```

#### Semaphore Rate Limiting
```go
type Semaphore struct {
    sem chan struct{}
}

func NewSemaphore(n int) *Semaphore {
    return &Semaphore{
        sem: make(chan struct{}, n),
    }
}

func (s *Semaphore) Acquire() {
    s.sem <- struct{}{}
}

func (s *Semaphore) Release() {
    <-s.sem
}

// 使用示例: 限制并发 RPC 调用
var rpcSemaphore = NewSemaphore(100) // Max 100 concurrent RPC

func CallRPC() error {
    rpcSemaphore.Acquire()
    defer rpcSemaphore.Release()
    
    // RPC 调用
    return doRPC()
}
```

#### Token Bucket Rate Limiting
```go
type TokenBucket struct {
    rate     float64       // Tokens generated per second
    capacity int           // Bucket capacity
    tokens   float64       // Current tokens
    lastTime time.Time     // Last update time
    mu       sync.Mutex
}

func NewTokenBucket(rate float64, capacity int) *TokenBucket {
    return &TokenBucket{
        rate:     rate,
        capacity: capacity,
        tokens:   float64(capacity),
        lastTime: time.Now(),
    }
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

// 使用示例
var apiLimiter = NewTokenBucket(100, 1000) // 100 req/s, Bucket capacity 1000

func HandleRequest(w http.ResponseWriter, r *http.Request) {
    if !apiLimiter.Allow() {
        http.Error(w, "Rate limit exceeded", 429)
        return
    }
    
    // 处理请求
}
```

### 1.4 Lock-free data structures

#### Atomic 操作
```go
type Counter struct {
    value int64
}

// ❌ 错误: 需要锁
func (c *Counter) IncWithLock() {
    mu.Lock()
    c.value++
    mu.Unlock()
}

// ✅ 正确: 无锁
func (c *Counter) Inc() {
    atomic.AddInt64(&c.value, 1)
}

func (c *Counter) Get() int64 {
    return atomic.LoadInt64(&c.value)
}
```

#### sync.Map(并发安全的 map)
```go
// ❌ 错误: 普通 map 不是并发安全的
var cache = make(map[string]interface{})

// ✅ 正确: 使用 sync.Map
var cache sync.Map

func Get(key string) (interface{}, bool) {
    return cache.Load(key)
}

func Set(key string, value interface{}) {
    cache.Store(key, value)
}

// 适用场景: 
// 1. 读多写少
// 2. key 集合稳定
```

#### Channel 作为队列
```go
// 无锁队列
type Queue struct {
    items chan interface{}
}

func NewQueue(size int) *Queue {
    return &Queue{
        items: make(chan interface{}, size),
    }
}

func (q *Queue) Push(item interface{}) error {
    select {
    case q.items <- item:
        return nil
    default:
        return errors.New("queue full")
    }
}

func (q *Queue) Pop() (interface{}, error) {
    select {
    case item := <-q.items:
        return item, nil
    default:
        return nil, errors.New("queue empty")
    }
}
```


## 2. High Availability设计

### 2.1 服务健康检查

#### Multi-Layer Health Checks
```go
type HealthChecker struct {
    db    *sql.DB
    redis *redis.Client
    s3    *minio.Client
}

// Liveness 探针(Process is alive)
func (h *HealthChecker) LivenessCheck(w http.ResponseWriter, r *http.Request) {
    // Simple check: process responds
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}

// Readiness 探针(Ready to receive traffic)
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

func (h *HealthChecker) checkDatabase(ctx context.Context) error {
    return h.db.PingContext(ctx)
}

func (h *HealthChecker) checkRedis(ctx context.Context) error {
    return h.redis.Ping(ctx).Err()
}

func (h *HealthChecker) checkS3(ctx context.Context) error {
    _, err := h.s3.ListBuckets(ctx)
    return err
}
```

### 2.2 Circuit Breaker Pattern

#### State Machine Implementation
```go
type CircuitBreaker struct {
    maxFailures  int
    timeout      time.Duration
    state        State
    failures     int
    lastFailTime time.Time
    mu           sync.RWMutex
    
    // Monitoring metrics
    totalCalls   int64
    successCalls int64
    failedCalls  int64
}

type State int

const (
    StateClosed State = iota  // Normal
    StateOpen                  // Broken
    StateHalfOpen             // Half-Open (recovering)
)

func (cb *CircuitBreaker) Call(fn func() error) error {
    atomic.AddInt64(&cb.totalCalls, 1)
    
    cb.mu.Lock()
    state := cb.state
    
    switch state {
    case StateOpen:
        // Check if can enter half-open state
        if time.Since(cb.lastFailTime) > cb.timeout {
            cb.state = StateHalfOpen
            cb.failures = 0
            cb.mu.Unlock()
        } else {
            cb.mu.Unlock()
            return errors.New("circuit breaker is open")
        }
        
    case StateHalfOpen:
        cb.mu.Unlock()
        
    case StateClosed:
        cb.mu.Unlock()
    }
    
    // Execute function
    err := fn()
    
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    if err != nil {
        atomic.AddInt64(&cb.failedCalls, 1)
        cb.failures++
        cb.lastFailTime = time.Now()
        
        if cb.failures >= cb.maxFailures {
            cb.state = StateOpen
            log.Warn("circuit breaker opened",
                "failures", cb.failures,
                "max", cb.maxFailures,
            )
        }
        
        return err
    }
    
    // Success
    atomic.AddInt64(&cb.successCalls, 1)
    
    if cb.state == StateHalfOpen {
        cb.state = StateClosed
        cb.failures = 0
        log.Info("circuit breaker closed")
    }
    
    return nil
}

// Monitoring metrics
func (cb *CircuitBreaker) Stats() map[string]interface{} {
    cb.mu.RLock()
    defer cb.mu.RUnlock()
    
    total := atomic.LoadInt64(&cb.totalCalls)
    success := atomic.LoadInt64(&cb.successCalls)
    failed := atomic.LoadInt64(&cb.failedCalls)
    
    var successRate float64
    if total > 0 {
        successRate = float64(success) / float64(total) * 100
    }
    
    return map[string]interface{}{
        "state":        cb.state.String(),
        "total_calls":  total,
        "success_rate": successRate,
        "failures":     cb.failures,
    }
}
```

### 2.3 Graceful Shutdown

#### Complete shutdown process
```go
type Server struct {
    httpServer *http.Server
    grpcServer *grpc.Server
    registry   ServiceRegistry
    workers    []*Worker
}

func (s *Server) GracefulShutdown(ctx context.Context) error {
    log.Info("starting graceful shutdown")
    
    // 1. Stop accepting new requests
    log.Info("step 1: stop accepting new requests")
    s.httpServer.SetKeepAlivesEnabled(false)
    
    // 2. Deregister from service registry
    log.Info("step 2: deregister from service registry")
    if err := s.registry.Deregister(s.instanceID); err != nil {
        log.Error("failed to deregister", "error", err)
    }
    
    // Wait for load balancer update (important!)
    time.Sleep(5 * time.Second)
    
    // 3. Stop all workers
    log.Info("step 3: stop workers")
    for _, worker := range s.workers {
        worker.Stop()
    }
    
    // 4. Wait for existing requests to complete
    log.Info("step 4: wait for existing requests")
    shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    // HTTP 服务器Graceful Shutdown
    if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
        log.Error("http server shutdown error", "error", err)
    }
    
    // gRPC 服务器Graceful Shutdown
    stopped := make(chan struct{})
    go func() {
        s.grpcServer.GracefulStop()
        close(stopped)
    }()
    
    select {
    case <-stopped:
        log.Info("grpc server stopped gracefully")
    case <-shutdownCtx.Done():
        log.Warn("grpc server force stopped")
        s.grpcServer.Stop()
    }
    
    // 5. Close database connections
    log.Info("step 5: close database connections")
    if err := s.db.Close(); err != nil {
        log.Error("failed to close database", "error", err)
    }
    
    log.Info("graceful shutdown completed")
    return nil
}

// 主函数中使用
func main() {
    server := NewServer()
    
    // 启动服务
    go server.Start()
    
    // Listen for signals
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    
    <-sigChan
    log.Info("received shutdown signal")
    
    // Graceful Shutdown
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()
    
    if err := server.GracefulShutdown(ctx); err != nil {
        log.Error("shutdown error", "error", err)
        os.Exit(1)
    }
    
    os.Exit(0)
}
```

### 2.4 故障转移

#### Master-slave switching
```go
type DatabaseCluster struct {
    primary   *sql.DB
    replicas  []*sql.DB
    mu        sync.RWMutex
    isPrimary bool
}

func (dc *DatabaseCluster) Query(query string, args ...interface{}) (*sql.Rows, error) {
    dc.mu.RLock()
    defer dc.mu.RUnlock()
    
    // 读操作: 优先使用从库
    if isReadQuery(query) && len(dc.replicas) > 0 {
        // Round-robin选择从库
        replica := dc.replicas[rand.Intn(len(dc.replicas))]
        rows, err := replica.Query(query, args...)
        if err == nil {
            return rows, nil
        }
        
        log.Warn("replica query failed, fallback to primary", "error", err)
    }
    
    // 写操作或从库失败: 使用主库
    return dc.primary.Query(query, args...)
}

// 健康检查和自动切换
func (dc *DatabaseCluster) HealthCheck(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            // 检查主库
            if err := dc.primary.PingContext(ctx); err != nil {
                log.Error("primary database unhealthy", "error", err)
                
                // 尝试切换到从库
                dc.promoteReplica()
            }
            
            // 检查从库
            for i, replica := range dc.replicas {
                if err := replica.PingContext(ctx); err != nil {
                    log.Warn("replica unhealthy", "index", i, "error", err)
                }
            }
            
        case <-ctx.Done():
            return
        }
    }
}

func (dc *DatabaseCluster) promoteReplica() {
    dc.mu.Lock()
    defer dc.mu.Unlock()
    
    if len(dc.replicas) == 0 {
        log.Error("no replicas available for promotion")
        return
    }
    
    // 提升第一个健康的从库为主库
    for i, replica := range dc.replicas {
        if err := replica.Ping(); err == nil {
            log.Info("promoting replica to primary", "index", i)
            
            oldPrimary := dc.primary
            dc.primary = replica
            dc.replicas = append(dc.replicas[:i], dc.replicas[i+1:]...)
            
            // 旧主库降级为从库(如果恢复)
            go func() {
                time.Sleep(30 * time.Second)
                if err := oldPrimary.Ping(); err == nil {
                    dc.mu.Lock()
                    dc.replicas = append(dc.replicas, oldPrimary)
                    dc.mu.Unlock()
                    log.Info("old primary rejoined as replica")
                }
            }()
            
            return
        }
    }
    
    log.Error("no healthy replicas found")
}
```


## 3. Easy Scalability设计

### 3.1 Stateless Service

#### State Externalization
```go
// ❌ 错误: State stored in memory
type Server struct {
    sessions map[string]*Session  // Cannot scale horizontally!
}

// ✅ 正确: State stored in Redis
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

func (s *Server) SetSession(sessionID string, session *Session) error {
    data, _ := json.Marshal(session)
    return s.redis.Set(context.Background(), "session:"+sessionID, data, 30*time.Minute).Err()
}
```

#### Distributed Lock
```go
type DistributedLock struct {
    redis *redis.Client
    key   string
    value string
    ttl   time.Duration
}

func NewDistributedLock(redis *redis.Client, key string, ttl time.Duration) *DistributedLock {
    return &DistributedLock{
        redis: redis,
        key:   "lock:" + key,
        value: uuid.New().String(),
        ttl:   ttl,
    }
}

func (l *DistributedLock) Acquire(ctx context.Context) (bool, error) {
    // SET key value NX EX ttl
    ok, err := l.redis.SetNX(ctx, l.key, l.value, l.ttl).Result()
    if err != nil {
        return false, err
    }
    
    if ok {
        // 启动续期 goroutine
        go l.renew(ctx)
    }
    
    return ok, nil
}

func (l *DistributedLock) Release(ctx context.Context) error {
    // Lua 脚本: 只删除自己的锁
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("del", KEYS[1])
        else
            return 0
        end
    `
    
    return l.redis.Eval(ctx, script, []string{l.key}, l.value).Err()
}

func (l *DistributedLock) renew(ctx context.Context) {
    ticker := time.NewTicker(l.ttl / 3)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            // 续期
            l.redis.Expire(ctx, l.key, l.ttl)
            
        case <-ctx.Done():
            return
        }
    }
}

// 使用示例: Prevent duplicate processing
func ProcessTask(taskID string) error {
    lock := NewDistributedLock(redis, "task:"+taskID, 30*time.Second)
    
    acquired, err := lock.Acquire(context.Background())
    if err != nil {
        return err
    }
    if !acquired {
        return errors.New("task already processing")
    }
    defer lock.Release(context.Background())
    
    // Process task
    return doProcess(taskID)
}
```

### 3.2 水平扩展

#### Consistent Hashing
```go
type ConsistentHash struct {
    circle map[uint32]string
    nodes  []string
    vnodes int  // Virtual nodes
    mu     sync.RWMutex
}

func NewConsistentHash(vnodes int) *ConsistentHash {
    return &ConsistentHash{
        circle: make(map[uint32]string),
        vnodes: vnodes,
    }
}

func (ch *ConsistentHash) AddNode(node string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()
    
    ch.nodes = append(ch.nodes, node)
    
    // Add virtual nodes
    for i := 0; i < ch.vnodes; i++ {
        hash := ch.hash(fmt.Sprintf("%s#%d", node, i))
        ch.circle[hash] = node
    }
}

func (ch *ConsistentHash) RemoveNode(node string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()
    
    // Remove virtual nodes
    for i := 0; i < ch.vnodes; i++ {
        hash := ch.hash(fmt.Sprintf("%s#%d", node, i))
        delete(ch.circle, hash)
    }
    
    // Remove node
    for i, n := range ch.nodes {
        if n == node {
            ch.nodes = append(ch.nodes[:i], ch.nodes[i+1:]...)
            break
        }
    }
}

func (ch *ConsistentHash) GetNode(key string) string {
    ch.mu.RLock()
    defer ch.mu.RUnlock()
    
    if len(ch.circle) == 0 {
        return ""
    }
    
    hash := ch.hash(key)
    
    // Find first node >= hash
    var keys []uint32
    for k := range ch.circle {
        keys = append(keys, k)
    }
    sort.Slice(keys, func(i, j int) bool {
        return keys[i] < keys[j]
    })
    
    for _, k := range keys {
        if k >= hash {
            return ch.circle[k]
        }
    }
    
    // Ring, return first node
    return ch.circle[keys[0]]
}

func (ch *ConsistentHash) hash(key string) uint32 {
    h := fnv.New32a()
    h.Write([]byte(key))
    return h.Sum32()
}

// 使用示例: Cache sharding
var cacheNodes = NewConsistentHash(150)

func init() {
    cacheNodes.AddNode("cache-1:6379")
    cacheNodes.AddNode("cache-2:6379")
    cacheNodes.AddNode("cache-3:6379")
}

func GetCache(key string) (string, error) {
    node := cacheNodes.GetNode(key)
    client := getRedisClient(node)
    return client.Get(context.Background(), key).Result()
}
```

#### 分片策略
```go
// 数据库分片
type ShardedDB struct {
    shards []*sql.DB
}

func (s *ShardedDB) GetShard(userID int64) *sql.DB {
    // 简单取模分片
    index := userID % int64(len(s.shards))
    return s.shards[index]
}

func (s *ShardedDB) QueryUser(userID int64) (*User, error) {
    db := s.GetShard(userID)
    
    var user User
    err := db.QueryRow("SELECT * FROM users WHERE id = $1", userID).Scan(&user)
    return &user, err
}

// 范围分片
type RangeShardedDB struct {
    shards []struct {
        db    *sql.DB
        start int64
        end   int64
    }
}

func (s *RangeShardedDB) GetShard(userID int64) *sql.DB {
    for _, shard := range s.shards {
        if userID >= shard.start && userID < shard.end {
            return shard.db
        }
    }
    return s.shards[len(s.shards)-1].db
}
```

### 3.3 动态扩缩容

#### HPA 配置
```yaml
# k8s/hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-gateway-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-gateway
  minReplicas: 3
  maxReplicas: 20
  metrics:
  # CPU 指标
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  # 内存指标
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
  # 自定义指标: 请求数
  - type: Pods
    pods:
      metric:
        name: http_requests_per_second
      target:
        type: AverageValue
        averageValue: "1000"
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300  # 5 分钟稳定期
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
        value: 4
        periodSeconds: 30
      selectPolicy: Max
```

## 4. High Performance优化

### 4.1 内存优化

#### Object Pool
```go
// sync.Pool: 自动管理的Object Pool
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func ProcessData(data []byte) ([]byte, error) {
    // Get buffer from pool
    buf := bufferPool.Get().(*bytes.Buffer)
    defer func() {
        buf.Reset()
        bufferPool.Put(buf)  // Return to pool
    }()
    
    // 使用 buffer
    buf.Write(data)
    // ... 处理 ...
    
    return buf.Bytes(), nil
}

// 自定义Object Pool
type TranscodeTaskPool struct {
    pool chan *TranscodeTask
}

func NewTranscodeTaskPool(size int) *TranscodeTaskPool {
    p := &TranscodeTaskPool{
        pool: make(chan *TranscodeTask, size),
    }
    
    // Pre-allocate objects
    for i := 0; i < size; i++ {
        p.pool <- &TranscodeTask{}
    }
    
    return p
}

func (p *TranscodeTaskPool) Get() *TranscodeTask {
    select {
    case task := <-p.pool:
        return task
    default:
        return &TranscodeTask{}
    }
}

func (p *TranscodeTaskPool) Put(task *TranscodeTask) {
    task.Reset()  // Reset state
    
    select {
    case p.pool <- task:
    default:
        // Pool full, discard
    }
}
```

#### Zero-Copy
```go
// Use io.Copy to avoid extra memory allocation
func StreamFile(w http.ResponseWriter, filePath string) error {
    file, err := os.Open(filePath)
    if err != nil {
        return err
    }
    defer file.Close()
    
    // Zero-Copy: Direct file to network
    _, err = io.Copy(w, file)
    return err
}

// Use sendfile system call (Linux)
func SendFile(conn net.Conn, file *os.File) error {
    // Go 的 io.Copy 在 Linux 上会自动使用 sendfile
    _, err := io.Copy(conn, file)
    return err
}
```

#### Memory Alignment
```go
// ❌ 不好: Memory not aligned
type BadStruct struct {
    a bool   // 1 byte
    b int64  // 8 bytes
    c bool   // 1 byte
    d int64  // 8 bytes
}
// Actual size: 32 bytes(Due to padding)

// ✅ 好: Memory Alignment
type GoodStruct struct {
    b int64  // 8 bytes
    d int64  // 8 bytes
    a bool   // 1 byte
    c bool   // 1 byte
}
// Actual size: 24 bytes

// Use tool to check
// go install golang.org/x/tools/go/analysis/passes/fieldalignment/cmd/fieldalignment@latest
// fieldalignment ./...
```

### 4.2 CPU 优化

#### Reduce system calls
```go
// ❌ Bad: Frequent small writes
for _, line := range lines {
    file.Write([]byte(line + "\n"))  // Each is a system call
}

// ✅ Good: Batch writes
buf := bufio.NewWriter(file)
for _, line := range lines {
    buf.WriteString(line + "\n")  // Write to buffer
}
buf.Flush()  // One system call
```

#### SIMD Optimization (using assembly)
```go
// For performance-critical paths, can use assembly
// 例如: Batch data processing

//go:noescape
func addVectors(a, b, result []float64)

// add_amd64.s
TEXT ·addVectors(SB), NOSPLIT, $0-72
    // SIMD 指令
    MOVQ a+0(FP), SI
    MOVQ b+24(FP), DI
    MOVQ result+48(FP), DX
    MOVQ a+8(FP), CX
    
loop:
    VMOVUPD (SI), Y0
    VMOVUPD (DI), Y1
    VADDPD Y0, Y1, Y2
    VMOVUPD Y2, (DX)
    
    ADDQ $32, SI
    ADDQ $32, DI
    ADDQ $32, DX
    SUBQ $4, CX
    JNZ loop
    
    RET
```

#### 并行处理
```go
// Process large amounts of data in parallel
func ProcessInParallel(items []Item) []Result {
    numWorkers := runtime.NumCPU()
    results := make([]Result, len(items))
    
    var wg sync.WaitGroup
    chunkSize := (len(items) + numWorkers - 1) / numWorkers
    
    for i := 0; i < numWorkers; i++ {
        start := i * chunkSize
        end := start + chunkSize
        if end > len(items) {
            end = len(items)
        }
        
        wg.Add(1)
        go func(start, end int) {
            defer wg.Done()
            
            for j := start; j < end; j++ {
                results[j] = ProcessItem(items[j])
            }
        }(start, end)
    }
    
    wg.Wait()
    return results
}
```

### 4.3 Network Optimization

#### HTTP/2 Multiplexing
```go
// Enable HTTP/2
server := &http.Server{
    Addr:    ":8080",
    Handler: handler,
    // HTTP/2 自动启用(If using TLS)
}

// 配置 TLS
tlsConfig := &tls.Config{
    NextProtos: []string{"h2", "http/1.1"},  // Prefer HTTP/2
}

server.TLSConfig = tlsConfig
server.ListenAndServeTLS("cert.pem", "key.pem")
```

#### gRPC Connection Reuse
```go
// Client connection pool
type GRPCPool struct {
    conns []*grpc.ClientConn
    next  uint32
}

func NewGRPCPool(target string, size int) (*GRPCPool, error) {
    pool := &GRPCPool{
        conns: make([]*grpc.ClientConn, size),
    }
    
    for i := 0; i < size; i++ {
        conn, err := grpc.Dial(target,
            grpc.WithInsecure(),
            grpc.WithKeepaliveParams(keepalive.ClientParameters{
                Time:                10 * time.Second,
                Timeout:             3 * time.Second,
                PermitWithoutStream: true,
            }),
        )
        if err != nil {
            return nil, err
        }
        pool.conns[i] = conn
    }
    
    return pool, nil
}

func (p *GRPCPool) GetConn() *grpc.ClientConn {
    // Round-robin
    n := atomic.AddUint32(&p.next, 1)
    return p.conns[n%uint32(len(p.conns))]
}
```

#### TCP Optimization
```go
// Configure TCP parameters
listener, err := net.Listen("tcp", ":8080")
if err != nil {
    log.Fatal(err)
}

// Set TCP parameters
if tcpListener, ok := listener.(*net.TCPListener); ok {
    tcpListener.SetDeadline(time.Now().Add(30 * time.Second))
}

// 接受连接时设置参数
for {
    conn, err := listener.Accept()
    if err != nil {
        continue
    }
    
    if tcpConn, ok := conn.(*net.TCPConn); ok {
        tcpConn.SetKeepAlive(true)
        tcpConn.SetKeepAlivePeriod(30 * time.Second)
        tcpConn.SetNoDelay(true)  // Disable Nagle algorithm
    }
    
    go handleConnection(conn)
}
```


## 5. Debuggability设计

### 5.1 Structured Logging

#### Unified log format
```go
import "go.uber.org/zap"

// Initialize logger
func InitLogger(env string) *zap.Logger {
    var config zap.Config
    
    if env == "production" {
        config = zap.NewProductionConfig()
    } else {
        config = zap.NewDevelopmentConfig()
    }
    
    // Custom configuration
    config.EncoderConfig.TimeKey = "timestamp"
    config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
    config.OutputPaths = []string{"stdout", "/var/log/app.log"}
    
    logger, _ := config.Build()
    return logger
}

// 使用示例
logger := InitLogger("production")
defer logger.Sync()

logger.Info("processing request",
    zap.String("request_id", requestID),
    zap.String("user_id", userID),
    zap.String("method", r.Method),
    zap.String("path", r.URL.Path),
    zap.Duration("duration", duration),
    zap.Int("status_code", statusCode),
)

// 错误日志
logger.Error("nft verification failed",
    zap.String("request_id", requestID),
    zap.String("wallet", wallet),
    zap.String("contract", contract),
    zap.Error(err),
    zap.Stack("stack"),  // 堆栈信息
)
```

#### Dynamic log level adjustment
```go
type LogLevelHandler struct {
    atomicLevel zap.AtomicLevel
}

func NewLogLevelHandler(level zap.AtomicLevel) *LogLevelHandler {
    return &LogLevelHandler{atomicLevel: level}
}

// HTTP interface to dynamically adjust log level
func (h *LogLevelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
        // Get current level
        w.Write([]byte(h.atomicLevel.String()))
        return
    }
    
    if r.Method == http.MethodPut {
        // Set new level
        level := r.URL.Query().Get("level")
        var zapLevel zapcore.Level
        if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
            http.Error(w, err.Error(), 400)
            return
        }
        
        h.atomicLevel.SetLevel(zapLevel)
        w.Write([]byte("OK"))
        return
    }
}

// 使用
// GET  /debug/log-level        # View current level
// PUT  /debug/log-level?level=debug  # Set to debug level
```

### 5.2 Distributed Tracing

#### OpenTelemetry Integration
```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
    "go.opentelemetry.io/otel/exporters/jaeger"
)

// Initialize tracing
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
            semconv.SchemaURL,
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
        
        ctx, span := tracer.Start(r.Context(), r.URL.Path,
            trace.WithSpanKind(trace.SpanKindServer),
        )
        defer span.End()
        
        // Add attributes
        span.SetAttributes(
            attribute.String("http.method", r.Method),
            attribute.String("http.url", r.URL.String()),
            attribute.String("http.user_agent", r.UserAgent()),
        )
        
        // Pass context
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// Use in business code
func VerifyNFT(ctx context.Context, wallet, contract string) (bool, error) {
    tracer := otel.Tracer("nft-verifier")
    ctx, span := tracer.Start(ctx, "VerifyNFT")
    defer span.End()
    
    span.SetAttributes(
        attribute.String("wallet", wallet),
        attribute.String("contract", contract),
    )
    
    // 1. Check cache
    ctx, cacheSpan := tracer.Start(ctx, "CheckCache")
    cached, found := checkCache(ctx, wallet, contract)
    cacheSpan.End()
    
    if found {
        span.SetAttributes(attribute.Bool("cache_hit", true))
        return cached, nil
    }
    
    // 2. Query on-chain
    ctx, rpcSpan := tracer.Start(ctx, "QueryBlockchain")
    result, err := queryBlockchain(ctx, wallet, contract)
    rpcSpan.End()
    
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        return false, err
    }
    
    return result, nil
}
```

### 5.3 Performance Analysis

#### pprof Integration
```go
import _ "net/http/pprof"

func main() {
    // Start pprof server
    go func() {
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()
    
    // Main service
    // ...
}

// 使用方法: 
// 1. CPU profile
//    go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
//
// 2. Memory profile
//    go tool pprof http://localhost:6060/debug/pprof/heap
//
// 3. Goroutine profile
//    go tool pprof http://localhost:6060/debug/pprof/goroutine
//
// 4. View flame graph
//    go tool pprof -http=:8080 profile.pb.gz
```

#### Custom Performance Metrics
```go
type PerformanceMonitor struct {
    requestDurations *prometheus.HistogramVec
    activeRequests   *prometheus.GaugeVec
    errorCount       *prometheus.CounterVec
}

func NewPerformanceMonitor() *PerformanceMonitor {
    return &PerformanceMonitor{
        requestDurations: promauto.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "http_request_duration_seconds",
                Help:    "HTTP request duration",
                Buckets: []float64{0.001, 0.01, 0.1, 0.5, 1, 2, 5},
            },
            []string{"method", "endpoint", "status"},
        ),
        activeRequests: promauto.NewGaugeVec(
            prometheus.GaugeOpts{
                Name: "http_active_requests",
                Help: "Number of active HTTP requests",
            },
            []string{"method", "endpoint"},
        ),
        errorCount: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "http_errors_total",
                Help: "Total number of HTTP errors",
            },
            []string{"method", "endpoint", "error_type"},
        ),
    }
}

func (pm *PerformanceMonitor) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        
        // Increase active requests
        pm.activeRequests.WithLabelValues(r.Method, r.URL.Path).Inc()
        defer pm.activeRequests.WithLabelValues(r.Method, r.URL.Path).Dec()
        
        // 包装 ResponseWriter 以捕获状态码
        rw := &responseWriter{ResponseWriter: w, statusCode: 200}
        
        // 处理请求
        next.ServeHTTP(rw, r)
        
        // 记录耗时
        duration := time.Since(start).Seconds()
        pm.requestDurations.WithLabelValues(
            r.Method,
            r.URL.Path,
            fmt.Sprintf("%d", rw.statusCode),
        ).Observe(duration)
        
        // 记录错误
        if rw.statusCode >= 400 {
            pm.errorCount.WithLabelValues(
                r.Method,
                r.URL.Path,
                fmt.Sprintf("%dxx", rw.statusCode/100),
            ).Inc()
        }
    })
}
```

### 5.4 调试工具

#### 请求追踪
```go
type RequestTracer struct {
    traces sync.Map  // requestID -> []Event
}

type Event struct {
    Timestamp time.Time
    Type      string
    Message   string
    Data      map[string]interface{}
}

func (rt *RequestTracer) Start(requestID string) {
    rt.traces.Store(requestID, []Event{
        {
            Timestamp: time.Now(),
            Type:      "start",
            Message:   "Request started",
        },
    })
}

func (rt *RequestTracer) Log(requestID, eventType, message string, data map[string]interface{}) {
    if val, ok := rt.traces.Load(requestID); ok {
        events := val.([]Event)
        events = append(events, Event{
            Timestamp: time.Now(),
            Type:      eventType,
            Message:   message,
            Data:      data,
        })
        rt.traces.Store(requestID, events)
    }
}

func (rt *RequestTracer) GetTrace(requestID string) []Event {
    if val, ok := rt.traces.Load(requestID); ok {
        return val.([]Event)
    }
    return nil
}

// HTTP 接口查看追踪
func (rt *RequestTracer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    requestID := r.URL.Query().Get("request_id")
    events := rt.GetTrace(requestID)
    
    json.NewEncoder(w).Encode(events)
}

// 使用
tracer.Start(requestID)
tracer.Log(requestID, "cache_check", "Checking cache", map[string]interface{}{
    "key": cacheKey,
})
tracer.Log(requestID, "rpc_call", "Calling blockchain RPC", map[string]interface{}{
    "wallet": wallet,
    "contract": contract,
})

// 查看: GET /debug/trace?request_id=xxx
```

#### 实时监控面板
```go
type DashboardHandler struct {
    stats *SystemStats
}

type SystemStats struct {
    Goroutines     int
    MemoryUsage    uint64
    CPUUsage       float64
    ActiveRequests int64
    TotalRequests  int64
    ErrorRate      float64
}

func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    stats := h.collectStats()
    
    // 返回 JSON
    if r.Header.Get("Accept") == "application/json" {
        json.NewEncoder(w).Encode(stats)
        return
    }
    
    // 返回 HTML 面板
    html := `
    <!DOCTYPE html>
    <html>
    <head>
        <title>System Dashboard</title>
        <script>
            setInterval(function() {
                fetch('/debug/dashboard', {
                    headers: {'Accept': 'application/json'}
                })
                .then(r => r.json())
                .then(data => {
                    document.getElementById('goroutines').textContent = data.Goroutines;
                    document.getElementById('memory').textContent = (data.MemoryUsage / 1024 / 1024).toFixed(2) + ' MB';
                    document.getElementById('cpu').textContent = data.CPUUsage.toFixed(2) + '%';
                    document.getElementById('requests').textContent = data.ActiveRequests;
                    document.getElementById('error_rate').textContent = (data.ErrorRate * 100).toFixed(2) + '%';
                });
            }, 1000);
        </script>
    </head>
    <body>
        <h1>System Dashboard</h1>
        <div>Goroutines: <span id="goroutines">-</span></div>
        <div>Memory: <span id="memory">-</span></div>
        <div>CPU: <span id="cpu">-</span></div>
        <div>Active Requests: <span id="requests">-</span></div>
        <div>Error Rate: <span id="error_rate">-</span></div>
    </body>
    </html>
    `
    
    w.Header().Set("Content-Type", "text/html")
    w.Write([]byte(html))
}

func (h *DashboardHandler) collectStats() SystemStats {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    return SystemStats{
        Goroutines:     runtime.NumGoroutine(),
        MemoryUsage:    m.Alloc,
        CPUUsage:       getCPUUsage(),
        ActiveRequests: atomic.LoadInt64(&activeRequests),
        TotalRequests:  atomic.LoadInt64(&totalRequests),
        ErrorRate:      getErrorRate(),
    }
}
```

## 6. Performance Benchmark

### 6.1 Benchmark example
```go
// benchmark_test.go
func BenchmarkNFTVerification(b *testing.B) {
    provider := setupTestProvider()
    
    b.Run("without_cache", func(b *testing.B) {
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            provider.VerifyNFT(testWallet, testContract)
        }
    })
    
    b.Run("with_cache", func(b *testing.B) {
        cache := NewCache()
        cachedProvider := &CachedProvider{provider, cache}
        
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            cachedProvider.VerifyNFT(testWallet, testContract)
        }
    })
}

// 运行: go test -bench=. -benchmem -cpuprofile=cpu.prof
// 结果示例: 
// BenchmarkNFTVerification/without_cache-8    2    500000000 ns/op    1024 B/op    10 allocs/op
// BenchmarkNFTVerification/with_cache-8    10000    100000 ns/op       128 B/op     2 allocs/op
```

### 6.2 Load Testing
```bash
# 使用 k6 进行Load Testing
cat > load_test.js << 'JS'
import http from 'k6/http';
import { check, sleep } from 'k6';

export let options = {
    stages: [
        { duration: '2m', target: 100 },   // 2 分钟内Increase to 100 users
        { duration: '5m', target: 100 },   // Keep 100 users 5 分钟
        { duration: '2m', target: 1000 },  // 2 分钟内Increase to 1000 users
        { duration: '5m', target: 1000 },  // Keep 1000 users 5 分钟
        { duration: '2m', target: 0 },     // 2 分钟内Drop to 0
    ],
    thresholds: {
        http_req_duration: ['p(95)<200'],  // 95% requests < 200ms
        http_req_failed: ['rate<0.01'],    // Error rate < 1%
    },
};

export default function() {
    let res = http.get('http://localhost:8080/api/v1/content/test');
    
    check(res, {
        'status is 200': (r) => r.status === 200,
        'response time < 200ms': (r) => r.timings.duration < 200,
    });
    
    sleep(1);
}
JS

k6 run load_test.js
```

## 7. 总结

### 7.1 Performance Goals

| 指标 | 目标 | 实现方式 |
|------|------|----------|
| 并发连接 | 10K+ | Goroutine + 连接池 |
| API 延迟 P95 | < 200ms | 多级缓存 + 异步处理 |
| 可用性 | 99.9% | Broken + 故障转移 + 健康检查 |
| 水平扩展 | Linear | 无状态 + Consistent Hashing |
| 内存使用 | < 500MB | Object Pool + Zero-Copy |

### 7.2 Key Technologies

**High Concurrency**: 
- Goroutine(Lightweight concurrency)
- 连接池(Connection reuse)
- Worker Pool(Control concurrency)
- Lock-free data structures(Atomic, sync.Map)

**High Availability**: 
- 健康检查(Liveness + Readiness)
- Broken器(Prevent cascading failures)
- Graceful Shutdown(Don't lose requests)
- 故障转移(Master-slave switching)

**Easy Scalability**: 
- 无状态设计(State Externalization)
- Distributed Lock(防止冲突)
- Consistent Hashing(Data sharding)
- HPA(Auto scaling)

**High Performance**: 
- Object Pool(Reduce GC)
- Zero-Copy(Reduce memory copying)
- 并行处理(Utilize multi-core)
- HTTP/2(Multiplexing)

**Debuggability**: 
- Structured Logging(Easy to query)
- Distributed Tracing(Call chain)
- pprof(Performance Analysis)
- 实时监控(System status)

### 7.3 C++ vs Go Comparison

| 特性 | C++ | Go |
|------|-----|-----|
| 并发模型 | Thread (heavy) | Goroutine (light) |
| 内存管理 | Manual/Smart pointers | GC |
| 编译速度 | Slow | Fast |
| 部署 | 依赖复杂 | 单二进制 |
| 学习曲线 | Steep | Gentle |
| 性能 | Ultimate | Excellent |

**Your Advantages**: 
- ✅ Understand memory management → Optimize Go GC
- ✅ Understand concurrency → Master Goroutine
- ✅ Understand system calls → Optimize I/O
- ✅ Understand performance optimization → 写出High Performance Go

Remember: Go's design philosophy is"Simple, efficient, reliable"，Don't over-optimize，Ensure correctness first, then optimize！
