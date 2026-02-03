package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestCircuitBreakerState_String(t *testing.T) {
	t.Run("closed state", func(t *testing.T) {
		assert.Equal(t, "closed", StateClosed.String())
	})

	t.Run("open state", func(t *testing.T) {
		assert.Equal(t, "open", StateOpen.String())
	})

	t.Run("half-open state", func(t *testing.T) {
		assert.Equal(t, "half-open", StateHalfOpen.String())
	})

	t.Run("unknown state", func(t *testing.T) {
		unknown := CircuitBreakerState(99)
		assert.Equal(t, "unknown", unknown.String())
	})
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()

	assert.Equal(t, 5, config.FailureThreshold)
	assert.Equal(t, 2, config.SuccessThreshold)
	assert.Equal(t, 30*time.Second, config.Timeout)
	assert.Equal(t, 3, config.MaxRequests)
	assert.Equal(t, 0.5, config.FailureRateThreshold)
	assert.Equal(t, 1*time.Minute, config.WindowTime)
}

func TestNewCircuitBreaker(t *testing.T) {
	t.Run("creates new circuit breaker", func(t *testing.T) {
		logger := zap.NewNop()
		config := DefaultCircuitBreakerConfig()
		cb := NewCircuitBreaker("test", config, logger)

		assert.NotNil(t, cb)
		assert.Equal(t, "test", cb.name)
		assert.Equal(t, StateClosed, cb.state)
		assert.Equal(t, 0, cb.failureCount)
		assert.Equal(t, 0, cb.successCount)
	})
}

func TestCircuitBreaker_Execute(t *testing.T) {
	t.Run("successful execution", func(t *testing.T) {
		logger := zap.NewNop()
		config := DefaultCircuitBreakerConfig()
		cb := NewCircuitBreaker("test", config, logger)

		err := cb.Execute(context.Background(), func() error {
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, 0, cb.failureCount)
		assert.Equal(t, 0, cb.successCount)
	})

	t.Run("failed execution", func(t *testing.T) {
		logger := zap.NewNop()
		config := DefaultCircuitBreakerConfig()
		cb := NewCircuitBreaker("test", config, logger)

		err := cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})

		assert.Error(t, err)
		assert.Equal(t, 1, cb.failureCount)
	})
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	t.Run("closed to open on threshold", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 3,
			Timeout:          1 * time.Second,
		}
		cb := NewCircuitBreaker("test", config, logger)

		assert.Equal(t, StateClosed, cb.State())

		for i := 0; i < 3; i++ {
			cb.Execute(context.Background(), func() error {
				return errors.New("error")
			})
		}

		assert.Equal(t, StateOpen, cb.State())
	})

	t.Run("open to half-open after timeout", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			SuccessThreshold: 2,
			Timeout:          100 * time.Millisecond,
		}
		cb := NewCircuitBreaker("test", config, logger)

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		assert.Equal(t, StateOpen, cb.State())

		time.Sleep(150 * time.Millisecond)

		cb.Execute(context.Background(), func() error {
			return nil
		})

		assert.Equal(t, StateHalfOpen, cb.State())
	})

	t.Run("half-open to closed on success", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			SuccessThreshold: 2,
			Timeout:          100 * time.Millisecond,
		}
		cb := NewCircuitBreaker("test", config, logger)

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		time.Sleep(150 * time.Millisecond)

		cb.Execute(context.Background(), func() error {
			return nil
		})

		assert.Equal(t, StateHalfOpen, cb.State())

		cb.Execute(context.Background(), func() error {
			return nil
		})

		assert.Equal(t, StateClosed, cb.State())
	})

	t.Run("half-open to open on failure", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			SuccessThreshold: 2,
			Timeout:          100 * time.Millisecond,
		}
		cb := NewCircuitBreaker("test", config, logger)

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		time.Sleep(150 * time.Millisecond)

		cb.Execute(context.Background(), func() error {
			return nil
		})

		assert.Equal(t, StateHalfOpen, cb.State())

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		assert.Equal(t, StateOpen, cb.State())
	})
}

func TestCircuitBreaker_Stats(t *testing.T) {
	t.Run("get stats", func(t *testing.T) {
		logger := zap.NewNop()
		config := DefaultCircuitBreakerConfig()
		cb := NewCircuitBreaker("test", config, logger)

		cb.Execute(context.Background(), func() error {
			return nil
		})
		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		stats := cb.Stats()

		assert.Equal(t, "test", stats.Name)
		assert.Equal(t, StateOpen, stats.State)
		assert.Equal(t, 1, stats.FailureCount)
		assert.Equal(t, 0, stats.SuccessCount)
		assert.Equal(t, 2, stats.RequestCount)
		assert.Equal(t, 0.5, stats.FailureRate)
	})

	t.Run("get stats with half-open success", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			SuccessThreshold: 2,
			Timeout:          100 * time.Millisecond,
		}
		cb := NewCircuitBreaker("test", config, logger)

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		time.Sleep(150 * time.Millisecond)

		cb.Execute(context.Background(), func() error {
			return nil
		})

		stats := cb.Stats()

		assert.Equal(t, "test", stats.Name)
		assert.Equal(t, StateHalfOpen, stats.State)
		assert.Equal(t, 0, stats.FailureCount)
		assert.Equal(t, 1, stats.SuccessCount)
		assert.Equal(t, 2, stats.RequestCount)
		assert.Equal(t, 0.0, stats.FailureRate)
	})
}

func TestCircuitBreaker_StateChecks(t *testing.T) {
	t.Run("is open", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			Timeout:          1 * time.Second,
		}
		cb := NewCircuitBreaker("test", config, logger)

		assert.False(t, cb.IsOpen())

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		assert.True(t, cb.IsOpen())
	})

	t.Run("is closed", func(t *testing.T) {
		logger := zap.NewNop()
		config := DefaultCircuitBreakerConfig()
		cb := NewCircuitBreaker("test", config, logger)

		assert.True(t, cb.IsClosed())
	})

	t.Run("is half-open", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			SuccessThreshold: 2,
			Timeout:          100 * time.Millisecond,
		}
		cb := NewCircuitBreaker("test", config, logger)

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		time.Sleep(150 * time.Millisecond)

		cb.Execute(context.Background(), func() error {
			return nil
		})

		assert.True(t, cb.IsHalfOpen())
	})
}

func TestCircuitBreaker_Reset(t *testing.T) {
	t.Run("reset to closed", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			Timeout:          1 * time.Second,
		}
		cb := NewCircuitBreaker("test", config, logger)

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		assert.Equal(t, StateOpen, cb.State())

		cb.Reset()

		assert.Equal(t, StateClosed, cb.State())
		assert.Equal(t, 0, cb.failureCount)
		assert.Equal(t, 0, cb.successCount)
	})
}

func TestCircuitBreaker_SetStateChangeCallback(t *testing.T) {
	t.Run("callback is called", func(t *testing.T) {
		logger := zap.NewNop()
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			Timeout:          1 * time.Second,
		}
		cb := NewCircuitBreaker("test", config, logger)

		called := false
		cb.SetStateChangeCallback(func(name string, from, to CircuitBreakerState) {
			called = true
			assert.Equal(t, "test", name)
			assert.Equal(t, StateClosed, from)
			assert.Equal(t, StateOpen, to)
		})

		cb.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		assert.True(t, called)
	})
}

func TestNewCircuitBreakerManager(t *testing.T) {
	t.Run("creates new manager", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewCircuitBreakerManager(logger)

		assert.NotNil(t, manager)
		assert.NotNil(t, manager.breakers)
	})
}

func TestCircuitBreakerManager_GetOrCreate(t *testing.T) {
	t.Run("creates new breaker", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewCircuitBreakerManager(logger)
		config := DefaultCircuitBreakerConfig()

		cb := manager.GetOrCreate("test", config)

		assert.NotNil(t, cb)
		assert.Equal(t, "test", cb.name)
	})

	t.Run("returns existing breaker", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewCircuitBreakerManager(logger)
		config := DefaultCircuitBreakerConfig()

		cb1 := manager.GetOrCreate("test", config)
		cb2 := manager.GetOrCreate("test", config)

		assert.Same(t, cb1, cb2)
	})
}

func TestCircuitBreakerManager_Get(t *testing.T) {
	t.Run("get existing breaker", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewCircuitBreakerManager(logger)
		config := DefaultCircuitBreakerConfig()

		manager.GetOrCreate("test", config)

		cb, err := manager.Get("test")

		assert.NoError(t, err)
		assert.NotNil(t, cb)
	})

	t.Run("get non-existent breaker", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewCircuitBreakerManager(logger)

		cb, err := manager.Get("nonexistent")

		assert.Error(t, err)
		assert.Nil(t, cb)
	})
}

func TestCircuitBreakerManager_GetAll(t *testing.T) {
	t.Run("get all breakers", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewCircuitBreakerManager(logger)
		config := DefaultCircuitBreakerConfig()

		manager.GetOrCreate("test1", config)
		manager.GetOrCreate("test2", config)
		manager.GetOrCreate("test3", config)

		breakers := manager.GetAll()

		assert.Len(t, breakers, 3)
		assert.Contains(t, breakers, "test1")
		assert.Contains(t, breakers, "test2")
		assert.Contains(t, breakers, "test3")
	})
}

func TestCircuitBreakerManager_GetAllStats(t *testing.T) {
	t.Run("get all stats", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewCircuitBreakerManager(logger)
		config := DefaultCircuitBreakerConfig()

		manager.GetOrCreate("test1", config)
		manager.GetOrCreate("test2", config)

		stats := manager.GetAllStats()

		assert.Len(t, stats, 2)
		assert.Contains(t, stats, "test1")
		assert.Contains(t, stats, "test2")
	})
}

func TestCircuitBreakerManager_ResetAll(t *testing.T) {
	t.Run("reset all breakers", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewCircuitBreakerManager(logger)
		config := CircuitBreakerConfig{
			FailureThreshold: 1,
			Timeout:          1 * time.Second,
		}

		cb1 := manager.GetOrCreate("test1", config)
		cb2 := manager.GetOrCreate("test2", config)

		cb1.Execute(context.Background(), func() error {
			return errors.New("error")
		})
		cb2.Execute(context.Background(), func() error {
			return errors.New("error")
		})

		assert.Equal(t, StateOpen, cb1.State())
		assert.Equal(t, StateOpen, cb2.State())

		manager.ResetAll()

		assert.Equal(t, StateClosed, cb1.State())
		assert.Equal(t, StateClosed, cb2.State())
	})
}
