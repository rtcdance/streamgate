package gateway

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/plugins/transcoder"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"

	"github.com/go-redis/redis/v8"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

func provideRedis(cfg *config.Config, log *zap.Logger, res *AppResources) *redis.Client {
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	client := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	ok := client.Ping(pingCtx).Err() == nil
	pingCancel()
	if !ok {
		log.Warn("Redis unavailable, components will use in-memory fallbacks", zap.String("addr", redisAddr))
		_ = client.Close()
		return nil
	}
	log.Info("Redis connected", zap.String("addr", redisAddr))
	res.SharedRedis = client
	return client
}

func provideWeb3Service(rc *RouterConfig, cfg *config.Config, log *zap.Logger) (*service.Web3Service, error) {
	if rc.Web3Service != nil {
		return rc.Web3Service, nil
	}
	svc, err := service.NewWeb3Service(service.DefaultWeb3Deps(cfg, log), cfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Web3 service: %w", err)
	}
	return svc, nil
}

func provideChallengeStore(rc *RouterConfig, log *zap.Logger, challengeTTL time.Duration, redisClient *redis.Client, res *AppResources) storage.ChallengeStore {
	if rc.ChallengeStore != nil {
		return rc.ChallengeStore
	}
	if redisClient == nil {
		log.Warn("Redis unavailable, falling back to in-memory challenge store")
		store := storage.NewMemoryChallengeStore()
		res.ChallengeStore = store
		return store
	}
	store := storage.NewRedisChallengeStoreWithClient(redisClient, challengeTTL)
	res.ChallengeStore = store
	return store
}

func provideTokenBlacklist(log *zap.Logger, redisClient *redis.Client, res *AppResources) storage.TokenBlacklist {
	if redisClient == nil {
		log.Warn("Redis unavailable, falling back to in-memory token blacklist")
		return storage.NewMemoryTokenBlacklist()
	}
	rbl, err := storage.NewRedisTokenBlacklist(redisClient)
	if err != nil {
		log.Warn("Redis token blacklist init failed, falling back to in-memory", zap.Error(err))
		return storage.NewMemoryTokenBlacklist()
	}
	res.TokenBlacklist = rbl
	return rbl
}

func provideAuthService(rc *RouterConfig, cfg *config.Config, log *zap.Logger, web3Svc *service.Web3Service, challengeStore storage.ChallengeStore, challengeTTL time.Duration, redisClient *redis.Client, res *AppResources) *service.AuthService {
	if rc.AuthService != nil {
		return rc.AuthService
	}
	solanaSigner := web3Svc.GetSolanaSigner()
	signatureVerifier := service.NewMultiChainSignatureVerifier(log, solanaSigner)
	eip712Verifier := web3Svc.GetEIP712Verifier()
	tokenBlacklist := provideTokenBlacklist(log, redisClient, res)

	jwtExpiry := 2 * time.Hour
	if cfg.Auth.JWTExpiry != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.JWTExpiry); err == nil && parsed > 0 {
			jwtExpiry = parsed
		}
	}

	return service.NewAuthService(cfg.Auth.JWTSecret, nil,
		service.WithSignatureVerifier(signatureVerifier),
		service.WithEIP712Verifier(eip712Verifier),
		service.WithChallengeStore(challengeStore),
		service.WithChallengeTTL(challengeTTL),
		service.WithTokenBlacklist(tokenBlacklist),
		service.WithJWTExpiry(jwtExpiry),
		service.WithSIWEDomain(cfg.Auth.SIWEDomain, cfg.Auth.SIWEURI),
	)
}

func provideDatabase(cfg *config.Config, log *zap.Logger, res *AppResources) (db storage.DB, sqlDB *sql.DB) {
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
	migCtx, migCancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = storage.RunEmbeddedMigrations(migCtx, d, migrationFS, "migrations")
	migCancel()
	if err != nil {
		log.Warn("Database migration failed, continuing with current schema", zap.Error(err))
	} else {
		log.Info("Database migrations applied")
	}
	db = pg
	sqlDB = d
	return
}

func provideContentService(rc *RouterConfig, db storage.DB, log *zap.Logger) *service.ContentService {
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

func provideObjectStorage(rc *RouterConfig, cfg *config.Config, log *zap.Logger, res *AppResources) service.SegmentStorage {
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

func provideTranscodingService(cfg *config.Config, log *zap.Logger, db storage.DB, objStorage service.SegmentStorage, res *AppResources) *service.TranscodingService {
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
	svc.StartWorker(log.Named("transcode-worker"))
	return svc
}

func provideUploadService(rc *RouterConfig, cfg *config.Config, log *zap.Logger, db storage.DB, objStorage service.SegmentStorage, transcodingSvc *service.TranscodingService) *service.UploadService {
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
	svc := service.NewUploadService(db, uploadObj, cfg.Storage.Bucket, log.Named("upload"))
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
		Bucket:         cfg.Storage.Bucket,
		Profiles:       cfg.Transcode.Profiles,
	})
	return svc
}

func provideOTelTracing(cfg *config.Config, log *zap.Logger, res *AppResources) {
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

func parseChallengeTTL(cfg *config.Config) time.Duration {
	ttl := 5 * time.Minute
	if cfg.Auth.NonceExpiry != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.NonceExpiry); err == nil && parsed > 0 {
			ttl = parsed
		}
	}
	return ttl
}
