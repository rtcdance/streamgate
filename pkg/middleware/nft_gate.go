package middleware

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"streamgate/pkg/web3"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	nftVerifyTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "streamgate_nft_verify_total",
		Help: "Total NFT ownership verification requests",
	}, []string{"chain_id", "result"})

	nftVerifyDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "streamgate_nft_verify_duration_seconds",
		Help:    "NFT verification latency in seconds",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	}, []string{"chain_id"})

	nftVerifyReorgInvalidated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "streamgate_nft_verify_reorg_invalidated_total",
		Help: "Total cached NFT verifications invalidated by reorg",
	})

	nftCacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "streamgate_nft_cache_hits_total",
		Help: "NFT cache hits by tier",
	}, []string{"tier"})
)

type NFTOwnershipChecker interface {
	VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error)
	VerifyNFTOwnershipAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error)
	VerifyNFTCollectionAutoDetect(ctx context.Context, contractAddress, ownerAddress string) (bool, error)
}

type BlockProver interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*BlockHeaderInfo, error)
}

type BlockHeaderInfo struct {
	Number     uint64
	Hash       string
	ParentHash string
}

type NFTGateConfig struct {
	Verifier            NFTOwnershipChecker
	BlockProver         BlockProver
	Cache               NFTAccessCache
	DefaultChainID      int64
	CacheTTL            time.Duration
	ReorgTTL            time.Duration
	MarketplaceURL      string
	BlockTag            web3.BlockTag
	AutoDetectStandard  bool
	reorgActive         bool
	reorgDetectedAt     time.Time
}

func NFTGateMiddleware(config NFTGateConfig, logger *zap.Logger) gin.HandlerFunc {
	if config.CacheTTL == 0 {
		config.CacheTTL = 60 * time.Second
	}
	if config.ReorgTTL == 0 {
		config.ReorgTTL = 5 * time.Second
	}

	return func(c *gin.Context) {
		walletAddress := GetWalletAddress(c)
		if walletAddress == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required", "code": "UNAUTHORIZED"})
			return
		}

		contract := c.Query("contract")
		if contract == "" {
			contract = c.Query("contract_address")
		}
		if contract == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "contract address is required",
				"code":  "MISSING_CONTRACT",
				"hint":  "provide 'contract' query parameter with the NFT contract address",
			})
			return
		}

		chainID := config.DefaultChainID
		if raw := c.Query("chain_id"); raw != "" {
			if parsed, err := parseInt64(raw); err == nil {
				chainID = parsed
			}
		}
		tokenID := c.Query("token_id")
		cacheKey := nftCacheKey(chainID, walletAddress, contract, tokenID)

		hasNFT, err := resolveOwnership(c.Request.Context(), &config, logger, cacheKey, chainID, contract, tokenID, walletAddress)
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
			return
		}

		c.Set("nft_verified", true)
		c.Set("nft_contract", contract)
		c.Set("nft_chain_id", chainID)
		c.Next()
	}
}

func resolveOwnership(ctx context.Context, config *NFTGateConfig, logger *zap.Logger, cacheKey string, chainID int64, contract, tokenID, walletAddress string) (bool, error) {
	start := time.Now()

	if config.Cache != nil {
		if entry, ok := config.Cache.Get(cacheKey); ok {
			if entry.Expires.After(time.Now()) {
				if entry.BlockHash != "" && config.BlockProver != nil {
					header, err := config.BlockProver.HeaderByNumber(ctx, big.NewInt(int64(entry.BlockNumber)))
					if err == nil && header != nil && header.Hash != entry.BlockHash {
						nftVerifyReorgInvalidated.Inc()
						logger.Warn("NFT cache entry invalidated by reorg",
							zap.String("cache_key", cacheKey),
							zap.Uint64("block", entry.BlockNumber),
							zap.String("cached_hash", entry.BlockHash),
							zap.String("actual_hash", header.Hash))
						config.reorgActive = true
						config.reorgDetectedAt = time.Now()
						goto verifyChain
					}
				}
				nftCacheHits.WithLabelValues("l1_l2").Inc()
				return entry.HasNFT, nil
			}
		}
	}

verifyChain:
	var hasNFT bool
	var err error
	var balance *big.Int

	if tokenID != "" {
		if config.AutoDetectStandard {
			hasNFT, err = config.Verifier.VerifyNFTOwnershipAutoDetect(ctx, contract, tokenID, walletAddress)
		} else {
			hasNFT, err = config.Verifier.VerifyNFTOwnership(ctx, chainID, contract, tokenID, walletAddress)
		}
		nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), boolStr(hasNFT && err == nil)).Inc()
		nftVerifyDuration.WithLabelValues(fmt.Sprintf("%d", chainID)).Observe(time.Since(start).Seconds())
		if err != nil {
			return false, err
		}
		if config.Cache != nil {
			setCachedEntry(ctx, config, logger, cacheKey, hasNFT, big.NewInt(1))
		}
		return hasNFT, nil
	}

	if config.AutoDetectStandard {
		hasNFT, err = config.Verifier.VerifyNFTCollectionAutoDetect(ctx, contract, walletAddress)
	} else {
		balance, err = config.Verifier.GetNFTBalance(ctx, chainID, contract, walletAddress)
		hasNFT = balance != nil && balance.Sign() > 0
	}
	nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), boolStr(hasNFT && err == nil)).Inc()
	nftVerifyDuration.WithLabelValues(fmt.Sprintf("%d", chainID)).Observe(time.Since(start).Seconds())
	if err != nil {
		return false, err
	}
	if config.Cache != nil {
		cacheBalance := balance
		if cacheBalance == nil && hasNFT {
			cacheBalance = big.NewInt(1)
		}
		setCachedEntry(ctx, config, logger, cacheKey, hasNFT, cacheBalance)
	}
	return hasNFT, nil
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
	if config.reorgActive && config.ReorgTTL > 0 {
		if time.Since(config.reorgDetectedAt) < config.CacheTTL {
			ttl = config.ReorgTTL
		} else {
			config.reorgActive = false
		}
	}
	config.Cache.Set(key, NFTAccessEntry{
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
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func chainName(chainID int64) string {
	if cfg, ok := web3.SupportedChains[chainID]; ok {
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
