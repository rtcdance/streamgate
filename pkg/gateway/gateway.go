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

	sharedRedis, err := provideRedis(cfg, log, resources)
	if err != nil {
		return nil, nil, err
	}

	// The database comes first so the shared nonce ledger (SG-13 A.3 — the DB
	// is the single nonce authority for a (chain, signer)) can be handed to
	// every signer path built below.
	db, _, err := provideDatabase(cfg, log, resources)
	if err != nil {
		return nil, nil, err
	}
	nonceLedger := provideNonceLedger(cfg, log, db, resources)

	// The gating-rule service owns the contract whitelist the event indexer
	// scans (SG-13 B.1), so it is built before the Web3 service — which reads
	// the whitelist at construction — and later reused for the HTTP routes.
	var gatingRuleSvc *service.GatingRuleService
	var gatingRuleRefresher service.GatingRuleFilterRefresher
	if db != nil {
		gatingRuleSvc = service.NewGatingRuleService(db, log.Named("gating-rule"))
		gatingRuleRefresher = gatingRuleSvc
	}

	web3Svc, err := provideWeb3Service(rc, cfg, log, nonceLedger, db, gatingRuleRefresher)
	if err != nil {
		return nil, nil, err
	}
	resources.Web3Service = web3Svc

	challengeTTL := parseChallengeTTL(cfg)
	challengeStore, err := provideChallengeStore(rc, cfg, log, challengeTTL, sharedRedis, resources)
	if err != nil {
		return nil, nil, err
	}

	authService, err := provideAuthService(rc, cfg, log, web3Svc, challengeStore, challengeTTL, sharedRedis, resources)
	if err != nil {
		return nil, nil, err
	}
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

	contentSvc := provideContentService(rc, db, log)
	resources.ContentService = contentSvc

	objStorage := provideObjectStorage(rc, cfg, log, resources)
	resources.SegmentStorage = objStorage

	transcodingSvc, err := provideTranscodingService(cfg, log, db, objStorage, resources)
	if err != nil {
		return nil, nil, err
	}
	resources.TranscodingSvc = transcodingSvc

	if transcodingSvc != nil {
		streamCache := NewStreamingCache()
		resources.StreamingCache = streamCache
		transcodingSvc.RegisterPostTranscodeHook(func(ctx context.Context, contentID, profile, outputURL string) {
			streamCache.Invalidate(contentID)
			if contentSvc != nil {
				if err := contentSvc.UpdateContentStatus(ctx, contentID, "ready"); err != nil {
					log.Warn("failed to update content status after transcode", zap.String("content_id", contentID), zap.Error(err))
				}
			}
		})
	}

	uploadSvc := provideUploadService(rc, cfg, log, db, objStorage, transcodingSvc)
	resources.UploadService = uploadSvc

	// SG-13 A: the durable on-chain registration stack replaces the old
	// synchronous post-commit chain call. The intent commits with the content
	// row, the publisher submits and tracks the chain write, and the attest
	// endpoint exposes the durable status. Everything degrades to "registry
	// disabled" when config or database is missing.
	registryHub := ProvideRegistryHub(cfg, log, db)
	if registryHub != nil {
		if contentSvc != nil {
			contentSvc.SetRegistryIntent(registryHub.Store)
		}
		if uploadSvc != nil {
			uploadSvc.SetRegistryIntent(registryHub.Store)
		}
		registryHub.Publisher.Start(context.Background())
	}
	resources.RegistryHub = registryHub

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
		DemoNFTMinter:   newDemoNFTMinter(cfg, log, nonceLedger),
		RegistryHub:     registryHub,
	}
	resources.StreamingSvc = svc.StreamingSvc

	if db != nil {
		// Reuse the instance built above (it also feeds the indexer whitelist);
		// building a second one would split two services over the same table.
		svc.GatingRuleSvc = gatingRuleSvc
		// Rules are resolved through the version authority so a change made in
		// another process is noticed within the version's age bound, not after a
		// local TTL expires.
		ruleVersions, err := provideRuleVersionStore(cfg, log, db, sharedRedis, resources)
		if err != nil {
			return nil, nil, err
		}
		svc.GatingRuleSvc.SetVersionPublisher(func(contentID string, version int64) {
			// The write already bumped the version inside its own statement, so
			// this only publishes it; a context is needed for the cache write and
			// the request's is already gone by the time the callback runs.
			ruleVersions.Publish(context.Background(), contentID, version)
		})
		svc.GatingRuleResolver = NewVersionedGatingRuleResolverAdapter(svc.GatingRuleSvc, ruleVersions)
		resources.GatingRuleResolver = svc.GatingRuleResolver
		// Capabilities carry the rule version they were issued under, so a rule
		// change reaches media already in flight without a per-segment chain read.
		if authService != nil {
			authService.SetRuleVersionReader(func(ctx context.Context, contentID string) (int64, error) {
				v, err := ruleVersions.Read(ctx, contentID)
				if err != nil {
					return 0, err
				}
				return v.Version, nil
			})
		}
	}
	// An explicitly injected resolver (RouterOption) wins over the DB-backed one.
	if rc.GatingRuleResolver != nil {
		svc.GatingRuleResolver = rc.GatingRuleResolver
		resources.GatingRuleResolver = rc.GatingRuleResolver
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
