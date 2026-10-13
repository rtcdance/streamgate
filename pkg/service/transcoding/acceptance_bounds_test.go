package transcoding

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcceptance_WorkerAndQueueBounds(t *testing.T) {
	t.Run("memory queue depth cap returns ErrQueueFull", func(t *testing.T) {
		q := NewMemoryTranscodingQueueWithLimit(2)
		require.NoError(t, q.Enqueue(&models.TranscodingTask{ID: "t1", Status: "pending"}))
		require.NoError(t, q.Enqueue(&models.TranscodingTask{ID: "t2", Status: "pending"}))
		err := q.Enqueue(&models.TranscodingTask{ID: "t3", Status: "pending"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrQueueFull), "want ErrQueueFull, got: %v", err)
	})

	t.Run("memory queue defaults to a 100-task depth cap", func(t *testing.T) {
		q := NewMemoryTranscodingQueue()
		for i := 0; i < 100; i++ {
			require.NoError(t, q.Enqueue(&models.TranscodingTask{ID: fmt.Sprintf("t%d", i), Status: "pending"}))
		}
		err := q.Enqueue(&models.TranscodingTask{ID: "overflow", Status: "pending"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrQueueFull), "want ErrQueueFull, got: %v", err)
	})

	t.Run("Transcode enforces the per-owner pending cap", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(), WithOwnerTaskLimit(2))
		_, err := svc.Transcode(context.Background(), "c-1", "720p", "https://example.com/in.mp4", 1, "0xOwner")
		require.NoError(t, err)
		_, err = svc.Transcode(context.Background(), "c-2", "720p", "https://example.com/in.mp4", 1, "0xOwner")
		require.NoError(t, err)
		_, err = svc.Transcode(context.Background(), "c-3", "720p", "https://example.com/in.mp4", 1, "0xOwner")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrOwnerTaskLimit), "want ErrOwnerTaskLimit, got: %v", err)
	})

	t.Run("Transcode enforces the queue depth cap", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(), WithQueueLimit(1))
		_, err := svc.Transcode(context.Background(), "c-1", "720p", "https://example.com/in.mp4", 1, "0xOwner")
		require.NoError(t, err)
		_, err = svc.Transcode(context.Background(), "c-2", "720p", "https://example.com/in.mp4", 1, "0xOwner")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrQueueFull), "want ErrQueueFull, got: %v", err)
	})
}
