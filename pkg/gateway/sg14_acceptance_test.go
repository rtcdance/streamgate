package gateway

// sg14_acceptance_test.go — TRANSITION_PLAN SG-14 part 2 acceptance:
//   - TestAcceptance_ProtectedOpsEndpoints   (API boundary: /metrics token, /circuit-breakers behind JWT, /docs debug-gated)
//   - TestAcceptance_MediaRateLimitAndNAT    (per-viewer media budget, template-keyed buckets, 429+Retry-After, cross-wallet isolation)
//   - TestAcceptance_StreamingDrain503Health200 (drain: stream group 503, /health 200)

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const sg14JWTSecret = "sg14-ops-secret-at-least-32-chars-long"

// newOpsTestRouter reproduces the production boundary for infrastructure and
// ops routes: infrastructure (health/metrics/ready, debug-gated docs) first,
// then the global JWT chain, then the protected ops routes.
func newOpsTestRouter(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()
	router := gin.New()
	registerInfrastructureRoutes(router, log, nil, nil, nil, cfg)
	router.Use(middleware.JWTAuthMiddleware(middleware.JWTAuthConfig{
		Secret:    sg14JWTSecret,
		SkipPaths: []string{"/health", "/ready", "/metrics"},
	}, log))
	registerProtectedOpsRoutes(router, nil)
	return router
}

// TestAcceptance_ProtectedOpsEndpoints pins the SG-14 API boundary:
//   - /metrics answers 200 when STREAMGATE_METRICS_TOKEN is unset (current
//     internal-network behaviour), 401 without the bearer, 200 with it.
//   - /circuit-breakers is behind the global JWT (401 anonymous, 200 with a
//     valid access token).
//   - /docs is debug-only: served when cfg.Debug, absent otherwise.
func TestAcceptance_ProtectedOpsEndpoints(t *testing.T) {
	const metricsToken = "sg14-metrics-token"

	// --- /metrics: token unset → open (Prometheus scrapes internally) ---
	t.Setenv("STREAMGATE_METRICS_TOKEN", "")
	r := newOpsTestRouter(t, config.DefaultConfig())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code, "unset metrics token must keep /metrics open")

	// --- /metrics: token set → 401 without, 200 with ---
	t.Setenv("STREAMGATE_METRICS_TOKEN", metricsToken)
	r = newOpsTestRouter(t, config.DefaultConfig())

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "missing metrics token must 401")

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+metricsToken)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "correct metrics token must 200")

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	req.Header.Set("Authorization", "Bearer wrong-token")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "wrong metrics token must 401")
	t.Setenv("STREAMGATE_METRICS_TOKEN", "")

	// --- /circuit-breakers: behind JWT ---
	r = newOpsTestRouter(t, config.DefaultConfig())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/circuit-breakers", http.NoBody))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "circuit-breakers must require auth")

	token := generateTestJWT(t, "0xops", sg14JWTSecret, "ops-jti")
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/circuit-breakers", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "circuit-breakers must answer 200 for a valid JWT")

	// --- /docs: debug-only ---
	cfg := config.DefaultConfig()
	cfg.Debug = true
	r = newOpsTestRouter(t, cfg)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/docs", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code, "/docs must be served in debug mode")

	r = newOpsTestRouter(t, config.DefaultConfig())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/docs", http.NoBody))
	// Non-debug: the route is not registered. The global JWT middleware still
	// intercepts the unmatched path first (401) — either answer proves the
	// page is not served.
	assert.Contains(t, []int{http.StatusNotFound, http.StatusUnauthorized}, w.Code,
		"/docs must not be served outside debug mode")

	// --- /docs/openapi.yaml: behind JWT ---
	r = newOpsTestRouter(t, config.DefaultConfig())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", http.NoBody))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "openapi.yaml must require auth")

	// --- /health stays unauthenticated by design ---
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code, "/health must stay public")

	// --- pprof: only in debug, gated by the metrics token ---
	cfg = config.DefaultConfig()
	cfg.Debug = true
	r = newOpsTestRouter(t, cfg)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/debug/pprof/", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code, "debug-mode pprof must be reachable")

	t.Setenv("STREAMGATE_METRICS_TOKEN", metricsToken)
	r = newOpsTestRouter(t, cfg)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/debug/pprof/", http.NoBody))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "pprof must be gated by the metrics token")
	t.Setenv("STREAMGATE_METRICS_TOKEN", "")

	r = newOpsTestRouter(t, config.DefaultConfig())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/debug/pprof/", http.NoBody))
	assert.Contains(t, []int{http.StatusNotFound, http.StatusUnauthorized}, w.Code,
		"pprof must not be served outside debug mode")
}

// mediaViewerRouter reproduces the production stream-group chain with a small
// media budget so the test can hit it: [PlaybackTokenAuth(skip), JWT, MediaRL,
// NFTGate(bypassed via playback_auth), handlers]. Wallet comes from the JWT;
// the segment handler needs a playback token, which the test mints with the
// real AuthService. Segment objects come from a fake storage returning bytes.
func mediaViewerRouter(t *testing.T, budget int, store service.SegmentStorage) (*gin.Engine, *service.AuthService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()

	authSvc := service.NewAuthService(sg14JWTSecret, nil)
	cache := NewStreamingCache()
	lim := newStreamLimiter(100)
	if store == nil {
		store = fakeSegmentStorage{}
	}
	r := gin.New()

	streamGroup := r.Group("/")
	streamGroup.Use(middleware.JWTAuthMiddleware(middleware.JWTAuthConfig{Secret: sg14JWTSecret}, log))

	mediaRL := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RequestsPerMinute: budget,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}, nil)
	t.Cleanup(mediaRL.Stop)
	streamGroup.Use(middleware.MediaRateLimitMiddleware(mediaRL))

	streamGroup.Use(func(c *gin.Context) {
		// Stand-in for PlaybackTokenAuth + the gate's capability bypass: mark
		// the request as carrying a validated capability for the JWT wallet.
		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		c.Set("playback_auth", wallet)
		c.Set("wallet_address", wallet)
		c.Set("nft_verified", true)
		c.Next()
	})

	RegisterStreamingSegmentRoute(streamGroup, log, authSvc, store, lim, cache, nil, "streamgate")
	return r, authSvc
}

// TestAcceptance_MediaRateLimitAndNAT pins the SG-14 media-scoped limiter:
//   - one viewer's different segment NAMES share one bucket (normalized route
//     template, not raw path) — no unbounded per-segment buckets,
//   - the request past the budget gets 429 + Retry-After,
//   - a different wallet has its own independent budget.
func TestAcceptance_MediaRateLimitAndNAT(t *testing.T) {
	const budget = 5
	r, authSvc := mediaViewerRouter(t, budget, nil)

	walletA := "0xaaaa000000000000000000000000000000000001"
	walletB := "0xbbbb000000000000000000000000000000000002"
	tokenA := generateTestJWT(t, walletA, sg14JWTSecret, "a-1")
	tokenB := generateTestJWT(t, walletB, sg14JWTSecret, "b-1")

	// Real playback capabilities so an allowed request reaches the handler
	// instead of stopping at token validation — the assertions are about the
	// limiter, not about auth.
	playbackA, err := authSvc.GeneratePlaybackToken(context.Background(), walletA, "content-1", "", "", 31337, time.Hour, "")
	require.NoError(t, err)
	playbackB, err := authSvc.GeneratePlaybackToken(context.Background(), walletB, "content-1", "", "", 31337, time.Hour, "")
	require.NoError(t, err)

	segmentReq := func(token, playback, name string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/streaming/content-1/segment/"+name+"?playback_token="+playback, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)
		return w
	}

	// Wallet A: budget distinct segment NAMES — all share one bucket because
	// the key is the route TEMPLATE (c.FullPath), not the raw path. A per-
	// segment bucketing bug would spread these across N buckets and never
	// trip the shared budget within budget requests.
	for i := 0; i < budget; i++ {
		w := segmentReq(tokenA, playbackA, "0000"+strconv.Itoa(i)+".ts")
		assert.NotEqual(t, http.StatusTooManyRequests, w.Code,
			"segment %d must not be rate limited within the budget", i)
	}

	// The 6th request on the same template — a NEW segment name, proving the
	// bucket is the template and not the segment — must be 429 + Retry-After.
	w := segmentReq(tokenA, playbackA, "99999.ts")
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "the 6th request in the window must be 429")
	assert.NotEmpty(t, w.Header().Get("Retry-After"), "429 must carry Retry-After")
	ra, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, ra, 1, "Retry-After must be at least 1 second")

	// A different wallet starts with a fresh budget: its first request is not
	// limited even though wallet A is over budget.
	w = segmentReq(tokenB, playbackB, "00001.ts")
	assert.NotEqual(t, http.StatusTooManyRequests, w.Code,
		"a different wallet must have an independent budget")
}

// TestAcceptance_StreamingDrain503Health200 pins the drain contract: while the
// server is draining, new requests to the stream group are refused with 503
// while /health keeps answering 200 (an orchestrator's liveness probe must not
// kill the instance mid-drain and cut the grace period short).
func TestAcceptance_StreamingDrain503Health200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	router := gin.New()

	// Production middleware order: setupMiddleware puts DrainMiddleware in the
	// global chain BEFORE registerInfrastructureRoutes/registerRoutes.
	router.Use(core.DrainMiddleware())
	registerInfrastructureRoutes(router, log, nil, nil, nil, cfg)

	streamGroup := router.Group("/")
	streamGroup.Use(func(c *gin.Context) {
		// Production's per-group drain check (routes.go, streamGroup.Use).
		if core.IsDraining() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "server shutting down"})
			return
		}
		c.Next()
	})
	streamGroup.GET("/api/v1/streaming/:id/manifest.m3u8", func(c *gin.Context) {
		c.String(http.StatusOK, "#EXTM3U")
	})

	// Not draining: everything answers.
	core.ResetDraining()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/streaming/c1/manifest.m3u8", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code, "stream route must answer 200 while serving")

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code, "/health must answer 200 while serving")

	// Draining: stream group 503, /health stays 200.
	core.SetDraining()
	defer core.ResetDraining()

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/streaming/c1/manifest.m3u8", http.NoBody))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"a new stream-group request during drain must be 503")

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/streaming/c1/segment/00001.ts", http.NoBody))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"a new segment request during drain must be 503")

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code,
		"/health must stay 200 during drain so liveness probes do not kill the instance")
}
