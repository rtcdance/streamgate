//go:build unix

package transcoder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func writeStubExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

// probeJSON builds an ffprobe -print_format json -show_format -show_streams
// shaped document. Empty codecs omit the corresponding stream.
func probeJSON(format, videoCodec, audioCodec, duration string, width, height int) string {
	var streams string
	if videoCodec != "" {
		streams += fmt.Sprintf(`{"codec_name":%q,"codec_type":"video","width":%d,"height":%d,"r_frame_rate":"30/1","bit_rate":"1000000","nb_frames":"100"},`, videoCodec, width, height)
	}
	if audioCodec != "" {
		streams += fmt.Sprintf(`{"codec_name":%q,"codec_type":"audio","bit_rate":"128000"},`, audioCodec)
	}
	streams = strings.TrimSuffix(streams, ",")
	return fmt.Sprintf(`{"format":{"format_name":%q,"duration":%q,"size":"1000000","bit_rate":"1000000"},"streams":[%s]}`, format, duration, streams)
}

func stubFFprobeTranscoder(t *testing.T, cfg *FFmpegConfig, jsonOut string) *FFmpegTranscoder {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'STUBEOF'\n" + jsonOut + "\nSTUBEOF\n"
	cfg.FFprobePath = writeStubExecutable(t, dir, "ffprobe", script)
	return NewFFmpegTranscoder(cfg, zap.NewNop())
}

func writeInputFile(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, []byte("input"), 0o644))
	return p
}

func TestAcceptance_MediaProbeLimits(t *testing.T) {
	limits := func(t *testing.T) *FFmpegConfig {
		return &FFmpegConfig{
			TempDir:      t.TempDir(),
			MaxDuration:  600,
			MaxWidth:     1920,
			MaxHeight:    1080,
			ProbeTimeout: 10 * time.Second,
		}
	}

	t.Run("mp4 h264 aac passes the gate", func(t *testing.T) {
		ft := stubFFprobeTranscoder(t, limits(t), probeJSON("mov,mp4,m4a,3gp,3g2,mj2", "h264", "aac", "5.0", 1280, 720))
		info, err := ft.ValidateMediaFile(context.Background(), writeInputFile(t, "ok.mp4"))
		require.NoError(t, err)
		assert.Equal(t, "mov,mp4,m4a,3gp,3g2,mj2", info.Format, "format_name must be parsed, not left a dead field")
		assert.Equal(t, "h264", info.VideoCodec)
		assert.Equal(t, "aac", info.AudioCodec)
	})

	t.Run("mp4 h264 without audio passes the gate", func(t *testing.T) {
		ft := stubFFprobeTranscoder(t, limits(t), probeJSON("mov,mp4,m4a,3gp,3g2,mj2", "h264", "", "5.0", 1920, 1080))
		_, err := ft.ValidateMediaFile(context.Background(), writeInputFile(t, "noaudio.mp4"))
		require.NoError(t, err)
	})

	rejected := []struct {
		name    string
		jsonOut string
	}{
		{"webm vp9 rejected", probeJSON("matroska,webm", "vp9", "opus", "5.0", 1280, 720)},
		{"matroska container rejected", probeJSON("matroska,webm", "h264", "aac", "5.0", 1280, 720)},
		{"h265 video rejected", probeJSON("mov,mp4,m4a,3gp,3g2,mj2", "hevc", "aac", "5.0", 1280, 720)},
		{"non-aac audio rejected", probeJSON("mov,mp4,m4a,3gp,3g2,mj2", "h264", "mp3", "5.0", 1280, 720)},
		{"audio-only file rejected", probeJSON("mov,mp4,m4a,3gp,3g2,mj2", "", "aac", "5.0", 0, 0)},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			ft := stubFFprobeTranscoder(t, limits(t), tc.jsonOut)
			_, err := ft.ValidateMediaFile(context.Background(), writeInputFile(t, "bad.mp4"))
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMediaLimitExceeded), "want ErrMediaLimitExceeded, got: %v", err)
			assert.Contains(t, err.Error(), "input exceeds limit")
		})
	}

	t.Run("non-media file rejected with the limit error", func(t *testing.T) {
		dir := t.TempDir()
		// Real ffprobe on a text file exits non-zero with no usable JSON; the
		// stub reproduces exactly that behaviour.
		p := writeStubExecutable(t, dir, "ffprobe", "#!/bin/sh\nexit 1\n")
		cfg := limits(t)
		cfg.FFprobePath = p
		ft := NewFFmpegTranscoder(cfg, zap.NewNop())
		input := filepath.Join(dir, "notes.txt")
		require.NoError(t, os.WriteFile(input, []byte("definitely not media"), 0o644))
		_, err := ft.ValidateMediaFile(context.Background(), input)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMediaLimitExceeded), "want ErrMediaLimitExceeded, got: %v", err)
	})

	t.Run("duration ceiling enforced", func(t *testing.T) {
		ft := stubFFprobeTranscoder(t, limits(t), probeJSON("mov,mp4,m4a,3gp,3g2,mj2", "h264", "aac", "700.0", 1280, 720))
		_, err := ft.ValidateMediaFile(context.Background(), writeInputFile(t, "long.mp4"))
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMediaLimitExceeded), "want ErrMediaLimitExceeded, got: %v", err)
		assert.Contains(t, err.Error(), "exceeds maximum")
	})

	t.Run("resolution ceiling enforced at 1921x1081", func(t *testing.T) {
		ft := stubFFprobeTranscoder(t, limits(t), probeJSON("mov,mp4,m4a,3gp,3g2,mj2", "h264", "aac", "5.0", 1921, 1081))
		_, err := ft.ValidateMediaFile(context.Background(), writeInputFile(t, "big.mp4"))
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMediaLimitExceeded), "want ErrMediaLimitExceeded, got: %v", err)
		assert.Contains(t, err.Error(), "resolution")
	})

	t.Run("ffprobe timeout errors instead of hanging", func(t *testing.T) {
		dir := t.TempDir()
		p := writeStubExecutable(t, dir, "ffprobe", "#!/bin/sh\nsleep 5\n")
		cfg := limits(t)
		cfg.FFprobePath = p
		cfg.ProbeTimeout = 300 * time.Millisecond
		ft := NewFFmpegTranscoder(cfg, zap.NewNop())
		start := time.Now()
		_, err := ft.ValidateMediaFile(context.Background(), writeInputFile(t, "hang.mp4"))
		require.Error(t, err)
		assert.Less(t, time.Since(start), 3*time.Second, "probe must abort at its own timeout")
		assert.True(t, errors.Is(err, context.DeadlineExceeded), "want a deadline error, got: %v", err)
	})

	t.Run("ffprobe output is capped", func(t *testing.T) {
		dir := t.TempDir()
		p := writeStubExecutable(t, dir, "ffprobe", "#!/bin/sh\nhead -c 10485760 /dev/zero | tr '\\0' 'A'\n")
		cfg := limits(t)
		cfg.FFprobePath = p
		ft := NewFFmpegTranscoder(cfg, zap.NewNop())
		_, err := ft.ValidateMediaFile(context.Background(), writeInputFile(t, "huge.mp4"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeded")
		assert.Less(t, len(err.Error()), 64<<10, "error output must stay bounded")
	})

	t.Run("filename with shell metacharacters is one argv element", func(t *testing.T) {
		dir := t.TempDir()
		argvFile := filepath.Join(dir, "argv.txt")
		jsonOut := probeJSON("mov,mp4,m4a,3gp,3g2,mj2", "h264", "aac", "5.0", 640, 360)
		script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\ncat <<'STUBEOF'\n%s\nSTUBEOF\n", argvFile, jsonOut)
		p := writeStubExecutable(t, dir, "ffprobe", script)
		cfg := limits(t)
		cfg.FFprobePath = p
		ft := NewFFmpegTranscoder(cfg, zap.NewNop())

		inputPath := filepath.Join(dir, "evil;touch pwned.mp4")
		require.NoError(t, os.WriteFile(inputPath, []byte("x"), 0o644))
		_, err := ft.ValidateMediaFile(context.Background(), inputPath)
		require.NoError(t, err)

		argv, err := os.ReadFile(argvFile)
		require.NoError(t, err)
		lines := strings.Split(strings.TrimRight(string(argv), "\n"), "\n")
		require.NotEmpty(t, lines)
		assert.Equal(t, inputPath, lines[len(lines)-1], "input path must reach ffprobe as a single argv element")
		assert.NoFileExists(t, filepath.Join(dir, "pwned.mp4"), "no shell may interpret the filename")
		assert.NoFileExists(t, "pwned.mp4", "no shell may interpret the filename")
	})

	t.Run("real ffprobe accepts a 10s testsrc mp4", func(t *testing.T) {
		if _, err := exec.LookPath("ffprobe"); err != nil {
			t.Skip("ffprobe not available")
		}
		ffmpegPath, err := exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("ffmpeg not available")
		}
		// Pick an available H.264 encoder: CI builds ship libx264, some host
		// builds (e.g. macOS videotoolbox-only) do not.
		encoder := ""
		if encOut, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput(); err == nil {
			switch {
			case strings.Contains(string(encOut), "libx264"):
				encoder = "libx264"
			case strings.Contains(string(encOut), "h264_videotoolbox"):
				encoder = "h264_videotoolbox"
			}
		}
		if encoder == "" {
			t.Skip("no h264 encoder available")
		}
		dir := t.TempDir()
		fixture := filepath.Join(dir, "testsrc.mp4")
		out, err := exec.Command(ffmpegPath,
			"-f", "lavfi",
			"-i", "testsrc=duration=10:size=640x360:rate=30",
			"-c:v", encoder, "-pix_fmt", "yuv420p",
			"-y", fixture).CombinedOutput()
		require.NoError(t, err, "ffmpeg fixture failed: %s", out)

		ft := NewFFmpegTranscoder(limits(t), zap.NewNop())
		info, err := ft.ValidateMediaFile(context.Background(), fixture)
		require.NoError(t, err, "a real 10s mp4 must pass the gate")
		assert.Contains(t, info.Format, "mp4")
		assert.Equal(t, "h264", info.VideoCodec)
		assert.InDelta(t, 10.0, info.Duration, 1.0)
	})
}
