package load

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// LoadTestResult tracks load test results
type LoadTestResult struct {
	Duration        time.Duration
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	Throughput      float64 // requests per second
	ErrorRate       float64 // percentage
	AvgLatency      time.Duration
	P95Latency      time.Duration
	P99Latency      time.Duration
	MaxLatency      time.Duration
	MemoryUsageMB   float64
	CPUUsagePercent float64
}

// LoadTestConfig configures load test parameters
type LoadTestConfig struct {
	Duration          time.Duration
	Concurrency       int
	RequestsPerSecond int
	TimeoutPerRequest time.Duration
	RampUpDuration    time.Duration
}

// TestUploadServiceLoad simulates upload service load
func TestUploadServiceLoad(t *testing.T) {
	config := LoadTestConfig{
		Duration:          10 * time.Second,
		Concurrency:       50,
		RequestsPerSecond: 500,
		TimeoutPerRequest: 5 * time.Second,
		RampUpDuration:    2 * time.Second,
	}

	result := runLoadTest(t, config, simulateUploadRequest)

	// Verify load test results
	if result.ErrorRate > 5 {
		t.Errorf("Upload service error rate too high: %.2f%% (expected < 5%%)", result.ErrorRate)
	}

	if result.P95Latency > 2*time.Second {
		t.Errorf("Upload service P95 latency too high: %v (expected < 2s)", result.P95Latency)
	}

	t.Logf("Upload service load test: %.0f req/sec, %.2f%% error rate, P95=%v",
		result.Throughput, result.ErrorRate, result.P95Latency)
}

// TestStreamingServiceLoad simulates streaming service load
func TestStreamingServiceLoad(t *testing.T) {
	config := LoadTestConfig{
		Duration:          10 * time.Second,
		Concurrency:       100,
		RequestsPerSecond: 1000,
		TimeoutPerRequest: 3 * time.Second,
		RampUpDuration:    2 * time.Second,
	}

	result := runLoadTest(t, config, simulateStreamingRequest)

	// Verify load test results
	if result.ErrorRate > 2 {
		t.Errorf("Streaming service error rate too high: %.2f%% (expected < 2%%)", result.ErrorRate)
	}

	if result.P95Latency > 500*time.Millisecond {
		t.Errorf("Streaming service P95 latency too high: %v (expected < 500ms)", result.P95Latency)
	}

	t.Logf("Streaming service load test: %.0f req/sec, %.2f%% error rate, P95=%v",
		result.Throughput, result.ErrorRate, result.P95Latency)
}

// TestMetadataServiceLoad simulates metadata service load
func TestMetadataServiceLoad(t *testing.T) {
	config := LoadTestConfig{
		Duration:          10 * time.Second,
		Concurrency:       50,
		RequestsPerSecond: 500,
		TimeoutPerRequest: 2 * time.Second,
		RampUpDuration:    2 * time.Second,
	}

	result := runLoadTest(t, config, simulateMetadataRequest)

	// Verify load test results
	if result.ErrorRate > 3 {
		t.Errorf("Metadata service error rate too high: %.2f%% (expected < 3%%)", result.ErrorRate)
	}

	if result.P95Latency > 300*time.Millisecond {
		t.Errorf("Metadata service P95 latency too high: %v (expected < 300ms)", result.P95Latency)
	}

	t.Logf("Metadata service load test: %.0f req/sec, %.2f%% error rate, P95=%v",
		result.Throughput, result.ErrorRate, result.P95Latency)
}

// TestAuthServiceLoad simulates auth service load
func TestAuthServiceLoad(t *testing.T) {
	config := LoadTestConfig{
		Duration:          10 * time.Second,
		Concurrency:       30,
		RequestsPerSecond: 300,
		TimeoutPerRequest: 3 * time.Second,
		RampUpDuration:    2 * time.Second,
	}

	result := runLoadTest(t, config, simulateAuthRequest)

	// Verify load test results
	if result.ErrorRate > 2 {
		t.Errorf("Auth service error rate too high: %.2f%% (expected < 2%%)", result.ErrorRate)
	}

	if result.P95Latency > 1*time.Second {
		t.Errorf("Auth service P95 latency too high: %v (expected < 1s)", result.P95Latency)
	}

	t.Logf("Auth service load test: %.0f req/sec, %.2f%% error rate, P95=%v",
		result.Throughput, result.ErrorRate, result.P95Latency)
}

// TestCacheServiceLoad simulates cache service load
func TestCacheServiceLoad(t *testing.T) {
	config := LoadTestConfig{
		Duration:          10 * time.Second,
		Concurrency:       100,
		RequestsPerSecond: 2000,
		TimeoutPerRequest: 1 * time.Second,
		RampUpDuration:    2 * time.Second,
	}

	result := runLoadTest(t, config, simulateCacheRequest)

	// Verify load test results
	if result.ErrorRate > 1 {
		t.Errorf("Cache service error rate too high: %.2f%% (expected < 1%%)", result.ErrorRate)
	}

	if result.P95Latency > 100*time.Millisecond {
		t.Errorf("Cache service P95 latency too high: %v (expected < 100ms)", result.P95Latency)
	}

	t.Logf("Cache service load test: %.0f req/sec, %.2f%% error rate, P95=%v",
		result.Throughput, result.ErrorRate, result.P95Latency)
}

// TestConcurrentUserSimulation simulates 1000 concurrent users
func TestConcurrentUserSimulation(t *testing.T) {
	config := LoadTestConfig{
		Duration:          30 * time.Second,
		Concurrency:       1000,
		RequestsPerSecond: 5000,
		TimeoutPerRequest: 5 * time.Second,
		RampUpDuration:    5 * time.Second,
	}

	result := runLoadTest(t, config, simulateMixedRequest)

	// Verify load test results
	if result.ErrorRate > 5 {
		t.Errorf("Mixed load error rate too high: %.2f%% (expected < 5%%)", result.ErrorRate)
	}

	if result.Throughput < 900 {
		t.Errorf("Mixed load throughput too low: %.0f req/sec (expected > 900)", result.Throughput)
	}

	t.Logf("Concurrent user simulation: %.0f req/sec, %.2f%% error rate, %d concurrent users",
		result.Throughput, result.ErrorRate, config.Concurrency)
}

// TestSpikeLoad simulates sudden traffic spike
func TestSpikeLoad(t *testing.T) {
	config := LoadTestConfig{
		Duration:          20 * time.Second,
		Concurrency:       200,
		RequestsPerSecond: 2000,
		TimeoutPerRequest: 5 * time.Second,
		RampUpDuration:    1 * time.Second,
	}

	result := runLoadTest(t, config, simulateUploadRequest)

	// Verify spike handling
	if result.ErrorRate > 10 {
		t.Errorf("Spike load error rate too high: %.2f%% (expected < 10%%)", result.ErrorRate)
	}

	t.Logf("Spike load test: %.0f req/sec, %.2f%% error rate, P95=%v",
		result.Throughput, result.ErrorRate, result.P95Latency)
}

// TestSustainedLoad simulates sustained load over time
func TestSustainedLoad(t *testing.T) {
	config := LoadTestConfig{
		Duration:          60 * time.Second,
		Concurrency:       50,
		RequestsPerSecond: 500,
		TimeoutPerRequest: 5 * time.Second,
		RampUpDuration:    5 * time.Second,
	}

	result := runLoadTest(t, config, simulateMixedRequest)

	// Verify sustained load handling
	if result.ErrorRate > 3 {
		t.Errorf("Sustained load error rate too high: %.2f%% (expected < 3%%)", result.ErrorRate)
	}

	t.Logf("Sustained load test (60s): %.0f req/sec, %.2f%% error rate",
		result.Throughput, result.ErrorRate)
}

// runLoadTest executes a load test with given configuration
func runLoadTest(t *testing.T, config LoadTestConfig, requestFunc func(context.Context) error) *LoadTestResult {
	result := &LoadTestResult{}
	var latencies []time.Duration
	var latencyMutex sync.Mutex

	ctx, cancel := context.WithTimeout(context.Background(), config.Duration+10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	ticker := time.NewTicker(time.Second / time.Duration(config.RequestsPerSecond))
	defer ticker.Stop()

	startTime := time.Now()
	rampUpEnd := startTime.Add(config.RampUpDuration)

	// Start workers
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					// Check if we're still in test duration
					if time.Since(startTime) > config.Duration {
						return
					}

					// Ramp up: gradually increase load
					if time.Now().Before(rampUpEnd) {
						if time.Now().UnixNano()%2 == 0 {
							continue
						}
					}

					reqCtx, cancel := context.WithTimeout(ctx, config.TimeoutPerRequest)
					reqStart := time.Now()

					err := requestFunc(reqCtx)
					latency := time.Since(reqStart)

					cancel()

					latencyMutex.Lock()
					latencies = append(latencies, latency)
					latencyMutex.Unlock()

					if err != nil {
						atomic.AddInt64(&result.FailedRequests, 1)
					} else {
						atomic.AddInt64(&result.SuccessRequests, 1)
					}
					atomic.AddInt64(&result.TotalRequests, 1)
				}
			}
		}()
	}

	wg.Wait()
	result.Duration = time.Since(startTime)

	// Calculate statistics
	if result.TotalRequests > 0 {
		result.Throughput = float64(result.SuccessRequests) / result.Duration.Seconds()
		result.ErrorRate = float64(result.FailedRequests) / float64(result.TotalRequests) * 100

		if len(latencies) > 0 {
			result.AvgLatency = calculateAvgLatency(latencies)
			result.P95Latency = calculatePercentileLatency(latencies, 95)
			result.P99Latency = calculatePercentileLatency(latencies, 99)
			result.MaxLatency = calculateMaxLatency(latencies)
		}
	}

	return result
}

// Simulation functions
func simulateUploadRequest(ctx context.Context) error {
	// Simulate upload processing (50-200ms)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(50+time.Now().UnixNano()%150) * time.Millisecond):
		return nil
	}
}

func simulateStreamingRequest(ctx context.Context) error {
	// Simulate streaming request (10-50ms)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(10+time.Now().UnixNano()%40) * time.Millisecond):
		return nil
	}
}

func simulateMetadataRequest(ctx context.Context) error {
	// Simulate metadata query (5-30ms)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(5+time.Now().UnixNano()%25) * time.Millisecond):
		return nil
	}
}

func simulateAuthRequest(ctx context.Context) error {
	// Simulate auth verification (50-300ms)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(50+time.Now().UnixNano()%250) * time.Millisecond):
		return nil
	}
}

func simulateCacheRequest(ctx context.Context) error {
	// Simulate cache operation (1-10ms)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(1+time.Now().UnixNano()%9) * time.Millisecond):
		return nil
	}
}

func simulateMixedRequest(ctx context.Context) error {
	// Simulate mixed request types (5-200ms)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(5+time.Now().UnixNano()%195) * time.Millisecond):
		return nil
	}
}

// Helper functions
func calculateAvgLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	return sum / time.Duration(len(latencies))
}

func calculatePercentileLatency(latencies []time.Duration, percentile int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	index := (len(latencies) * percentile) / 100
	if index >= len(latencies) {
		index = len(latencies) - 1
	}
	return latencies[index]
}

func calculateMaxLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	max := latencies[0]
	for _, l := range latencies {
		if l > max {
			max = l
		}
	}
	return max
}
