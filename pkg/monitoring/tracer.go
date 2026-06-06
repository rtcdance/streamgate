package monitoring

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Tracer is a thin wrapper over the package's OTel tracer. The wrapper
// exists so callers and tests can use a small surface (Tags map, AddLog)
// without depending on the full trace.Span API.
type Tracer struct {
	name   string
	logger *zap.Logger
}

// Span is the handle returned by Tracer.StartSpan. The Tags map is
// flushed to OTel attributes on FinishSpan. AddLog records an OTel event
// with the given name and attributes.
type Span struct {
	Context context.Context
	Tags    map[string]string
	otel    trace.Span
}

// NewTracer constructs a Tracer. logger may be nil.
func NewTracer(name string, logger *zap.Logger) *Tracer {
	return &Tracer{name: name, logger: logger}
}

// StartSpan begins a new span. Note the return order is (*Span, ctx),
// opposite of the raw OTel API.
func (t *Tracer) StartSpan(ctx context.Context, name string) (*Span, context.Context) {
	otelCtx, otelSp := OTelTracer().Start(ctx, name)
	return &Span{
		Context: otelCtx,
		Tags:    make(map[string]string, 4),
		otel:    otelSp,
	}, otelCtx
}

// FinishSpan flushes Tags to OTel attributes and ends the underlying span.
func (t *Tracer) FinishSpan(span *Span) {
	if span == nil {
		return
	}
	if len(span.Tags) > 0 {
		attrs := make([]attribute.KeyValue, 0, len(span.Tags))
		for k, v := range span.Tags {
			attrs = append(attrs, attribute.String(k, v))
		}
		span.otel.SetAttributes(attrs...)
	}
	span.otel.End()
}

// AddLog records an event on the span. fields may be nil; values are
// coerced to strings for attribute compatibility.
func (s *Span) AddLog(name string, fields map[string]interface{}) {
	if s == nil {
		return
	}
	if len(fields) == 0 {
		s.otel.AddEvent(name)
		return
	}
	attrs := make([]attribute.KeyValue, 0, len(fields))
	for k, v := range fields {
		if str, ok := v.(string); ok {
			attrs = append(attrs, attribute.String(k, str))
			continue
		}
		attrs = append(attrs, attribute.String(k, fmt.Sprintf("%v", v)))
	}
	s.otel.AddEvent(name, trace.WithAttributes(attrs...))
}
