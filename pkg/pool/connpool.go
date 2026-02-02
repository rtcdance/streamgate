package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Connection represents a connection in the pool
type Connection interface {
	Close() error
	IsHealthy() bool
	LastUsed() time.Time
}

// PoolConfig holds configuration for connection pool
type PoolConfig struct {
	MaxOpen     int           // Maximum number of open connections
	MinIdle     int           // Minimum number of idle connections
	MaxIdle     int           // Maximum number of idle connections
	MaxLifetime time.Duration // Maximum lifetime of a connection
	MaxIdleTime time.Duration // Maximum idle time of a connection
	HealthCheck time.Duration // Health check interval
}

// DefaultPoolConfig returns default pool configuration
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxOpen:     25,
		MinIdle:     5,
		MaxIdle:     10,
		MaxLifetime: 30 * time.Minute,
		MaxIdleTime: 5 * time.Minute,
		HealthCheck: 30 * time.Second,
	}
}

// ConnectionPool manages a pool of connections
type ConnectionPool struct {
	config        PoolConfig
	factory       func() (Connection, error)
	idle          []Connection
	active        []Connection
	mu            sync.Mutex
	cond          *sync.Cond
	logger        *zap.Logger
	closed        bool
	stats         PoolStats
	healthChecker *time.Ticker
}

// PoolStats holds pool statistics
type PoolStats struct {
	OpenConnections int
	IdleConnections int
	MaxOpen         int
	TotalCreated    int64
	TotalClosed     int64
	TotalErrors     int64
	LastHealthCheck time.Time
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config PoolConfig, factory func() (Connection, error), logger *zap.Logger) *ConnectionPool {
	if config.MaxOpen <= 0 {
		config.MaxOpen = 25
	}
	if config.MinIdle < 0 {
		config.MinIdle = 0
	}
	if config.MaxIdle < 0 {
		config.MaxIdle = 10
	}

	pool := &ConnectionPool{
		config:  config,
		factory: factory,
		idle:    make([]Connection, 0, config.MaxIdle),
		active:  make([]Connection, 0, config.MaxOpen),
		logger:  logger,
		stats:   PoolStats{MaxOpen: config.MaxOpen},
	}
	pool.cond = sync.NewCond(&pool.mu)

	pool.startHealthChecker()
	return pool
}

// Get returns a connection from the pool
func (p *ConnectionPool) Get(ctx context.Context) (Connection, error) {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("connection pool is closed")
	}

	for {
		if len(p.idle) > 0 {
			conn := p.idle[len(p.idle)-1]
			p.idle = p.idle[:len(p.idle)-1]
			p.active = append(p.active, conn)
			p.mu.Unlock()

			if conn.IsHealthy() {
				return conn, nil
			}
			p.Close(conn)
			p.mu.Lock()
			continue
		}

		if len(p.active)+len(p.idle) < p.config.MaxOpen {
			conn, err := p.createConnection()
			p.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return conn, nil
		}

		select {
		case <-ctx.Done():
			p.mu.Unlock()
			return nil, ctx.Err()
		default:
			p.cond.Wait()
		}
	}
}

// Put returns a connection to the pool
func (p *ConnectionPool) Put(conn Connection) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return conn.Close()
	}

	for i, c := range p.active {
		if c == conn {
			p.active = append(p.active[:i], p.active[i+1:]...)
			break
		}
	}

	if !conn.IsHealthy() {
		return p.Close(conn)
	}

	if len(p.idle) >= p.config.MaxIdle {
		return p.Close(conn)
	}

	p.idle = append(p.idle, conn)
	p.cond.Signal()
	return nil
}

// Close closes a connection
func (p *ConnectionPool) Close(conn Connection) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, c := range p.idle {
		if c == conn {
			p.idle = append(p.idle[:i], p.idle[i+1:]...)
			break
		}
	}

	err := conn.Close()
	p.stats.TotalClosed++
	p.stats.IdleConnections = len(p.idle)
	p.stats.OpenConnections = len(p.active) + len(p.idle)
	return err
}

// createConnection creates a new connection
func (p *ConnectionPool) createConnection() (Connection, error) {
	conn, err := p.factory()
	if err != nil {
		p.stats.TotalErrors++
		p.logger.Error("Failed to create connection", zap.Error(err))
		return nil, err
	}

	p.active = append(p.active, conn)
	p.stats.TotalCreated++
	p.stats.OpenConnections = len(p.active) + len(p.idle)
	p.stats.MaxOpen = max(p.stats.MaxOpen, p.stats.OpenConnections)

	return conn, nil
}

// startHealthChecker starts the health check routine
func (p *ConnectionPool) startHealthChecker() {
	if p.config.HealthCheck <= 0 {
		return
	}

	p.healthChecker = time.NewTicker(p.config.HealthCheck)

	go func() {
		for range p.healthChecker.C {
			p.checkHealth()
		}
	}()
}

// checkHealth checks the health of idle connections
func (p *ConnectionPool) checkHealth() {
	p.mu.Lock()
	defer p.mu.Unlock()

	healthy := make([]Connection, 0, len(p.idle))

	for _, conn := range p.idle {
		if conn.IsHealthy() {
			healthy = append(healthy, conn)
		} else {
			conn.Close()
			p.stats.TotalClosed++
		}
	}

	p.idle = healthy
	p.stats.IdleConnections = len(p.idle)
	p.stats.LastHealthCheck = time.Now()
}

// Stats returns pool statistics
func (p *ConnectionPool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats := p.stats
	stats.IdleConnections = len(p.idle)
	stats.OpenConnections = len(p.active) + len(p.idle)
	return stats
}

// Shutdown closes all connections in the pool
func (p *ConnectionPool) Shutdown() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true

	if p.healthChecker != nil {
		p.healthChecker.Stop()
	}

	var lastErr error

	for _, conn := range p.idle {
		if err := conn.Close(); err != nil {
			lastErr = err
		}
	}

	for _, conn := range p.active {
		if err := conn.Close(); err != nil {
			lastErr = err
		}
	}

	p.idle = nil
	p.active = nil
	p.cond.Broadcast()

	return lastErr
}

// Ping checks if the pool is healthy
func (p *ConnectionPool) Ping(ctx context.Context) error {
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	if !conn.IsHealthy() {
		return fmt.Errorf("connection is not healthy")
	}

	return nil
}

// DBConnection wraps a database connection
type DBConnection struct {
	conn     interface{}
	lastUsed time.Time
}

// NewDBConnection creates a new DB connection wrapper
func NewDBConnection(conn interface{}) *DBConnection {
	return &DBConnection{
		conn:     conn,
		lastUsed: time.Now(),
	}
}

// Close closes the connection
func (db *DBConnection) Close() error {
	return nil
}

// IsHealthy checks if the connection is healthy
func (db *DBConnection) IsHealthy() bool {
	return true
}

// LastUsed returns the last used time
func (db *DBConnection) LastUsed() time.Time {
	return db.lastUsed
}

// HTTPConnection wraps an HTTP client connection
type HTTPConnection struct {
	client   interface{}
	lastUsed time.Time
}

// NewHTTPConnection creates a new HTTP connection wrapper
func NewHTTPConnection(client interface{}) *HTTPConnection {
	return &HTTPConnection{
		client:   client,
		lastUsed: time.Now(),
	}
}

// Close closes the connection
func (h *HTTPConnection) Close() error {
	return nil
}

// IsHealthy checks if the connection is healthy
func (h *HTTPConnection) IsHealthy() bool {
	return true
}

// LastUsed returns the last used time
func (h *HTTPConnection) LastUsed() time.Time {
	return h.lastUsed
}

// PoolManager manages multiple connection pools
type PoolManager struct {
	pools  map[string]*ConnectionPool
	mu     sync.RWMutex
	logger *zap.Logger
}

// NewPoolManager creates a new pool manager
func NewPoolManager(logger *zap.Logger) *PoolManager {
	return &PoolManager{
		pools:  make(map[string]*ConnectionPool),
		logger: logger,
	}
}

// RegisterPool registers a connection pool
func (pm *PoolManager) RegisterPool(name string, pool *ConnectionPool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.pools[name] = pool
	pm.logger.Info("Connection pool registered", zap.String("pool", name))
}

// GetPool returns a connection pool by name
func (pm *PoolManager) GetPool(name string) (*ConnectionPool, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pool, exists := pm.pools[name]
	if !exists {
		return nil, fmt.Errorf("pool '%s' not found", name)
	}

	return pool, nil
}

// GetAllStats returns statistics for all pools
func (pm *PoolManager) GetAllStats() map[string]PoolStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[string]PoolStats, len(pm.pools))
	for name, pool := range pm.pools {
		stats[name] = pool.Stats()
	}
	return stats
}

// CloseAll closes all connection pools
func (pm *PoolManager) CloseAll() error {
	pm.mu.RLock()
	pools := make([]*ConnectionPool, 0, len(pm.pools))
	for _, pool := range pm.pools {
		pools = append(pools, pool)
	}
	pm.mu.RUnlock()

	var lastErr error
	for _, pool := range pools {
		if err := pool.Shutdown(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}
