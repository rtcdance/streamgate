package gateway

import (
	"context"
	"embed"
	"strconv"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const defaultMaxBodySize int64 = 10 << 20 // 10MB global body size limit

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
