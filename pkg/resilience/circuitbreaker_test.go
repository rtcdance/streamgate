package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCircuitBreakerState_String(t *testing.T) {
	tests := []struct {
		state CircuitBreakerState
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitBreakerState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	assert.Equal(t, 5, cfg.FailureThreshold)
	assert.Equal(t, 2, cfg.SuccessThreshold)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.Equal(t, 3, cfg.MaxRequests)
	assert.Equal(t, 0.5, cfg.FailureRateThreshold)
	assert.Equal(t, 1*time.Minute, cfg.WindowTime)
}

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), zap.NewNop())
	assert.NotNil(t, cb)
	assert.Equal(t, StateClosed, cb.State())
	assert.True(t, cb.IsClosed())
	assert.False(t, cb.IsOpen())
	assert.False(t, cb.IsHalfOpen())
}

func TestCircuitBreaker_Execute_Success(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), zap.NewNop())

	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, cb.IsClosed())
}

func TestCircuitBreaker_Execute_Failure(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 3
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	for i := 0; i < 3; i++ {
		_ = cb.Execute(context.Background(), func() error {
			return errors.New("fail")
		})
	}

	assert.True(t, cb.IsOpen())
}

func TestCircuitBreaker_Open_Rejects(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	_ = cb.Execute(context.Background(), func() error {
		return errors.New("fail")
	})

	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is open")
}

func TestCircuitBreaker_HalfOpen_AfterTimeout(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cfg.Timeout = 50 * time.Millisecond
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	_ = cb.Execute(context.Background(), func() error {
		return errors.New("fail")
	})
	assert.True(t, cb.IsOpen())

	time.Sleep(60 * time.Millisecond)

	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_HalfOpen_Success_ClosesCircuit(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cfg.SuccessThreshold = 1
	cfg.Timeout = 50 * time.Millisecond
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	_ = cb.Execute(context.Background(), func() error {
		return errors.New("fail")
	})
	assert.True(t, cb.IsOpen())

	time.Sleep(60 * time.Millisecond)

	err := cb.Execute(context.Background(), func() error {
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, cb.IsClosed())
}

func TestCircuitBreaker_HalfOpen_Failure_Reopens(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cfg.SuccessThreshold = 2
	cfg.Timeout = 50 * time.Millisecond
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	_ = cb.Execute(context.Background(), func() error {
		return errors.New("fail")
	})
	assert.True(t, cb.IsOpen())

	time.Sleep(60 * time.Millisecond)

	_ = cb.Execute(context.Background(), func() error {
		return errors.New("fail again")
	})
	assert.True(t, cb.IsOpen())
}

func TestCircuitBreaker_HalfOpen_MaxRequests(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cfg.SuccessThreshold = 2
	cfg.Timeout = 50 * time.Millisecond
	cfg.MaxRequests = 1
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	_ = cb.Execute(context.Background(), func() error {
		return errors.New("fail")
	})

	time.Sleep(60 * time.Millisecond)

	started := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		close(started)
		_ = cb.Execute(context.Background(), func() error {
			time.Sleep(100 * time.Millisecond)
			return nil
		})
		close(firstDone)
	}()

	<-started
	time.Sleep(10 * time.Millisecond)

	err2 := cb.Execute(context.Background(), func() error {
		return nil
	})
	assert.Error(t, err2)
	assert.Contains(t, err2.Error(), "max requests exceeded")

	<-firstDone
}

func TestCircuitBreaker_Execute_Panic(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), zap.NewNop())

	err := cb.Execute(context.Background(), func() error {
		panic("test panic")
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic")
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), zap.NewNop())

	cb.RecordSuccess()

	stats := cb.Stats()
	assert.Equal(t, 1, stats.RequestCount)
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 5
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	cb.RecordFailure()

	stats := cb.Stats()
	assert.Equal(t, 1, stats.FailureCount)
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureRateThreshold = 1.0
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	cb.RecordSuccess()
	cb.RecordFailure()

	stats := cb.Stats()
	assert.Equal(t, "test", stats.Name)
	assert.Equal(t, StateClosed, stats.State)
	assert.Equal(t, 1, stats.FailureCount)
	assert.Equal(t, 2, stats.RequestCount)
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	cb.RecordFailure()
	assert.True(t, cb.IsOpen())

	cb.Reset()
	assert.True(t, cb.IsClosed())
}

func TestCircuitBreaker_Allow(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), zap.NewNop())
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_SetStateChangeCallback(t *testing.T) {
	var fromState, toState CircuitBreakerState
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	cb.SetStateChangeCallback(func(name string, from, to CircuitBreakerState) {
		fromState = from
		toState = to
	})

	cb.RecordFailure()

	assert.Equal(t, StateClosed, fromState)
	assert.Equal(t, StateOpen, toState)
}

func TestCircuitBreaker_FailureRateThreshold(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 100
	cfg.FailureRateThreshold = 0.5
	cfg.WindowTime = 1 * time.Minute
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	for i := 0; i < 10; i++ {
		cb.RecordFailure()
	}
	for i := 0; i < 10; i++ {
		cb.RecordSuccess()
	}

	assert.True(t, cb.IsOpen())
}

func TestNewCircuitBreakerManager(t *testing.T) {
	mgr := NewCircuitBreakerManager(zap.NewNop())
	assert.NotNil(t, mgr)
}

func TestCircuitBreakerManager_GetOrCreate(t *testing.T) {
	mgr := NewCircuitBreakerManager(zap.NewNop())

	cb1 := mgr.GetOrCreate("test", DefaultCircuitBreakerConfig())
	assert.NotNil(t, cb1)

	cb2 := mgr.GetOrCreate("test", DefaultCircuitBreakerConfig())
	assert.Equal(t, cb1, cb2)
}

func TestCircuitBreakerManager_Get_NotFound(t *testing.T) {
	mgr := NewCircuitBreakerManager(zap.NewNop())

	_, err := mgr.Get("nonexistent")
	assert.Error(t, err)
}

func TestCircuitBreakerManager_GetAll(t *testing.T) {
	mgr := NewCircuitBreakerManager(zap.NewNop())
	mgr.GetOrCreate("cb1", DefaultCircuitBreakerConfig())
	mgr.GetOrCreate("cb2", DefaultCircuitBreakerConfig())

	all := mgr.GetAll()
	assert.Len(t, all, 2)
}

func TestCircuitBreakerManager_GetAllStats(t *testing.T) {
	mgr := NewCircuitBreakerManager(zap.NewNop())
	mgr.GetOrCreate("cb1", DefaultCircuitBreakerConfig())

	stats := mgr.GetAllStats()
	assert.Contains(t, stats, "cb1")
}

func TestCircuitBreakerManager_ResetAll(t *testing.T) {
	mgr := NewCircuitBreakerManager(zap.NewNop())
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cb := mgr.GetOrCreate("test", cfg)

	cb.RecordFailure()
	assert.True(t, cb.IsOpen())

	mgr.ResetAll()
	assert.True(t, cb.IsClosed())
}

func TestTrimWindow(t *testing.T) {
	now := time.Now()
	window := []time.Time{
		now.Add(-2 * time.Minute),
		now.Add(-1 * time.Minute),
		now.Add(-30 * time.Second),
	}

	cutoff := now.Add(-1 * time.Minute)
	trimWindow(&window, cutoff)

	assert.Len(t, window, 1)
}

func TestTrimWindow_AllOld(t *testing.T) {
	now := time.Now()
	window := []time.Time{
		now.Add(-5 * time.Minute),
		now.Add(-4 * time.Minute),
	}

	cutoff := now.Add(-1 * time.Minute)
	trimWindow(&window, cutoff)

	assert.Empty(t, window)
}

func TestCircuitBreaker_CalculateFailureRate(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), zap.NewNop())

	cb.RecordSuccess()
	cb.RecordSuccess()
	cb.RecordFailure()

	rate := cb.calculateFailureRate()
	assert.Greater(t, rate, 0.0)
}

func TestCircuitBreaker_CalculateFailureRate_NoRequests(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), zap.NewNop())

	rate := cb.calculateFailureRate()
	assert.Equal(t, 0.0, rate)
}

func TestCircuitBreaker_Execute_ContextCancelled(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 1
	cfg.Timeout = 5 * time.Second
	cb := NewCircuitBreaker("test", cfg, zap.NewNop())

	cb.RecordFailure()
	require.True(t, cb.IsOpen())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cb.Execute(ctx, func() error {
		return nil
	})
	assert.Error(t, err)
}
