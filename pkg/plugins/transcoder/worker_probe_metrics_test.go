//go:build media

package transcoder

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	transcodingsvc "github.com/rtcdance/streamgate/pkg/service/transcoding"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMedia_WorkerProbeMetadata(t *testing.T) {
	testWorkerProbeMetadata(t, nil)
}

func TestMedia_WorkerProbeMetadataAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set")
	}
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, helpers.ApplyMigrations(t, db))
	testWorkerProbeMetadata(t, db)
}

func testWorkerProbeMetadata(t *testing.T, db storage.DB) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	require.NoError(t, err, "the media acceptance requires real FFmpeg")
	ffprobe, err := exec.LookPath("ffprobe")
	require.NoError(t, err)
	input := filepath.Join(t.TempDir(), "source.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=15", "-t", "2",
		"-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", input).CombinedOutput()
	require.NoError(t, err, "%s", output)
	fixture, err := os.ReadFile(input)
	require.NoError(t, err)
	t.Logf("fixture_sha256=%x", sha256.Sum256(fixture))
	log := zap.NewNop()
	beforeDurationCount, beforeDurationSum := workerHistogramSnapshot(t, "streamgate_transcoding_duration_seconds")
	beforeRTFCount, beforeRTFSum := workerHistogramSnapshot(t, "streamgate_transcoding_rtf")
	ft := NewFFmpegTranscoder(&FFmpegConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe,
		TempDir: t.TempDir(), Threads: 1, ProbeTimeout: 10 * time.Second}, log)
	svc := transcodingsvc.NewTranscodingService(db, transcodingsvc.NewMemoryTranscodingQueue(),
		transcodingsvc.WithTranscoder(NewVideoTranscoder(ft, log)),
		transcodingsvc.WithLogger(log), transcodingsvc.WithMinWorkers(1), transcodingsvc.WithMaxWorkers(1))
	svc.StartWorker(log)
	t.Cleanup(svc.StopWorker)
	contentID := uuid.NewString()
	if db != nil {
		_, err = db.Exec(ctx, "INSERT INTO contents (id, title, type) VALUES ($1, 'probe fixture', 'video')", contentID)
		require.NoError(t, err)
	}
	id, err := svc.Transcode(ctx, contentID, "360p", input, 1, "0xOwner")
	require.NoError(t, err)
	var completed *transcodingsvc.TranscodingTask
	require.Eventually(t, func() bool {
		task, statusErr := svc.GetTranscodingStatus(ctx, id)
		if statusErr == nil && task.Status == "completed" {
			completed = task
			return true
		}
		return false
	}, 40*time.Second, 20*time.Millisecond, "the real worker must finish transcoding")
	svc.StopWorker()
	media, ok := completed.Metadata["media_duration_seconds"].(float64)
	require.True(t, ok, "the worker must carry the actual ffprobe result")
	require.InDelta(t, 2.0, media, 0.1)
	probe, ok := completed.Metadata["probe_duration_seconds"].(float64)
	require.True(t, ok)
	require.Greater(t, probe, 0.0)
	wall, ok := completed.Metadata["transcode_duration_seconds"].(float64)
	require.True(t, ok)
	require.Greater(t, wall, 0.0)
	rtf, ok := completed.Metadata["transcoding_rtf"].(float64)
	require.True(t, ok)
	require.InDelta(t, wall/media, rtf, 0.000001)
	afterDurationCount, afterDurationSum := workerHistogramSnapshot(t, "streamgate_transcoding_duration_seconds")
	afterRTFCount, afterRTFSum := workerHistogramSnapshot(t, "streamgate_transcoding_rtf")
	require.Equal(t, beforeDurationCount+1, afterDurationCount)
	require.Equal(t, beforeRTFCount+1, afterRTFCount)
	require.InDelta(t, wall, afterDurationSum-beforeDurationSum, 0.000001)
	require.InDelta(t, rtf, afterRTFSum-beforeRTFSum, 0.000001)
	t.Logf("media_seconds=%.6f probe_seconds=%.6f transcode_seconds=%.6f rtf=%.6f", media, probe, wall, rtf)
}

func workerHistogramSnapshot(t *testing.T, name string) (count uint64, sum float64) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "profile" && label.GetValue() == "360p" {
					return metric.GetHistogram().GetSampleCount(), metric.GetHistogram().GetSampleSum()
				}
			}
		}
	}
	return 0, 0
}
