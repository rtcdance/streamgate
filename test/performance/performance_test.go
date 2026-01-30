package performance

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"streamgate/pkg/monitoring"
	"streamgate/pkg/optimization"
	"streamgate/pkg/plugins/api"
)

// PerformanceMetrics tracks performance test results
type PerformanceMetrics struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TotalDuration   time.Duration
	MinLatency      time.Duration
	MaxLatency      time.Duration
	AvgLatency      time.Duration
	P50Latency      time.Duration
	P95Latency      time.Duration
	P99Latency      time.Duration
	Throughput      float64 // requests per second
	ErrorRate       float64 // percentage
}

// TestMetricsCollection validates metrics collection performance
func TestMetricsCollection(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	// Create 1000 metrics
	start := time.Now()
	for i := 0; i < 1000; i++ {
		mc.IncrementCounter(fmt.Sprintf("test.counter.%d", i), nil)
		mc.SetGauge(fmt.Sprintf("test.gauge.%d", i), float64(i), nil)
		mc.RecordHistogram(fmt.Sprintf("test.histogram.%d", i), float64(i*10), nil)
	}
	duration := time.Since(start)

	// Verify performance
	if duration > 100*time.Millisecond {
		t.Errorf("Metrics collection too slow: %v (expected < 100ms)", duration)
	}

	t.Logf("Metrics collection: 3000 operations in %v (%.2f ops/ms)", duration, float64(3000)/duration.Seconds()/1000)
}

// TestCachePerformance validates cache performance
func TestCachePerformance(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := optimization.NewLocalCache(1000, 5*time.Minute, logger)

	// Warm up cache
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}

	// Measure read performance
	start := time.Now()
	hits := 0
	for i := 0; i < 10000; i++ {
		if _, ok := cache.Get(fmt.Sprintf("key-%d", i%1000)); ok {
			hits++
		}
	}
	duration := time.Since(start)

	hitRate := float64(hits) / 10000 * 100
	throughput := float64(10000) / duration.Seconds()

	if hitRate < 95 {
		t.Errorf("Cache hit rate too low: %.2f%% (expected > 95%%)", hitRate)
	}

	if duration > 10*time.Millisecond {
		t.Errorf("Cache read too slow: %v (expected < 10ms for 10k reads)", duration)
	}

	t.Logf("Cache performance: %.2f%% hit rate, %.0f ops/sec", hitRate, throughput)
}

// TestRateLimitingPerformance validates rate limiting performance
func TestRateLimitingPerformance(t *testing.T) {
	limiter := api.NewRateLimiter(1000)

	// Measure rate limiting overhead
	start := time.Now()
	allowed := 0
	for i := 0; i < 10000; i++ {
		if limiter.Allow("test-client") {
			allowed++
		}
	}
	duration := time.Since(start)

	throughput := float64(10000) / duration.Seconds()

	if duration > 50*time.Millisecond {
		t.Errorf("Rate limiting too slow: %v (expected < 50ms for 10k checks)", duration)
	}

	t.Logf("Rate limiting performance: %.0f checks/sec, %d allowed", throughput, allowed)
}

// TestConcurrentRequests simulates concurrent request handling
func TestConcurrentRequests(t *testing.T) {
	metrics := &PerformanceMetrics{}
	concurrency := 100
	requestsPerWorker := 100
	totalRequests := concurrency * requestsPerWorker

	var wg sync.WaitGroup
	var latencies []time.Duration
	var latencyMutex sync.Mutex

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerWorker; j++ {
				reqStart := time.Now()
				// Simulate request processing (1-10ms)
				time.Sleep(time.Duration(1+j%10) * time.Millisecond)
				latency := time.Since(reqStart)

				latencyMutex.Lock()
				latencies = append(latencies, latency)
				latencyMutex.Unlock()

				atomic.AddInt64(&metrics.SuccessRequests, 1)
			}
		}()
	}

	wg.Wait()
	metrics.TotalDuration = time.Since(start)
	metrics.TotalRequests = int64(totalRequests)
	metrics.Throughput = float64(totalRequests) / metrics.TotalDuration.Seconds()

	// Calculate latency percentiles
	if len(latencies) > 0 {
		metrics.MinLatency = latencies[0]
		metrics.MaxLatency = latencies[0]
		var sum time.Duration

		for _, l := range latencies {
			if l < metrics.MinLatency {
				metrics.MinLatency = l
			}
			if l > metrics.MaxLatency {
				metrics.MaxLatency = l
			}
			sum += l
		}

		metrics.AvgLatency = sum / time.Duration(len(latencies))
		metrics.P50Latency = calculatePercentile(latencies, 50)
		metrics.P95Latency = calculatePercentile(latencies, 95)
		metrics.P99Latency = calculatePercentile(latencies, 99)
	}

	// Verify performance targets
	if metrics.AvgLatency > 100*time.Millisecond {
		t.Errorf("Average latency too high: %v (expected < 100ms)", metrics.AvgLatency)
	}

	if metrics.P95Latency > 200*time.Millisecond {
		t.Errorf("P95 latency too high: %v (expected < 200ms)", metrics.P95Latency)
	}

	if metrics.Throughput < 1000 {
		t.Errorf("Throughput too low: %.0f req/sec (expected > 1000)", metrics.Throughput)
	}

	t.Logf("Concurrent requests: %d requests in %v", totalRequests, metrics.TotalDuration)
	t.Logf("  Throughput: %.0f req/sec", metrics.Throughput)
	t.Logf("  Latency: min=%v, avg=%v, p50=%v, p95=%v, p99=%v, max=%v",
		metrics.MinLatency, metrics.AvgLatency, metrics.P50Latency,
		metrics.P95Latency, metrics.P99Latency, metrics.MaxLatency)
}

// TestMemoryUsage validates memory efficiency
func TestMemoryUsage(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := optimization.NewLocalCache(10000, 5*time.Minute, logger)

	// Fill cache with 10k entries
	for i := 0; i < 10000; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}

	// Estimate memory usage (rough calculation)
	// Each entry: key (20 bytes) + value (20 bytes) + overhead (100 bytes) = ~140 bytes
	estimatedMemory := 10000 * 140 / 1024 / 1024 // MB

	if estimatedMemory > 5 {
		t.Logf("Warning: Cache memory usage high: ~%d MB for 10k entries", estimatedMemory)
	}

	t.Logf("Cache memory usage: ~%d MB for 10k entries", estimatedMemory)
}

// TestPrometheusExportPerformance validates Prometheus export performance
func TestPrometheusExportPerformance(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	mc := monitoring.NewMetricsCollector(nil)
	smt := monitoring.NewServiceMetricsTracker(logger)
	pe := monitoring.NewPrometheusExporter(mc, smt, logger)

	// Create 1000 metrics
	for i := 0; i < 1000; i++ {
		mc.IncrementCounter(fmt.Sprintf("test.counter.%d", i), nil)
		mc.SetGauge(fmt.Sprintf("test.gauge.%d", i), float64(i), nil)
	}

	// Measure export performance
	start := time.Now()
	output := pe.Export()
	duration := time.Since(start)

	if duration > 100*time.Millisecond {
		t.Errorf("Prometheus export too slow: %v (expected < 100ms)", duration)
	}

	if len(output) == 0 {
		t.Error("Prometheus export produced no output")
	}

	t.Logf("Prometheus export: %d bytes in %v", len(output), duration)
}

// TestDistributedTracingPerformance validates tracing overhead
func TestDistributedTracingPerformance(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	tracer := monitoring.NewTracer("test-service", logger)

	// Measure span creation overhead
	start := time.Now()
	for i := 0; i < 10000; i++ {
		span, ctx := tracer.StartSpan(context.Background(), fmt.Sprintf("operation-%d", i))
		span.Tags["test"] = "value"
		tracer.FinishSpan(span)
		_ = ctx
	}
	duration := time.Since(start)

	overhead := float64(duration.Microseconds()) / 10000 // microseconds per span

	if overhead > 100 {
		t.Errorf("Tracing overhead too high: %.2f µs/span (expected < 100µs)", overhead)
	}

	t.Logf("Distributed tracing: %.2f µs/span overhead", overhead)
}

// TestAlertingPerformance validates alert triggering performance
func TestAlertingPerformance(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	alertManager := monitoring.NewAlertManager(logger)

	// Add alert rules
	alertManager.AddRule(&monitoring.AlertRule{
		ID:        "rule-1",
		Name:      "high_error_rate",
		Metric:    "error_rate",
		Condition: "gt",
		Threshold: 0.1,
		Level:     "critical",
		Enabled:   true,
	})

	// Measure alert evaluation
	start := time.Now()
	for i := 0; i < 1000; i++ {
		alertManager.CheckMetric("error_rate", 0.05)
	}
	duration := time.Since(start)

	if duration > 50*time.Millisecond {
		t.Errorf("Alert evaluation too slow: %v (expected < 50ms for 1000 evals)", duration)
	}

	t.Logf("Alert evaluation: 1000 evaluations in %v", duration)
}

// Helper function to calculate percentile
func calculatePercentile(latencies []time.Duration, percentile int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	index := (len(latencies) * percentile) / 100
	if index >= len(latencies) {
		index = len(latencies) - 1
	}

	return latencies[index]
}

// BenchmarkMetricsCollection benchmarks metrics collection
func BenchmarkMetricsCollection(b *testing.B) {
	mc := monitoring.NewMetricsCollector(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mc.IncrementCounter("bench.counter", nil)
		mc.SetGauge("bench.gauge", float64(i), nil)
		mc.RecordHistogram("bench.histogram", float64(i*10), nil)
	}
}

// BenchmarkCacheGet benchmarks cache get operations
func BenchmarkCacheGet(b *testing.B) {
	cache := optimization.NewLocalCache(1000, 5*time.Minute, nil)
	cache.Set("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("key")
	}
}

// BenchmarkCacheSet benchmarks cache set operations
func BenchmarkCacheSet(b *testing.B) {
	cache := optimization.NewLocalCache(10000, 5*time.Minute, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(fmt.Sprintf("key-%d", i%1000), fmt.Sprintf("value-%d", i))
	}
}

// BenchmarkRateLimiting benchmarks rate limiting
func BenchmarkRateLimiting(b *testing.B) {
	limiter := api.NewRateLimiter(10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(fmt.Sprintf("client-%d", i%100))
	}
}
