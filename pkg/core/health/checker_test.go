package health

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewChecker(t *testing.T) {
	t.Run("creates new checker", func(t *testing.T) {
		checker := NewChecker()

		assert.NotNil(t, checker)
		assert.NotNil(t, checker.checks)
	})
}

func TestChecker_Register(t *testing.T) {
	t.Run("register health check", func(t *testing.T) {
		checker := NewChecker()

		checkFunc := func(ctx context.Context) error {
			return nil
		}

		checker.Register("test", checkFunc)

		assert.Len(t, checker.checks, 1)
		assert.Contains(t, checker.checks, "test")
	})

	t.Run("register multiple health checks", func(t *testing.T) {
		checker := NewChecker()

		checker.Register("check1", func(ctx context.Context) error { return nil })
		checker.Register("check2", func(ctx context.Context) error { return nil })
		checker.Register("check3", func(ctx context.Context) error { return nil })

		assert.Len(t, checker.checks, 3)
	})

	t.Run("overwrite existing check", func(t *testing.T) {
		checker := NewChecker()

		checker.Register("test", func(ctx context.Context) error { return errors.New("old") })
		checker.Register("test", func(ctx context.Context) error { return errors.New("new") })

		assert.Len(t, checker.checks, 1)
	})
}

func TestChecker_Check(t *testing.T) {
	t.Run("all checks pass", func(t *testing.T) {
		checker := NewChecker()

		checker.Register("check1", func(ctx context.Context) error { return nil })
		checker.Register("check2", func(ctx context.Context) error { return nil })
		checker.Register("check3", func(ctx context.Context) error { return nil })

		ctx := context.Background()
		err := checker.Check(ctx)

		assert.NoError(t, err)
	})

	t.Run("one check fails", func(t *testing.T) {
		checker := NewChecker()

		checker.Register("check1", func(ctx context.Context) error { return nil })
		checker.Register("check2", func(ctx context.Context) error { return errors.New("check failed") })
		checker.Register("check3", func(ctx context.Context) error { return nil })

		ctx := context.Background()
		err := checker.Check(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "check failed")
	})

	t.Run("no checks registered", func(t *testing.T) {
		checker := NewChecker()

		ctx := context.Background()
		err := checker.Check(ctx)

		assert.NoError(t, err)
	})

	t.Run("works with nil context", func(t *testing.T) {
		checker := NewChecker()

		checker.Register("test", func(ctx context.Context) error { return nil })

		err := checker.Check(nil)

		assert.NoError(t, err)
	})

	t.Run("works with canceled context", func(t *testing.T) {
		checker := NewChecker()

		checker.Register("test", func(ctx context.Context) error { return nil })

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := checker.Check(ctx)

		assert.NoError(t, err)
	})
}
