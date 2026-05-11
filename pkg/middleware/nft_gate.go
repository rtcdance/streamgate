package middleware

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"streamgate/pkg/web3"
)

// NFTOwnershipChecker verifies NFT ownership for a wallet address.
type NFTOwnershipChecker interface {
	VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error)
}

// NFTAccessCache caches NFT access check results.
type NFTAccessCache interface {
	Get(key string) (NFTAccessEntry, bool)
	Set(key string, entry NFTAccessEntry)
}

// NFTAccessEntry represents a cached NFT access check.
type NFTAccessEntry struct {
	HasNFT  bool
	Balance *big.Int
	Expires time.Time
}

// NFTGateConfig configures the NFT gate middleware.
type NFTGateConfig struct {
	Verifier       NFTOwnershipChecker
	Cache          NFTAccessCache
	DefaultChainID int64
	CacheTTL       time.Duration
	MarketplaceURL string     // Template with {contract} and {token_id} placeholders
	BlockTag       web3.BlockTag // optional: read from finalized/safe blocks to prevent reorg issues
}

// NFTGateMiddleware returns a gin middleware that verifies NFT ownership
// before allowing access to protected resources.
//
// Requires JWTAuthMiddleware to run first (reads wallet_address from context).
// Reads "contract" and optional "token_id" and "chain_id" from query params.
// On success, injects "nft_verified" and "nft_contract" into the context.
func NFTGateMiddleware(config NFTGateConfig, logger *zap.Logger) gin.HandlerFunc {
	cacheTTL := config.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 60 * time.Second
	}

	return func(c *gin.Context) {
		// Get wallet address from JWTAuthMiddleware
		walletAddress := GetWalletAddress(c)
		if walletAddress == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required", "code": "UNAUTHORIZED"})
			return
		}

		// Get contract from query param
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

		// Get optional chain_id
		chainID := config.DefaultChainID
		if raw := c.Query("chain_id"); raw != "" {
			if parsed, err := parseInt64(raw); err == nil {
				chainID = parsed
			}
		}

		tokenID := c.Query("token_id")

		// Check cache
		var hasNFT bool
		cacheKey := nftCacheKey(chainID, walletAddress, contract, tokenID)

		if config.Cache != nil {
			if entry, ok := config.Cache.Get(cacheKey); ok && entry.Expires.After(time.Now()) {
				hasNFT = entry.HasNFT
			}
		}

		// If not cached, verify on-chain
		if config.Cache == nil || !cacheHit(config.Cache, cacheKey) {
			var err error
			if tokenID != "" {
				hasNFT, err = config.Verifier.VerifyNFTOwnership(c.Request.Context(), chainID, contract, tokenID, walletAddress)
			} else {
				balance, balanceErr := config.Verifier.GetNFTBalance(c.Request.Context(), chainID, contract, walletAddress)
				err = balanceErr
				hasNFT = balance != nil && balance.Sign() > 0
			}

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

			// Cache the result
			if config.Cache != nil {
				config.Cache.Set(cacheKey, NFTAccessEntry{
					HasNFT:  hasNFT,
					Balance: big.NewInt(1),
					Expires: time.Now().Add(cacheTTL),
				})
			}
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

		// Inject verified ownership into context
		c.Set("nft_verified", true)
		c.Set("nft_contract", contract)
		c.Set("nft_chain_id", chainID)
		c.Next()
	}
}

// GetNFTVerified checks if NFT verification passed.
func GetNFTVerified(c *gin.Context) bool {
	v, _ := c.Get("nft_verified")
	verified, _ := v.(bool)
	return verified
}

// GetNFTContract returns the verified NFT contract address.
func GetNFTContract(c *gin.Context) string {
	v, _ := c.Get("nft_contract")
	contract, _ := v.(string)
	return contract
}

func nftCacheKey(chainID int64, wallet, contract, tokenID string) string {
	return fmt.Sprintf("%d:%s:%s:%s", chainID, wallet, contract, tokenID)
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func cacheHit(cache NFTAccessCache, key string) bool {
	entry, ok := cache.Get(key)
	return ok && entry.Expires.After(time.Now())
}

func chainName(chainID int64) string {
	if cfg, ok := web3.SupportedChains[chainID]; ok {
		return cfg.Name
	}
	return fmt.Sprintf("Chain %d", chainID)
}
