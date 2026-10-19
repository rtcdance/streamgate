package load

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPerfHarness_SamplingBarrier(t *testing.T) {
	t.Run("waits for release", func(t *testing.T) {
		dir := t.TempDir()
		done := make(chan error, 1)
		go func() { done <- WaitForSamplingRelease(context.Background(), dir, time.Second) }()
		require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "ready.json")); return err == nil }, time.Second, time.Millisecond)
		select {
		case <-done:
			t.Fatal("sampling released before preparation")
		default:
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, "release"), nil, 0o600))
		require.NoError(t, <-done)
	})
	t.Run("bounded wait", func(t *testing.T) {
		require.Error(t, WaitForSamplingRelease(context.Background(), t.TempDir(), 10*time.Millisecond))
	})
	t.Run("stale release", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "release"), nil, 0o600))
		require.Error(t, WaitForSamplingRelease(context.Background(), dir, time.Second))
	})
}
