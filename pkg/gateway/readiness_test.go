package gateway

// readiness_test.go — SG-14 part 1 acceptance: TestAcceptance_RuntimeWiringAndProfiles
//
// Pins the readiness contract:
//   - /ready answers 503 when a REQUIRED dependency for the current profile is
//     down or unwired, and 200 when everything the profile needs is healthy;
//   - /health stays process-only and is unaffected by dependency state;
//   - a single bad RPC endpoint with a working fallback does NOT fail
//     readiness; all endpoints bad does.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeBucket stands in for the object-storage readiness probe.
type fakeBucket struct {
	err error
}

func (f *fakeBucket) HeadBucket(_ context.Context, _ string) error { return f.err }

// fakeQueue stands in for the durable-queue readiness probe.
type fakeQueue struct {
	err error
}

func (f *fakeQueue) Depth() (int, error) { return 0, f.err }

// readyRouter serves /ready and /health from a readiness checker the test
// composes, mirroring the production wiring (guard in the global chain, static
// /health handler).
func readyRouter(t *testing.T, checker *health.HealthChecker) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(readinessGuard(checker))
	registerInfrastructureRoutes(router, zap.NewNop(), nil, nil, nil, config.DefaultConfig())
	return router
}

func getReady(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return w
}

// TestAcceptance_RuntimeWiringAndProfiles pins the /ready-per-profile contract.
func TestAcceptance_RuntimeWiringAndProfiles(t *testing.T) {
	prod := func() *config.Config {
		c := config.DefaultConfig()
		c.Environment = "production"
		return c
	}
	demo := func() *config.Config {
		c := config.DefaultConfig()
		c.Debug = true // derives the demo profile
		return c
	}

	t.Run("production: unwired required dependency is 503, /health stays 200", func(t *testing.T) {
		// No db/redis/queue/bucket wired at all: a production process must not
		// have started like this, so readiness reports every one as down.
		checker := buildReadinessChecker(prod(), zap.NewNop(), readinessInputs{})
		router := readyRouter(t, checker)

		w := getReady(t, router, "/ready")
		assert.Equal(t, http.StatusServiceUnavailable, w.Code,
			"/ready must be 503 when required production dependencies are unavailable")

		w = getReady(t, router, "/health")
		assert.Equal(t, http.StatusOK, w.Code,
			"/health must stay process-only and unaffected by dependency state")
	})

	t.Run("production: all wired dependencies healthy is 200", func(t *testing.T) {
		checker := buildReadinessChecker(prod(), zap.NewNop(), readinessInputs{
			db:     func(context.Context) error { return nil },
			redis:  func(context.Context) error { return nil },
			queue:  &fakeQueue{},
			bucket: &fakeBucket{},
		})
		router := readyRouter(t, checker)

		w := getReady(t, router, "/ready")
		assert.Equal(t, http.StatusOK, w.Code, "/ready must be 200 when every required dependency is healthy")
	})

	t.Run("production: redis down flips only /ready", func(t *testing.T) {
		checker := buildReadinessChecker(prod(), zap.NewNop(), readinessInputs{
			db:     func(context.Context) error { return nil },
			redis:  func(context.Context) error { return errors.New("redis: connection refused") },
			queue:  &fakeQueue{},
			bucket: &fakeBucket{},
		})
		router := readyRouter(t, checker)

		w := getReady(t, router, "/ready")
		assert.Equal(t, http.StatusServiceUnavailable, w.Code, "a down redis must fail /ready in production")
		assert.Contains(t, w.Body.String(), "redis")

		w = getReady(t, router, "/health")
		assert.Equal(t, http.StatusOK, w.Code, "/health stays 200 — it is process-only by design")
	})

	t.Run("demo: missing dependencies do not fail readiness", func(t *testing.T) {
		// The demo/unit profiles do not require the durable dependencies, so an
		// unwired one is simply not checked — readiness reflects the process.
		checker := buildReadinessChecker(demo(), zap.NewNop(), readinessInputs{})
		router := readyRouter(t, checker)

		w := getReady(t, router, "/ready")
		assert.Equal(t, http.StatusOK, w.Code, "demo profile must not 503 on dependencies it does not require")
	})

	t.Run("queue and storage down are 503 in production", func(t *testing.T) {
		checker := buildReadinessChecker(prod(), zap.NewNop(), readinessInputs{
			db:     func(context.Context) error { return nil },
			redis:  func(context.Context) error { return nil },
			queue:  &fakeQueue{err: errors.New("nats: no responders")},
			bucket: &fakeBucket{err: errors.New("minio: connection refused")},
		})
		router := readyRouter(t, checker)

		w := getReady(t, router, "/ready")
		assert.Equal(t, http.StatusServiceUnavailable, w.Code, "a down durable queue or storage must fail /ready in production")
	})
}

// TestReadiness_RPCCheckFallback pins the RPC probe: a single bad endpoint
// with a working fallback stays ready; every endpoint bad is not.
func TestReadiness_RPCCheckFallback(t *testing.T) {
	const wantChain = int64(31337)

	t.Run("one bad endpoint, one good ⇒ ready", func(t *testing.T) {
		fetch := func(_ context.Context, endpoint string) (int64, error) {
			if endpoint == "http://bad:8545" {
				return 0, errors.New("dial tcp: connection refused")
			}
			return wantChain, nil
		}
		check := health.RPCCheck([]string{"http://bad:8545", "http://good:8545"}, wantChain, fetch)
		assert.NoError(t, check(context.Background()),
			"a working fallback endpoint must keep readiness green")
	})

	t.Run("all endpoints bad ⇒ not ready", func(t *testing.T) {
		fetch := func(context.Context, string) (int64, error) {
			return 0, errors.New("dial tcp: connection refused")
		}
		check := health.RPCCheck([]string{"http://bad:8545", "http://bad2:8545"}, wantChain, fetch)
		assert.Error(t, check(context.Background()),
			"every endpoint bad must fail readiness")
	})

	t.Run("endpoint answers with the wrong chain ⇒ not ready", func(t *testing.T) {
		fetch := func(context.Context, string) (int64, error) { return 1, nil }
		check := health.RPCCheck([]string{"http://wrongchain:8545"}, wantChain, fetch)
		require.Error(t, check(context.Background()),
			"an endpoint on a different chain must not satisfy the readiness probe")
	})
}
