package monitoring

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"
	"google.golang.org/grpc/credentials"
)

func InitOTelTracing(ctx context.Context, serviceName, endpoint string, logger *zap.Logger) (shutdown func(ctx context.Context) error, err error) {
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimSuffix(endpoint, "/api/traces")
	endpoint = strings.TrimSuffix(endpoint, "/v1/traces")

	if endpoint == "" {
		logger.Info("OTel tracing disabled (no endpoint configured)")
		return func(_ context.Context) error { return nil }, nil
	}

	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, dialErr := net.DialTimeout("tcp", endpoint, 3*time.Second)
	if dialErr != nil {
		logger.Warn("OTel tracing disabled (endpoint unreachable)",
			zap.String("endpoint", endpoint),
			zap.Error(dialErr))
		return func(_ context.Context) error { return nil }, nil
	}
	conn.Close()
	_ = dialCtx

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if strings.Contains(endpoint, "443") {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(nil)))
	} else {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info("OTel tracing initialized",
		zap.String("service", serviceName),
		zap.String("endpoint", endpoint))

	return tp.Shutdown, nil
}
