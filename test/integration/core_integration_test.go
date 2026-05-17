package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"streamgate/pkg/core/config"
	"streamgate/pkg/core/event"
	"streamgate/pkg/middleware"
)

func TestEventBusIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("MemoryEventBus", func(t *testing.T) {
		bus, err := event.NewMemoryEventBus()
		require.NoError(t, err)
		defer bus.Close()

		received := make(chan *event.Event, 1)
		handler := func(ctx context.Context, e *event.Event) error {
			received <- e
			return nil
		}

		err = bus.Subscribe(ctx, "test.topic", handler)
		require.NoError(t, err)

		evt := &event.Event{
			Type:   "test.topic",
			Source: "integration-test",
			Data:   map[string]interface{}{"key": "value"},
		}

		err = bus.Publish(ctx, evt)
		require.NoError(t, err)

		select {
		case receivedEvent := <-received:
			assert.Equal(t, "test.topic", receivedEvent.Type)
			assert.Equal(t, "value", receivedEvent.Data["key"])
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for event")
		}
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
