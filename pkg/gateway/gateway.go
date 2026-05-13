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

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/docs"
	"streamgate/pkg/health"
	"streamgate/pkg/middleware"
	"streamgate/pkg/plugins/transcoder"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/service"
	"streamgate/pkg/storage"
	"streamgate/pkg/web3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const defaultMaxBodySize int64 = 10 << 20 // 10MB global body size limit

// AppResources holds closeable resources created by SetupRouter.
// Callers should defer resources.Close() to ensure cleanup on shutdown.
type AppResources struct {
	DB             *sql.DB
	ChallengeStore io.Closer
	ObjStorage     io.Closer
	TokenBlacklist io.Closer
	RateLimiter    *middleware.RateLimiter
	OTelShutdown   func(ctx context.Context) error
}

// Close releases all held resources. Errors from individual closes are
// joined but never prevent closing remaining resources.
func (r *AppResources) Close() error {
	var errs []error
	if r.RateLimiter != nil {
		r.RateLimiter.Stop()
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

// SetupRouter initializes all services and configures the gin router with the
// full middleware chain and route registrations. Used by both api-gateway and monolith.
// Optional RouterOption overrides allow tests to inject mock dependencies.
// Returns the router engine and an AppResources that must be closed on shutdown.
func SetupRouter(cfg *config.Config, log *zap.Logger, opts ...RouterOption) (*gin.Engine, *AppResources, error) {
	rc := &RouterConfig{}
	for _, opt := range opts {
		opt(rc)
	}

	resources := &AppResources{}

	var web3Svc *service.Web3Service
	if rc.Web3Service != nil {
		web3Svc = rc.Web3Service
	} else {
		var err error
		web3Svc, err = service.NewWeb3Service(service.DefaultWeb3Deps(cfg, log), cfg, log)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to initialize Web3 service: %w", err)
		}
	}

	challengeTTL := 5 * time.Minute
	if cfg.Auth.NonceExpiry != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.NonceExpiry); err == nil && parsed > 0 {
			challengeTTL = parsed
		}
	}

	var challengeStore service.ChallengeStore
	if rc.ChallengeStore != nil {
		challengeStore = rc.ChallengeStore
	} else {
		redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
		if store, err := service.NewRedisChallengeStore(redisAddr, challengeTTL,
			service.WithRedisPassword(cfg.Redis.Password),
			service.WithRedisDB(cfg.Redis.DB),
			service.WithRedisPoolSize(cfg.Redis.PoolSize),
		); err == nil {
			challengeStore = store
			resources.ChallengeStore = store // track for shutdown
		} else {
			log.Warn("Falling back to in-memory challenge store", zap.Error(err))
		}
	}

	var authService *service.AuthService
	if rc.AuthService != nil {
		authService = rc.AuthService
	} else {
		// Create a chain-aware signature verifier that supports both EVM and Solana
		solanaVerifier := web3.NewSolanaVerifier(log.Named("solana"), cfg.Web3.SolanaRPC)
		signatureVerifier := service.NewMultiChainSignatureVerifier(log, solanaVerifier)

		// Try Redis-backed token blacklist; fall back to in-memory if Redis is unavailable
		var tokenBlacklist service.TokenBlacklist
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
		if rbl, err := storage.NewRedisTokenBlacklist(redisClient); err != nil {
			log.Warn("Redis unavailable, falling back to in-memory token blacklist", zap.Error(err))
			_ = redisClient.Close()
			tokenBlacklist = service.NewMemoryTokenBlacklist()
		} else {
			log.Info("Using Redis token blacklist", zap.String("addr", redisAddr))
			tokenBlacklist = rbl
		}

		resources.TokenBlacklist = tokenBlacklist // track for shutdown

		authService = service.NewAuthServiceWithDeps(cfg.Auth.JWTSecret, nil, signatureVerifier, challengeStore, challengeTTL, tokenBlacklist)
	}
	nftCache := NewNFTAccessCache()

	// Determine NFT verifier: use injected mock if provided, otherwise Web3Service.
	var nftVerifier middleware.NFTOwnershipChecker
	if rc.NFTVerifier != nil {
		nftVerifier = rc.NFTVerifier
	} else {
		nftVerifier = web3Svc
	}

	// Initialize database
	var db storage.DB
	var sqlDB *sql.DB // kept for migrations
	dbConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.User, cfg.Database.Password,
		cfg.Database.Database, cfg.Database.SSLMode)
	if d, err := sql.Open("postgres", dbConnStr); err == nil {
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pingCancel()
		if err := d.PingContext(pingCtx); err != nil {
			log.Warn("Database ping failed, using in-memory fallback", zap.Error(err))
			_ = d.Close()
		} else {
			pg := storage.NewPostgresDBFromDB(d)
			pg.SetMaxOpenConns(cfg.Database.MaxConns)
			db = pg
			sqlDB = d // track for migrations and shutdown
			resources.DB = d
			log.Info("Database connected", zap.String("host", cfg.Database.Host))
			if err := storage.RunEmbeddedMigrations(sqlDB, migrationFS, "migrations"); err != nil {
				log.Warn("Database migration failed, continuing with current schema", zap.Error(err))
			} else {
				log.Info("Database migrations applied")
			}
		}
	} else {
		log.Warn("Database unavailable, using in-memory fallback", zap.Error(err))
	}

	// Initialize ContentService
	var contentSvc *service.ContentService
	if rc.ContentService != nil {
		contentSvc = rc.ContentService
	} else if db != nil {
		contentSvc = service.NewContentService(db, nil, nil)
		log.Info("Content service initialized")
	} else {
		log.Warn("Content service unavailable, database not connected")
	}

	// Initialize MinIO storage
	var objStorage service.SegmentStorage
	if rc.SegmentStorage != nil {
		objStorage = rc.SegmentStorage
	} else {
		minioCfg := storage.MinIOConfig{
			Endpoint:        cfg.Storage.Endpoint,
			AccessKeyID:     cfg.Storage.AccessKey,
			SecretAccessKey: cfg.Storage.SecretKey,
			UseSSL:          false,
		}
		if ms, err := storage.NewMinIOStorage(minioCfg); err == nil {
			objStorage = ms
			resources.ObjStorage = ms // track for shutdown
			bucketCtx, bucketCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer bucketCancel()
			if err := ms.CreateBucket(bucketCtx, "streamgate"); err != nil {
				log.Warn("Failed to create streamgate bucket", zap.Error(err))
			}
			log.Info("MinIO storage initialized", zap.String("endpoint", cfg.Storage.Endpoint))
		} else {
			log.Warn("MinIO unavailable, segment serving disabled", zap.Error(err))
		}
	}

	// Initialize FFmpeg transcoder
	var videoTranscoder service.VideoTranscoder
	ffmpegCfg := &transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     30 * time.Minute,
	}
	ft := transcoder.NewFFmpegTranscoder(ffmpegCfg, log.Named("ffmpeg"))
	videoTranscoder = &ffmpegRouterAdapter{ft: ft, log: log.Named("ffmpeg")}

	// Initialize transcoding queue: try NATS JetStream, fall back to in-memory
	var transcodingQueue service.TranscodingQueue
	if nq, err := storage.NewNATSTranscodingQueue(cfg.NATS.URL, log.Named("nats-queue")); err != nil {
		log.Warn("NATS unavailable, falling back to in-memory transcoding queue", zap.Error(err))
		transcodingQueue = service.NewMemoryTranscodingQueue()
	} else {
		log.Info("Using NATS JetStream transcoding queue", zap.String("url", cfg.NATS.URL))
		transcodingQueue = nq
	}

	transcodingSvc := service.NewTranscodingService(db, transcodingQueue,
		service.WithTranscoder(videoTranscoder),
		service.WithStorage(objStorage),
	)
	transcodingSvc.StartWorker(&zapRouterInfoLogger{log.Named("transcode-worker")})

	// Initialize upload service (wires UploadService to DB + object storage)
	var uploadSvc *service.UploadService
	if rc.UploadService != nil {
		uploadSvc = rc.UploadService
	} else if db != nil && objStorage != nil {
		// objStorage is SegmentStorage (no Delete); MinIOStorage also implements
		// UploadObjectStorage (with Delete) and PresignedURLer.
		if uploadObj, ok := objStorage.(service.UploadObjectStorage); ok {
			uploadSvc = service.NewUploadService(db, uploadObj, "streamgate", log.Named("upload"))
			if presigner, ok := objStorage.(service.PresignedURLer); ok {
				uploadSvc.SetPresigner(presigner)
			}
		} else {
			log.Warn("Object storage does not implement UploadObjectStorage, upload service disabled")
		}
	}
	// Plugin handlers use MetricsCollector which bridges to Prometheus;
	// promhttp.Handler() on /metrics is the single source of truth.

	// Initialize OTel tracing if a Jaeger/OTLP endpoint is configured
	if cfg.Monitoring.JaegerEndpoint != "" {
		otelShutdown, err := monitoring.InitOTelTracing(context.Background(), "streamgate", cfg.Monitoring.JaegerEndpoint, log)
		if err != nil {
			log.Warn("OTel tracing init failed, continuing without tracing", zap.Error(err))
		} else {
			resources.OTelShutdown = otelShutdown
		}
	}

	// Initialize HTTP router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	router.Use(RequestIDMiddleware())
	middlewareSvc := middleware.NewService(log)
	rl, rlHandler := middlewareSvc.RateLimitMiddlewareWithConfig(middleware.DefaultRateLimitConfig())
	resources.RateLimiter = rl
	router.Use(core.DrainMiddleware())
	router.Use(middlewareSvc.TraceIDMiddleware())
	router.Use(middlewareSvc.LoggingMiddleware())
	router.Use(middlewareSvc.RecoveryMiddleware())
	router.Use(middlewareSvc.SecurityHeadersMiddleware())
	router.Use(middlewareSvc.RequestSizeLimitMiddleware(defaultMaxBodySize))
	router.Use(middlewareSvc.CORSMiddleware(cfg.CORS.AllowedOrigins...))
	router.Use(middlewareSvc.TracingMiddleware())
	router.Use(rlHandler)
	router.Use(func(c *gin.Context) {
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
	})

	// Setup health checker with dependency checks
	healthChecker := health.NewHealthChecker(log)
	if db != nil {
		healthChecker.RegisterCheck("database", func(ctx context.Context) error { return db.Ping(ctx) })
	}
	if objStorage != nil {
		healthChecker.RegisterCheck("storage", func(ctx context.Context) error {
			if _, err := objStorage.ListObjects(ctx, "streamgate", ""); err != nil {
				return fmt.Errorf("storage check failed: %w", err)
			}
			return nil
		})
	}

	// Infrastructure endpoints
	router.GET("/health", func(c *gin.Context) {
		monitoring.HealthCheckTotal.WithLabelValues("health").Inc()
		resp := healthChecker.CheckAll(c.Request.Context())
		status := http.StatusOK
		if resp.Status == health.StatusUnhealthy {
			status = http.StatusServiceUnavailable
		} else if resp.Status == health.StatusDegraded {
			status = http.StatusMultiStatus
		}
		c.JSON(status, resp)
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
		c.JSON(status, resp)
	})

	// Swagger UI and OpenAPI spec
	router.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(docs.SwaggerUIHTML))
	})
	router.StaticFile("/docs/swagger.yaml", filepath.Join(".", "docs", "swagger.yaml"))

	// H5 Demo (creator upload + wallet login + video playback)
	// Serves from ./h5-demo/ directory when present (bundled in Docker image)
	if _, err := os.Stat("./h5-demo"); err == nil {
		router.Static("/demo", "./h5-demo")
		log.Info("H5 Demo served at /demo/")
	}

	// Public routes (no JWT required)
	jwtConfig := middleware.JWTAuthConfig{
		Secret:    cfg.Auth.JWTSecret,
		Blacklist: authService,
		SkipPaths: []string{
			"/api/v1/auth/challenge",
			"/api/v1/auth/login",
			"/api/v1/auth/register",
			"/api/v1/auth/refresh",
			"/api/v1/web3/rpc-status",
			"/api/v1/web3/supported-chains",
		},
	}

	RegisterAuthRoutes(router, log, authService)
	RegisterWeb3Routes(router, log, web3Svc)

	// Segment route: registered on the bare router (no JWT middleware).
	// The handler validates playback_token from the query string — a short-lived
	// JWT-signed token embedded in the manifest by the streaming handler.
	// Placing this outside authGroup avoids double-auth and allows HLS players
	// to fetch segments without an Authorization header.
	RegisterStreamingSegmentRoute(router, log, authService, objStorage, cfg.Storage.Bucket)

	// JWT-protected routes
	authGroup := router.Group("/")
	authGroup.Use(middleware.JWTAuthMiddleware(jwtConfig, log))
	{
		RegisterAuthProtectedRoutes(authGroup, log, authService)

		cbSvc := middleware.NewService(log)
		nftCBGroup := authGroup.Group("/")
		nftCBGroup.Use(cbSvc.CircuitBreakerMiddleware("nft-verify", middleware.CircuitBreakerConfig{
			FailureThreshold: 5, SuccessThreshold: 3, Timeout: 30 * time.Second,
		}))
		RegisterNFTRoutes(nftCBGroup, log, nftVerifier, &NFTAccessCacheAdapter{Cache: nftCache}, cfg.Web3.ChainID, 60*time.Second)

		RegisterUploadRoutes(authGroup, log, uploadSvc)

		nftGateConfig := middleware.NFTGateConfig{
			Verifier:       nftVerifier,
			Cache:          &NFTAccessCacheAdapter{Cache: nftCache},
			DefaultChainID: cfg.Web3.ChainID,
			CacheTTL:       60 * time.Second,
			MarketplaceURL: "https://opensea.io/assets/ethereum/{contract}/{token_id}",
		}
		nftGroup := authGroup.Group("/")
		nftGroup.Use(middleware.NFTGateMiddleware(nftGateConfig, log))
		RegisterStreamingRoutes(nftGroup, log, authService, objStorage, cfg.Storage.Bucket)

		RegisterContentRoutes(authGroup, log, contentSvc)
		RegisterTranscodingRoutes(authGroup, log, transcodingSvc)
	}

	return router, resources, nil
}
