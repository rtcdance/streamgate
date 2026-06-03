package gateway

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
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

func registerRoutes(router *gin.Engine, cfg *config.Config, log *zap.Logger, svc *serviceInit, res *AppResources) {
	registerInfrastructureRoutes(router, log, svc.DB, svc.SegmentStorage, res.MiddlewareSvc, cfg)

	/* Global JWT middleware for all /api/v1/ routes.
	   Public endpoints are excluded via SkipPaths so we don't need
	   router.Group("/") which has path-matching issues. */
	jwtConfig := middleware.JWTAuthConfig{
		Secret:    cfg.Auth.JWTSecret,
		Blacklist: svc.AuthService,
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
	// Segment route must be registered before JWT middleware — HLS.js sends
	// segment requests without an Authorization header, using playback_token
	// query param for auth instead.
	RegisterStreamingSegmentRoute(router, log, svc.AuthService, svc.SegmentStorage, streamLim, streamCache, cfg.Storage.Bucket)

	router.Use(middleware.JWTAuthMiddleware(jwtConfig, log))

	authRL := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RequestsPerMinute: 10,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}, nil)
	res.AuthRateLimiter = authRL
	RegisterAuthRoutes(router, log, cfg, svc.AuthService, authRL)
	RegisterWeb3Routes(router, log, svc.Web3Service)

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)
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
		if resp.Status == health.StatusUnhealthy {
			onlyStorageDown := true
			for name, check := range resp.Checks {
				if name != "storage" && check.Status != health.StatusHealthy {
					onlyStorageDown = false
					break
				}
			}
			if onlyStorageDown {
				resp.Status = health.StatusDegraded
			}
		}
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

func registerProtectedRoutes(router *gin.Engine, cfg *config.Config, log *zap.Logger, svc *serviceInit, streamLim *streamLimiter, streamCache *StreamingCache) {
	RegisterAuthProtectedRoutes(router, log, svc.AuthService)

	cbSvc := middleware.NewService(log)
	nftGroup := router.Group("/")
	nftGroup.Use(cbSvc.CircuitBreakerMiddleware("nft-verify", middleware.CircuitBreakerConfig{
		FailureThreshold: 5, SuccessThreshold: 3, Timeout: 30 * time.Second,
	}))
	RegisterNFTRoutes(nftGroup, log, svc.NFTVerifier, svc.NFTCacheBackend, cfg.Web3.ChainID, 60*time.Second)

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
	}
	nftGateConfig.Enabled.Store(cfg.Features.NFTGating)
	streamingGroup := router.Group("/")
	streamingGroup.Use(middleware.NFTGateMiddleware(&nftGateConfig, log))
	streamingGroup.Use(func(c *gin.Context) {
		if core.IsDraining() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "server shutting down"})
			return
		}
		c.Next()
	})
	RegisterStreamingRoutes(streamingGroup, log, svc.AuthService, svc.StreamingSvc, svc.SegmentStorage, streamLim, streamCache, cfg.Storage.Bucket)

	RegisterContentRoutes(router, log, svc.ContentService)
	RegisterTranscodingRoutes(router, log, svc.TranscodingSvc, cfg.Debug)

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
