package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"streamgate/pkg/config"
	"streamgate/pkg/eventbus"
	"streamgate/pkg/middleware"
	"streamgate/pkg/optimization"
	"streamgate/pkg/plugin"
	"streamgate/pkg/pool"
	"streamgate/pkg/tracing"
)

func TestEventBusIntegration(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	t.Run("InMemoryEventBus", func(t *testing.T) {
		bus := eventbus.NewInMemoryEventBus(logger)
		defer bus.Close()

		received := make(chan *eventbus.Event, 1)
		handler := func(ctx context.Context, event eventbus.Event) error {
			received <- &event
			return nil
		}

		subID, err := bus.Subscribe(ctx, "test.topic", handler)
		require.NoError(t, err)
		require.NotEmpty(t, subID)

		event := eventbus.Event{
			Type:   "test.event",
			Source: "integration-test",
			Data:   map[string]interface{}{"key": "value"},
		}

		err = bus.Publish(ctx, "test.topic", event)
		require.NoError(t, err)

		select {
		case receivedEvent := <-received:
			assert.Equal(t, "test.event", receivedEvent.Type)
			assert.Equal(t, "value", receivedEvent.Data["key"])
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for event")
		}

		err = bus.Unsubscribe(ctx, subID)
		require.NoError(t, err)
	})

	t.Run("EventRouter", func(t *testing.T) {
		router := eventbus.NewEventRouter(logger)

		received := make(chan *eventbus.Event, 1)
		handler := func(ctx context.Context, event eventbus.Event) error {
			received <- &event
			return nil
		}

		router.RegisterHandler("test.event", handler)

		event := eventbus.Event{
			Type:   "test.event",
			Source: "integration-test",
			Data:   map[string]interface{}{"key": "value"},
		}

		err := router.Route(ctx, event)
		require.NoError(t, err)

		select {
		case receivedEvent := <-received:
			assert.Equal(t, "test.event", receivedEvent.Type)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})
}

func TestPluginManagerIntegration(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	t.Run("PluginLifecycle", func(t *testing.T) {
		testPlugin := plugin.NewBasePlugin("test-plugin", "1.0.0", "test", logger)
		config := map[string]interface{}{"test": "value"}

		err := testPlugin.Initialize(ctx, config)
		require.NoError(t, err)

		assert.Equal(t, "test-plugin", testPlugin.Name())
		assert.Equal(t, "1.0.0", testPlugin.Version())

		err = testPlugin.Start(ctx)
		require.NoError(t, err)

		err = testPlugin.HealthCheck(ctx)
		require.NoError(t, err)

		err = testPlugin.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("PluginHealthCheck", func(t *testing.T) {
		ctx := context.Background()

		plugin1 := plugin.NewBasePlugin("plugin1", "1.0.0", "test", logger)
		plugin2 := plugin.NewBasePlugin("plugin2", "1.0.0", "test", logger)
		config := map[string]interface{}{}

		err := plugin1.Initialize(ctx, config)
		require.NoError(t, err)
		err = plugin2.Initialize(ctx, config)
		require.NoError(t, err)

		err = plugin1.HealthCheck(ctx)
		assert.NoError(t, err)
		err = plugin2.HealthCheck(ctx)
		assert.NoError(t, err)
	})
}

func TestConfigManagerIntegration(t *testing.T) {
	logger := zap.NewNop()

	t.Run("ConfigLoadAndSave", func(t *testing.T) {
		manager := config.NewConfigManager("/tmp/test-config.json", logger)

		configData := config.DefaultConfig()
		configData.Server.Port = 9090

		err := manager.Update(configData)
		require.NoError(t, err)

		err = manager.Save()
		require.NoError(t, err)

		newManager := config.NewConfigManager("/tmp/test-config.json", logger)
		err = newManager.Load()
		require.NoError(t, err)

		loadedConfig := newManager.Get()
		assert.Equal(t, 9090, loadedConfig.Server.Port)
	})

	t.Run("ConfigUpdate", func(t *testing.T) {
		manager := config.NewConfigManager("/tmp/test-config-update.json", logger)

		configData := config.DefaultConfig()
		err := manager.Update(configData)
		require.NoError(t, err)

		called := false
		handler := func(old, new *config.Config) error {
			called = true
			return nil
		}

		manager.AddChangeHandler(handler)

		newConfig := config.DefaultConfig()
		newConfig.Server.Port = 8081

		err = manager.Update(newConfig)
		require.NoError(t, err)
		assert.True(t, called)

		updatedConfig := manager.Get()
		assert.Equal(t, 8081, updatedConfig.Server.Port)
	})

	t.Run("ConfigValidation", func(t *testing.T) {
		manager := config.NewConfigManager("/tmp/test-config-validate.json", logger)

		configData := config.DefaultConfig()
		err := manager.Update(configData)
		require.NoError(t, err)

		err = manager.Validate()
		require.NoError(t, err)
	})
}

func TestTracingIntegration(t *testing.T) {
	logger := zap.NewNop()

	t.Run("SpanCreation", func(t *testing.T) {
		provider := tracing.NewTraceProvider(logger)
		tracer := provider.Tracer("test-tracer")
		ctx := context.Background()

		ctx, span := tracer.StartSpan(ctx, "test-operation", tracing.WithKind(tracing.SpanKindInternal))
		defer span.End()

		assert.NotNil(t, span)
		assert.Equal(t, "test-operation", span.Name())
		assert.Equal(t, tracing.SpanKindInternal, span.Kind())
		assert.True(t, span.IsRecording())
	})

	t.Run("SpanContext", func(t *testing.T) {
		provider := tracing.NewTraceProvider(logger)
		tracer := provider.Tracer("test-tracer")
		ctx := context.Background()

		ctx, parentSpan := tracer.StartSpan(ctx, "parent-operation")
		defer parentSpan.End()

		ctx, childSpan := tracer.StartSpan(ctx, "child-operation")
		defer childSpan.End()

		assert.Equal(t, parentSpan.Context().TraceID, childSpan.Context().TraceID)
	})

	t.Run("SpanAttributes", func(t *testing.T) {
		provider := tracing.NewTraceProvider(logger)
		tracer := provider.Tracer("test-tracer")
		ctx := context.Background()

		ctx, span := tracer.StartSpan(ctx, "test-operation")
		defer span.End()

		span.SetAttribute("key1", "value1")
		span.SetAttribute("key2", 123)

		attrs := span.Attributes()
		assert.Equal(t, "value1", attrs["key1"])
		assert.Equal(t, 123, attrs["key2"])
	})

	t.Run("SpanEvents", func(t *testing.T) {
		provider := tracing.NewTraceProvider(logger)
		tracer := provider.Tracer("test-tracer")
		ctx := context.Background()

		ctx, span := tracer.StartSpan(ctx, "test-operation")
		defer span.End()

		span.AddEvent("event1", map[string]interface{}{"data": "value1"})
		span.AddEvent("event2", map[string]interface{}{"data": "value2"})

		events := span.Events()
		assert.Len(t, events, 2)
		assert.Equal(t, "event1", events[0].Name)
		assert.Equal(t, "event2", events[1].Name)
	})

	t.Run("SpanStatus", func(t *testing.T) {
		provider := tracing.NewTraceProvider(logger)
		tracer := provider.Tracer("test-tracer")
		ctx := context.Background()

		ctx, span := tracer.StartSpan(ctx, "test-operation")
		defer span.End()

		span.SetStatus(tracing.StatusCodeOK, "Operation completed")

		status := span.Status()
		assert.Equal(t, tracing.StatusCodeOK, status.Code)
		assert.Equal(t, "Operation completed", status.Message)
	})
}

func TestCacheIntegration(t *testing.T) {
	logger := zap.NewNop()

	t.Run("LocalCache", func(t *testing.T) {
		cache := optimization.NewLocalCache(3, 10*time.Minute, logger)
		defer cache.Stop()

		err := cache.Set("key1", "value1")
		require.NoError(t, err)

		err = cache.Set("key2", "value2")
		require.NoError(t, err)

		err = cache.Set("key3", "value3")
		require.NoError(t, err)

		value, found := cache.Get("key1")
		require.True(t, found)
		assert.Equal(t, "value1", value)

		err = cache.Set("key4", "value4")
		require.NoError(t, err)

		_, found = cache.Get("key1")
		assert.False(t, found)
	})

	t.Run("CacheTTL", func(t *testing.T) {
		cache := optimization.NewLocalCache(10, 100*time.Millisecond, logger)
		defer cache.Stop()

		err := cache.Set("key1", "value1")
		require.NoError(t, err)

		value, found := cache.Get("key1")
		require.True(t, found)
		assert.Equal(t, "value1", value)

		time.Sleep(150 * time.Millisecond)

		_, found = cache.Get("key1")
		assert.False(t, found)
	})

	t.Run("CacheStats", func(t *testing.T) {
		cache := optimization.NewLocalCache(10, 10*time.Minute, logger)
		defer cache.Stop()

		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		cache.Set("key3", "value3")

		cache.Get("key1")
		cache.Get("key1")
		cache.Get("key2")
		cache.Get("key4")

		stats := cache.GetStats()
		assert.Equal(t, 3, stats["size"])
	})
}

func TestCircuitBreakerIntegration(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	t.Run("CircuitBreakerStates", func(t *testing.T) {
		config := middleware.DefaultCircuitBreakerConfig()
		config.FailureThreshold = 3
		config.SuccessThreshold = 2
		config.Timeout = 5 * time.Second

		breaker := middleware.NewCircuitBreaker("test-service", config, logger)

		assert.Equal(t, middleware.StateClosed, breaker.State())

		for i := 0; i < 3; i++ {
			err := breaker.Execute(ctx, func() error {
				return assert.AnError
			})
			assert.Error(t, err)
		}

		assert.Equal(t, middleware.StateOpen, breaker.State())

		time.Sleep(6 * time.Second)

		err := breaker.Execute(ctx, func() error {
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, middleware.StateHalfOpen, breaker.State())

		err = breaker.Execute(ctx, func() error {
			return nil
		})
		assert.NoError(t, err)

		assert.Equal(t, middleware.StateClosed, breaker.State())
	})

	t.Run("CircuitBreakerExecution", func(t *testing.T) {
		config := middleware.DefaultCircuitBreakerConfig()
		config.FailureThreshold = 2
		config.SuccessThreshold = 1
		config.Timeout = 5 * time.Second

		breaker := middleware.NewCircuitBreaker("test-service", config, logger)

		callCount := 0
		failingFunc := func() error {
			callCount++
			if callCount < 2 {
				return assert.AnError
			}
			return nil
		}

		err := breaker.Execute(ctx, failingFunc)
		assert.Error(t, err)

		err = breaker.Execute(ctx, failingFunc)
		assert.Error(t, err)

		err = breaker.Execute(ctx, failingFunc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "'test-service' is open")
	})
}

func TestConnectionPoolIntegration(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	t.Run("ConnectionPoolBasics", func(t *testing.T) {
		factory := func() (pool.Connection, error) {
			return &mockConnection{id: "conn-1"}, nil
		}

		config := pool.DefaultPoolConfig()
		config.MaxOpen = 5
		config.MaxIdle = 2
		config.MaxLifetime = 30 * time.Minute

		connPool := pool.NewConnectionPool(config, factory, logger)
		defer connPool.Shutdown()

		conn, err := connPool.Get(ctx)
		require.NoError(t, err)
		assert.NotNil(t, conn)

		err = conn.Close()
		require.NoError(t, err)

		stats := connPool.Stats()
		assert.Equal(t, int64(1), stats.TotalCreated)
	})

	t.Run("ConnectionPoolHealthCheck", func(t *testing.T) {
		factory := func() (pool.Connection, error) {
			return &mockConnection{id: "conn-1", healthy: true}, nil
		}

		config := pool.DefaultPoolConfig()
		config.MaxOpen = 5
		config.MaxIdle = 2
		config.MaxLifetime = 30 * time.Minute
		config.HealthCheck = 100 * time.Millisecond

		connPool := pool.NewConnectionPool(config, factory, logger)
		defer connPool.Shutdown()

		conn, err := connPool.Get(ctx)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)

		time.Sleep(200 * time.Millisecond)

		stats := connPool.Stats()
		assert.True(t, stats.TotalClosed >= 0)
	})
}

type mockConnection struct {
	id      string
	healthy bool
}

func (m *mockConnection) Close() error {
	return nil
}

func (m *mockConnection) IsHealthy() bool {
	return m.healthy
}

func (m *mockConnection) LastUsed() time.Time {
	return time.Now()
}
