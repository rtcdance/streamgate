//go:build unix

package transcoder

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAcceptance_WorkerAndQueueBounds(t *testing.T) {
	t.Run("ffmpeg transcode invocations carry -threads", func(t *testing.T) {
		dir := t.TempDir()
		argvFile := filepath.Join(dir, "argv.txt")
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + strconv.Quote(argvFile) + "\nexit 0\n"
		ffmpegPath := writeStubExecutable(t, dir, "ffmpeg", script)

		ft := NewFFmpegTranscoder(&FFmpegConfig{FFmpegPath: ffmpegPath, TempDir: dir, Threads: 2}, zap.NewNop())
		err := ft.Transcode(context.Background(), filepath.Join(dir, "in.mp4"), filepath.Join(dir, "out.mp4"),
			TranscodeProfile{Resolution: "720p", Bitrate: "2500k", Format: "mp4"}, nil)
		require.NoError(t, err)

		argv, err := os.ReadFile(argvFile)
		require.NoError(t, err)
		lines := strings.Split(strings.TrimSpace(string(argv)), "\n")
		idx := -1
		for i, l := range lines {
			if l == "-threads" {
				idx = i
				break
			}
		}
		require.GreaterOrEqual(t, idx, 0, "ffmpeg args must include -threads, got: %v", lines)
		require.Less(t, idx+1, len(lines))
		assert.Equal(t, "2", lines[idx+1], "-threads must carry the configured value")
	})

	t.Run("cancel kills the whole process group", func(t *testing.T) {
		dir := t.TempDir()
		childFile := filepath.Join(dir, "child.pid")
		// The stub spawns a long-lived child (like ffmpeg worker processes) and
		// then waits; a kill of only the direct child would leak the `sleep`.
		script := "#!/bin/sh\nsleep 30 &\necho $! > " + strconv.Quote(childFile) + "\nwait\n"
		ffmpegPath := writeStubExecutable(t, dir, "ffmpeg", script)

		ft := NewFFmpegTranscoder(&FFmpegConfig{FFmpegPath: ffmpegPath, TempDir: dir}, zap.NewNop())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		errCh := make(chan error, 1)
		go func() {
			errCh <- ft.ExtractThumbnail(ctx, filepath.Join(dir, "in.mp4"), filepath.Join(dir, "out.jpg"), "00:00:01")
		}()

		// Cancel only once the stub has recorded its child pid, so the kill
		// race cannot make this test flaky.
		var childPID int
		require.Eventually(t, func() bool {
			data, err := os.ReadFile(childFile)
			if err != nil {
				return false
			}
			childPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
			return err == nil && childPID > 0
		}, 5*time.Second, 20*time.Millisecond, "stub must record its child pid")

		cancel()
		select {
		case err := <-errCh:
			require.Error(t, err, "cancelled ffmpeg run must fail")
		case <-time.After(5 * time.Second):
			t.Fatal("ffmpeg run did not abort on cancel")
		}

		require.Eventually(t, func() bool {
			return syscall.Kill(childPID, 0) != nil
		}, 5*time.Second, 50*time.Millisecond, "ffmpeg child process must die with the process group")
	})
}
