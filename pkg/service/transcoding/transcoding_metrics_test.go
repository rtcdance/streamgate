package transcoding

// transcoding_metrics_test.go — SG-14 part 2: TranscodingTasksTotal and
// TranscodingRTF wiring at the terminal transitions.
//
// Counters are read straight from the default registry instead of prometheus
// testutil, which would add a new module (kylelemons/godebug) — this project
// adds no dependencies.

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transcodingCounter(t *testing.T, name string, want map[string]string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if !tcLabelsMatch(m, want) {
				continue
			}
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func transcodingHistogram(t *testing.T, name string, want map[string]string) (count uint64, sum float64) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if !tcLabelsMatch(m, want) {
				continue
			}
			return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
		}
	}
	return 0, 0
}

func tcLabelsMatch(m *dto.Metric, want map[string]string) bool {
	got := map[string]string{}
	for _, lp := range m.GetLabel() {
		got[lp.GetName()] = lp.GetValue()
	}
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// TestTranscodingMetrics_CompleteTask_CountsSucceeded pins the memory-mode
// terminal path: CompleteTask increments TranscodingTasksTotal{ succeeded }.
func TestTranscodingMetrics_CompleteTask_CountsSucceeded(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	id, err := svc.Transcode(context.Background(), "content-metrics-ok", "720p", "http://in/video.mp4", 1, "0xwallet")
	require.NoError(t, err)

	before := transcodingCounter(t, "streamgate_transcoding_tasks_total",
		map[string]string{"outcome": "succeeded", "profile": "720p"})
	require.NoError(t, svc.CompleteTask(context.Background(), id, "streams/content-metrics-ok/720p"))
	after := transcodingCounter(t, "streamgate_transcoding_tasks_total",
		map[string]string{"outcome": "succeeded", "profile": "720p"})

	assert.Equal(t, before+1, after, "CompleteTask must increment TranscodingTasksTotal{succeeded,720p}")
}

// TestTranscodingMetrics_FailTask_CountsFailed pins FailTask.
func TestTranscodingMetrics_FailTask_CountsFailed(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	id, err := svc.Transcode(context.Background(), "content-metrics-fail", "360p", "http://in/video.mp4", 1, "0xwallet")
	require.NoError(t, err)

	before := transcodingCounter(t, "streamgate_transcoding_tasks_total",
		map[string]string{"outcome": "failed", "profile": "360p"})
	require.NoError(t, svc.FailTask(context.Background(), id, "boom"))
	after := transcodingCounter(t, "streamgate_transcoding_tasks_total",
		map[string]string{"outcome": "failed", "profile": "360p"})

	assert.Equal(t, before+1, after, "FailTask must increment TranscodingTasksTotal{failed,360p}")
}

// TestTranscodingMetrics_CancelTask_CountsCancelled pins CancelTask.
func TestTranscodingMetrics_CancelTask_CountsCancelled(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	id, err := svc.Transcode(context.Background(), "content-metrics-cancel", "480p", "http://in/video.mp4", 1, "0xwallet")
	require.NoError(t, err)

	before := transcodingCounter(t, "streamgate_transcoding_tasks_total",
		map[string]string{"outcome": "cancelled", "profile": "480p"})
	require.NoError(t, svc.CancelTask(context.Background(), id))
	after := transcodingCounter(t, "streamgate_transcoding_tasks_total",
		map[string]string{"outcome": "cancelled", "profile": "480p"})

	assert.Equal(t, before+1, after, "CancelTask must increment TranscodingTasksTotal{cancelled,480p}")
}

// TestTranscodingMetrics_CancelTask_AlreadyCancelledNotCounted proves a no-op
// cancel does not inflate the outcome counter.
func TestTranscodingMetrics_CancelTask_AlreadyCancelledNotCounted(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	id, err := svc.Transcode(context.Background(), "content-metrics-cancel2", "480p", "http://in/video.mp4", 1, "0xwallet")
	require.NoError(t, err)
	require.NoError(t, svc.CancelTask(context.Background(), id))

	before := transcodingCounter(t, "streamgate_transcoding_tasks_total",
		map[string]string{"outcome": "cancelled", "profile": "480p"})
	// Second cancel: the task is already terminal; updateTask keeps the status,
	// so no new terminal transition happens.
	_ = svc.CancelTask(context.Background(), id)
	after := transcodingCounter(t, "streamgate_transcoding_tasks_total",
		map[string]string{"outcome": "cancelled", "profile": "480p"})

	assert.Equal(t, before, after, "a cancel of an already-cancelled task must not double-count")
}

// TestTranscodingMetrics_RTF_OnlyWhenDurationKnown: RTF is observed when the
// task metadata carries a real media duration and a start time exists; without
// one it is deliberately NOT recorded (never faked).
func TestTranscodingMetrics_RTF_OnlyWhenDurationKnown(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	id, err := svc.Transcode(context.Background(), "content-metrics-rtf", "720p", "http://in/video.mp4", 1, "0xwallet")
	require.NoError(t, err)

	// Simulate the worker marking the task started, with a probe-plumbed
	// duration of 100 media seconds in the task metadata.
	started := time.Now().Add(-time.Second)
	task, err := svc.getTask(id)
	require.NoError(t, err)
	task.Status = "processing"
	task.StartedAt = &started
	task.Metadata["media_duration_seconds"] = 100.0
	svc.storeTask(task)

	countBefore, _ := transcodingHistogram(t, "streamgate_transcoding_rtf",
		map[string]string{"profile": "720p"})
	require.NoError(t, svc.CompleteTask(context.Background(), id, "streams/content-metrics-rtf/720p"))
	countAfter, sumAfter := transcodingHistogram(t, "streamgate_transcoding_rtf",
		map[string]string{"profile": "720p"})

	assert.Equal(t, countBefore+1, countAfter,
		"a completion with a known media duration must record one RTF observation")
	assert.Greater(t, sumAfter, 0.0, "the RTF observation must be positive")

	// Without a duration there is nothing to divide by — no RTF is recorded.
	id2, err := svc.Transcode(context.Background(), "content-metrics-nodur", "1080p", "http://in/video2.mp4", 1, "0xwallet")
	require.NoError(t, err)
	task2, err := svc.getTask(id2)
	require.NoError(t, err)
	task2.Status = "processing"
	started2 := time.Now().Add(-time.Second)
	task2.StartedAt = &started2
	svc.storeTask(task2)

	noDurBefore, _ := transcodingHistogram(t, "streamgate_transcoding_rtf",
		map[string]string{"profile": "1080p"})
	require.NoError(t, svc.CompleteTask(context.Background(), id2, "streams/content-metrics-nodur/1080p"))
	noDurAfter, _ := transcodingHistogram(t, "streamgate_transcoding_rtf",
		map[string]string{"profile": "1080p"})

	assert.Equal(t, noDurBefore, noDurAfter,
		"a completion without a known media duration must NOT record a (fake) RTF")
}
