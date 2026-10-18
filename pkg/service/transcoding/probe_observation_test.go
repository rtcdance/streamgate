package transcoding

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMediaProbeObservation_RejectsInvalidMeasurements(t *testing.T) {
	count := 0
	ctx := WithMediaProbeObserver(context.Background(), func(seconds float64, elapsed time.Duration) {
		count++
		require.Equal(t, 2.0, seconds)
		require.Equal(t, time.Millisecond, elapsed)
	})
	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		ReportMediaProbe(ctx, value, time.Millisecond)
	}
	ReportMediaProbe(ctx, 2, 0)
	ReportMediaProbe(ctx, 2, -time.Second)
	require.Zero(t, count)
	ReportMediaProbe(ctx, 2, time.Millisecond)
	require.Equal(t, 1, count)
	ReportMediaProbe(context.Background(), 2, time.Millisecond)
	ReportMediaProbe(WithMediaProbeObserver(context.Background(), nil), 2, time.Millisecond)
}

func TestTranscodingMetadata_SnapshotsAreIndependent(t *testing.T) {
	for _, surface := range []string{"store", "get", "list"} {
		t.Run(surface, func(t *testing.T) {
			svc := NewTranscodingService(nil, nil)
			original := &TranscodingTask{ID: "snapshot", ContentID: "content", Metadata: map[string]interface{}{
				"media_duration_seconds": 2.0,
				"variant_progress":       map[string]interface{}{"360p": 25},
			}}
			svc.storeTask(original)
			mutated := original
			switch surface {
			case "get":
				var err error
				mutated, err = svc.getTask(original.ID)
				require.NoError(t, err)
			case "list":
				tasks := svc.listTasks("content", "", 10, 0)
				require.Len(t, tasks, 1)
				mutated = tasks[0]
			}
			mutated.Metadata["media_duration_seconds"] = 999.0
			mutated.Metadata["variant_progress"].(map[string]interface{})["360p"] = 99
			stored, err := svc.getTask(original.ID)
			require.NoError(t, err)
			require.Equal(t, 2.0, stored.Metadata["media_duration_seconds"])
			require.Equal(t, 25, stored.Metadata["variant_progress"].(map[string]interface{})["360p"])
		})
	}
}
