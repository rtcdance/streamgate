package tracing

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// SpanKind represents the kind of span
type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

func (s SpanKind) String() string {
	switch s {
	case SpanKindInternal:
		return "internal"
	case SpanKindServer:
		return "server"
	case SpanKindClient:
		return "client"
	case SpanKindProducer:
		return "producer"
	case SpanKindConsumer:
		return "consumer"
	default:
		return "unknown"
	}
}

// TraceID represents a trace identifier
type TraceID string

// SpanID represents a span identifier
type SpanID string

// SpanContext holds the trace context
type SpanContext struct {
	TraceID    TraceID
	SpanID     SpanID
	TraceFlags byte
}

// Span represents a tracing span
type Span struct {
	name       string
	context    SpanContext
	parent     *Span
	startTime  time.Time
	endTime    time.Time
	kind       SpanKind
	attributes map[string]interface{}
	events     []SpanEvent
	status     SpanStatus
	logger     *zap.Logger
}

// SpanEvent represents an event in a span
type SpanEvent struct {
	Name       string
	Timestamp  time.Time
	Attributes map[string]interface{}
}

// SpanStatus represents the status of a span
type SpanStatus struct {
	Code    int
	Message string
}

// SpanStatus codes
const (
	StatusCodeUnset = 0
	StatusCodeOK    = 1
	StatusCodeError = 2
)

// NewSpan creates a new span
func NewSpan(name string, parent *Span, kind SpanKind, logger *zap.Logger) *Span {
	now := time.Now()

	span := &Span{
		name:       name,
		startTime:  now,
		kind:       kind,
		attributes: make(map[string]interface{}),
		events:     make([]SpanEvent, 0),
		status:     SpanStatus{Code: StatusCodeUnset},
		logger:     logger,
	}

	if parent != nil {
		span.parent = parent
		span.context = SpanContext{
			TraceID:    parent.context.TraceID,
			SpanID:     generateSpanID(),
			TraceFlags: parent.context.TraceFlags,
		}
	} else {
		span.context = SpanContext{
			TraceID:    generateTraceID(),
			SpanID:     generateSpanID(),
			TraceFlags: 1,
		}
	}

	return span
}

// Name returns the span name
func (s *Span) Name() string {
	return s.name
}

// Context returns the span context
func (s *Span) Context() SpanContext {
	return s.context
}

// StartTime returns the span start time
func (s *Span) StartTime() time.Time {
	return s.startTime
}

// EndTime returns the span end time
func (s *Span) EndTime() time.Time {
	return s.endTime
}

// Duration returns the span duration
func (s *Span) Duration() time.Duration {
	if s.endTime.IsZero() {
		return time.Since(s.startTime)
	}
	return s.endTime.Sub(s.startTime)
}

// Kind returns the span kind
func (s *Span) Kind() SpanKind {
	return s.kind
}

// Attributes returns the span attributes
func (s *Span) Attributes() map[string]interface{} {
	return s.attributes
}

// Events returns the span events
func (s *Span) Events() []SpanEvent {
	return s.events
}

// Status returns the span status
func (s *Span) Status() SpanStatus {
	return s.status
}

// SetAttribute sets an attribute on the span
func (s *Span) SetAttribute(key string, value interface{}) {
	s.attributes[key] = value
}

// SetAttributes sets multiple attributes on the span
func (s *Span) SetAttributes(attrs map[string]interface{}) {
	for k, v := range attrs {
		s.attributes[k] = v
	}
}

// AddEvent adds an event to the span
func (s *Span) AddEvent(name string, attributes map[string]interface{}) {
	event := SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attributes,
	}
	s.events = append(s.events, event)
}

// SetStatus sets the span status
func (s *Span) SetStatus(code int, message string) {
	s.status = SpanStatus{
		Code:    code,
		Message: message,
	}
}

// RecordError records an error on the span
func (s *Span) RecordError(err error) {
	if err == nil {
		return
	}

	s.SetStatus(StatusCodeError, err.Error())
	s.AddEvent("error", map[string]interface{}{
		"error.message": err.Error(),
		"error.type":    fmt.Sprintf("%T", err),
	})
}

// End ends the span
func (s *Span) End() {
	if !s.endTime.IsZero() {
		return
	}

	s.endTime = time.Now()

	if s.logger != nil {
		s.logger.Debug("Span ended",
			zap.String("name", s.name),
			zap.String("trace_id", string(s.context.TraceID)),
			zap.String("span_id", string(s.context.SpanID)),
			zap.Duration("duration", s.Duration()),
			zap.Int("status_code", s.status.Code),
			zap.String("status_message", s.status.Message))
	}
}

// IsRecording returns whether the span is recording
func (s *Span) IsRecording() bool {
	return s.endTime.IsZero()
}

// Tracer manages span creation
type Tracer struct {
	name   string
	logger *zap.Logger
}

// NewTracer creates a new tracer
func NewTracer(name string, logger *zap.Logger) *Tracer {
	return &Tracer{
		name:   name,
		logger: logger,
	}
}

// StartSpan starts a new span
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span) {
	options := &SpanOptions{
		kind: SpanKindInternal,
	}

	for _, opt := range opts {
		opt(options)
	}

	var parent *Span
	if span := SpanFromContext(ctx); span != nil {
		parent = span
	}

	span := NewSpan(name, parent, options.kind, t.logger)

	if options.attributes != nil {
		span.SetAttributes(options.attributes)
	}

	if t.logger != nil {
		t.logger.Debug("Span started",
			zap.String("tracer", t.name),
			zap.String("name", name),
			zap.String("trace_id", string(span.context.TraceID)),
			zap.String("span_id", string(span.context.SpanID)),
			zap.String("kind", span.kind.String()))
	}

	return ContextWithSpan(ctx, span), span
}

// SpanOptions holds span options
type SpanOptions struct {
	kind       SpanKind
	attributes map[string]interface{}
}

// SpanOption is a function that configures a span
type SpanOption func(*SpanOptions)

// WithKind sets the span kind
func WithKind(kind SpanKind) SpanOption {
	return func(opts *SpanOptions) {
		opts.kind = kind
	}
}

// WithAttributes sets the span attributes
func WithAttributes(attrs map[string]interface{}) SpanOption {
	return func(opts *SpanOptions) {
		opts.attributes = attrs
	}
}

// TraceContextKey is the context key for storing the span
type TraceContextKey struct{}

// ContextWithSpan returns a new context with the span
func ContextWithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, TraceContextKey{}, span)
}

// SpanFromContext returns the span from the context
func SpanFromContext(ctx context.Context) *Span {
	if span, ok := ctx.Value(TraceContextKey{}).(*Span); ok {
		return span
	}
	return nil
}

// TraceIDFromContext returns the trace ID from the context
func TraceIDFromContext(ctx context.Context) TraceID {
	if span := SpanFromContext(ctx); span != nil {
		return span.context.TraceID
	}
	return ""
}

// SpanIDFromContext returns the span ID from the context
func SpanIDFromContext(ctx context.Context) SpanID {
	if span := SpanFromContext(ctx); span != nil {
		return span.context.SpanID
	}
	return ""
}

// generateTraceID generates a trace ID
func generateTraceID() TraceID {
	return TraceID(fmt.Sprintf("%016x%016x", time.Now().UnixNano(), time.Now().UnixNano()))
}

// generateSpanID generates a span ID
func generateSpanID() SpanID {
	return SpanID(fmt.Sprintf("%016x", time.Now().UnixNano()))
}

// TraceProvider provides tracing functionality
type TraceProvider struct {
	tracers map[string]*Tracer
	logger  *zap.Logger
}

// NewTraceProvider creates a new trace provider
func NewTraceProvider(logger *zap.Logger) *TraceProvider {
	return &TraceProvider{
		tracers: make(map[string]*Tracer),
		logger:  logger,
	}
}

// Tracer returns a tracer with the given name
func (tp *TraceProvider) Tracer(name string) *Tracer {
	if tracer, exists := tp.tracers[name]; exists {
		return tracer
	}

	tracer := NewTracer(name, tp.logger)
	tp.tracers[name] = tracer
	return tracer
}

// TracerFromContext returns a tracer from the context
func TracerFromContext(ctx context.Context, name string, logger *zap.Logger) *Tracer {
	return NewTracer(name, logger)
}

// TracePropagation handles trace context propagation
type TracePropagation struct {
	logger *zap.Logger
}

// NewTracePropagation creates a new trace propagation handler
func NewTracePropagation(logger *zap.Logger) *TracePropagation {
	return &TracePropagation{
		logger: logger,
	}
}

// Inject injects trace context into a carrier
func (tp *TracePropagation) Inject(ctx context.Context, carrier map[string]string) {
	if span := SpanFromContext(ctx); span != nil {
		carrier["traceparent"] = fmt.Sprintf("00-%s-%s-%02x",
			span.context.TraceID,
			span.context.SpanID,
			span.context.TraceFlags)
	}
}

// Extract extracts trace context from a carrier
func (tp *TracePropagation) Extract(carrier map[string]string) SpanContext {
	traceparent, ok := carrier["traceparent"]
	if !ok {
		return SpanContext{}
	}

	var traceID, spanID string
	var traceFlags byte

	fmt.Sscanf(traceparent, "00-%s-%s-%02x", &traceID, &spanID, &traceFlags)

	return SpanContext{
		TraceID:    TraceID(traceID),
		SpanID:     SpanID(spanID),
		TraceFlags: traceFlags,
	}
}

// StartSpanFromContext starts a span from an extracted context
func (tp *TracePropagation) StartSpanFromContext(ctx context.Context, name string, carrier map[string]string, kind SpanKind, logger *zap.Logger) (context.Context, *Span) {
	extracted := tp.Extract(carrier)

	if extracted.TraceID == "" {
		return TracerFromContext(ctx, "default", logger).StartSpan(ctx, name, WithKind(kind))
	}

	span := NewSpan(name, nil, kind, logger)
	span.context = extracted

	if logger != nil {
		logger.Debug("Span started from extracted context",
			zap.String("name", name),
			zap.String("trace_id", string(extracted.TraceID)),
			zap.String("span_id", string(extracted.SpanID)),
			zap.String("kind", kind.String()))
	}

	return ContextWithSpan(ctx, span), span
}

// SpanProcessor processes spans
type SpanProcessor interface {
	OnStart(span *Span)
	OnEnd(span *Span)
	Shutdown(ctx context.Context) error
	ForceFlush(ctx context.Context) error
}

// ConsoleSpanProcessor prints spans to console
type ConsoleSpanProcessor struct {
	logger *zap.Logger
}

// NewConsoleSpanProcessor creates a new console span processor
func NewConsoleSpanProcessor(logger *zap.Logger) *ConsoleSpanProcessor {
	return &ConsoleSpanProcessor{
		logger: logger,
	}
}

// OnStart is called when a span starts
func (csp *ConsoleSpanProcessor) OnStart(span *Span) {
	if csp.logger != nil {
		csp.logger.Debug("Span started",
			zap.String("name", span.name),
			zap.String("trace_id", string(span.context.TraceID)),
			zap.String("span_id", string(span.context.SpanID)),
			zap.String("kind", span.kind.String()))
	}
}

// OnEnd is called when a span ends
func (csp *ConsoleSpanProcessor) OnEnd(span *Span) {
	if csp.logger != nil {
		csp.logger.Info("Span ended",
			zap.String("name", span.name),
			zap.String("trace_id", string(span.context.TraceID)),
			zap.String("span_id", string(span.context.SpanID)),
			zap.Duration("duration", span.Duration()),
			zap.Int("status_code", span.status.Code),
			zap.String("status_message", span.status.Message))
	}
}

// Shutdown shuts down the processor
func (csp *ConsoleSpanProcessor) Shutdown(ctx context.Context) error {
	return nil
}

// ForceFlush forces a flush
func (csp *ConsoleSpanProcessor) ForceFlush(ctx context.Context) error {
	return nil
}

// BatchSpanProcessor batches spans for export
type BatchSpanProcessor struct {
	spans     []*Span
	batchSize int
	logger    *zap.Logger
	exporter  SpanExporter
}

// SpanExporter exports spans
type SpanExporter interface {
	ExportSpans(ctx context.Context, spans []*Span) error
	Shutdown(ctx context.Context) error
}

// NewBatchSpanProcessor creates a new batch span processor
func NewBatchSpanProcessor(exporter SpanExporter, batchSize int, logger *zap.Logger) *BatchSpanProcessor {
	return &BatchSpanProcessor{
		spans:     make([]*Span, 0, batchSize),
		batchSize: batchSize,
		logger:    logger,
		exporter:  exporter,
	}
}

// OnStart is called when a span starts
func (bsp *BatchSpanProcessor) OnStart(span *Span) {
}

// OnEnd is called when a span ends
func (bsp *BatchSpanProcessor) OnEnd(span *Span) {
	bsp.spans = append(bsp.spans, span)

	if len(bsp.spans) >= bsp.batchSize {
		bsp.export(context.Background())
	}
}

// export exports the batch of spans
func (bsp *BatchSpanProcessor) export(ctx context.Context) {
	if len(bsp.spans) == 0 {
		return
	}

	if err := bsp.exporter.ExportSpans(ctx, bsp.spans); err != nil && bsp.logger != nil {
		bsp.logger.Error("Failed to export spans", zap.Error(err))
	}

	bsp.spans = make([]*Span, 0, bsp.batchSize)
}

// Shutdown shuts down the processor
func (bsp *BatchSpanProcessor) Shutdown(ctx context.Context) error {
	bsp.export(ctx)
	return bsp.exporter.Shutdown(ctx)
}

// ForceFlush forces a flush
func (bsp *BatchSpanProcessor) ForceFlush(ctx context.Context) error {
	bsp.export(ctx)
	return nil
}

// NoopSpanExporter is a no-op exporter
type NoopSpanExporter struct{}

// NewNoopSpanExporter creates a new no-op exporter
func NewNoopSpanExporter() *NoopSpanExporter {
	return &NoopSpanExporter{}
}

// ExportSpans exports spans (no-op)
func (nse *NoopSpanExporter) ExportSpans(ctx context.Context, spans []*Span) error {
	return nil
}

// Shutdown shuts down the exporter
func (nse *NoopSpanExporter) Shutdown(ctx context.Context) error {
	return nil
}

// TracerConfig holds tracer configuration
type TracerConfig struct {
	Sampler   float64
	BatchSize int
	Processor SpanProcessor
}

// DefaultTracerConfig returns default tracer configuration
func DefaultTracerConfig(logger *zap.Logger) TracerConfig {
	return TracerConfig{
		Sampler:   1.0,
		BatchSize: 100,
		Processor: NewConsoleSpanProcessor(logger),
	}
}

// InitTracing initializes tracing
func InitTracing(config TracerConfig, logger *zap.Logger) *TraceProvider {
	provider := NewTraceProvider(logger)

	if config.Sampler < 0 || config.Sampler > 1 {
		config.Sampler = 1.0
	}

	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}

	if config.Processor == nil {
		config.Processor = NewConsoleSpanProcessor(logger)
	}

	logger.Info("Tracing initialized",
		zap.Float64("sampler", config.Sampler),
		zap.Int("batch_size", config.BatchSize))

	return provider
}
