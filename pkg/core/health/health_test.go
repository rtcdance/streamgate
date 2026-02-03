package health

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheck(t *testing.T) {
	t.Run("returns healthy status", func(t *testing.T) {
		ctx := context.Background()
		result := Check(ctx)

		assert.NotNil(t, result)
		assert.Equal(t, "healthy", result.Status)
	})

	t.Run("works with nil context", func(t *testing.T) {
		result := Check(nil)

		assert.NotNil(t, result)
		assert.Equal(t, "healthy", result.Status)
	})

	t.Run("works with canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result := Check(ctx)

		assert.NotNil(t, result)
		assert.Equal(t, "healthy", result.Status)
	})
}
