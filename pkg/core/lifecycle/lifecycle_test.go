package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLifecycle(t *testing.T) {
	t.Run("creates new lifecycle", func(t *testing.T) {
		lc := NewLifecycle()

		assert.NotNil(t, lc)
		assert.NotNil(t, lc.startHooks)
		assert.NotNil(t, lc.stopHooks)
		assert.Empty(t, lc.startHooks)
		assert.Empty(t, lc.stopHooks)
	})
}

func TestLifecycle_OnStart(t *testing.T) {
	t.Run("register start hook", func(t *testing.T) {
		lc := NewLifecycle()

		hook := func(ctx context.Context) error {
			return nil
		}

		lc.OnStart(hook)

		assert.Len(t, lc.startHooks, 1)
	})

	t.Run("register multiple start hooks", func(t *testing.T) {
		lc := NewLifecycle()

		lc.OnStart(func(ctx context.Context) error { return nil })
		lc.OnStart(func(ctx context.Context) error { return nil })
		lc.OnStart(func(ctx context.Context) error { return nil })

		assert.Len(t, lc.startHooks, 3)
	})
}

func TestLifecycle_OnStop(t *testing.T) {
	t.Run("register stop hook", func(t *testing.T) {
		lc := NewLifecycle()

		hook := func(ctx context.Context) error {
			return nil
		}

		lc.OnStop(hook)

		assert.Len(t, lc.stopHooks, 1)
	})

	t.Run("register multiple stop hooks", func(t *testing.T) {
		lc := NewLifecycle()

		lc.OnStop(func(ctx context.Context) error { return nil })
		lc.OnStop(func(ctx context.Context) error { return nil })
		lc.OnStop(func(ctx context.Context) error { return nil })

		assert.Len(t, lc.stopHooks, 3)
	})
}

func TestLifecycle_Start(t *testing.T) {
	t.Run("all hooks pass", func(t *testing.T) {
		lc := NewLifecycle()

		called := 0
		lc.OnStart(func(ctx context.Context) error {
			called++
			return nil
		})
		lc.OnStart(func(ctx context.Context) error {
			called++
			return nil
		})

		ctx := context.Background()
		err := lc.Start(ctx)

		assert.NoError(t, err)
		assert.Equal(t, 2, called)
	})

	t.Run("one hook fails", func(t *testing.T) {
		lc := NewLifecycle()

		called := 0
		lc.OnStart(func(ctx context.Context) error {
			called++
			return nil
		})
		lc.OnStart(func(ctx context.Context) error {
			called++
			return errors.New("hook failed")
		})
		lc.OnStart(func(ctx context.Context) error {
			called++
			return nil
		})

		ctx := context.Background()
		err := lc.Start(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hook failed")
		assert.Equal(t, 2, called)
	})

	t.Run("no hooks registered", func(t *testing.T) {
		lc := NewLifecycle()

		ctx := context.Background()
		err := lc.Start(ctx)

		assert.NoError(t, err)
	})

	t.Run("works with nil context", func(t *testing.T) {
		lc := NewLifecycle()

		lc.OnStart(func(ctx context.Context) error { return nil })

		err := lc.Start(nil)

		assert.NoError(t, err)
	})
}

func TestLifecycle_Stop(t *testing.T) {
	t.Run("all hooks pass", func(t *testing.T) {
		lc := NewLifecycle()

		called := 0
		lc.OnStop(func(ctx context.Context) error {
			called++
			return nil
		})
		lc.OnStop(func(ctx context.Context) error {
			called++
			return nil
		})

		ctx := context.Background()
		err := lc.Stop(ctx)

		assert.NoError(t, err)
		assert.Equal(t, 2, called)
	})

	t.Run("one hook fails", func(t *testing.T) {
		lc := NewLifecycle()

		called := 0
		lc.OnStop(func(ctx context.Context) error {
			called++
			return nil
		})
		lc.OnStop(func(ctx context.Context) error {
			called++
			return errors.New("hook failed")
		})
		lc.OnStop(func(ctx context.Context) error {
			called++
			return nil
		})

		ctx := context.Background()
		err := lc.Stop(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hook failed")
		assert.Equal(t, 2, called)
	})

	t.Run("no hooks registered", func(t *testing.T) {
		lc := NewLifecycle()

		ctx := context.Background()
		err := lc.Stop(ctx)

		assert.NoError(t, err)
	})

	t.Run("works with nil context", func(t *testing.T) {
		lc := NewLifecycle()

		lc.OnStop(func(ctx context.Context) error { return nil })

		err := lc.Stop(nil)

		assert.NoError(t, err)
	})
}
