package pool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockConnection struct {
	healthy  bool
	lastUsed time.Time
	closed   bool
}

func newMockConn(healthy bool) *mockConnection {
	return &mockConnection{
		healthy:  healthy,
		lastUsed: time.Now(),
	}
}

func (m *mockConnection) Close() error {
	m.closed = true
	return nil
}

func (m *mockConnection) IsHealthy() bool {
	return m.healthy
}

func (m *mockConnection) LastUsed() time.Time {
	return m.lastUsed
}

func TestDefaultPoolConfig(t *testing.T) {
	cfg := DefaultPoolConfig()
	assert.Equal(t, 25, cfg.MaxOpen)
	assert.Equal(t, 5, cfg.MinIdle)
	assert.Equal(t, 10, cfg.MaxIdle)
	assert.Equal(t, 30*time.Minute, cfg.MaxLifetime)
	assert.Equal(t, 5*time.Minute, cfg.MaxIdleTime)
	assert.Equal(t, 30*time.Second, cfg.HealthCheck)
}

func TestNewConnectionPool(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5}, factory, zap.NewNop())
	assert.NotNil(t, pool)
	_ = pool.Shutdown()
}

func TestNewConnectionPool_DefaultMaxOpen(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	stats := pool.Stats()
	assert.Equal(t, 25, stats.MaxOpen)
}

func TestConnectionPool_GetAndPut(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn, err := pool.Get(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, conn)

	err = pool.Put(conn)
	assert.NoError(t, err)

	stats := pool.Stats()
	assert.Equal(t, 1, stats.IdleConnections)
	assert.Equal(t, 1, stats.OpenConnections)
	assert.Equal(t, int64(1), stats.TotalCreated)
}

func TestConnectionPool_Get_UnhealthyIdleConn(t *testing.T) {
	callCount := 0
	factory := func() (Connection, error) {
		callCount++
		if callCount == 1 {
			return newMockConn(false), nil
		}
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn1, err := pool.Get(context.Background())
	require.NoError(t, err)

	_ = pool.Put(conn1)

	conn2, err := pool.Get(context.Background())
	require.NoError(t, err)
	assert.True(t, conn2.IsHealthy())
}

func TestConnectionPool_Put_Unhealthy(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 2, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn, _ := pool.Get(context.Background())
	mc := conn.(*mockConnection)
	mc.healthy = false

	err := pool.Put(conn)
	assert.NoError(t, err)

	stats := pool.Stats()
	assert.Equal(t, 0, stats.IdleConnections)
}

func TestConnectionPool_Put_ExceedsMaxIdle(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 1, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn1, _ := pool.Get(context.Background())
	conn2, _ := pool.Get(context.Background())

	_ = pool.Put(conn1)
	_ = pool.Put(conn2)

	stats := pool.Stats()
	assert.Equal(t, 1, stats.IdleConnections)
}

func TestConnectionPool_FactoryError(t *testing.T) {
	factory := func() (Connection, error) {
		return nil, errors.New("factory error")
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn, err := pool.Get(context.Background())
	assert.Error(t, err)
	assert.Nil(t, conn)

	stats := pool.Stats()
	assert.Equal(t, int64(1), stats.TotalErrors)
}

func TestConnectionPool_Get_ClosedPool(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())
	_ = pool.Shutdown()

	conn, err := pool.Get(context.Background())
	assert.Error(t, err)
	assert.Nil(t, conn)
}

func TestConnectionPool_Put_ClosedPool(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())
	conn, _ := pool.Get(context.Background())

	_ = pool.Shutdown()

	err := pool.Put(conn)
	assert.NoError(t, err)
}

func TestConnectionPool_Shutdown_Twice(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())

	err := pool.Shutdown()
	assert.NoError(t, err)

	err = pool.Shutdown()
	assert.NoError(t, err)
}

func TestConnectionPool_Ping(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	err := pool.Ping(context.Background())
	assert.NoError(t, err)
}

func TestConnectionPool_Stats(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn, _ := pool.Get(context.Background())
	_ = pool.Put(conn)

	stats := pool.Stats()
	assert.Equal(t, 1, stats.OpenConnections)
	assert.Equal(t, 1, stats.IdleConnections)
	assert.Equal(t, int64(1), stats.TotalCreated)
}

func TestDBConnection(t *testing.T) {
	conn := NewDBConnection("test")
	assert.NotNil(t, conn)
	assert.True(t, conn.IsHealthy())
	assert.NoError(t, conn.Close())
	assert.False(t, conn.LastUsed().IsZero())
}

func TestHTTPConnection(t *testing.T) {
	conn := NewHTTPConnection("test")
	assert.NotNil(t, conn)
	assert.True(t, conn.IsHealthy())
	assert.NoError(t, conn.Close())
	assert.False(t, conn.LastUsed().IsZero())
}

func TestNewPoolManager(t *testing.T) {
	pm := NewPoolManager(zap.NewNop())
	assert.NotNil(t, pm)
}

func TestPoolManager_RegisterAndGetPool(t *testing.T) {
	pm := NewPoolManager(zap.NewNop())
	factory := func() (Connection, error) { return newMockConn(true), nil }
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())

	pm.RegisterPool("test", pool)

	retrieved, err := pm.GetPool("test")
	assert.NoError(t, err)
	assert.Equal(t, pool, retrieved)
}

func TestPoolManager_GetPool_NotFound(t *testing.T) {
	pm := NewPoolManager(zap.NewNop())

	_, err := pm.GetPool("nonexistent")
	assert.Error(t, err)
}

func TestPoolManager_GetAllStats(t *testing.T) {
	pm := NewPoolManager(zap.NewNop())
	factory := func() (Connection, error) { return newMockConn(true), nil }
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	pm.RegisterPool("test", pool)

	stats := pm.GetAllStats()
	assert.Contains(t, stats, "test")
}

func TestPoolManager_CloseAll(t *testing.T) {
	pm := NewPoolManager(zap.NewNop())
	factory := func() (Connection, error) { return newMockConn(true), nil }
	pool1 := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())
	pool2 := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())

	pm.RegisterPool("pool1", pool1)
	pm.RegisterPool("pool2", pool2)

	err := pm.CloseAll()
	assert.NoError(t, err)
}

func TestConnectionPool_MaxConcurrent(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}

	pool := NewConnectionPool(PoolConfig{MaxOpen: 2, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn1, err := pool.Get(context.Background())
	require.NoError(t, err)

	conn2, err := pool.Get(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = pool.Get(ctx)
	assert.Error(t, err)

	_ = pool.Put(conn1)
	_ = pool.Put(conn2)
}
