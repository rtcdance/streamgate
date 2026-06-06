package gateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

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

// serviceInit groups the internal service dependencies created by SetupRouter
// for passing to registerRoutes.
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
	DemoNFTMinter      *service.DemoNFTMinter
}

func newDemoNFTMinter(cfg *config.Config, log *zap.Logger) *service.DemoNFTMinter {
	if cfg.Web3.AnvilDeployerKey == "" || cfg.Web3.AnvilDemoContract == "" {
		log.Info("Demo NFT minter disabled (anvil_deployer_key or anvil_demo_contract not set)")
		return nil
	}
	rpcURL := cfg.Web3.EthereumRPC
	if rpcURL == "" {
		log.Warn("Demo NFT minter disabled (no ethereum rpc configured)")
		return nil
	}
	minter, err := service.NewDemoNFTMinter(rpcURL, cfg.Web3.AnvilDemoContract, cfg.Web3.AnvilDeployerKey, cfg.Web3.ChainID, log.Named("demo-nft-minter"))
	if err != nil {
		log.Warn("Demo NFT minter init failed", zap.Error(err))
		return nil
	}
	log.Info("Demo NFT minter ready", zap.String("contract", cfg.Web3.AnvilDemoContract), zap.String("from", minter.FromAddress().Hex()))
	return minter
}
