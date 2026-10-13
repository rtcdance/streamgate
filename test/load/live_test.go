//go:build load

// Live load entry point against a RUNNING fullchain stack (make
// fullchain-deploy). It is excluded from the default `go test ./...` run via
// the `load` build tag, exactly like the repo's `anvil` / `testnet` tags.
//
// Environment (see scripts/perf-acceptance.sh which sets all of these):
//
//	PERF_BASE_URL      running stack root            (default http://localhost:18080)
//	PERF_SCENARIO      scenario label                (default warm_cache; the script drives
//	                     cold_cache / rpc_primary_down / rpc_all_down via fault injection)
//	PERF_JWT           pre-minted JWT (optional; omit to use real login)
//	PERF_WALLET        wallet address for real login (optional; derived from key)
//	PERF_PRIVATE_KEY   hex secp256key of a REAL logged-in, NFT-holding wallet
//	PERF_CHAIN_ID      chain id for the login challenge (default 31337)
//	PERF_CONTENT_ID    gated content to play         (required)
//	PERF_QUALITY       variant quality               (optional; empty = master manifest)
//	PERF_VIEWERS       virtual viewers               (default 1)
//	PERF_INTERVAL      per-viewer request pace       (default 2s)
//	PERF_WARMUP        warmup window                 (default 10s)
//	PERF_DURATION      timed sampling window         (default 30s)
//	PERF_FIXTURE_HASH  sha256 of the media fixture   (optional; recorded)
//	PERF_RESULT_JSON   output path for the JSON result (default /tmp/perf-result.json)
//	PERF_SEGMENT_SHA256  optional hex digest — when set, every segment body is
//	                    sha256-checked against it (mismatch = error, not success)
//
// Exit: TestLoad fails the run on a nonzero error count for scenarios that must
// stay healthy; fault scenarios (rpc_primary_down / rpc_all_down) record but
// do not fail on elevated errors — the safety assertion (no unauthorized 2xx
// when the gate must deny) belongs to the fault-injection scripts, not here.
package load_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rtcdance/streamgate/test/load"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(t *testing.T, key string, def int) int {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	require.NoError(t, err, "%s must be an integer", key)
	return n
}

func TestLoad_AuthorizedMedia(t *testing.T) {
	baseURL := envOr("PERF_BASE_URL", "http://localhost:18080")
	scenario := envOr("PERF_SCENARIO", "warm_cache")
	contentID := os.Getenv("PERF_CONTENT_ID")
	require.NotEmpty(t, contentID, "PERF_CONTENT_ID is required — point the harness at real gated content")

	viewers := envInt(t, "PERF_VIEWERS", 1)
	interval := 2 * time.Second
	if v := os.Getenv("PERF_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err)
		interval = d
	}
	warmup := 10 * time.Second
	if v := os.Getenv("PERF_WARMUP"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err)
		warmup = d
	}
	duration := 30 * time.Second
	if v := os.Getenv("PERF_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err)
		duration = d
	}
	chainID := int64(envInt(t, "PERF_CHAIN_ID", 31337))

	cfg := load.Config{
		BaseURL:     baseURL,
		Scenario:    scenario,
		JWTToken:    os.Getenv("PERF_JWT"),
		Wallet:      os.Getenv("PERF_WALLET"),
		PrivateKey:  os.Getenv("PERF_PRIVATE_KEY"),
		ChainID:     chainID,
		ContentID:   contentID,
		Quality:     os.Getenv("PERF_QUALITY"),
		Viewers:     viewers,
		Interval:    interval,
		Warmup:      warmup,
		Duration:    duration,
		FixtureHash: os.Getenv("PERF_FIXTURE_HASH"),
	}
	if sha := os.Getenv("PERF_SEGMENT_SHA256"); sha != "" {
		expect := load.ExpectedSHA256(sha)
		cfg.Expect = func(u string, body []byte) error {
			// The digest describes media segments, not the playlist text.
			if isManifest(u) {
				return nil
			}
			return expect(u, body)
		}
	}

	res, err := load.Run(context.Background(), cfg)
	require.NoError(t, err)

	out := envOr("PERF_RESULT_JSON", "/tmp/perf-result.json")
	require.NoError(t, res.WriteJSON(out))
	t.Logf("scenario=%s commit=%s success=%d errors=%d bytes=%d ttfb_p95=%.2fms latency_p95=%.2fms result=%s",
		res.Scenario, res.Commit, res.SuccessCount, res.ErrorCount, res.TotalBytes,
		res.TTFBMillis.P95, res.LatencyMillis.P95, out)

	// Healthy scenarios must meet the SG-07 bar: ≥99% success at 50 viewers.
	// Fault scenarios record but don't enforce the bar here.
	switch scenario {
	case "cold_cache", "warm_cache":
		total := res.SuccessCount + res.ErrorCount
		require.Greater(t, total, int64(0), "no requests were executed")
		successRate := float64(res.SuccessCount) / float64(total)
		require.GreaterOrEqual(t, successRate, 0.99,
			"healthy scenario %s must keep ≥99%% success (got %.2f%%)", scenario, successRate*100)
	}
}

func isManifest(u string) bool {
	return strings.Contains(u, "manifest.m3u8")
}
