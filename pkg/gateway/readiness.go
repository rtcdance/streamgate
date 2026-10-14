package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/health"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// bucketProber reports whether the media bucket exists and is reachable.
// *storage.MinIOStorage satisfies it; injected SegmentStorage implementations
// that do not simply have no storage readiness check.
type bucketProber interface {
	HeadBucket(ctx context.Context, bucket string) error
}

// queuePinger is the durability surface the queue readiness check touches.
// *storage.NATSTranscodingQueue satisfies it via Depth (which queries the
// JetStream stream, so a dead broker fails even when the client exists).
type queuePinger interface {
	Depth() (int, error)
}

// natsQueueProbe narrows an io.Closer to the queuePinger the readiness check
// needs, or nil when no durable queue was wired. A typed-nil must not sneak
// through as a non-nil interface: calling Depth on it would panic.
func natsQueueProbe(c io.Closer) queuePinger {
	if c == nil {
		return nil
	}
	q, ok := c.(queuePinger)
	if !ok {
		return nil
	}
	if v := reflect.ValueOf(q); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil
	}
	return q
}

// rpcChainIDFetcher dials one RPC endpoint and reads its chain id. It is a
// package variable so tests can substitute a fake chain view without a live
// node.
var rpcChainIDFetcher health.ChainIDFunc = func(ctx context.Context, endpoint string) (int64, error) {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return 0, err
	}
	defer client.Close()
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return 0, err
	}
	return chainID.Int64(), nil
}

// rpcEndpoints collects every configured Ethereum RPC endpoint: the primary
// one plus each chain entry's endpoints (a chain entry may carry a fallback
// list). Duplicates and empties are dropped so the readiness probe does not
// dial the same endpoint twice.
func rpcEndpoints(cfg *config.Config) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(ep string) {
		if ep == "" || seen[ep] {
			return
		}
		seen[ep] = true
		out = append(out, ep)
	}
	add(cfg.Web3.EthereumRPC)
	for _, ch := range cfg.Web3.Chains {
		add(ch.RPC)
		for _, ep := range ch.RPCs {
			add(ep)
		}
	}
	return out
}

// rpcCheckConfigured reports whether an RPC readiness probe makes sense: a
// chain id and at least one endpoint that is not the infura placeholder. The
// placeholder is a default, not an operator choice, so probing it would turn
// every deployment that never configured a chain into a permanent 503.
func rpcCheckConfigured(cfg *config.Config) bool {
	if cfg.Web3.ChainID <= 0 {
		return false
	}
	for _, ep := range rpcEndpoints(cfg) {
		if ep != "" && !strings.Contains(ep, "YOUR_KEY") {
			return true
		}
	}
	return false
}

// requiredDown is the readiness failure for a dependency the current profile
// requires but that was not wired (unreachable at startup). Naming the
// profile in the message tells the operator the dependency was mandatory,
// not optional.
func requiredDown(name, profile string) error {
	return fmt.Errorf("%s is required by the %s profile but is not available", name, profile)
}

// readinessInputs are the wired dependencies a readiness checker probes.
// A nil dependency means it was not wired: in a strict profile that is
// itself a readiness failure (the profile requires it), elsewhere the check
// is simply absent.
type readinessInputs struct {
	db     func(ctx context.Context) error
	redis  func(ctx context.Context) error
	queue  queuePinger
	bucket bucketProber
}

// buildReadinessChecks composes the readiness checks for the current profile.
//
// /health stays process-only; /ready must reflect whether this profile's
// REQUIRED dependencies are usable:
//
//   - database, redis, the durable queue and object storage are required in a
//     strict profile (durable, production) — when they were not wired at all
//     the check fails immediately — and are probed whenever they were;
//   - a same-chain RPC is probed whenever one is configured, iterating every
//     endpoint so a single bad endpoint with a working fallback does not
//     produce a false 503 — only "all endpoints bad" fails readiness.
func buildReadinessChecks(cfg *config.Config, in readinessInputs) []health.CheckSpec {
	strict := cfg.IsStrictProfile()
	profile := cfg.EffectiveProfile()
	var checks []health.CheckSpec

	if in.db != nil {
		checks = append(checks, health.CheckSpec{
			Name:  "database",
			Check: health.PingCheck("database", in.db),
		})
	} else if strict {
		checks = append(checks, health.CheckSpec{
			Name:  "database",
			Check: func(context.Context) error { return requiredDown("database", profile) },
		})
	}

	if in.redis != nil {
		checks = append(checks, health.CheckSpec{
			Name:  "redis",
			Check: health.PingCheck("redis", in.redis),
		})
	} else if strict {
		checks = append(checks, health.CheckSpec{
			Name:  "redis",
			Check: func(context.Context) error { return requiredDown("redis", profile) },
		})
	}

	if in.queue != nil {
		checks = append(checks, health.CheckSpec{
			Name: "queue",
			Check: health.PingCheck("queue", func(_ context.Context) error {
				_, err := in.queue.Depth()
				return err
			}),
		})
	} else if strict {
		checks = append(checks, health.CheckSpec{
			Name:  "queue",
			Check: func(context.Context) error { return requiredDown("durable queue", profile) },
		})
	}

	if in.bucket != nil {
		checks = append(checks, health.CheckSpec{
			Name: "object-storage",
			// HeadBucket reports both an unreachable endpoint and a missing
			// bucket, without creating anything.
			Check: func(ctx context.Context) error {
				return in.bucket.HeadBucket(ctx, cfg.Storage.Bucket)
			},
		})
	} else if strict {
		checks = append(checks, health.CheckSpec{
			Name:  "object-storage",
			Check: func(context.Context) error { return requiredDown("object storage", profile) },
		})
	}

	// The RPC probe is registered whenever an operator configured an endpoint,
	// regardless of profile: readiness must never claim "ready" when the chain
	// view this deployment depends on is entirely unreachable. A single bad
	// endpoint with a working fallback stays ready (see RPCCheck).
	if rpcCheckConfigured(cfg) {
		checks = append(checks, health.CheckSpec{
			Name:  "rpc",
			Check: health.RPCCheck(rpcEndpoints(cfg), cfg.Web3.ChainID, rpcChainIDFetcher),
		})
	}
	return checks
}

// buildReadinessChecker builds the checker the /ready guard evaluates.
func buildReadinessChecker(cfg *config.Config, log *zap.Logger, in readinessInputs) *health.HealthChecker {
	checks := buildReadinessChecks(cfg, in)
	names := make([]string, 0, len(checks))
	for _, c := range checks {
		names = append(names, c.Name)
	}
	log.Info("readiness checks composed",
		zap.String("profile", cfg.EffectiveProfile()),
		zap.Strings("checks", names))
	return health.NewCheckerWithChecks(log, checks)
}

// readinessGuard answers GET /ready from the profile's dependency checker
// BEFORE the infrastructure handler runs: a required dependency that is down
// yields 503 with the failed checks; otherwise the request continues to the
// static handler. /health is untouched — it stays process-only.
//
// It is installed in the global chain before registerInfrastructureRoutes
// runs, so the /ready route (which is registered before the JWT middleware)
// still passes through it.
func readinessGuard(checker *health.HealthChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path != "/ready" || c.Request.Method != http.MethodGet {
			c.Next()
			return
		}
		resp := checker.Readiness(c.Request.Context())
		status := http.StatusOK
		if !resp.Ready {
			status = http.StatusServiceUnavailable
		}
		respond(c, status, resp)
		c.Abort()
	}
}
