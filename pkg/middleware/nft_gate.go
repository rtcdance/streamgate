package middleware

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

var (
	nftVerifyTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "streamgate_nft_verify_total",
		Help: "Total NFT ownership verification requests",
	}, []string{"chain_id", "result"})

	nftVerifyDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "streamgate_nft_verify_duration_seconds",
		Help:    "NFT verification latency in seconds",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	}, []string{"chain_id"})

	nftVerifyReorgInvalidated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "streamgate_nft_verify_reorg_invalidated_total",
		Help: "Total cached NFT verifications invalidated by reorg",
	})

	nftCacheHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "streamgate_nft_cache_hits_total",
		Help: "NFT cache hits by tier",
	}, []string{"tier"})

	nftBlockHashCacheHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "streamgate_nft_block_hash_cache_hits_total",
		Help: "Block hash verification cache hits (RPC call avoided)",
	})

	nftSF singleflight.Group
)

func init() {
	for _, c := range []prometheus.Collector{nftVerifyTotal, nftVerifyDuration, nftVerifyReorgInvalidated, nftCacheHits, nftBlockHashCacheHits} {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}
}

type NFTOwnershipChecker interface {
	VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error)
	VerifyNFTOwnershipAutoDetect(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	VerifyNFTCollectionAutoDetect(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (bool, error)
	GetNFTInfo(ctx context.Context, chainID int64, contractAddress, tokenID string) (*NFTMetadata, error)
}

type NFTMetadata struct {
	Name            string
	TokenURI        string
	ContractAddress string
	TokenID         string
}

type BlockProver interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*BlockHeaderInfo, error)
}

type BlockHeaderInfo struct {
	Number     uint64
	Hash       string
	ParentHash string
}

type GatingRuleResolver interface {
	GetActiveRulesForContent(ctx context.Context, contentID string) ([]GatingRule, error)
}

type GatingRule struct {
	ContractAddress string
	TokenID         string
	ChainID         int64
	Standard        string
	MinBalance      int
}

type NFTGateConfig struct {
	Verifier           NFTOwnershipChecker
	BlockProver        BlockProver
	Cache              NFTAccessCache
	RuleResolver       GatingRuleResolver
	AuditLogger        storage.AuditLogger
	CircuitBreaker     *CircuitBreaker
	BlockVerifyCache   *BlockHashCache
	DefaultChainID     int64
	CacheTTL           time.Duration
	ReorgTTL           time.Duration
	BlockVerifyTTL     time.Duration
	MarketplaceURL     string
	BlockTag           web3.BlockTag
	AutoDetectStandard bool
	Enabled            atomic.Bool // atomic for safe concurrent runtime toggling
	reorgActive        atomic.Bool
	reorgDetectedAt    atomic.Int64
}

type blockHashEntry struct {
	hash      string
	expiresAt time.Time
}

type BlockHashCache struct {
	mu         sync.RWMutex
	entries    map[uint64]blockHashEntry
	ttl        time.Duration
	maxEntries int
}

func NewBlockHashCache(ttl ...time.Duration) *BlockHashCache {
	d := 5 * time.Minute
	if len(ttl) > 0 && ttl[0] > 0 {
		d = ttl[0]
	}
	return &BlockHashCache{entries: make(map[uint64]blockHashEntry), ttl: d, maxEntries: 2048}
}

func (bhc *BlockHashCache) Get(blockNumber uint64) (string, bool) {
	bhc.mu.RLock()
	entry, ok := bhc.entries[blockNumber]
	bhc.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.hash, true
}

func (bhc *BlockHashCache) Set(blockNumber uint64, hash string) {
	bhc.mu.Lock()
	defer bhc.mu.Unlock()
	if bhc.entries == nil {
		bhc.entries = make(map[uint64]blockHashEntry)
	}
	bhc.entries[blockNumber] = blockHashEntry{hash: hash, expiresAt: time.Now().Add(bhc.ttl)}
	if len(bhc.entries) > bhc.maxEntries {
		now := time.Now()
		for k, v := range bhc.entries {
			if now.After(v.expiresAt) {
				delete(bhc.entries, k)
				if len(bhc.entries) <= bhc.maxEntries {
					break
				}
			}
		}
		if len(bhc.entries) > bhc.maxEntries {
			clear(bhc.entries)
		}
	}
}

func NFTGateMiddleware(config *NFTGateConfig, logger *zap.Logger) gin.HandlerFunc {
	if config.CacheTTL == 0 {
		config.CacheTTL = 60 * time.Second
	}
	if config.ReorgTTL == 0 {
		config.ReorgTTL = 5 * time.Second
	}
	if config.BlockVerifyTTL == 0 {
		config.BlockVerifyTTL = 5 * time.Second
	}
	if config.BlockVerifyCache == nil {
		config.BlockVerifyCache = NewBlockHashCache()
	}
	config.Enabled.Store(true)

	return func(c *gin.Context) {
		if !config.Enabled.Load() {
			c.Next()
			return
		}

		walletAddress := GetWalletAddress(c)
		if walletAddress == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required", "code": "UNAUTHORIZED"})
			return
		}

		contract, chainID, tokenID, contentID := parseNFTParams(c, config)

		resolvedRules, minBalance, errResp := resolveNFTGateRules(c, config, logger, contentID, &contract, &tokenID, &chainID)
		if errResp != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, errResp)
			return
		}

		if !common.IsHexAddress(contract) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "invalid contract address format",
				"code":  "INVALID_CONTRACT",
			})
			return
		}

		autoDetect := resolveAutoDetect(c, config, contract, resolvedRules)

		cacheKey := nftCacheKey(chainID, walletAddress, contract, tokenID)
		hasNFT, err := resolveOwnershipWithAutoDetect(c.Request.Context(), config, logger, cacheKey, chainID, contract, tokenID, walletAddress, minBalance, autoDetect)
		if err != nil {
			logger.Error("NFT verification failed", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":      "verification service unavailable",
				"code":       "NFT_VERIFY_ERROR",
				"chain_id":   chainID,
				"chain_name": chainName(chainID),
			})
			return
		}

		if !hasNFT {
			hasNFT, contract, tokenID, chainID = tryFallbackGatingRules(c, config, logger, walletAddress, contract, tokenID, chainID)
		}

		if !hasNFT {
			nftGateDenied(c, config, walletAddress, contract, tokenID, chainID, contentID)
			return
		}

		c.Set("nft_verified", true)
		c.Set("wallet_address", walletAddress)
		c.Set("nft_contract", contract)
		c.Set("nft_chain_id", chainID)
		if config.AuditLogger != nil {
			config.AuditLogger.Log(c.Request.Context(), "nft.gate_passed", walletAddress, "content", contentID, true, "", fmt.Sprintf("%s:%d", contract, chainID))
		}
		c.Next()
	}
}

func parseNFTParams(c *gin.Context, config *NFTGateConfig) (contract string, chainID int64, tokenID, contentID string) {
	contract = c.Query("contract")
	if contract == "" {
		contract = c.Query("contract_address")
	}
	chainID = config.DefaultChainID
	if raw := c.Query("chain_id"); raw != "" {
		if parsed, err := parseInt64(raw); err == nil {
			chainID = parsed
		}
	}
	tokenID = c.Query("token_id")
	contentID = c.Param("id")
	return
}

func resolveNFTGateRules(c *gin.Context, config *NFTGateConfig, logger *zap.Logger, contentID string, contract, tokenID *string, chainID *int64) ([]GatingRule, int, gin.H) {
	var resolvedRules []GatingRule
	if contentID != "" && config.RuleResolver != nil {
		rules, err := config.RuleResolver.GetActiveRulesForContent(c.Request.Context(), contentID)
		if err != nil {
			logger.Error("failed to resolve gating rules", zap.String("content_id", contentID), zap.Error(err))
		} else {
			resolvedRules = rules
			c.Set("gating_rules", rules)
			c.Set("gating_rule_id", contentID)
			c.Set("gating_rules_count", len(rules))
		}
	}

	var minBalance int
	if *contract == "" && len(resolvedRules) > 0 {
		rule := resolvedRules[0]
		*contract = rule.ContractAddress
		*tokenID = rule.TokenID
		*chainID = rule.ChainID
		minBalance = rule.MinBalance
	}

	if *contract == "" {
		return nil, 0, gin.H{
			"error": "contract address is required",
			"code":  "MISSING_CONTRACT",
			"hint":  "provide 'contract' query parameter with the NFT contract address",
		}
	}
	return resolvedRules, minBalance, nil
}

func resolveAutoDetect(c *gin.Context, config *NFTGateConfig, contract string, resolvedRules []GatingRule) bool {
	autoDetect := config.AutoDetectStandard
	if config.RuleResolver == nil {
		return autoDetect
	}
	// resolvedRules already fetched by resolveNFTGateRules — no need to re-fetch
	for _, r := range resolvedRules {
		if r.ContractAddress == contract && r.Standard != "" && r.Standard != "erc721" {
			return true
		}
	}
	return autoDetect
}

func tryFallbackGatingRules(c *gin.Context, config *NFTGateConfig, logger *zap.Logger, walletAddress, contract, tokenID string, chainID int64) (bool, string, string, int64) {
	if config.RuleResolver == nil {
		return false, contract, tokenID, chainID
	}
	contentID := c.Param("id")
	if contentID == "" {
		return false, contract, tokenID, chainID
	}
	rules, rErr := config.RuleResolver.GetActiveRulesForContent(c.Request.Context(), contentID)
	if rErr != nil || len(rules) <= 1 {
		return false, contract, tokenID, chainID
	}
	for _, rule := range rules[1:] {
		ruleCacheKey := nftCacheKey(rule.ChainID, walletAddress, rule.ContractAddress, rule.TokenID)
		altHasNFT, altErr := resolveOwnership(c.Request.Context(), config, logger, ruleCacheKey, rule.ChainID, rule.ContractAddress, rule.TokenID, walletAddress, rule.MinBalance)
		if altErr == nil && altHasNFT {
			return true, rule.ContractAddress, rule.TokenID, rule.ChainID
		}
	}
	return false, contract, tokenID, chainID
}

func nftGateDenied(c *gin.Context, config *NFTGateConfig, walletAddress, contract, tokenID string, chainID int64, contentID string) {
	if config.AuditLogger != nil {
		config.AuditLogger.Log(c.Request.Context(), "nft.gate_denied", walletAddress, "content", contentID, false, "nft_access_denied", contract)
	}
	resp := gin.H{
		"error": "nft access denied",
		"code":  "NFT_REQUIRED",
		"required_nft": gin.H{
			"contract":   contract,
			"chain_id":   chainID,
			"chain_name": chainName(chainID),
		},
	}
	if tokenID != "" {
		resp["required_nft"].(gin.H)["token_id"] = tokenID
	}
	if config.MarketplaceURL != "" {
		url := strings.ReplaceAll(config.MarketplaceURL, "{contract}", contract)
		url = strings.ReplaceAll(url, "{token_id}", tokenID)
		resp["required_nft"].(gin.H)["marketplace_url"] = url
	}
	c.AbortWithStatusJSON(http.StatusForbidden, resp)
}

func resolveOwnership(ctx context.Context, config *NFTGateConfig, logger *zap.Logger, cacheKey string, chainID int64, contract, tokenID, walletAddress string, minBalance int) (bool, error) {
	return resolveOwnershipWithAutoDetect(ctx, config, logger, cacheKey, chainID, contract, tokenID, walletAddress, minBalance, config.AutoDetectStandard)
}

func resolveOwnershipWithAutoDetect(ctx context.Context, config *NFTGateConfig, logger *zap.Logger, cacheKey string, chainID int64, contract, tokenID, walletAddress string, minBalance int, autoDetect bool) (bool, error) {
	start := time.Now()

	if config.Cache != nil {
		if entry, ok := config.Cache.Get(ctx, cacheKey); ok {
			if entry.Expires.After(time.Now()) {
				if entry.BlockHash != "" && config.BlockProver != nil {
					if cachedHash, ok := config.BlockVerifyCache.Get(entry.BlockNumber); ok && cachedHash == entry.BlockHash {
						nftBlockHashCacheHits.Inc()
						nftCacheHits.WithLabelValues("l1_l2").Inc()
						return entry.HasNFT, nil
					}
					header, err := config.BlockProver.HeaderByNumber(ctx, big.NewInt(int64(entry.BlockNumber)))
					if err == nil && header != nil {
						config.BlockVerifyCache.Set(entry.BlockNumber, header.Hash)
						if header.Hash != entry.BlockHash {
							nftVerifyReorgInvalidated.Inc()
							logger.Warn("NFT cache entry invalidated by reorg",
								zap.String("cache_key", cacheKey),
								zap.Uint64("block", entry.BlockNumber),
								zap.String("cached_hash", entry.BlockHash),
								zap.String("actual_hash", header.Hash))
							config.reorgActive.Store(true)
							config.reorgDetectedAt.Store(time.Now().UnixNano())
						} else {
							nftCacheHits.WithLabelValues("l1_l2").Inc()
							return entry.HasNFT, nil
						}
					} else {
						logger.Warn("Block prover RPC failed during reorg check, using degraded cache TTL",
							zap.Uint64("block", entry.BlockNumber),
							zap.Error(err))
						degradedTTL := config.ReorgTTL
						if degradedTTL == 0 {
							degradedTTL = 5 * time.Second
						}
						degradedExpiry := entry.Expires.Add(-config.CacheTTL).Add(degradedTTL)
						if time.Now().Before(degradedExpiry) {
							nftCacheHits.WithLabelValues("l1_l2_degraded").Inc()
							return entry.HasNFT, nil
						}
						logger.Warn("Degraded cache TTL expired, refusing to serve stale NFT verification",
							zap.String("cache_key", cacheKey))
						return false, fmt.Errorf("block prover unavailable and degraded cache expired for chain %d", chainID)
					}
				} else {
					nftCacheHits.WithLabelValues("l1_l2").Inc()
					return entry.HasNFT, nil
				}
			}
		}
	}

	v, err, _ := nftSF.Do(cacheKey, func() (interface{}, error) {
		var hasNFT bool
		var verifyErr error
		var balance *big.Int

		if config.CircuitBreaker != nil && !config.CircuitBreaker.Allow() {
			return false, fmt.Errorf("nft verification circuit breaker is open for chain %d", chainID)
		}

		if tokenID != "" {
			if autoDetect {
				hasNFT, verifyErr = config.Verifier.VerifyNFTOwnershipAutoDetect(ctx, chainID, contract, tokenID, walletAddress)
			} else {
				hasNFT, verifyErr = config.Verifier.VerifyNFTOwnership(ctx, chainID, contract, tokenID, walletAddress)
			}
			nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), boolStr(hasNFT && verifyErr == nil)).Inc()
			nftVerifyDuration.WithLabelValues(fmt.Sprintf("%d", chainID)).Observe(time.Since(start).Seconds())
			if verifyErr != nil {
				if config.CircuitBreaker != nil {
					config.CircuitBreaker.RecordFailure()
				}
				return false, verifyErr
			}
			if config.CircuitBreaker != nil {
				config.CircuitBreaker.RecordSuccess()
			}
			if config.Cache != nil {
				setCachedEntry(ctx, config, logger, cacheKey, hasNFT, big.NewInt(1))
			}
			return hasNFT, nil
		}

		if autoDetect {
			hasNFT, verifyErr = config.Verifier.VerifyNFTCollectionAutoDetect(ctx, chainID, contract, walletAddress)
		} else {
			balance, verifyErr = config.Verifier.GetNFTBalance(ctx, chainID, contract, walletAddress)
			if minBalance > 1 {
				hasNFT = balance != nil && balance.Cmp(big.NewInt(int64(minBalance))) >= 0
			} else {
				hasNFT = balance != nil && balance.Sign() > 0
			}
		}
		nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), boolStr(hasNFT && verifyErr == nil)).Inc()
		nftVerifyDuration.WithLabelValues(fmt.Sprintf("%d", chainID)).Observe(time.Since(start).Seconds())
		if verifyErr != nil {
			if config.CircuitBreaker != nil {
				config.CircuitBreaker.RecordFailure()
			}
			return false, verifyErr
		}
		if config.CircuitBreaker != nil {
			config.CircuitBreaker.RecordSuccess()
		}
		if config.Cache != nil {
			cacheBalance := balance
			if cacheBalance == nil && hasNFT {
				cacheBalance = big.NewInt(1)
			}
			setCachedEntry(ctx, config, logger, cacheKey, hasNFT, cacheBalance)
		}
		return hasNFT, nil
	})
	if err != nil {
		return false, err
	}
	return v.(bool), nil
}

func setCachedEntry(ctx context.Context, config *NFTGateConfig, logger *zap.Logger, key string, hasNFT bool, balance *big.Int) {
	var blockNumber uint64
	var blockHash string
	if config.BlockProver != nil {
		header, err := config.BlockProver.HeaderByNumber(ctx, nil)
		if err == nil && header != nil {
			blockNumber = header.Number
			blockHash = header.Hash
		} else {
			logger.Debug("Failed to fetch block header for cache entry", zap.Error(err))
		}
	}
	ttl := config.CacheTTL
	if hasNFT {
		positiveTTL := config.CacheTTL / 4
		if positiveTTL < 5*time.Second {
			positiveTTL = 5 * time.Second
		}
		if positiveTTL > 15*time.Second {
			positiveTTL = 15 * time.Second
		}
		ttl = positiveTTL
	}
	if config.reorgActive.Load() && config.ReorgTTL > 0 {
		detectedAt := time.Unix(0, config.reorgDetectedAt.Load())
		if time.Since(detectedAt) < config.CacheTTL {
			ttl = config.ReorgTTL
		} else {
			config.reorgActive.Store(false)
		}
	}
	config.Cache.Set(ctx, key, NFTAccessEntry{
		HasNFT:      hasNFT,
		Balance:     balance,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		Expires:     time.Now().Add(ttl),
	})
}

func GetNFTVerified(c *gin.Context) bool {
	v, _ := c.Get("nft_verified")
	verified, _ := v.(bool)
	return verified
}

func GetNFTContract(c *gin.Context) string {
	v, _ := c.Get("nft_contract")
	contract, _ := v.(string)
	return contract
}

func nftCacheKey(chainID int64, wallet, contract, tokenID string) string {
	if tokenID == "" {
		tokenID = "__collection__"
	}
	return fmt.Sprintf("%d:%s:%s:%s", chainID, wallet, contract, tokenID)
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func chainName(chainID int64) string {
	if cfg, ok := web3.GetChainConfig(chainID); ok {
		return cfg.Name
	}
	return fmt.Sprintf("Chain %d", chainID)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
