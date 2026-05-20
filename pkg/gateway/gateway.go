package gateway

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"streamgate/docs"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/health"
	"streamgate/pkg/middleware"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/service"
	"streamgate/pkg/storage"
	"streamgate/pkg/web3"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const defaultMaxBodySize int64 = 10 << 20 // 10MB global body size limit

// AppResources holds closeable resources created by SetupRouter.
// Callers should defer resources.Close() to ensure cleanup on shutdown.
type AppResources struct {
	DB              *sql.DB
	ChallengeStore  io.Closer
	ObjStorage      io.Closer
	TokenBlacklist  io.Closer
	RateLimiter     middleware.RateLimiter
	AuthRateLimiter middleware.RateLimiter
	SharedRedis     *redis.Client
	OTelShutdown    func(ctx context.Context) error
	AuthService     *service.AuthService
	Web3Service     *service.Web3Service
	NFTVerifier     middleware.NFTOwnershipChecker
	StreamingSvc    *service.StreamingService
	ContentService  *service.ContentService
	SegmentStorage  service.SegmentStorage
	UploadService   *service.UploadService
	TranscodingSvc  *service.TranscodingService
	NFTCache        *NFTAccessCache
	StreamingCache  *StreamingCache
	NATSQueue       io.Closer
	MiddlewareSvc   *middleware.Service
}

// Close releases all held resources. Errors from individual closes are
// joined but never prevent closing remaining resources.
func (r *AppResources) Close() error {
	var errs []error
	if r.RateLimiter != nil {
		r.RateLimiter.Stop()
	}
	if r.AuthRateLimiter != nil {
		r.AuthRateLimiter.Stop()
	}
	// Shared redis client closed below; individual Redis-backed components
	// use the same client so we only close it once.
	if r.TranscodingSvc != nil {
		r.TranscodingSvc.StopWorker()
	}
	if r.UploadService != nil {
		r.UploadService.Close()
	}
	if r.NFTCache != nil {
		r.NFTCache.Stop()
	}
	if r.NATSQueue != nil {
		_ = r.NATSQueue.Close()
	}
	if r.DB != nil {
		if err := r.DB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close db: %w", err))
		}
	}
	if r.ChallengeStore != nil {
		if err := r.ChallengeStore.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close challenge store: %w", err))
		}
	}
	if r.ObjStorage != nil {
		if err := r.ObjStorage.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close object storage: %w", err))
		}
	}
	if r.TokenBlacklist != nil {
		if err := r.TokenBlacklist.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close token blacklist: %w", err))
		}
	}
	if r.SharedRedis != nil {
		if err := r.SharedRedis.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close shared redis: %w", err))
		}
	}
	if r.OTelShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.OTelShutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown otel: %w", err))
		}
	}
	return errors.Join(errs...)
}

// RouterConfig holds optional overrides for dependencies created by SetupRouter.
// When a field is non-nil, SetupRouter uses the injected value instead of
// creating one from config. This enables E2E tests to inject mocks while
// production callers use the defaults (zero-value RouterConfig).
type RouterConfig struct {
	AuthService    *service.AuthService
	Web3Service    *service.Web3Service
	SegmentStorage service.SegmentStorage
	ChallengeStore storage.ChallengeStore
	NFTVerifier    middleware.NFTOwnershipChecker
	ContentService *service.ContentService
	UploadService  *service.UploadService
}

// RouterOption configures a RouterConfig.
type RouterOption func(*RouterConfig)

// WithAuthService injects a pre-built AuthService.
func WithAuthService(svc *service.AuthService) RouterOption {
	return func(c *RouterConfig) { c.AuthService = svc }
}

// WithWeb3Service injects a pre-built Web3Service.
func WithWeb3Service(svc *service.Web3Service) RouterOption {
	return func(c *RouterConfig) { c.Web3Service = svc }
}

// WithSegmentStorage injects a SegmentStorage implementation.
func WithSegmentStorage(st service.SegmentStorage) RouterOption {
	return func(c *RouterConfig) { c.SegmentStorage = st }
}

// WithChallengeStore injects a ChallengeStore implementation.
func WithChallengeStore(store storage.ChallengeStore) RouterOption {
	return func(c *RouterConfig) { c.ChallengeStore = store }
}

// WithNFTVerifier injects an NFTOwnershipChecker for NFT routes and middleware.
func WithNFTVerifier(v middleware.NFTOwnershipChecker) RouterOption {
	return func(c *RouterConfig) { c.NFTVerifier = v }
}

// WithContentService injects a ContentService for content routes.
func WithContentService(svc *service.ContentService) RouterOption {
	return func(c *RouterConfig) { c.ContentService = svc }
}

// WithUploadService injects an UploadService for upload routes.
func WithUploadService(svc *service.UploadService) RouterOption {
	return func(c *RouterConfig) { c.UploadService = svc }
}

type serviceInit struct {
	Web3Service        *service.Web3Service
	AuthService        *service.AuthService
	StreamingSvc       *service.StreamingService
	NFTVerifier        middleware.NFTOwnershipChecker
	NFTCache           *NFTAccessCache
	NFTCacheBackend    middleware.NFTAccessCache
	GatingRuleSvc      *service.GatingRuleService
	GatingRuleResolver middleware.GatingRuleResolver
	PlaybackStatsSvc   *service.PlaybackStatsService
	CategorySvc        *service.CategoryService
	DB                 storage.DB
	ContentService     *service.ContentService
	SegmentStorage     service.SegmentStorage
	TranscodingSvc     *service.TranscodingService
	UploadService      *service.UploadService
}

func SetupRouter(cfg *config.Config, log *zap.Logger, opts ...RouterOption) (*gin.Engine, *AppResources, error) {
	rc := &RouterConfig{}
	for _, opt := range opts {
		opt(rc)
	}
	resources := &AppResources{}

	sharedRedis := provideRedis(cfg, log, resources)

	web3Svc, err := provideWeb3Service(rc, cfg, log)
	if err != nil {
		return nil, nil, err
	}
	resources.Web3Service = web3Svc

	challengeTTL := parseChallengeTTL(cfg)
	challengeStore := provideChallengeStore(rc, log, challengeTTL, sharedRedis, resources)

	authService := provideAuthService(rc, cfg, log, web3Svc, challengeStore, challengeTTL, sharedRedis, resources)
	resources.AuthService = authService

	nftCache := NewNFTAccessCache()
	resources.NFTCache = nftCache

	var nftCacheBackend middleware.NFTAccessCache
	if sharedRedis != nil {
		nftCacheBackend = NewRedisNFTAccessCache(nftCache, sharedRedis)
	} else {
		nftCacheBackend = &NFTAccessCacheAdapter{Cache: nftCache}
	}

	web3Svc.SetNFTAccessCache(nftCacheBackend)

	nftVerifier := rc.NFTVerifier
	if nftVerifier == nil {
		nftVerifier = web3Svc
	}
	resources.NFTVerifier = nftVerifier

	db, _ := provideDatabase(cfg, log, resources)
	contentSvc := provideContentService(rc, db, log)
	resources.ContentService = contentSvc

	objStorage := provideObjectStorage(rc, cfg, log, resources)
	resources.SegmentStorage = objStorage

	transcodingSvc := provideTranscodingService(cfg, log, db, objStorage, resources)
	resources.TranscodingSvc = transcodingSvc

	if transcodingSvc != nil {
		streamCache := NewStreamingCache()
		resources.StreamingCache = streamCache
		transcodingSvc.RegisterPostTranscodeHook(func(ctx context.Context, contentID, profile, outputURL string) {
			streamCache.Invalidate(contentID)
		})
	}

	uploadSvc := provideUploadService(rc, cfg, log, db, objStorage, transcodingSvc)
	resources.UploadService = uploadSvc

	provideOTelTracing(cfg, log, resources)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	setupMiddleware(router, cfg, log, sharedRedis, resources)

	svc := &serviceInit{
		Web3Service:     web3Svc,
		AuthService:     authService,
		StreamingSvc:    service.NewStreamingService(db, nil, nil, "", log.Named("streaming")),
		NFTVerifier:     nftVerifier,
		NFTCache:        nftCache,
		NFTCacheBackend: nftCacheBackend,
		DB:              db,
		ContentService:  contentSvc,
		SegmentStorage:  objStorage,
		TranscodingSvc:  transcodingSvc,
		UploadService:   uploadSvc,
	}
	resources.StreamingSvc = svc.StreamingSvc

	if db != nil {
		svc.GatingRuleSvc = service.NewGatingRuleService(db, log.Named("gating-rule"))
		svc.GatingRuleResolver = NewGatingRuleResolverAdapter(svc.GatingRuleSvc)
		svc.PlaybackStatsSvc = service.NewPlaybackStatsService(db, log.Named("playback-stats"))
		svc.CategorySvc = service.NewCategoryService(db, log.Named("category"))
	}

	registerRoutes(router, cfg, log, svc, resources)

	return router, resources, nil
}

func setupMiddleware(router *gin.Engine, cfg *config.Config, log *zap.Logger, redisClient *redis.Client, res *AppResources) {
	var middlewareSvc *middleware.Service
	if redisClient != nil {
		log.Info("Using Redis-backed rate limiter")
		middlewareSvc = middleware.NewServiceWithRedis(log, redisClient)
	} else {
		log.Warn("Redis unavailable for rate limiter, falling back to in-memory")
		middlewareSvc = middleware.NewService(log)
	}

	rl, rlHandler := middlewareSvc.RateLimitMiddlewareWithConfig(middleware.RateLimitConfig{
		RequestsPerMinute: cfg.RateLimiting.RequestsPerMinute,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	})
	res.RateLimiter = rl
	res.MiddlewareSvc = middlewareSvc

	router.Use(RequestIDMiddleware())
	router.Use(middlewareSvc.RecoveryMiddleware())
	router.Use(rlHandler)
	router.Use(core.DrainMiddleware())
	router.Use(middlewareSvc.TraceIDMiddleware())
	router.Use(middlewareSvc.LoggingMiddleware())
	router.Use(middlewareSvc.SecurityHeadersMiddleware())
	router.Use(middlewareSvc.ContentTypeMiddleware())
	router.Use(middlewareSvc.RequestSizeLimitMiddleware(defaultMaxBodySize))
	router.Use(middlewareSvc.CORSMiddleware(cfg.CORS.AllowedOrigins...))
	router.Use(middlewareSvc.TracingMiddleware())
	router.Use(prometheusMiddleware())
}

func prometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		status := strconv.Itoa(c.Writer.Status())
		monitoring.HTTPRequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()
		elapsed := time.Since(startedAt).Milliseconds()
		monitoring.ServiceRequestDuration.WithLabelValues("api-gateway").Observe(float64(elapsed))
	}
}

func registerRoutes(router *gin.Engine, cfg *config.Config, log *zap.Logger, svc *serviceInit, res *AppResources) {
	registerInfrastructureRoutes(router, log, svc.DB, svc.SegmentStorage, res.MiddlewareSvc, cfg)

	authRL := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RequestsPerMinute: 10,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}, nil)
	res.AuthRateLimiter = authRL
	RegisterAuthRoutes(router, log, cfg, svc.AuthService, authRL)
	RegisterWeb3Routes(router, log, svc.Web3Service)

	streamLim := newStreamLimiter(cfg.Streaming.MaxConcurrentStreams)
	streamCache := res.StreamingCache
	if streamCache == nil {
		streamCache = NewStreamingCache()
	}
	RegisterStreamingSegmentRoute(router, log, svc.AuthService, svc.SegmentStorage, streamLim, streamCache, cfg.Storage.Bucket)

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
			if mwSvc != nil && cfg.CircuitBreaker.Enabled {
				return mwSvc.ExecuteWithCB(ctx, "s3", cbConfig, func() error {
					if _, err := objStorage.ListObjects(ctx, "streamgate", ""); err != nil {
						return fmt.Errorf("storage check failed: %w", err)
					}
					return nil
				})
			}
			if _, err := objStorage.ListObjects(ctx, "streamgate", ""); err != nil {
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

	if _, err := os.Stat("./h5-demo"); err == nil {
		router.Static("/demo", "./h5-demo")
		log.Info("H5 Demo served at /demo/")
	}
}

func registerProtectedRoutes(router *gin.Engine, cfg *config.Config, log *zap.Logger, svc *serviceInit, streamLim *streamLimiter, streamCache *StreamingCache) {
	jwtConfig := middleware.JWTAuthConfig{
		Secret:    cfg.Auth.JWTSecret,
		Blacklist: svc.AuthService,
		SkipPaths: []string{
			"/api/v1/auth/challenge",
			"/api/v1/auth/login",
			"/api/v1/auth/register",
			"/api/v1/auth/refresh",
			"/api/v1/web3/rpc-status",
			"/api/v1/web3/supported-chains",
		},
	}

	authGroup := router.Group("/")
	authGroup.Use(middleware.JWTAuthMiddleware(jwtConfig, log))
	{
		RegisterAuthProtectedRoutes(authGroup, log, svc.AuthService)

		cbSvc := middleware.NewService(log)
		nftCBGroup := authGroup.Group("/")
		nftCBGroup.Use(cbSvc.CircuitBreakerMiddleware("nft-verify", middleware.CircuitBreakerConfig{
			FailureThreshold: 5, SuccessThreshold: 3, Timeout: 30 * time.Second,
		}))
		RegisterNFTRoutes(nftCBGroup, log, svc.NFTVerifier, svc.NFTCacheBackend, cfg.Web3.ChainID, 60*time.Second)

		RegisterUploadRoutes(authGroup, log, svc.UploadService)

		blockProver := svc.Web3Service
		nftGateConfig := middleware.NFTGateConfig{
			Verifier:       svc.NFTVerifier,
			BlockProver:    blockProver,
			Cache:          svc.NFTCacheBackend,
			RuleResolver:   svc.GatingRuleResolver,
			DefaultChainID: cfg.Web3.ChainID,
			CacheTTL:       60 * time.Second,
			MarketplaceURL: "https://opensea.io/assets/ethereum/{contract}/{token_id}",
			BlockTag:       parseBlockTag(cfg.Web3.BlockTag),
		}
		nftGroup := authGroup.Group("/")
		nftGroup.Use(middleware.NFTGateMiddleware(&nftGateConfig, log))
		nftGroup.Use(func(c *gin.Context) {
			if core.IsDraining() {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "server shutting down"})
				return
			}
			c.Next()
		})
		RegisterStreamingRoutes(nftGroup, log, svc.AuthService, svc.StreamingSvc, svc.SegmentStorage, streamLim, streamCache, cfg.Storage.Bucket)

		RegisterContentRoutes(authGroup, log, svc.ContentService)
		RegisterTranscodingRoutes(authGroup, log, svc.TranscodingSvc)

		if svc.GatingRuleSvc != nil {
			RegisterGatingRuleRoutes(authGroup, svc.GatingRuleSvc)
		}
		if svc.PlaybackStatsSvc != nil {
			RegisterPlaybackStatsRoutes(authGroup, svc.PlaybackStatsSvc)
		}
		if svc.CategorySvc != nil {
			RegisterCategoryRoutes(authGroup, svc.CategorySvc)
		}
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
