package gateway

import (
	"context"
	"crypto/ecdsa"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/plugins/transcoder"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/service/content"
	"github.com/rtcdance/streamgate/pkg/storage"
	web3contract "github.com/rtcdance/streamgate/pkg/web3/contract"
	txtx "github.com/rtcdance/streamgate/pkg/web3/tx"

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
		// The playback capability lifetime comes from the policy rather than a
		// constant buried in the handler.
		service.WithPlaybackTokenTTL(cfg.Features.CapabilityTTL),
		// Session epochs must outlive the process: an in-memory store would
		// forget every session on restart and silently un-revoke them.
		service.WithSessionStore(provideSessionStore(log, redisClient)),
	)
}

func provideSessionStore(log *zap.Logger, redisClient *redis.Client) storage.SessionStore {
	if redisClient == nil {
		log.Warn("Redis unavailable, session epochs will not survive a restart")
		return storage.NewMemorySessionStore()
	}
	store, err := storage.NewRedisSessionStore(redisClient)
	if err != nil {
		log.Warn("Redis session store init failed, session epochs will not survive a restart", zap.Error(err))
		return storage.NewMemorySessionStore()
	}
	return store
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

// provideContentRegistry wires the on-chain content registration binding when
// a deployed ContentRegistry address is configured. Every missing piece
// (address, signer key, RPC) degrades to nil — content creation must never
// depend on the chain being reachable (the service degrades the same way at
// registration time).
func ProvideContentRegistry(cfg *config.Config, log *zap.Logger) content.ContentRegistry {
	if cfg.Web3.ContentRegistryAddress == "" {
		log.Info("On-chain content registry disabled (web3.content_registry_address not set)")
		return nil
	}
	if cfg.Web3.AnvilDeployerKey == "" {
		log.Info("On-chain content registry disabled (no deployer key to sign registration txs)")
		return nil
	}
	if cfg.Web3.EthereumRPC == "" {
		log.Info("On-chain content registry disabled (no ethereum rpc configured)")
		return nil
	}

	ethClient, err := ethclient.Dial(cfg.Web3.EthereumRPC)
	if err != nil {
		log.Warn("On-chain content registry disabled (rpc dial failed)", zap.Error(err))
		return nil
	}

	key := strings.TrimPrefix(cfg.Web3.AnvilDeployerKey, "0x")
	pk, err := crypto.HexToECDSA(key)
	if err != nil {
		log.Warn("On-chain content registry disabled (invalid deployer key)", zap.Error(err))
		ethClient.Close()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		log.Warn("On-chain content registry disabled (chain id resolve failed)", zap.Error(err))
		ethClient.Close()
		return nil
	}

	reader := web3contract.NewContractInteractor(ethClient, log.Named("registry-reader"))
	writer := txtx.NewContractWriter(txtx.ContractWriterConfig{
		Client:      txtx.NewEthClientAdapter(ethClient),
		Key:         privateKeyProvider{key: pk},
		NonceMgr:    txtx.NewNonceManager(txtx.NewEthClientAdapter(ethClient), log.Named("registry-nonce")),
		FromAddress: crypto.PubkeyToAddress(pk.PublicKey).Hex(),
		ChainID:     chainID.Int64(),
		Logger:      log.Named("registry-writer"),
	})
	binding := web3contract.NewContentRegistryBinding(cfg.Web3.ContentRegistryAddress, reader, writer, log.Named("content-registry"))
	log.Info("On-chain content registry enabled",
		zap.String("contract", cfg.Web3.ContentRegistryAddress),
		zap.Int64("chain_id", chainID.Int64()))
	return binding
}

// privateKeyProvider adapts a fixed ECDSA key to tx.KeyProvider.
type privateKeyProvider struct {
	key *ecdsa.PrivateKey
}

func (p privateKeyProvider) UseKey(fn func(*ecdsa.PrivateKey) error) error {
	return fn(p.key)
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

// provideRuleVersionStore builds the rule-version authority: Postgres owns the
// version, Redis shares the committed value across processes, and the age bound
// comes from the configured positive-cache TTL.
//
// The Redis layer is what makes a write in ANOTHER process visible; without it
// each process would only ever notice its own writes (via the local flush) or
// wait out the TTL. When Redis is unavailable the store still works, but the
// wiring says so out loud, because that is the case where a second process can
// serve the previous rule set until its copy ages out.
func provideRuleVersionStore(cfg *config.Config, log *zap.Logger, db storage.DB, redisClient *redis.Client, res *AppResources) *storage.CachingRuleVersionStore {
	authority := storage.NewPostgresRuleVersionStore(db)

	var cache storage.RuleVersionCache
	if redisClient != nil {
		redisCache, err := storage.NewRedisRuleVersionCache(redisClient)
		if err != nil {
			log.Warn("rule version cache unavailable, falling back to in-memory", zap.Error(err))
		} else {
			cache = redisCache
		}
	}
	if cache == nil {
		log.Warn("rule version cache is process-local: a write in another process will not be " +
			"visible until this process's copy ages out")
		cache = storage.NewMemoryRuleVersionCache()
	}

	store := storage.NewCachingRuleVersionStore(authority, cache, cfg.Features.PositiveCacheTTL)
	res.RuleVersionStore = store
	return store
}
