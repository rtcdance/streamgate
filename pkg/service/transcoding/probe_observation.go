package transcoding

import (
	"context"
	"math"
	"time"
)

type mediaProbeObserverKey struct{}

// WithMediaProbeObserver carries a task-local observer to the existing probe
// call. It avoids probing again just to obtain telemetry.
func WithMediaProbeObserver(ctx context.Context, observer func(float64, time.Duration)) context.Context {
	return context.WithValue(ctx, mediaProbeObserverKey{}, observer)
}

// ReportMediaProbe records a successful probe's media duration and measured
// elapsed time. Invalid measurements never become task metadata or metrics.
func ReportMediaProbe(ctx context.Context, mediaSeconds float64, elapsed time.Duration) {
	if !finitePositive(mediaSeconds) || elapsed <= 0 {
		return
	}
	if observer, ok := ctx.Value(mediaProbeObserverKey{}).(func(float64, time.Duration)); ok && observer != nil {
		observer(mediaSeconds, elapsed)
	}
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
