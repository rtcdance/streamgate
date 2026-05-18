// Package load provides HTTP load testing for StreamGate using vegeta.
//
// Usage:
//
//	go test ./test/load/ -v -run TestLoad -count=1
//
// Prerequisites:
//   - StreamGate running on the target URL (default: http://localhost:8080)
//   - Set STREAMGATE_LOAD_TARGET env var to override target URL
//
// This test is NOT part of CI. Run manually for performance benchmarking.
package load

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/lib"
)

func getTargetURL() string {
	if url := os.Getenv("STREAMGATE_LOAD_TARGET"); url != "" {
		return strings.TrimRight(url, "/")
	}
	return "http://localhost:8080"
}

// LoadTestResult holds summarized load test results
type LoadTestResult struct {
	Endpoint     string        `json:"endpoint"`
	Rate         float64       `json:"rate_rps"`
	Duration     string        `json:"duration"`
	SuccessRatio float64       `json:"success_ratio"`
	LatencyP50   time.Duration `json:"latency_p50"`
	LatencyP95   time.Duration `json:"latency_p95"`
	LatencyP99   time.Duration `json:"latency_p99"`
	Throughput   float64       `json:"throughput_bytes_sec"`
	Errors       int           `json:"errors"`
}

func runLoadTest(t *testing.T, name string, target vegeta.Target, rate vegeta.Rate, duration time.Duration) LoadTestResult {
	t.Helper()

	attacker := vegeta.NewAttacker()
	metrics := &vegeta.Metrics{}

	for res := range attacker.Attack(vegeta.NewStaticTargeter(target), rate, duration, name) {
		metrics.Add(res)
	}
	metrics.Close()

	result := LoadTestResult{
		Endpoint:     fmt.Sprintf("%s %s", target.Method, target.URL),
		Rate:         float64(rate.Freq),
		Duration:     duration.String(),
		SuccessRatio: metrics.Success,
		LatencyP50:   metrics.Latencies.P50,
		LatencyP95:   metrics.Latencies.P95,
		LatencyP99:   metrics.Latencies.P99,
		Throughput:   0, // not available in this vegeta version
		Errors:       len(metrics.Errors),
	}

	// Log results
	t.Logf("--- %s ---", name)
	t.Logf("  Rate: %d req/s for %s", rate.Freq, duration)
	t.Logf("  Success: %.2f%%", result.SuccessRatio*100)
	t.Logf("  Latency P50: %s", result.LatencyP50)
	t.Logf("  Latency P95: %s", result.LatencyP95)
	t.Logf("  Latency P99: %s", result.LatencyP99)
	t.Logf("  Throughput: %.0f bytes/s", result.Throughput)
	t.Logf("  Errors: %d", result.Errors)

	// Assert minimum quality thresholds
	if result.SuccessRatio < 0.99 {
		t.Errorf("%s: success ratio %.2f%% below 99%% threshold", name, result.SuccessRatio*100)
	}
	if result.LatencyP99 > 500*time.Millisecond {
		t.Errorf("%s: P99 latency %s exceeds 500ms threshold", name, result.LatencyP99)
	}

	return result
}

func TestLoad_HealthEndpoint(t *testing.T) {
	t.Skip("requires external service")
	baseURL := getTargetURL()

	results := make([]LoadTestResult, 0, 3)
	rates := []int{50, 100, 200}

	for _, rps := range rates {
		name := fmt.Sprintf("health-%drps", rps)
		target := vegeta.Target{
			Method: "GET",
			URL:    baseURL + "/health",
		}
		rate := vegeta.Rate{Freq: rps, Per: time.Second}
		result := runLoadTest(t, name, target, rate, 10*time.Second)
		results = append(results, result)
	}

	// Output JSON summary
	data, _ := json.MarshalIndent(results, "", "  ")
	t.Logf("\nSummary JSON:\n%s", string(data))
}

func TestLoad_ReadyEndpoint(t *testing.T) {
	t.Skip("requires external service")
	baseURL := getTargetURL()

	target := vegeta.Target{
		Method: "GET",
		URL:    baseURL + "/ready",
	}
	rate := vegeta.Rate{Freq: 100, Per: time.Second}
	runLoadTest(t, "ready-100rps", target, rate, 10*time.Second)
}

func TestLoad_MetricsEndpoint(t *testing.T) {
	t.Skip("requires external service")
	baseURL := getTargetURL()

	target := vegeta.Target{
		Method: "GET",
		URL:    baseURL + "/metrics",
	}
	rate := vegeta.Rate{Freq: 50, Per: time.Second}
	runLoadTest(t, "metrics-50rps", target, rate, 10*time.Second)
}

func TestLoad_AuthChallenge(t *testing.T) {
	t.Skip("requires external service")
	baseURL := getTargetURL()

	target := vegeta.Target{
		Method: "POST",
		URL:    baseURL + "/api/v1/auth/challenge",
		Header: map[string][]string{"Content-Type": {"application/json"}},
		Body:   []byte(`{"wallet_address":"0x1234567890123456789012345678901234567890"}`),
	}
	rate := vegeta.Rate{Freq: 50, Per: time.Second}
	runLoadTest(t, "auth-challenge-50rps", target, rate, 10*time.Second)
}

func TestLoad_NFTVerify(t *testing.T) {
	t.Skip("requires external service")
	baseURL := getTargetURL()

	target := vegeta.Target{
		Method: "POST",
		URL:    baseURL + "/api/v1/nft/verify",
		Header: map[string][]string{
			"Content-Type":  {"application/json"},
			"Authorization": {"Bearer invalid-token-for-load-test"},
		},
		Body: []byte(`{"chain_id":11155111,"contract_address":"0x0000000000000000000000000000000000000000","token_id":"1","owner_address":"0x0000000000000000000000000000000000000000"}`),
	}
	rate := vegeta.Rate{Freq: 50, Per: time.Second}
	runLoadTest(t, "nft-verify-50rps", target, rate, 10*time.Second)
}

func TestLoad_DocsEndpoint(t *testing.T) {
	t.Skip("requires external service")
	baseURL := getTargetURL()

	target := vegeta.Target{
		Method: "GET",
		URL:    baseURL + "/docs",
	}
	rate := vegeta.Rate{Freq: 100, Per: time.Second}
	runLoadTest(t, "docs-100rps", target, rate, 10*time.Second)
}
