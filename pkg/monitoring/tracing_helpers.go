package monitoring

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const otelTracerName = "streamgate"

func OTelTracer() trace.Tracer {
	return otel.Tracer(otelTracerName)
}

func StartOTelSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return OTelTracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

func SetSpanAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

func RecordSpanError(span trace.Span, err error) {
	if err != nil && span.IsRecording() {
		span.RecordError(err)
	}
}
