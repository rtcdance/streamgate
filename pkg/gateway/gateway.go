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
	"streamgate/pkg/plugins/transcoder"
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
	DB                *sql.DB
	ChallengeStore    io.Closer
	ObjStorage        io.Closer
	TokenBlacklist    io.Closer
	RateLimiter       middleware.RateLimiter
	RateLimiterRedis  io.Closer
	OTelShutdown      func(ctx context.Context) error
	AuthService       *service.AuthService
	Web3Service       *service.Web3Service
	NFTVerifier       middleware.NFTOwnershipChecker
	ContentService    *service.ContentService
	SegmentStorage    service.SegmentStorage
	UploadService     *service.UploadService
	TranscodingSvc    *service.TranscodingService
	NFTCache          *NFTAccessCache
	NFTRedisClient    io.Closer
	NATSQueue         io.Closer
	MiddlewareSvc     *middleware.Service
}

// Close releases all held resources. Errors from individual closes are
// joined but never prevent closing remaining resources.
func (r *AppResources) Close() error {
	var errs []error
	if r.RateLimiter != nil {
		r.RateLimiter.Stop()
	}
	if r.RateLimiterRedis != nil {
		_ = r.RateLimiterRedis.Close()
	}
	if r.NFTRedisClient != nil {
		_ = r.NFTRedisClient.Close()
	}
	if r.TranscodingSvc != nil {
		r.TranscodingSvc.StopWorker()
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
	ChallengeStore service.ChallengeStore
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
func WithChallengeStore(store service.ChallengeStore) RouterOption {
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
	Web3Service    *service.Web3Service
	AuthService    *service.AuthService
	NFTVerifier    middleware.NFTOwnershipChecker
	NFTCache       *NFTAccessCache
	NFTCacheBackend middleware.NFTAccessCache
	DB             storage.DB
	ContentService *service.ContentService
	SegmentStorage service.SegmentStorage
	TranscodingSvc *service.TranscodingService
	UploadService  *service.UploadService
}

func SetupRouter(cfg *config.Config, log *zap.Logger, opts ...RouterOption) (*gin.Engine, *AppResources, error) {
	rc := &RouterConfig{}
	for _, opt := range opts {
		opt(rc)
	}
	resources := &AppResources{}

	web3Svc, err := initWeb3Service(rc, cfg, log)
	if err != nil {
		return nil, nil, err
	}
	resources.Web3Service = web3Svc

	challengeTTL := parseChallengeTTL(cfg)
	challengeStore := initChallengeStore(rc, cfg, log, challengeTTL, resources)

	authService := initAuthService(rc, cfg, log, challengeStore, challengeTTL, resources)
	resources.AuthService = authService

	nftCache := NewNFTAccessCache()
	resources.NFTCache = nftCache

	nftRedisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	nftRedisClient := redis.NewClient(&redis.Options{
		Addr:         nftRedisAddr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	nftPingCtx, nftPingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	var nftCacheBackend middleware.NFTAccessCache
	if err := nftRedisClient.Ping(nftPingCtx).Err(); err != nil {
		log.Warn("Redis unavailable for NFT cache, falling back to in-memory only", zap.Error(err))
		_ = nftRedisClient.Close()
		nftRedisClient = nil
		nftCacheBackend = &NFTAccessCacheAdapter{Cache: nftCache}
	} else {
		log.Info("Using Redis-backed NFT access cache", zap.String("addr", nftRedisAddr))
		nftCacheBackend = NewRedisNFTAccessCache(nftCache, nftRedisClient)
		resources.NFTRedisClient = nftRedisClient
	}
	nftPingCancel()

	nftVerifier := rc.NFTVerifier
	if nftVerifier == nil {
		nftVerifier = web3Svc
	}
	resources.NFTVerifier = nftVerifier

	db, _ := initDatabase(cfg, log, resources)
	contentSvc := initContentService(rc, db, log)
	resources.ContentService = contentSvc

	objStorage := initObjectStorage(rc, cfg, log, resources)
	resources.SegmentStorage = objStorage

	transcodingSvc := initTranscodingService(cfg, log, db, objStorage, resources)
	resources.TranscodingSvc = transcodingSvc

	uploadSvc := initUploadService(rc, cfg, log, db, objStorage, transcodingSvc)
	resources.UploadService = uploadSvc

	initOTelTracing(cfg, log, resources)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	setupMiddleware(router, cfg, log, resources)

	svc := &serviceInit{
		Web3Service:     web3Svc,
		AuthService:     authService,
		NFTVerifier:     nftVerifier,
		NFTCache:        nftCache,
		NFTCacheBackend: nftCacheBackend,
		DB:              db,
		ContentService:  contentSvc,
		SegmentStorage:  objStorage,
		TranscodingSvc:  transcodingSvc,
		UploadService:   uploadSvc,
	}
	registerRoutes(router, cfg, log, svc, resources)

	return router, resources, nil
}

func parseChallengeTTL(cfg *config.Config) time.Duration {
	ttl := 5 * time.Minute
	if cfg.Auth.NonceExpiry != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.NonceExpiry); err == nil && parsed > 0 {
			ttl = parsed
		}
	}
	return ttl
}

func initWeb3Service(rc *RouterConfig, cfg *config.Config, log *zap.Logger) (*service.Web3Service, error) {
	if rc.Web3Service != nil {
		return rc.Web3Service, nil
	}
	svc, err := service.NewWeb3Service(service.DefaultWeb3Deps(cfg, log), cfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Web3 service: %w", err)
	}
	return svc, nil
}

func initChallengeStore(rc *RouterConfig, cfg *config.Config, log *zap.Logger, challengeTTL time.Duration, res *AppResources) service.ChallengeStore {
	if rc.ChallengeStore != nil {
		return rc.ChallengeStore
	}
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	store, err := service.NewRedisChallengeStore(redisAddr, challengeTTL,
		service.WithRedisPassword(cfg.Redis.Password),
		service.WithRedisDB(cfg.Redis.DB),
		service.WithRedisPoolSize(cfg.Redis.PoolSize),
	)
	if err != nil {
		log.Warn("Falling back to in-memory challenge store", zap.Error(err))
		return nil
	}
	res.ChallengeStore = store
	return store
}

func initTokenBlacklist(cfg *config.Config, log *zap.Logger, res *AppResources) service.TokenBlacklist {
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	redisClient := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	rbl, err := storage.NewRedisTokenBlacklist(redisClient)
	if err != nil {
		log.Warn("Redis unavailable, falling back to in-memory token blacklist", zap.Error(err))
		_ = redisClient.Close()
		return service.NewMemoryTokenBlacklist()
	}
	log.Info("Using Redis token blacklist", zap.String("addr", redisAddr))
	res.TokenBlacklist = rbl
	return rbl
}

func initAuthService(rc *RouterConfig, cfg *config.Config, log *zap.Logger, challengeStore service.ChallengeStore, challengeTTL time.Duration, res *AppResources) *service.AuthService {
	if rc.AuthService != nil {
		return rc.AuthService
	}
	solanaVerifier := web3.NewSolanaVerifier(log.Named("solana"), cfg.Web3.SolanaRPC)
	signatureVerifier := service.NewMultiChainSignatureVerifier(log, solanaVerifier)
	tokenBlacklist := initTokenBlacklist(cfg, log, res)

	jwtExpiry := 2 * time.Hour
	if cfg.Auth.JWTExpiry != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.JWTExpiry); err == nil && parsed > 0 {
			jwtExpiry = parsed
		}
	}

	return service.NewAuthService(cfg.Auth.JWTSecret, nil,
		service.WithSignatureVerifier(signatureVerifier),
		service.WithChallengeStore(challengeStore),
		service.WithChallengeTTL(challengeTTL),
		service.WithTokenBlacklist(tokenBlacklist),
		service.WithJWTExpiry(jwtExpiry),
	)
}

func initDatabase(cfg *config.Config, log *zap.Logger, res *AppResources) (db storage.DB, sqlDB *sql.DB) {
	dbConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.User, cfg.Database.Password,
		cfg.Database.Database, cfg.Database.SSLMode)
	d, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Warn("Database unavailable, using in-memory fallback",
			zap.String("host", cfg.Database.Host),
			zap.String("database", cfg.Database.Database))
		return
	}
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := d.PingContext(pingCtx); err != nil {
		log.Warn("Database ping failed, using in-memory fallback",
			zap.String("host", cfg.Database.Host),
			zap.String("database", cfg.Database.Database))
		_ = d.Close()
		return
	}
	pg := storage.NewPostgresDBFromDB(d)
	pg.SetMaxOpenConns(cfg.Database.MaxConns)
	if cfg.Database.MaxIdleConns > 0 {
		pg.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime != "" {
		if dur, parseErr := time.ParseDuration(cfg.Database.ConnMaxLifetime); parseErr == nil {
			pg.SetConnMaxLifetime(dur)
		}
	}
	res.DB = d
	log.Info("Database connected", zap.String("host", cfg.Database.Host))
	if err := storage.RunEmbeddedMigrations(d, migrationFS, "migrations"); err != nil {
		log.Warn("Database migration failed, continuing with current schema", zap.Error(err))
	} else {
		log.Info("Database migrations applied")
	}
	db = pg
	sqlDB = d
	return
}

func initContentService(rc *RouterConfig, db storage.DB, log *zap.Logger) *service.ContentService {
	if rc.ContentService != nil {
		return rc.ContentService
	}
	if db != nil {
		log.Info("Content service initialized")
		return service.NewContentService(db, nil, nil)
	}
	log.Warn("Content service unavailable, database not connected")
	return nil
}

func initObjectStorage(rc *RouterConfig, cfg *config.Config, log *zap.Logger, res *AppResources) service.SegmentStorage {
	if rc.SegmentStorage != nil {
		return rc.SegmentStorage
	}
	minioCfg := storage.MinIOConfig{
		Endpoint:        cfg.Storage.Endpoint,
		AccessKeyID:     cfg.Storage.AccessKey,
		SecretAccessKey: cfg.Storage.SecretKey,
		UseSSL:          cfg.Storage.UseSSL,
	}
	ms, err := storage.NewMinIOStorage(minioCfg)
	if err != nil {
		log.Warn("MinIO unavailable, segment serving disabled", zap.Error(err))
		return nil
	}
	res.ObjStorage = ms
	bucketCtx, bucketCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bucketCancel()
	if err := ms.CreateBucket(bucketCtx, "streamgate"); err != nil {
		log.Warn("Failed to create streamgate bucket", zap.Error(err))
	}
	log.Info("MinIO storage initialized", zap.String("endpoint", cfg.Storage.Endpoint))
	return ms
}

func initTranscodingService(cfg *config.Config, log *zap.Logger, db storage.DB, objStorage service.SegmentStorage, res *AppResources) *service.TranscodingService {
	ffmpegCfg := &transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     30 * time.Minute,
	}
	ft := transcoder.NewFFmpegTranscoder(ffmpegCfg, log.Named("ffmpeg"))
	videoTranscoder := &ffmpegRouterAdapter{ft: ft, log: log.Named("ffmpeg")}

	var transcodingQueue service.TranscodingQueue
	nq, err := storage.NewNATSTranscodingQueue(cfg.NATS.URL, log.Named("nats-queue"))
	if err != nil {
		log.Warn("NATS unavailable, falling back to in-memory transcoding queue", zap.Error(err))
		transcodingQueue = service.NewMemoryTranscodingQueue()
	} else {
		log.Info("Using NATS JetStream transcoding queue", zap.String("url", cfg.NATS.URL))
		transcodingQueue = nq
		res.NATSQueue = nq
	}

	svc := service.NewTranscodingService(db, transcodingQueue,
		service.WithTranscoder(videoTranscoder),
		service.WithStorage(objStorage),
		service.WithLogger(log),
	)
	svc.StartWorker(&zapRouterInfoLogger{log.Named("transcode-worker")})
	return svc
}

func initUploadService(rc *RouterConfig, cfg *config.Config, log *zap.Logger, db storage.DB, objStorage service.SegmentStorage, transcodingSvc *service.TranscodingService) *service.UploadService {
	if rc.UploadService != nil {
		return rc.UploadService
	}
	if db == nil || objStorage == nil {
		return nil
	}
	uploadObj, ok := objStorage.(service.UploadObjectStorage)
	if !ok {
		log.Warn("Object storage does not implement UploadObjectStorage, upload service disabled")
		return nil
	}
	svc := service.NewUploadService(db, uploadObj, "streamgate", log.Named("upload"))
	if cfg.Upload.MaxSize > 0 {
		svc.SetMaxUploadSize(cfg.Upload.MaxSize)
	}
	if cfg.Upload.StorageQuota > 0 {
		svc.SetStorageQuota(cfg.Upload.StorageQuota)
	}
	var presigner service.PresignedURLer
	if ps, ok := objStorage.(service.PresignedURLer); ok {
		presigner = ps
		svc.SetPresigner(ps)
	}
	if ups, ok := objStorage.(service.UploadPresignedURLer); ok {
		svc.SetUploadPresigner(ups)
	}
	svc.RegisterAutoTranscodeHook(service.AutoTranscodeHookDeps{
		TranscodingSvc: transcodingSvc,
		Presigner:      presigner,
		Bucket:         "streamgate",
		Profiles:       cfg.Transcode.Profiles,
	})
	return svc
}

func initOTelTracing(cfg *config.Config, log *zap.Logger, res *AppResources) {
	if cfg.Monitoring.JaegerEndpoint == "" {
		return
	}
	shutdown, err := monitoring.InitOTelTracing(context.Background(), "streamgate", cfg.Monitoring.JaegerEndpoint, log)
	if err != nil {
		log.Warn("OTel tracing init failed, continuing without tracing", zap.Error(err))
		return
	}
	res.OTelShutdown = shutdown
}

func setupMiddleware(router *gin.Engine, cfg *config.Config, log *zap.Logger, res *AppResources) {
	router.Use(RequestIDMiddleware())

	rlRedisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	rlRedisClient := redis.NewClient(&redis.Options{
		Addr:         rlRedisAddr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	rlPingCtx, rlPingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	var middlewareSvc *middleware.Service
	if err := rlRedisClient.Ping(rlPingCtx).Err(); err != nil {
		log.Warn("Redis unavailable for rate limiter, falling back to in-memory", zap.Error(err))
		_ = rlRedisClient.Close()
		rlRedisClient = nil
		middlewareSvc = middleware.NewService(log)
	} else {
		log.Info("Using Redis-backed rate limiter", zap.String("addr", rlRedisAddr))
		middlewareSvc = middleware.NewServiceWithRedis(log, rlRedisClient)
		res.RateLimiterRedis = rlRedisClient
	}
	rlPingCancel()

	rl, rlHandler := middlewareSvc.RateLimitMiddlewareWithConfig(middleware.RateLimitConfig{
		RequestsPerMinute: cfg.RateLimiting.RequestsPerMinute,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	})
	res.RateLimiter = rl
	res.MiddlewareSvc = middlewareSvc

	router.Use(core.DrainMiddleware())
	router.Use(middlewareSvc.TraceIDMiddleware())
	router.Use(middlewareSvc.LoggingMiddleware())
	router.Use(middlewareSvc.RecoveryMiddleware())
	router.Use(middlewareSvc.SecurityHeadersMiddleware())
	router.Use(middlewareSvc.ContentTypeMiddleware())
	router.Use(middlewareSvc.RequestSizeLimitMiddleware(defaultMaxBodySize))
	router.Use(middlewareSvc.CORSMiddleware(cfg.CORS.AllowedOrigins...))
	router.Use(middlewareSvc.TracingMiddleware())
	router.Use(rlHandler)
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

	RegisterAuthRoutes(router, log, cfg, svc.AuthService)
	RegisterWeb3Routes(router, log, svc.Web3Service)

	streamLim := newStreamLimiter(cfg.Streaming.MaxConcurrentStreams)
	RegisterStreamingSegmentRoute(router, log, svc.AuthService, svc.SegmentStorage, streamLim, cfg.Storage.Bucket)

	registerProtectedRoutes(router, cfg, log, svc, streamLim)
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
				"name":             name,
				"state":            s.State.String(),
				"failure_count":    s.FailureCount,
				"success_count":    s.SuccessCount,
				"request_count":    s.RequestCount,
				"failure_rate":     s.FailureRate,
				"last_failure":     s.LastFailureTime,
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

func registerProtectedRoutes(router *gin.Engine, cfg *config.Config, log *zap.Logger, svc *serviceInit, streamLim *streamLimiter) {
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

		nftGateConfig := middleware.NFTGateConfig{
			Verifier:       svc.NFTVerifier,
			Cache:          svc.NFTCacheBackend,
			DefaultChainID: cfg.Web3.ChainID,
			CacheTTL:       60 * time.Second,
			MarketplaceURL: "https://opensea.io/assets/ethereum/{contract}/{token_id}",
		}
		nftGroup := authGroup.Group("/")
		nftGroup.Use(middleware.NFTGateMiddleware(nftGateConfig, log))
		RegisterStreamingRoutes(nftGroup, log, svc.AuthService, svc.SegmentStorage, streamLim, cfg.Storage.Bucket)

		RegisterContentRoutes(authGroup, log, svc.ContentService)
		RegisterTranscodingRoutes(authGroup, log, svc.TranscodingSvc)
	}
}
