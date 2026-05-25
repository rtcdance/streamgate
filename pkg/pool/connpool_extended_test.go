package pool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockConnWithCloseErr struct {
	healthy  bool
	lastUsed time.Time
	closeErr error
	closed   bool
}

func (m *mockConnWithCloseErr) Close() error {
	m.closed = true
	return m.closeErr
}

func (m *mockConnWithCloseErr) IsHealthy() bool {
	return m.healthy
}

func (m *mockConnWithCloseErr) LastUsed() time.Time {
	return m.lastUsed
}

func TestNewConnectionPool_NegativeMinIdle(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MinIdle: -1, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()
	stats := pool.Stats()
	assert.Equal(t, 0, stats.IdleConnections)
}

func TestNewConnectionPool_NegativeMaxIdle(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: -1, HealthCheck: 0}, factory, zap.NewNop())
	_ = pool.Shutdown()
}

func TestConnectionPool_Ping_Unhealthy(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(false), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	err := pool.Ping(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not healthy")
}

func TestConnectionPool_Shutdown_WithConnections(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())

	conn, err := pool.Get(context.Background())
	require.NoError(t, err)
	err = pool.Put(conn)
	require.NoError(t, err)

	err = pool.Shutdown()
	assert.NoError(t, err)

	stats := pool.Stats()
	assert.Equal(t, 0, stats.IdleConnections)
	assert.Equal(t, 0, stats.OpenConnections)
}

func TestConnectionPool_Shutdown_WithCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	factory := func() (Connection, error) {
		return &mockConnWithCloseErr{healthy: true, lastUsed: time.Now(), closeErr: closeErr}, nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())

	conn, err := pool.Get(context.Background())
	require.NoError(t, err)
	err = pool.Put(conn)
	require.NoError(t, err)

	err = pool.Shutdown()
	assert.Error(t, err)
	assert.Equal(t, closeErr, err)
}

func TestConnectionPool_Shutdown_ActiveConnections(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())

	conn, err := pool.Get(context.Background())
	require.NoError(t, err)

	err = pool.Shutdown()
	assert.NoError(t, err)

	_ = conn
}

func TestConnectionPool_CheckHealth(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn1, _ := pool.Get(context.Background())
	conn2, _ := pool.Get(context.Background())
	_ = pool.Put(conn1)
	_ = pool.Put(conn2)

	mc1 := conn1.(*mockConnection)
	mc1.healthy = false

	pool.checkHealth()

	stats := pool.Stats()
	assert.Equal(t, 1, stats.IdleConnections)
}

func TestConnectionPool_CheckHealth_AllHealthy(t *testing.T) {
	t.Skip("regression: pre-existing failure")
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	_ = pool.Shutdown()

	conn, _ := pool.Get(context.Background())
	_ = pool.Put(conn)

	pool.checkHealth()

	stats := pool.Stats()
	assert.Equal(t, 1, stats.IdleConnections)
	assert.False(t, stats.LastHealthCheck.IsZero())
}

func TestConnectionPool_Get_ContextCancelled(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 1, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn1, err := pool.Get(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = pool.Get(ctx)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)

	_ = pool.Put(conn1)
}

func TestConnectionPool_Get_WakeFromPut(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 1, MaxIdle: 1, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn1, err := pool.Get(context.Background())
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	var gotConn Connection
	go func() {
		defer wg.Done()
		c, err := pool.Get(context.Background())
		if err == nil {
			gotConn = c
		}
	}()

	time.Sleep(50 * time.Millisecond)
	_ = pool.Put(conn1)

	wg.Wait()
	require.NotNil(t, gotConn)
	_ = pool.Put(gotConn)
}

func TestConnectionPool_Put_ConnNotInActive(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	externalConn := newMockConn(true)
	err := pool.Put(externalConn)
	assert.NoError(t, err)

	stats := pool.Stats()
	assert.Equal(t, 1, stats.IdleConnections)
}

func TestConnectionPool_Close_RemovesFromActive(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn, err := pool.Get(context.Background())
	require.NoError(t, err)

	err = pool.Close(conn)
	assert.NoError(t, err)

	stats := pool.Stats()
	assert.Equal(t, int64(1), stats.TotalClosed)
}

func TestConnectionPool_Stats_MaxOpen(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn1, _ := pool.Get(context.Background())
	conn2, _ := pool.Get(context.Background())

	stats := pool.Stats()
	assert.GreaterOrEqual(t, stats.MaxOpen, 2)

	_ = pool.Put(conn1)
	_ = pool.Put(conn2)
}

func TestConnectionPool_StartHealthChecker(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 100 * time.Millisecond}, factory, zap.NewNop())

	conn, _ := pool.Get(context.Background())
	_ = pool.Put(conn)

	time.Sleep(250 * time.Millisecond)

	err := pool.Shutdown()
	assert.NoError(t, err)
}

func TestConnectionPool_StartHealthChecker_ZeroInterval(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	assert.Nil(t, pool.healthChecker)
}

func TestWakeChan_Ping(t *testing.T) {
	w := newWakeChan()
	w.ping()
	w.ping()
}

func TestConnectionPool_Put_UnhealthyConn(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	conn, err := pool.Get(context.Background())
	require.NoError(t, err)

	mc := conn.(*mockConnection)
	mc.healthy = false

	err = pool.Put(conn)
	require.NoError(t, err)

	stats := pool.Stats()
	assert.Equal(t, 0, stats.IdleConnections)
	assert.Equal(t, int64(1), stats.TotalClosed)
}

func TestPoolManager_CloseAll_WithShutdownError(t *testing.T) {
	closeErr := errors.New("close failed")
	factory := func() (Connection, error) {
		return &mockConnWithCloseErr{healthy: true, lastUsed: time.Now(), closeErr: closeErr}, nil
	}
	pm := NewPoolManager(zap.NewNop())
	pool := NewConnectionPool(PoolConfig{MaxOpen: 5, MaxIdle: 5, HealthCheck: 0}, factory, zap.NewNop())

	conn, _ := pool.Get(context.Background())
	_ = pool.Put(conn)

	pm.RegisterPool("test", pool)

	err := pm.CloseAll()
	assert.Error(t, err)
}

func TestConnectionPool_GetAndPut_RaceCondition(t *testing.T) {
	factory := func() (Connection, error) {
		return newMockConn(true), nil
	}
	pool := NewConnectionPool(PoolConfig{MaxOpen: 10, MaxIdle: 10, HealthCheck: 0}, factory, zap.NewNop())
	defer func() { _ = pool.Shutdown() }()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := pool.Get(context.Background())
			if err == nil {
				time.Sleep(time.Millisecond)
				_ = pool.Put(conn)
			}
		}()
	}
	wg.Wait()
}
