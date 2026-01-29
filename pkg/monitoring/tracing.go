package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Span represents a distributed trace span
type Span struct {
	ID            string
	TraceID       string
	ParentSpanID  string
	OperationName string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	Tags          map[string]interface{}
	Logs          []SpanLog
	Status        string
	Error         error
}

// SpanLog represents a log entry in a span
type SpanLog struct {
	Timestamp time.Time
	Message   string
	Fields    map[string]interface{}
}

// Tracer manages distributed tracing
type Tracer struct {
	logger      *zap.Logger
	mu          sync.RWMutex
	spans       map[string]*Span
	traces      map[string][]*Span
	serviceName string
}

// NewTracer creates a new tracer
func NewTracer(serviceName string, logger *zap.Logger) *Tracer {
	return &Tracer{
		logger:      logger,
		spans:       make(map[string]*Span),
		traces:      make(map[string][]*Span),
		serviceName: serviceName,
	}
}

// StartSpan starts a new span
func (t *Tracer) StartSpan(ctx context.Context, operationName string) (*Span, context.Context) {
	traceID := t.getTraceID(ctx)
	if traceID == "" {
		traceID = t.generateTraceID()
	}

	parentSpanID := t.getSpanID(ctx)

	span := &Span{
		ID:            t.generateSpanID(),
		TraceID:       traceID,
		ParentSpanID:  parentSpanID,
		OperationName: operationName,
		StartTime:     time.Now(),
		Tags:          make(map[string]interface{}),
		Logs:          make([]SpanLog, 0),
		Status:        "running",
	}

	t.mu.Lock()
	t.spans[span.ID] = span
	t.traces[traceID] = append(t.traces[traceID], span)
	t.mu.Unlock()

	t.logger.Debug("Span started", "span_id", span.ID, "trace_id", traceID, "operation", operationName)

	// Create new context with span information
	ctx = context.WithValue(ctx, "trace_id", traceID)
	ctx = context.WithValue(ctx, "span_id", span.ID)

	return span, ctx
}

// FinishSpan finishes a span
func (t *Tracer) FinishSpan(span *Span) {
	span.EndTime = time.Now()
	span.Duration = span.EndTime.Sub(span.StartTime)
	span.Status = "finished"

	t.logger.Debug("Span finished", "span_id", span.ID, "trace_id", span.TraceID, "duration_ms", span.Duration.Milliseconds())
}

// AddTag adds a tag to a span
func (span *Span) AddTag(key string, value interface{}) {
	span.Tags[key] = value
}

// AddLog adds a log entry to a span
func (span *Span) AddLog(message string, fields map[string]interface{}) {
	log := SpanLog{
		Timestamp: time.Now(),
		Message:   message,
		Fields:    fields,
	}

	span.Logs = append(span.Logs, log)
}

// SetError sets an error on a span
func (span *Span) SetError(err error) {
	span.Error = err
	span.Status = "error"
	span.AddTag("error", true)
	span.AddTag("error.message", err.Error())
}

// GetSpan gets a span by ID
func (t *Tracer) GetSpan(spanID string) *Span {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.spans[spanID]
}

// GetTrace gets a trace by ID
func (t *Tracer) GetTrace(traceID string) []*Span {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.traces[traceID]
}

// GetAllTraces gets all traces
func (t *Tracer) GetAllTraces() map[string][]*Span {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string][]*Span)
	for traceID, spans := range t.traces {
		result[traceID] = spans
	}

	return result
}

// ExportTrace exports a trace in JSON format
func (t *Tracer) ExportTrace(traceID string) map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	spans := t.traces[traceID]
	spanData := make([]map[string]interface{}, 0)

	for _, span := range spans {
		spanData = append(spanData, map[string]interface{}{
			"id":             span.ID,
			"trace_id":       span.TraceID,
			"parent_span_id": span.ParentSpanID,
			"operation":      span.OperationName,
			"start_time":     span.StartTime,
			"end_time":       span.EndTime,
			"duration_ms":    span.Duration.Milliseconds(),
			"tags":           span.Tags,
			"logs":           span.Logs,
			"status":         span.Status,
			"error":          span.Error,
		})
	}

	return map[string]interface{}{
		"trace_id": traceID,
		"spans":    spanData,
		"service":  t.serviceName,
	}
}

// generateTraceID generates a unique trace ID
func (t *Tracer) generateTraceID() string {
	return fmt.Sprintf("trace_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond())
}

// generateSpanID generates a unique span ID
func (t *Tracer) generateSpanID() string {
	return fmt.Sprintf("span_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond())
}

// getTraceID gets trace ID from context
func (t *Tracer) getTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value("trace_id").(string); ok {
		return traceID
	}
	return ""
}

// getSpanID gets span ID from context
func (t *Tracer) getSpanID(ctx context.Context) string {
	if spanID, ok := ctx.Value("span_id").(string); ok {
		return spanID
	}
	return ""
}

// TracingMiddleware provides tracing middleware
type TracingMiddleware struct {
	tracer *Tracer
	logger *zap.Logger
}

// NewTracingMiddleware creates a new tracing middleware
func NewTracingMiddleware(tracer *Tracer, logger *zap.Logger) *TracingMiddleware {
	return &TracingMiddleware{
		tracer: tracer,
		logger: logger,
	}
}

// TraceRequest traces a request
func (tm *TracingMiddleware) TraceRequest(ctx context.Context, operationName string, fn func(context.Context) error) error {
	span, ctx := tm.tracer.StartSpan(ctx, operationName)
	defer tm.tracer.FinishSpan(span)

	span.AddTag("service", tm.tracer.serviceName)
	span.AddTag("operation", operationName)

	err := fn(ctx)
	if err != nil {
		span.SetError(err)
	}

	return err
}

// TraceSpan traces a span with custom operation
func (tm *TracingMiddleware) TraceSpan(ctx context.Context, operationName string, tags map[string]interface{}, fn func(context.Context) error) error {
	span, ctx := tm.tracer.StartSpan(ctx, operationName)
	defer tm.tracer.FinishSpan(span)

	for key, value := range tags {
		span.AddTag(key, value)
	}

	err := fn(ctx)
	if err != nil {
		span.SetError(err)
	}

	return err
}

// TraceCollector collects traces for analysis
type TraceCollector struct {
	logger    *zap.Logger
	mu        sync.RWMutex
	traces    map[string]*TraceMetrics
	maxTraces int
}

// TraceMetrics represents metrics for a trace
type TraceMetrics struct {
	TraceID       string
	ServiceName   string
	OperationName string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	SpanCount     int
	ErrorCount    int
	Status        string
}

// NewTraceCollector creates a new trace collector
func NewTraceCollector(logger *zap.Logger) *TraceCollector {
	return &TraceCollector{
		logger:    logger,
		traces:    make(map[string]*TraceMetrics),
		maxTraces: 10000,
	}
}

// RecordTrace records a trace
func (tc *TraceCollector) RecordTrace(traceID string, metrics *TraceMetrics) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if len(tc.traces) >= tc.maxTraces {
		// Remove oldest trace
		var oldestKey string
		var oldestTime time.Time

		for key, trace := range tc.traces {
			if oldestTime.IsZero() || trace.StartTime.Before(oldestTime) {
				oldestKey = key
				oldestTime = trace.StartTime
			}
		}

		if oldestKey != "" {
			delete(tc.traces, oldestKey)
		}
	}

	tc.traces[traceID] = metrics
	tc.logger.Debug("Trace recorded", "trace_id", traceID, "duration_ms", metrics.Duration.Milliseconds())
}

// GetTrace gets trace metrics
func (tc *TraceCollector) GetTrace(traceID string) *TraceMetrics {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	return tc.traces[traceID]
}

// GetAllTraces gets all trace metrics
func (tc *TraceCollector) GetAllTraces() map[string]*TraceMetrics {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	result := make(map[string]*TraceMetrics)
	for traceID, metrics := range tc.traces {
		result[traceID] = metrics
	}

	return result
}

// GetTracesByService gets traces by service
func (tc *TraceCollector) GetTracesByService(serviceName string) []*TraceMetrics {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	result := make([]*TraceMetrics, 0)
	for _, metrics := range tc.traces {
		if metrics.ServiceName == serviceName {
			result = append(result, metrics)
		}
	}

	return result
}

// GetTraceStatistics gets trace statistics
func (tc *TraceCollector) GetTraceStatistics() map[string]interface{} {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	totalTraces := len(tc.traces)
	totalDuration := time.Duration(0)
	errorCount := 0
	minDuration := time.Duration(0)
	maxDuration := time.Duration(0)

	for _, metrics := range tc.traces {
		totalDuration += metrics.Duration

		if metrics.ErrorCount > 0 {
			errorCount++
		}

		if minDuration == 0 || metrics.Duration < minDuration {
			minDuration = metrics.Duration
		}

		if metrics.Duration > maxDuration {
			maxDuration = metrics.Duration
		}
	}

	avgDuration := time.Duration(0)
	if totalTraces > 0 {
		avgDuration = totalDuration / time.Duration(totalTraces)
	}

	return map[string]interface{}{
		"total_traces":   totalTraces,
		"error_count":    errorCount,
		"error_rate":     float64(errorCount) / float64(totalTraces),
		"total_duration": totalDuration.Milliseconds(),
		"avg_duration":   avgDuration.Milliseconds(),
		"min_duration":   minDuration.Milliseconds(),
		"max_duration":   maxDuration.Milliseconds(),
	}
}
