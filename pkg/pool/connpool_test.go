package pool

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubConn is a test Connection implementation.
type stubConn struct {
	healthy atomic.Bool
	closed  atomic.Int32
	used    time.Time
}

func newStubConn(healthy bool) *stubConn {
	c := &stubConn{used: time.Now()}
	c.healthy.Store(healthy)
	return c
}

func (c *stubConn) Close() error {
	c.closed.Add(1)
	return nil
}

func (c *stubConn) IsHealthy() bool {
	return c.healthy.Load()
}

func (c *stubConn) LastUsed() time.Time {
	return c.used
}

func TestConnectionPool_GetPut(t *testing.T) {
	var created int32
	factory := func() (Connection, error) {
		atomic.AddInt32(&created, 1)
		return newStubConn(true), nil
	}

	cfg := PoolConfig{MaxOpen: 5, MaxIdle: 3, HealthCheck: 0}
	p := NewConnectionPool(cfg, factory, zap.NewNop())
	defer p.Shutdown()

	// Get a connection
	conn, err := p.Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Equal(t, int32(1), atomic.LoadInt32(&created))

	// Return it
	err = p.Put(conn)
	require.NoError(t, err)

	// Get again — should reuse idle connection
	conn2, err := p.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&created)) // no new creation

	_ = p.Put(conn2)
}

func TestConnectionPool_UnhealthyDiscarded(t *testing.T) {
	var created int32
	factory := func() (Connection, error) {
		atomic.AddInt32(&created, 1)
		return newStubConn(true), nil
	}

	cfg := PoolConfig{MaxOpen: 5, MaxIdle: 3, HealthCheck: 0}
	p := NewConnectionPool(cfg, factory, zap.NewNop())
	defer p.Shutdown()

	conn, err := p.Get(context.Background())
	require.NoError(t, err)

	// Mark as unhealthy before returning
	stub := conn.(*stubConn)
	stub.healthy.Store(false)

	err = p.Put(conn)
	require.NoError(t, err)

	// Next Get should create a new connection since the unhealthy one was discarded
	conn2, err := p.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&created))

	_ = p.Put(conn2)
}

func TestConnectionPool_MaxOpen(t *testing.T) {
	factory := func() (Connection, error) {
		return newStubConn(true), nil
	}

	cfg := PoolConfig{MaxOpen: 2, MaxIdle: 1, HealthCheck: 0}
	p := NewConnectionPool(cfg, factory, zap.NewNop())
	defer p.Shutdown()

	c1, err := p.Get(context.Background())
	require.NoError(t, err)
	c2, err := p.Get(context.Background())
	require.NoError(t, err)

	// Third Get should block (or timeout with context)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = p.Get(ctx)
	assert.Error(t, err) // context deadline exceeded

	_ = p.Put(c1)
	_ = p.Put(c2)
}

func TestConnectionPool_MaxIdle(t *testing.T) {
	factory := func() (Connection, error) {
		return newStubConn(true), nil
	}

	cfg := PoolConfig{MaxOpen: 5, MaxIdle: 2, HealthCheck: 0}
	p := NewConnectionPool(cfg, factory, zap.NewNop())
	defer p.Shutdown()

	conns := make([]Connection, 4)
	for i := range conns {
		c, err := p.Get(context.Background())
		require.NoError(t, err)
		conns[i] = c
	}

	// Return all — excess over MaxIdle should be closed
	for _, c := range conns {
		_ = p.Put(c)
	}

	stats := p.Stats()
	assert.LessOrEqual(t, stats.IdleConnections, 2)
}

func TestConnectionPool_Shutdown(t *testing.T) {
	factory := func() (Connection, error) {
		return newStubConn(true), nil
	}

	cfg := PoolConfig{MaxOpen: 5, MaxIdle: 3, HealthCheck: 0}
	p := NewConnectionPool(cfg, factory, zap.NewNop())

	c, err := p.Get(context.Background())
	require.NoError(t, err)
	_ = p.Put(c)

	err = p.Shutdown()
	require.NoError(t, err)

	// After shutdown, Get should fail
	_, err = p.Get(context.Background())
	assert.Error(t, err)

	// Double shutdown is ok
	err = p.Shutdown()
	assert.NoError(t, err)
}

func TestConnectionPool_FactoryError(t *testing.T) {
	factoryErr := errors.New("factory failed")
	factory := func() (Connection, error) {
		return nil, factoryErr
	}

	cfg := PoolConfig{MaxOpen: 5, MaxIdle: 3, HealthCheck: 0}
	p := NewConnectionPool(cfg, factory, zap.NewNop())
	defer p.Shutdown()

	_, err := p.Get(context.Background())
	assert.ErrorIs(t, err, factoryErr)

	stats := p.Stats()
	assert.Equal(t, int64(1), stats.TotalErrors)
}

func TestConnectionPool_Stats(t *testing.T) {
	factory := func() (Connection, error) {
		return newStubConn(true), nil
	}

	cfg := PoolConfig{MaxOpen: 5, MaxIdle: 3, HealthCheck: 0}
	p := NewConnectionPool(cfg, factory, zap.NewNop())
	defer p.Shutdown()

	c, _ := p.Get(context.Background())
	_ = p.Put(c)

	stats := p.Stats()
	assert.Equal(t, int64(1), stats.TotalCreated)
	assert.GreaterOrEqual(t, stats.OpenConnections, 1)
}

func TestConnectionPool_CancelledContext(t *testing.T) {
	factory := func() (Connection, error) {
		return newStubConn(true), nil
	}

	cfg := PoolConfig{MaxOpen: 1, MaxIdle: 1, HealthCheck: 0}
	p := NewConnectionPool(cfg, factory, zap.NewNop())
	defer p.Shutdown()

	// Exhaust pool
	c, err := p.Get(context.Background())
	require.NoError(t, err)

	// Cancelled context should return immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = p.Get(ctx)
	assert.Error(t, err)

	_ = p.Put(c)
}

func TestDBConnection_IsHealthy_NilConn(t *testing.T) {
	db := &DBConnection{conn: nil}
	assert.False(t, db.IsHealthy())
	assert.NoError(t, db.Close())
}

func TestHTTPConnection_IsHealthy_NilClient(t *testing.T) {
	h := &HTTPConnection{client: nil}
	assert.False(t, h.IsHealthy())
	assert.NoError(t, h.Close())
}

func TestPoolManager(t *testing.T) {
	pm := NewPoolManager(zap.NewNop())

	factory := func() (Connection, error) {
		return newStubConn(true), nil
	}

	p1 := NewConnectionPool(DefaultPoolConfig(), factory, zap.NewNop())
	p2 := NewConnectionPool(DefaultPoolConfig(), factory, zap.NewNop())

	pm.RegisterPool("db", p1)
	pm.RegisterPool("cache", p2)

	got, err := pm.GetPool("db")
	require.NoError(t, err)
	assert.Equal(t, p1, got)

	_, err = pm.GetPool("missing")
	assert.Error(t, err)

	stats := pm.GetAllStats()
	assert.Len(t, stats, 2)
	assert.Contains(t, stats, "db")
	assert.Contains(t, stats, "cache")

	err = pm.CloseAll()
	assert.NoError(t, err)
}

func TestDBConnection_WithRealDB(t *testing.T) {
	// Uses sql.DB with an in-memory driver — skips if no driver available.
	// This test mainly validates that the type assertion and PingContext path work.
	db, err := sql.Open("postgres", "host=localhost port=5432 sslmode=disable")
	if err != nil {
		t.Skip("postgres driver not available")
	}
	defer db.Close()

	// The connection will fail to actually connect, so IsHealthy should return false
	conn := NewDBConnection(db)
	// Ping will fail because there's no real postgres, but the method should not panic
	result := conn.IsHealthy()
	assert.False(t, result) // no real DB, so ping fails
}
