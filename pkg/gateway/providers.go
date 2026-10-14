package gateway

import (
	"context"
	"crypto/ecdsa"
	"database/sql"
	"fmt"
	"math/big"
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
	"github.com/rtcdance/streamgate/pkg/service/registry"
	"github.com/rtcdance/streamgate/pkg/storage"
	web3contract "github.com/rtcdance/streamgate/pkg/web3/contract"
	"github.com/rtcdance/streamgate/pkg/web3/event"
	txtx "github.com/rtcdance/streamgate/pkg/web3/tx"

	"github.com/go-redis/redis/v8"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

func provideRedis(cfg *config.Config, log *zap.Logger, res *AppResources) (*redis.Client, error) {
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	if cfg.IsStrictProfile() && cfg.Redis.Host == "" {
		// A strict profile must not silently fall back to memory — an in-memory
		// blacklist would let a revoked token keep working on this instance.
		return nil, fmt.Errorf("redis host is empty — %s profile requires a Redis (challenge store, token blacklist, sessions)", cfg.EffectiveProfile())
	}
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
		if cfg.IsStrictProfile() {
			_ = client.Close()
			// Fail fast: a strict profile must not silently fall back to memory
			// — a single-instance challenge store or blacklist would break
			// replay protection and cross-instance revocation.
			return nil, fmt.Errorf("redis unreachable at %s — %s profile requires a reachable Redis", redisAddr, cfg.EffectiveProfile())
		}
		log.Warn("Redis unavailable, components will use in-memory fallbacks (demo/unit profile behavior; durable and production profiles refuse this fallback)",
			zap.String("addr", redisAddr), zap.String("profile", cfg.EffectiveProfile()))
		_ = client.Close()
		return nil, nil
	}
	log.Info("Redis connected", zap.String("addr", redisAddr))
	res.SharedRedis = client
	return client, nil
}

func provideWeb3Service(rc *RouterConfig, cfg *config.Config, log *zap.Logger, nonceLedger *txtx.NonceLedger, db storage.DB, gatingRules service.GatingRuleFilterRefresher) (*service.Web3Service, error) {
	if rc.Web3Service != nil {
		return rc.Web3Service, nil
	}
	var opts []service.Web3Option
	if nonceLedger != nil {
		opts = append(opts, service.WithNonceLedger(nonceLedger))
	}
	if gatingRules != nil {
		opts = append(opts, service.WithGatingRules(gatingRules))
	}
	if db != nil {
		opts = append(opts, service.WithEventStore(event.NewPostgresEventStore(db)))
	}
	svc, err := service.NewWeb3Service(service.DefaultWeb3Deps(cfg, log), cfg, log, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Web3 service: %w", err)
	}
	return svc, nil
}

// provideNonceLedger builds the shared DB-backed nonce authority (SG-13 A.3)
// whenever a database AND a chain are configured: one tx.NonceLedger per
// process, shared by the Web3Service signing path and the dev minter, drawing
// from the SAME Postgres reservations table the registry publisher uses. It
// degrades to nil (in-memory nonce managers, previous behaviour) exactly like
// the other providers when the database or the chain is unavailable.
func provideNonceLedger(cfg *config.Config, log *zap.Logger, db storage.DB, res *AppResources) *txtx.NonceLedger {
	if db == nil {
		return nil
	}
	if cfg.Web3.ChainID <= 0 || cfg.Web3.EthereumRPC == "" {
		log.Warn("Nonce ledger disabled (no chain configured); nonce allocation stays process-local")
		return nil
	}
	ethClient, err := ethclient.Dial(cfg.Web3.EthereumRPC)
	if err != nil {
		log.Warn("Nonce ledger disabled: rpc dial failed", zap.String("rpc", cfg.Web3.EthereumRPC), zap.Error(err))
		return nil
	}
	res.NonceLedgerEth = ethClient
	log.Info("Shared nonce ledger enabled (Postgres is the single nonce authority)",
		zap.Int64("chain_id", cfg.Web3.ChainID))
	return txtx.NewNonceLedger(db, cfg.Web3.ChainID, txtx.NewEthClientAdapter(ethClient), log.Named("nonce-ledger"))
}

func provideChallengeStore(rc *RouterConfig, cfg *config.Config, log *zap.Logger, challengeTTL time.Duration, redisClient *redis.Client, res *AppResources) (storage.ChallengeStore, error) {
	if rc.ChallengeStore != nil {
		return rc.ChallengeStore, nil
	}
	if redisClient == nil {
		if cfg.IsStrictProfile() {
			// Fail fast: an in-memory challenge store is single-instance, so a
			// challenge issued by one replica could be replayed against another.
			return nil, fmt.Errorf("challenge store requires Redis — %s profile must not fall back to in-memory", cfg.EffectiveProfile())
		}
		log.Warn("Redis unavailable, falling back to in-memory challenge store (demo/unit profile behavior)",
			zap.String("profile", cfg.EffectiveProfile()))
		store := storage.NewMemoryChallengeStore()
		res.ChallengeStore = store
		return store, nil
	}
	store := storage.NewRedisChallengeStoreWithClient(redisClient, challengeTTL)
	res.ChallengeStore = store
	return store, nil
}

func provideTokenBlacklist(cfg *config.Config, log *zap.Logger, redisClient *redis.Client, res *AppResources) (storage.TokenBlacklist, error) {
	if redisClient == nil {
		if cfg.IsStrictProfile() {
			// Fail fast: a revoked token must stay revoked on every replica; a
			// process-local blacklist would keep honouring it here.
			return nil, fmt.Errorf("token blacklist requires Redis — %s profile must not fall back to in-memory", cfg.EffectiveProfile())
		}
		log.Warn("Redis unavailable, falling back to in-memory token blacklist (demo/unit profile behavior)",
			zap.String("profile", cfg.EffectiveProfile()))
		return storage.NewMemoryTokenBlacklist(), nil
	}
	rbl, err := storage.NewRedisTokenBlacklist(redisClient)
	if err != nil {
		if cfg.IsStrictProfile() {
			return nil, fmt.Errorf("redis token blacklist init failed: %w", err)
		}
		log.Warn("Redis token blacklist init failed, falling back to in-memory (demo/unit profile behavior)", zap.Error(err))
		return storage.NewMemoryTokenBlacklist(), nil
	}
	res.TokenBlacklist = rbl
	return rbl, nil
}

func provideAuthService(rc *RouterConfig, cfg *config.Config, log *zap.Logger, web3Svc *service.Web3Service, challengeStore storage.ChallengeStore, challengeTTL time.Duration, redisClient *redis.Client, res *AppResources) (*service.AuthService, error) {
	if rc.AuthService != nil {
		return rc.AuthService, nil
	}
	solanaSigner := web3Svc.GetSolanaSigner()
	signatureVerifier := service.NewMultiChainSignatureVerifier(log, solanaSigner)
	eip712Verifier := web3Svc.GetEIP712Verifier()
	tokenBlacklist, err := provideTokenBlacklist(cfg, log, redisClient, res)
	if err != nil {
		return nil, err
	}
	sessionStore, err := provideSessionStore(cfg, log, redisClient)
	if err != nil {
		return nil, err
	}

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
		// Key separation (SG-14): access JWTs and playback capabilities verify
		// under independent keys — an explicit capability key, else HKDF-derived
		// from the master secret. The access key stays the master secret when
		// unset, so tokens issued before this split keep verifying.
		service.WithTokenKeySeparation(
			cfg.EffectiveAccessSecret(),
			cfg.Auth.PreviousAccessSecrets,
			cfg.EffectiveCapabilitySecret(),
			cfg.Auth.PreviousCapabilitySecrets,
		),
		// The playback capability lifetime comes from the policy rather than a
		// constant buried in the handler.
		service.WithPlaybackTokenTTL(cfg.Features.CapabilityTTL),
		// The clock tolerance is an input to the revocation bound, so it is the
		// configured value too: a hardcoded tolerance larger than the policy would
		// make the realised window exceed the bound that was computed and
		// validated.
		service.WithClockSkew(cfg.Features.ClockSkew),
		// Session epochs must outlive the process: an in-memory store would
		// forget every session on restart and silently un-revoke them.
		service.WithSessionStore(sessionStore),
	), nil
}

func provideSessionStore(cfg *config.Config, log *zap.Logger, redisClient *redis.Client) (storage.SessionStore, error) {
	if redisClient == nil {
		if cfg.IsStrictProfile() {
			// Fail fast: session epochs must survive restarts; an in-memory
			// store would silently un-revoke every session on a bounce.
			return nil, fmt.Errorf("session store requires Redis — %s profile must not fall back to in-memory", cfg.EffectiveProfile())
		}
		log.Warn("Redis unavailable, session epochs will not survive a restart (demo/unit profile behavior)",
			zap.String("profile", cfg.EffectiveProfile()))
		return storage.NewMemorySessionStore(), nil
	}
	store, err := storage.NewRedisSessionStore(redisClient)
	if err != nil {
		if cfg.IsStrictProfile() {
			return nil, fmt.Errorf("redis session store init failed: %w", err)
		}
		log.Warn("Redis session store init failed, session epochs will not survive a restart (demo/unit profile behavior)", zap.Error(err))
		return storage.NewMemorySessionStore(), nil
	}
	return store, nil
}

func provideDatabase(cfg *config.Config, log *zap.Logger, res *AppResources) (db storage.DB, sqlDB *sql.DB, err error) {
	if cfg.IsStrictProfile() && cfg.Database.Host == "" {
		return nil, nil, fmt.Errorf("database host is empty — %s profile requires a PostgreSQL (content, gating rules, outbox)", cfg.EffectiveProfile())
	}
	dbConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.User, cfg.Database.Password,
		cfg.Database.Database, cfg.Database.SSLMode)
	d, openErr := sql.Open("postgres", dbConnStr)
	if openErr != nil {
		if cfg.IsStrictProfile() {
			// Fail fast: without Postgres there is no content, no gating rules
			// and no durable outbox — a strict profile must not pretend otherwise.
			return nil, nil, fmt.Errorf("database open failed: %w", openErr)
		}
		log.Warn("Database unavailable, using in-memory fallback (demo/unit profile behavior)",
			zap.String("host", cfg.Database.Host),
			zap.String("database", cfg.Database.Database))
		return nil, nil, nil
	}
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if pingErr := d.PingContext(pingCtx); pingErr != nil {
		if cfg.IsStrictProfile() {
			_ = d.Close()
			return nil, nil, fmt.Errorf("database unreachable at %s — %s profile requires a reachable PostgreSQL: %w",
				cfg.Database.Host, cfg.EffectiveProfile(), pingErr)
		}
		log.Warn("Database ping failed, using in-memory fallback (demo/unit profile behavior)",
			zap.String("host", cfg.Database.Host),
			zap.String("database", cfg.Database.Database))
		_ = d.Close()
		return nil, nil, nil
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
	migErr := storage.RunEmbeddedMigrations(migCtx, d, migrationFS, "migrations")
	migCancel()
	if migErr != nil {
		log.Warn("Database migration failed, continuing with current schema", zap.Error(migErr))
	} else {
		log.Info("Database migrations applied")
	}
	db = pg
	sqlDB = d
	return db, sqlDB, nil
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
	binding, _, _, _, err := buildContentRegistryBinding(cfg, log)
	if err != nil {
		log.Info("On-chain content registry disabled", zap.Error(err))
		return nil
	}
	return binding
}

// buildContentRegistryBinding performs the shared validation/dial and returns
// the binding plus the pieces a durable registry hub also needs (chain client,
// signer key, chain id).
func buildContentRegistryBinding(cfg *config.Config, log *zap.Logger) (*web3contract.ContentRegistryBinding, *ethclient.Client, *ecdsa.PrivateKey, *big.Int, error) {
	if cfg.Web3.ContentRegistryAddress == "" {
		return nil, nil, nil, nil, fmt.Errorf("web3.content_registry_address not set")
	}
	if cfg.Web3.AnvilDeployerKey == "" {
		return nil, nil, nil, nil, fmt.Errorf("no deployer key to sign registration txs")
	}
	if cfg.Web3.EthereumRPC == "" {
		return nil, nil, nil, nil, fmt.Errorf("no ethereum rpc configured")
	}

	ethClient, err := ethclient.Dial(cfg.Web3.EthereumRPC)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("rpc dial failed: %w", err)
	}

	key := strings.TrimPrefix(cfg.Web3.AnvilDeployerKey, "0x")
	pk, err := crypto.HexToECDSA(key)
	if err != nil {
		ethClient.Close()
		return nil, nil, nil, nil, fmt.Errorf("invalid deployer key: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		ethClient.Close()
		return nil, nil, nil, nil, fmt.Errorf("chain id resolve failed: %w", err)
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
	return binding, ethClient, pk, chainID, nil
}

// RegistryHub is the durable on-chain registration stack (SG-13 A): the
// binding, the Postgres-backed intent/nonce/attempt stores, the HTTP
// idempotency store, the attest service and the publisher loop. It exists ONLY
// when the registry is enabled AND a database is available — durable intents
// are a hard requirement, so there is no memory fallback.
type RegistryHub struct {
	Binding     *web3contract.ContentRegistryBinding
	Store       *registry.Store
	Idempotency *registry.IdempotencyStore
	Service     *registry.Service
	Publisher   *registry.Publisher
}

// Attest satisfies the gateway's ContentAttester (nil-safe delegation).
func (h *RegistryHub) Attest(ctx context.Context, actor, contentID, idemKey string) (*registry.AttestResponse, bool, error) {
	if h == nil || h.Service == nil {
		return nil, false, registry.ErrRegistryMisconfigured
	}
	return h.Service.Attest(ctx, actor, contentID, idemKey)
}

// Close stops the publisher loop.
func (h *RegistryHub) Close() {
	if h == nil || h.Publisher == nil {
		return
	}
	h.Publisher.Stop()
}

// ProvideRegistryHub builds the full durable registry stack. Returning nil
// (registry disabled) is the same degradation ProvideContentRegistry performs,
// but here a missing DATABASE also disables it: an intent that cannot be
// persisted must not be broadcast (A.4 — database unwritable ⇒ do not
// broadcast).
func ProvideRegistryHub(cfg *config.Config, log *zap.Logger, db storage.DB) *RegistryHub {
	binding, ethClient, pk, chainID, err := buildContentRegistryBinding(cfg, log)
	if err != nil {
		log.Info("On-chain content registry disabled", zap.Error(err))
		return nil
	}
	if db == nil {
		log.Warn("On-chain content registry disabled (no database: durable register intents require Postgres)")
		ethClient.Close()
		return nil
	}

	adapter := txtx.NewEthClientAdapter(ethClient)
	// WithNonceClient makes the store's SHARED tx.NonceLedger able to prime a
	// first allocation from the chain and to adjudicate releasing a nonce a
	// signed attempt references — the same client the publisher broadcasts
	// with, so allocation and adjudication see one chain view.
	store := registry.NewStore(db, chainID.Int64(), cfg.Web3.ContentRegistryAddress, registry.WithNonceClient(adapter))
	idem := registry.NewIdempotencyStore(db)
	svc := registry.NewService(store, idem)
	publisher := registry.NewPublisher(store, adapter, binding, registry.PublisherConfig{
		Signer:  crypto.PubkeyToAddress(pk.PublicKey).Hex(),
		ChainID: chainID.Int64(),
	}, log)

	log.Info("On-chain content registry enabled (durable intents)",
		zap.String("contract", cfg.Web3.ContentRegistryAddress),
		zap.Int64("chain_id", chainID.Int64()))
	return &RegistryHub{
		Binding:     binding,
		Store:       store,
		Idempotency: idem,
		Service:     svc,
		Publisher:   publisher,
	}
}

// privateKeyProvider adapts a fixed ECDSA key to tx.KeyProvider.
type privateKeyProvider struct {
	key *ecdsa.PrivateKey
}

func (p privateKeyProvider) UseKey(fn func(*ecdsa.PrivateKey) error) error {
	return fn(p.key)
}

// BucketProbe returns the prober for readiness checks: the concrete MinIO
// client when one was built, else nil (injected storage that does not probe
// simply has no storage readiness check).
func (r *AppResources) BucketProbe() bucketProber {
	if r.ObjStorage == nil {
		return nil
	}
	if p, ok := r.ObjStorage.(bucketProber); ok {
		return p
	}
	return nil
}

func provideObjectStorage(rc *RouterConfig, cfg *config.Config, log *zap.Logger, res *AppResources) (service.SegmentStorage, error) {
	if rc.SegmentStorage != nil {
		return rc.SegmentStorage, nil
	}
	minioCfg := storage.MinIOConfig{
		Endpoint:        cfg.Storage.Endpoint,
		AccessKeyID:     cfg.Storage.AccessKey,
		SecretAccessKey: cfg.Storage.SecretKey,
		UseSSL:          cfg.Storage.UseSSL,
	}
	ms, err := storage.NewMinIOStorage(minioCfg)
	if err != nil {
		if cfg.IsStrictProfile() {
			return nil, fmt.Errorf("minio client init failed at %s — %s profile requires private object storage: %w",
				cfg.Storage.Endpoint, cfg.EffectiveProfile(), err)
		}
		log.Warn("MinIO unavailable, segment serving disabled (demo/unit profile behavior)",
			zap.String("profile", cfg.EffectiveProfile()), zap.Error(err))
		return nil, nil
	}
	res.ObjStorage = ms
	bucketCtx, bucketCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bucketCancel()
	if err := ms.CreateBucket(bucketCtx, "streamgate"); err != nil {
		if cfg.IsStrictProfile() {
			// Without the media bucket there is no place to serve segments from,
			// so a strict profile refuses to start rather than serving 404s.
			_ = ms.Close()
			return nil, fmt.Errorf("object storage bucket unavailable at %s — %s profile requires it: %w",
				cfg.Storage.Endpoint, cfg.EffectiveProfile(), err)
		}
		log.Warn("Failed to create streamgate bucket (demo/unit profile behavior)",
			zap.String("profile", cfg.EffectiveProfile()), zap.Error(err))
	}
	log.Info("MinIO storage initialized", zap.String("endpoint", cfg.Storage.Endpoint))
	return ms, nil
}

// natsQueueDialer connects the NATS JetStream transcoding queue. It is a
// package variable so tests can substitute an unreachable dialer without a
// live broker.
var natsQueueDialer = storage.NewNATSTranscodingQueue

func provideTranscodingService(cfg *config.Config, log *zap.Logger, db storage.DB, objStorage service.SegmentStorage, res *AppResources) (*service.TranscodingService, error) {
	ffmpegCfg := &transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     30 * time.Minute,
		// Media gate ceilings for untrusted originals, from config.
		MaxFileSize: cfg.Transcoding.MaxInputSize,
		MaxDuration: float64(cfg.Transcoding.MaxDurationSeconds),
		MaxWidth:    cfg.Transcoding.MaxWidth,
		MaxHeight:   cfg.Transcoding.MaxHeight,
		Threads:     cfg.Transcoding.FFmpegThreads,
	}
	ft := transcoder.NewFFmpegTranscoder(ffmpegCfg, log.Named("ffmpeg"))
	videoTranscoder := transcoder.NewVideoTranscoder(ft, log.Named("ffmpeg"))

	if cfg.IsStrictProfile() && cfg.NATS.URL == "" {
		return nil, fmt.Errorf("nats.url is empty — %s profile requires a durable queue (transcoding tasks)", cfg.EffectiveProfile())
	}
	var transcodingQueue service.TranscodingQueue
	nq, err := natsQueueDialer(cfg.NATS.URL, log.Named("nats-queue"))
	if err != nil {
		if cfg.IsStrictProfile() {
			// Fail fast: an in-memory queue loses every pending task on
			// restart — a strict profile must not silently downgrade durability.
			return nil, fmt.Errorf("nats unavailable at %s — %s profile requires a durable transcoding queue: %w", cfg.NATS.URL, cfg.EffectiveProfile(), err)
		}
		log.Warn("NATS unavailable, falling back to in-memory transcoding queue (demo/unit profile behavior)",
			zap.String("profile", cfg.EffectiveProfile()), zap.Error(err))
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
		// An original is untrusted media written to disk and handed to FFmpeg, so
		// the ceiling comes from config rather than from whatever the upload
		// endpoint happens to allow.
		service.WithMaxInputBytes(cfg.Transcoding.MaxInputSize),
		// Worker, queue and per-owner bounds keep saturation retryable
		// (429/503 + Retry-After) instead of a bare 500.
		service.WithMinWorkers(1),
		service.WithMaxWorkers(cfg.Transcoding.MaxWorkers),
		service.WithQueueLimit(cfg.Transcoding.MaxQueuePending),
		service.WithOwnerTaskLimit(cfg.Transcoding.MaxOwnerPending),
	)
	// The role of THIS process, decided by transcoding.enabled: when the flag
	// is off the process still submits (task row + outbox intent in one
	// transaction, published to JetStream) but must not parse untrusted media —
	// the external transcoder worker does that in its own trust domain
	// (TRANSITION_PLAN.md SG-12 step 5). The outbox pump runs in both shapes:
	// StartWorker drives it from the worker context, and the submitter-only
	// profile starts it standalone so a crash between commit and publish
	// cannot strand an intent.
	if cfg.Transcoding.Enabled {
		svc.StartWorker(log.Named("transcode-worker"))
		log.Info("transcoding role: submitter+worker (embedded worker enabled)")
	} else {
		svc.StartOutboxPublisher(log.Named("transcode-outbox"))
		log.Info("transcoding role: submitter only (embedded worker disabled; tasks execute on the external transcoder worker)")
	}
	return svc, nil
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
func provideRuleVersionStore(cfg *config.Config, log *zap.Logger, db storage.DB, redisClient *redis.Client, res *AppResources) (*storage.CachingRuleVersionStore, error) {
	authority := storage.NewPostgresRuleVersionStore(db)

	var cache storage.RuleVersionCache
	if redisClient != nil {
		redisCache, err := storage.NewRedisRuleVersionCache(redisClient)
		if err != nil {
			if cfg.IsStrictProfile() {
				return nil, fmt.Errorf("rule version cache unavailable: %w", err)
			}
			log.Warn("rule version cache unavailable, falling back to in-memory (demo/unit profile behavior)", zap.Error(err))
		} else {
			cache = redisCache
		}
	}
	if cache == nil {
		if cfg.IsStrictProfile() {
			// Fail fast: without a shared cache a rule change in another
			// process stays invisible here until the local copy ages out.
			return nil, fmt.Errorf("rule version cache requires Redis — %s profile must not fall back to in-memory", cfg.EffectiveProfile())
		}
		log.Warn("rule version cache is process-local: a write in another process will not be "+
			"visible until this process's copy ages out (demo/unit profile behavior)",
			zap.String("profile", cfg.EffectiveProfile()))
		cache = storage.NewMemoryRuleVersionCache()
	}

	store := storage.NewCachingRuleVersionStore(authority, cache, cfg.Features.PositiveCacheTTL)
	res.RuleVersionStore = store
	return store, nil
}
