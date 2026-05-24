package monitoring

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewTracer(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	assert.NotNil(t, tracer)
	assert.Equal(t, "test-service", tracer.serviceName)
	assert.NotNil(t, tracer.spans)
	assert.NotNil(t, tracer.traces)
}

func TestTracer_StartSpan_NewTrace(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	span, ctx := tracer.StartSpan(ctx, "test-operation")

	assert.NotNil(t, span)
	assert.NotEmpty(t, span.ID)
	assert.NotEmpty(t, span.TraceID)
	assert.Equal(t, "test-operation", span.OperationName)
	assert.Equal(t, "running", span.Status)
	assert.Empty(t, span.ParentSpanID)

	traceIDFromCtx := tracer.getTraceID(ctx)
	assert.Equal(t, span.TraceID, traceIDFromCtx)
}

func TestTracer_StartSpan_ChildSpan(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	parentSpan, ctx := tracer.StartSpan(ctx, "parent-operation")
	childSpan, _ := tracer.StartSpan(ctx, "child-operation")

	assert.Equal(t, parentSpan.TraceID, childSpan.TraceID)
	assert.Equal(t, parentSpan.ID, childSpan.ParentSpanID)
}

func TestTracer_FinishSpan(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	span, _ := tracer.StartSpan(ctx, "test-operation")
	time.Sleep(1 * time.Millisecond)
	tracer.FinishSpan(span)

	assert.Equal(t, "finished", span.Status)
	assert.True(t, span.Duration > 0)
	assert.False(t, span.EndTime.IsZero())
}

func TestSpan_AddTag(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	span, _ := tracer.StartSpan(ctx, "test-operation")
	span.AddTag("key1", "value1")
	span.AddTag("key2", 42)

	assert.Equal(t, "value1", span.Tags["key1"])
	assert.Equal(t, 42, span.Tags["key2"])
}

func TestSpan_AddLog(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	span, _ := tracer.StartSpan(ctx, "test-operation")
	span.AddLog("test message", map[string]interface{}{"detail": "value"})

	require.Len(t, span.Logs, 1)
	assert.Equal(t, "test message", span.Logs[0].Message)
	assert.Equal(t, "value", span.Logs[0].Fields["detail"])
	assert.False(t, span.Logs[0].Timestamp.IsZero())
}

func TestSpan_SetError(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	span, _ := tracer.StartSpan(ctx, "test-operation")
	testErr := errors.New("something went wrong")
	span.SetError(testErr)

	assert.Equal(t, "error", span.Status)
	assert.Equal(t, testErr, span.Error)
	assert.Equal(t, true, span.Tags["error"])
	assert.Equal(t, "something went wrong", span.Tags["error.message"])
}

func TestTracer_GetSpan(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	span, _ := tracer.StartSpan(ctx, "test-operation")

	retrieved := tracer.GetSpan(span.ID)
	assert.Equal(t, span, retrieved)

	missing := tracer.GetSpan("nonexistent")
	assert.Nil(t, missing)
}

func TestTracer_GetTrace(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	span1, ctx := tracer.StartSpan(ctx, "operation-1")
	span2, _ := tracer.StartSpan(ctx, "operation-2")

	spans := tracer.GetTrace(span1.TraceID)
	require.Len(t, spans, 2)
	assert.Equal(t, span1.TraceID, spans[0].TraceID)
	assert.Equal(t, span2.TraceID, spans[1].TraceID)

	missing := tracer.GetTrace("nonexistent")
	assert.Empty(t, missing)
}

func TestTracer_GetAllTraces(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	_, _ = tracer.StartSpan(ctx, "trace-1-op1")
	_, _ = tracer.StartSpan(context.Background(), "trace-2-op1")

	allTraces := tracer.GetAllTraces()
	assert.Len(t, allTraces, 2)
}

func TestTracer_ExportTrace(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	ctx := context.Background()

	span, _ := tracer.StartSpan(ctx, "test-operation")
	span.AddTag("key", "value")
	tracer.FinishSpan(span)

	exported := tracer.ExportTrace(span.TraceID)
	assert.Equal(t, span.TraceID, exported["trace_id"])
	assert.Equal(t, "test-service", exported["service"])

	spansData, ok := exported["spans"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, spansData, 1)
	assert.Equal(t, span.ID, spansData[0]["id"])
	assert.Equal(t, "finished", spansData[0]["status"])
}

func TestTracer_Eviction(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	tracer.maxSpans = 5
	tracer.maxTraces = 3

	for i := 0; i < 10; i++ {
		_, _ = tracer.StartSpan(context.Background(), "op")
	}

	assert.LessOrEqual(t, len(tracer.spans), 5)
	assert.LessOrEqual(t, len(tracer.traces), 3)
}

func TestNewTracingMiddleware(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	middleware := NewTracingMiddleware(tracer, zap.NewNop())

	assert.NotNil(t, middleware)
	assert.Equal(t, tracer, middleware.tracer)
}

func TestTracingMiddleware_TraceRequest_Success(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	middleware := NewTracingMiddleware(tracer, zap.NewNop())

	err := middleware.TraceRequest(context.Background(), "test-op", func(ctx context.Context) error {
		return nil
	})

	assert.NoError(t, err)
}

func TestTracingMiddleware_TraceRequest_Error(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	middleware := NewTracingMiddleware(tracer, zap.NewNop())

	testErr := errors.New("request failed")
	err := middleware.TraceRequest(context.Background(), "test-op", func(ctx context.Context) error {
		return testErr
	})

	assert.Equal(t, testErr, err)
}

func TestTracingMiddleware_TraceSpan_WithTags(t *testing.T) {
	tracer := NewTracer("test-service", zap.NewNop())
	middleware := NewTracingMiddleware(tracer, zap.NewNop())

	tags := map[string]interface{}{
		"http.method": "GET",
		"http.path":   "/api/v1/streaming/123",
	}

	err := middleware.TraceSpan(context.Background(), "http-request", tags, func(ctx context.Context) error {
		return nil
	})

	assert.NoError(t, err)
}

func TestNewTraceCollector(t *testing.T) {
	collector := NewTraceCollector(zap.NewNop())
	assert.NotNil(t, collector)
	assert.NotNil(t, collector.traces)
}

func TestTraceCollector_RecordTrace(t *testing.T) {
	collector := NewTraceCollector(zap.NewNop())

	metrics := &TraceMetrics{
		TraceID:       "trace-1",
		ServiceName:   "test-service",
		OperationName: "test-op",
		StartTime:     time.Now().Add(-1 * time.Second),
		EndTime:       time.Now(),
		Duration:      1 * time.Second,
		SpanCount:     3,
		ErrorCount:    0,
		Status:        "completed",
	}

	collector.RecordTrace("trace-1", metrics)

	retrieved := collector.GetTrace("trace-1")
	assert.Equal(t, metrics, retrieved)
}

func TestTraceCollector_GetTrace_NonExistent(t *testing.T) {
	collector := NewTraceCollector(zap.NewNop())
	retrieved := collector.GetTrace("nonexistent")
	assert.Nil(t, retrieved)
}

func TestTraceCollector_GetAllTraces(t *testing.T) {
	collector := NewTraceCollector(zap.NewNop())

	for i := 0; i < 3; i++ {
		collector.RecordTrace(fmt.Sprintf("trace-%d", i), &TraceMetrics{
			TraceID: fmt.Sprintf("trace-%d", i),
		})
	}

	all := collector.GetAllTraces()
	assert.Len(t, all, 3)
}

func TestTraceCollector_GetTracesByService(t *testing.T) {
	collector := NewTraceCollector(zap.NewNop())

	collector.RecordTrace("trace-1", &TraceMetrics{TraceID: "trace-1", ServiceName: "auth"})
	collector.RecordTrace("trace-2", &TraceMetrics{TraceID: "trace-2", ServiceName: "streaming"})
	collector.RecordTrace("trace-3", &TraceMetrics{TraceID: "trace-3", ServiceName: "auth"})

	authTraces := collector.GetTracesByService("auth")
	assert.Len(t, authTraces, 2)

	streamingTraces := collector.GetTracesByService("streaming")
	assert.Len(t, streamingTraces, 1)

	missingTraces := collector.GetTracesByService("missing")
	assert.Len(t, missingTraces, 0)
}

func TestTraceCollector_GetTraceStatistics(t *testing.T) {
	collector := NewTraceCollector(zap.NewNop())

	collector.RecordTrace("trace-1", &TraceMetrics{
		Duration:   100 * time.Millisecond,
		ErrorCount: 0,
		StartTime:  time.Now().Add(-2 * time.Second),
	})
	collector.RecordTrace("trace-2", &TraceMetrics{
		Duration:   200 * time.Millisecond,
		ErrorCount: 1,
		StartTime:  time.Now().Add(-1 * time.Second),
	})

	stats := collector.GetTraceStatistics()
	assert.Equal(t, 2, stats["total_traces"])
	assert.Equal(t, 1, stats["error_count"])
	assert.Equal(t, 0.5, stats["error_rate"])
}

func TestTraceCollector_GetTraceStatistics_Empty(t *testing.T) {
	collector := NewTraceCollector(zap.NewNop())
	stats := collector.GetTraceStatistics()

	assert.Equal(t, 0, stats["total_traces"])
	assert.Equal(t, 0, stats["error_count"])
}

func TestTraceCollector_Eviction(t *testing.T) {
	collector := NewTraceCollector(zap.NewNop())
	collector.maxTraces = 3

	for i := 0; i < 5; i++ {
		collector.RecordTrace(fmt.Sprintf("trace-%d", i), &TraceMetrics{
			TraceID:   fmt.Sprintf("trace-%d", i),
			StartTime: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	assert.LessOrEqual(t, len(collector.traces), 3)
}
