package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/docs"
	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/health"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// AnvilChainID is the local development chain. Server-side signing is only ever
// exposed on it.
const AnvilChainID int64 = 31337

// DemoSignerAllowed reports whether the development-only server-side signer
// routes may be registered. ALL of the following must hold:
//
//   - the process runs in development mode (cfg.Debug),
//   - the configured chain is the local Anvil chain,
//   - the operator explicitly opted in (features.demo_tx_signer).
//
// Missing any one means the routes are not registered at all, so a production
// deployment cannot be probed for them and cannot enable them by accident.
func DemoSignerAllowed(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.Debug && cfg.Web3.ChainID == AnvilChainID && cfg.Features.DemoTxSigner
}

func registerRoutes(router *gin.Engine, cfg *config.Config, log *zap.Logger, svc *serviceInit, res *AppResources) {
	registerInfrastructureRoutes(router, log, svc.DB, svc.SegmentStorage, res.MiddlewareSvc, cfg)

	/* Global JWT middleware for all /api/v1/ routes.
	   Public endpoints are excluded via SkipPaths so we don't need
	   router.Group("/") which has path-matching issues. */
	jwtConfig := middleware.JWTAuthConfig{
		Secret:    cfg.Auth.JWTSecret,
		Blacklist: svc.AuthService,
		// Every access token this service mints carries these; requiring them
		// here means a token minted for another purpose or service is rejected
		// even though its signature verifies.
		Issuer:   service.AccessTokenIssuer,
		Audience: service.AccessTokenAudience,
		// Sessions are checked positively: a credential is valid only while its
		// session is live and its epoch is current, so a logout invalidates every
		// credential of that session at once.
		SessionValidator: svc.AuthService,
		SkipPaths: []string{
			APIPrefix + "/auth/challenge",
			APIPrefix + "/auth/login",
			APIPrefix + "/auth/register",
			APIPrefix + "/auth/refresh",
			APIPrefix + "/web3/rpc-status",
			APIPrefix + "/web3/supported-chains",
			"/health", "/ready", "/metrics", "/docs",
		},
	}
	streamLim := newStreamLimiter(cfg.Streaming.MaxConcurrentStreams)
	streamCache := res.StreamingCache
	if streamCache == nil {
		streamCache = NewStreamingCache()
	}
	// Player routes resolve ONLY the published artifact generation. The
	// resolver is nil without a database, which keeps generation 0 (legacy
	// flat layout) — the same behaviour every caller had before.
	genResolver := newPublishedGenerationResolver(svc.DB)
	res.GenerationResolver = genResolver
	// PlaybackTokenAuth must run BEFORE the global JWT middleware: HLS.js cannot
	// send an Authorization header on media requests, so segment/sub-manifest
	// URLs carry playback_token in the query string. The token is validated
	// cryptographically and its wallet injected here — the mere presence of the
	// query param never grants access.
	//
	// The group is created BEFORE router.Use(JWT) below. gin's RouterGroup
	// snapshots the parent handler chain at creation and Use only appends, so
	// JWT and the NFT gate are appended to this group explicitly afterwards to
	// get the chain [PlaybackTokenAuth, JWT, NFTGate].
	playbackAuth := middleware.PlaybackTokenAuth(middleware.PlaybackTokenAuthConfig{
		Validate: func(ctx context.Context, token, contentID, fingerprint string) (*middleware.PlaybackIdentity, error) {
			claims, err := svc.AuthService.ParsePlaybackToken(ctx, token, contentID, fingerprint)
			if err != nil {
				return nil, err
			}
			return &middleware.PlaybackIdentity{
				Wallet:    claims.WalletAddress,
				SessionID: claims.SessionID,
				AuthEpoch: claims.AuthEpoch,
			}, nil
		},
	}, log)
	streamGroup := router.Group("/")
	streamGroup.Use(playbackAuth)

	router.Use(middleware.JWTAuthMiddleware(jwtConfig, log))
	streamGroup.Use(middleware.JWTAuthMiddleware(jwtConfig, log))

	authRL := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RequestsPerMinute: 10,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}, nil)
	res.AuthRateLimiter = authRL
	RegisterAuthRoutes(router, log, cfg, svc.AuthService, authRL)
	RegisterWeb3Routes(router, log, svc.Web3Service)

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache, streamGroup, genResolver)
}

func buildCircuitBreakerConfig(cfg *config.Config) middleware.CircuitBreakerConfig {
	cbConfig := middleware.DefaultCircuitBreakerConfig()
	if !cfg.CircuitBreaker.Enabled {
		return cbConfig
	}
	if d, err := time.ParseDuration(cfg.CircuitBreaker.Timeout); err == nil {
		cbConfig.Timeout = d
	}
	if cfg.CircuitBreaker.FailureThreshold > 0 {
		cbConfig.FailureThreshold = cfg.CircuitBreaker.FailureThreshold
	}
	if cfg.CircuitBreaker.SuccessThreshold > 0 {
		cbConfig.SuccessThreshold = cfg.CircuitBreaker.SuccessThreshold
	}
	if cfg.CircuitBreaker.MaxRequests > 0 {
		cbConfig.MaxRequests = cfg.CircuitBreaker.MaxRequests
	}
	if d, err := time.ParseDuration(cfg.CircuitBreaker.WindowTime); err == nil {
		cbConfig.WindowTime = d
	}
	return cbConfig
}

func registerInfrastructureRoutes(router *gin.Engine, log *zap.Logger, db storage.DB, objStorage service.SegmentStorage, mwSvc *middleware.Service, cfg *config.Config) {
	healthChecker := health.NewHealthChecker(log)

	cbConfig := buildCircuitBreakerConfig(cfg)

	if mwSvc != nil {
		_ = mwSvc.DependencyCircuitBreaker("db", cbConfig)
		_ = mwSvc.DependencyCircuitBreaker("redis", cbConfig)
		_ = mwSvc.DependencyCircuitBreaker("s3", cbConfig)
		_ = mwSvc.DependencyCircuitBreaker("rpc", cbConfig)
	}

	if db != nil {
		healthChecker.RegisterCheck("database", func(ctx context.Context) error {
			if mwSvc != nil && cfg.CircuitBreaker.Enabled {
				return mwSvc.ExecuteWithCB(ctx, "db", cbConfig, func() error {
					return db.Ping(ctx)
				})
			}
			return db.Ping(ctx)
		})
	}
	if objStorage != nil {
		healthChecker.RegisterCheck("storage", func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := objStorage.Exists(checkCtx, cfg.Storage.Bucket, "__health_check__"); err != nil {
				return fmt.Errorf("storage check failed: %w", err)
			}
			return nil
		})
	}

	router.GET("/health", func(c *gin.Context) {
		monitoring.HealthCheckTotal.WithLabelValues("health").Inc()
		resp := healthChecker.CheckAll(c.Request.Context())
		status := http.StatusOK
		if resp.Status == health.StatusUnhealthy {
			status = http.StatusServiceUnavailable
		} else if resp.Status == health.StatusDegraded {
			status = http.StatusMultiStatus
		}
		respond(c, status, resp)
	})
	router.GET("/metrics", func(c *gin.Context) {
		promhttp.Handler().ServeHTTP(c.Writer, c.Request)
	})
	router.GET("/ready", func(c *gin.Context) {
		resp := healthChecker.Readiness(c.Request.Context())
		status := http.StatusOK
		if !resp.Ready {
			status = http.StatusServiceUnavailable
		}
		respond(c, status, resp)
	})
	router.GET("/circuit-breakers", func(c *gin.Context) {
		if mwSvc == nil {
			respondOK(c, gin.H{"circuit_breakers": nil})
			return
		}
		stats := mwSvc.AllCircuitBreakerStats()
		breakers := make([]gin.H, 0, len(stats))
		for name, s := range stats {
			breakers = append(breakers, gin.H{
				"name":              name,
				"state":             s.State.String(),
				"failure_count":     s.FailureCount,
				"success_count":     s.SuccessCount,
				"request_count":     s.RequestCount,
				"failure_rate":      s.FailureRate,
				"last_failure":      s.LastFailureTime,
				"last_state_change": s.LastStateChange,
			})
		}
		respondOK(c, gin.H{"circuit_breakers": breakers})
	})
	router.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(docs.SwaggerUIHTML))
	})
	router.StaticFile("/docs/openapi.yaml", filepath.Join(".", "docs", "api", "openapi.yaml"))
	router.Static("/demo", "./h5-demo")
}

func registerProtectedRoutes(router *gin.Engine, cfg *config.Config, log *zap.Logger, svc *serviceInit, streamLim *streamLimiter, streamCache *StreamingCache, streamGroup *gin.RouterGroup, genResolver PublishedGenerationResolver) {
	RegisterAuthProtectedRoutes(router, log, svc.AuthService)

	cbSvc := middleware.NewService(log)
	nftGroup := router.Group("/")
	nftGroup.Use(cbSvc.CircuitBreakerMiddleware("nft-verify", middleware.CircuitBreakerConfig{
		FailureThreshold: 5, SuccessThreshold: 3, Timeout: 30 * time.Second,
	}))
	RegisterNFTRoutes(nftGroup, log, svc.NFTVerifier, svc.NFTCacheBackend, cfg.Web3.ChainID, 60*time.Second)
	RegisterNFTDevMintRoute(router, svc.DemoNFTMinter, log, DemoSignerAllowed(cfg))

	RegisterUploadRoutes(router, log, svc.UploadService)

	nftGateConfig := middleware.NFTGateConfig{
		Verifier:       svc.NFTVerifier,
		BlockProver:    svc.Web3Service,
		Cache:          svc.NFTCacheBackend,
		RuleResolver:   svc.GatingRuleResolver,
		DefaultChainID: cfg.Web3.ChainID,
		CacheTTL:       60 * time.Second,
		MarketplaceURL: "https://opensea.io/assets/ethereum/{contract}/{token_id}",
		BlockTag:       parseBlockTag(cfg.Web3.BlockTag),
		GatingMode:     cfg.Features.GatingMode, // empty → "closed" (fail-closed default set by the middleware)
	}
	// Streaming routes (manifest + segment) live on streamGroup, whose chain is
	// [PlaybackTokenAuth, JWT, NFTGate] — the group was created before the
	// global JWT Use in registerRoutes.
	nftGate := middleware.NFTGateMiddleware(&nftGateConfig, log)
	// A reorg on the indexed chain must drop cached authorization evidence, so a
	// later request cannot be authorized from a chain state that no longer
	// exists. The middleware also invalidates on a block-hash mismatch; this is
	// the event-driven half.
	if svc.Web3Service != nil {
		gateCfg := &nftGateConfig
		svc.Web3Service.SetReorgHandler(func(chainID int64, _ []string) {
			middleware.InvalidateNFTEvidence(gateCfg, chainID)
		})
	}
	// NFTGateMiddleware defaults to enabled; apply the operator flag AFTER
	// construction (the middleware reads config.Enabled per request), so
	// features.nft_gating=false really disables the gate — and since the gate
	// is fail-closed, disabled means streaming routes answer 503 GATING_DISABLED.
	if !cfg.Features.NFTGating {
		nftGateConfig.Enabled.Store(false)
	}
	streamGroup.Use(nftGate)
	streamGroup.Use(func(c *gin.Context) {
		if core.IsDraining() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "server shutting down"})
			return
		}
		c.Next()
	})
	RegisterStreamingSegmentRoute(streamGroup, log, svc.AuthService, svc.SegmentStorage, streamLim, streamCache, genResolver, cfg.Storage.Bucket)
	RegisterStreamingRoutes(streamGroup, log, svc.AuthService, svc.StreamingSvc, svc.SegmentStorage, streamLim, streamCache, genResolver, cfg.Storage.Bucket)

	// A typed-nil *RegistryHub must not slip into the interface: the handler's
	// nil check decides between 503 (registry disabled) and real handling.
	var registryAttester ContentAttester
	if svc.RegistryHub != nil {
		registryAttester = svc.RegistryHub
	}
	RegisterContentRoutes(router, log, svc.ContentService, registryAttester)
	// The transcode API fetches the input itself, so its accepted sources are an
	// allowlist: the configured object store, plus public internet URLs only if
	// the deployment explicitly opted in. "Debug mode allows localhost" is NOT
	// used here — that would open every private address to a caller-supplied URL
	// just to make a local object store reachable.
	RegisterTranscodingRoutes(router, log, svc.TranscodingSvc, TranscodeInputPolicy{
		AllowedHosts:  transcodeInputAllowlist(cfg),
		AllowExternal: cfg.Features.AllowExternalTranscodeInput,
	})

	// Use a root sub-group for RouteGroup-specific registrations
	rootG := router.Group("/")
	if svc.GatingRuleSvc != nil {
		RegisterGatingRuleRoutes(rootG, svc.GatingRuleSvc)
	}
	if svc.PlaybackStatsSvc != nil {
		RegisterPlaybackStatsRoutes(rootG, svc.PlaybackStatsSvc)
	}
	if svc.CategorySvc != nil {
		RegisterCategoryRoutes(rootG, svc.CategorySvc)
	}
}

func parseBlockTag(s string) web3.BlockTag {
	switch s {
	case "finalized":
		return web3.BlockTagFinalized
	case "latest":
		return web3.BlockTagLatest
	default:
		return web3.BlockTagSafe
	}
}

// transcodeInputAllowlist lists the hosts a transcode input URL may name without
// being treated as external input. It is the configured object store, in both
// "host:port" and bare "host" form, because a presigned URL may carry the port
// and an internal one may not.
//
// This is deliberately narrow. The previous behaviour ("debug mode allows
// localhost") allowed EVERY private address whenever the flag was on, which is a
// far larger hole than making one object store reachable.
func transcodeInputAllowlist(cfg *config.Config) []string {
	endpoint := strings.TrimSpace(cfg.Storage.Endpoint)
	if endpoint == "" {
		return nil
	}
	// Strip any scheme: Endpoint is documented as host[:port] but a deployment
	// may write it with one.
	if idx := strings.Index(endpoint, "://"); idx >= 0 {
		endpoint = endpoint[idx+3:]
	}
	endpoint = strings.TrimSuffix(endpoint, "/")
	if endpoint == "" {
		return nil
	}
	hosts := []string{endpoint}
	if host, _, err := net.SplitHostPort(endpoint); err == nil && host != "" {
		hosts = append(hosts, host)
	}
	return hosts
}
