package gateway

import (
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"streamgate/pkg/middleware"
	"streamgate/pkg/util"
)

// RegisterNFTRoutes registers NFT verification and ownership routes.
func RegisterNFTRoutes(router gin.IRouter, log *zap.Logger, verifier middleware.NFTOwnershipChecker, cache middleware.NFTAccessCache, defaultChainID int64, cacheTTL time.Duration) {
	nft := router.Group("/api/v1/nft")
	nft.GET("", func(c *gin.Context) {
		wallet := middleware.GetWalletAddress(c)
		contract := c.Query("contract")
		if contract == "" || !util.IsValidAddress(contract) {
			abortWithError(c, http.StatusBadRequest, ErrMissingContract, "valid contract address is required (0x-prefixed 40-hex)")
			return
		}
		chainID := defaultChainID
		if v := c.Query("chain_id"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				chainID = n
			}
		}
		balance, err := verifier.GetNFTBalance(c.Request.Context(), chainID, contract, wallet)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrNFTVerifyError, internalErrMsg(err), err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"wallet": wallet, "contract": contract, "chain_id": chainID, "balance": balance.String(), "has_nft": balance.Sign() > 0})
	})
	nft.GET("/:id", func(c *gin.Context) {
		tokenID := c.Param("id")
		contract := c.Query("contract")
		if contract == "" || !util.IsValidAddress(contract) {
			abortWithError(c, http.StatusBadRequest, ErrMissingContract, "valid contract address is required (0x-prefixed 40-hex)")
			return
		}
		chainID := defaultChainID
		if v := c.Query("chain_id"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				chainID = n
			}
		}
		wallet := middleware.GetWalletAddress(c)
		hasNFT, err := verifier.VerifyNFTOwnership(c.Request.Context(), chainID, contract, tokenID, wallet)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrNFTVerifyError, internalErrMsg(err), err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"wallet": wallet, "contract": contract, "token_id": tokenID, "chain_id": chainID, "has_nft": hasNFT})
	})
	nft.POST("/verify", func(c *gin.Context) {
		var req struct {
			ChainID         int64  `json:"chain_id"`
			Address         string `json:"address"`
			Wallet          string `json:"wallet"`
			OwnerAddress    string `json:"owner_address"`
			Contract        string `json:"contract"`
			ContractAddress string `json:"contract_address"`
			TokenID         string `json:"token_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
			return
		}
		wallet := req.Wallet
		if wallet == "" {
			if req.Address != "" {
				wallet = req.Address
			} else {
				wallet = req.OwnerAddress
			}
		}
		contract := req.Contract
		if contract == "" {
			contract = req.ContractAddress
		}
		// Validate contract address format
		if contract == "" || !util.IsValidAddress(contract) {
			abortWithError(c, http.StatusBadRequest, ErrMissingContract, "valid contract address is required (0x-prefixed 40-hex)")
			return
		}
		// Validate tokenID is numeric if provided
		if req.TokenID != "" {
			if _, ok := new(big.Int).SetString(req.TokenID, 10); !ok {
				abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "token_id must be a valid numeric string")
				return
			}
		}
		chainID := req.ChainID
		if chainID == 0 {
			chainID = defaultChainID
		}
		var hasNFT bool
		var balance *big.Int
		var err error
		var cacheHit bool
		cacheKey := fmt.Sprintf("%d:%s:%s:%s", chainID, wallet, contract, req.TokenID)
		if cache != nil {
			if entry, ok := cache.Get(cacheKey); ok && entry.Expires.After(time.Now()) {
				hasNFT = entry.HasNFT
				balance = entry.Balance
				cacheHit = true
			}
		}
		if !cacheHit {
			if req.TokenID != "" {
				hasNFT, err = verifier.VerifyNFTOwnership(c.Request.Context(), chainID, contract, req.TokenID, wallet)
				if hasNFT {
					balance = big.NewInt(1)
				}
			} else {
				balance, err = verifier.GetNFTBalance(c.Request.Context(), chainID, contract, wallet)
				hasNFT = balance != nil && balance.Sign() > 0
			}
		}
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrNFTVerifyError, internalErrMsg(err), err.Error())
			return
		}
		if cache != nil && !cacheHit {
			cache.Set(cacheKey, middleware.NFTAccessEntry{HasNFT: hasNFT, Balance: balance, Expires: time.Now().Add(cacheTTL)})
		}
		c.JSON(http.StatusOK, gin.H{"has_nft": hasNFT, "balance": balance.String(), "chain_id": chainID, "contract": contract, "cache_hit": cacheHit})
	})
	log.Info("NFT routes registered")
}

// --- NFT Access Cache ---

// CachedNFTAccess stores a cached NFT access check result.
type CachedNFTAccess struct {
	HasNFT    bool
	Balance   *big.Int
	ExpiresAt time.Time
}

// NFTAccessCache is a thread-safe in-memory cache for NFT access checks.
type NFTAccessCache struct {
	mu      sync.RWMutex
	entries map[string]CachedNFTAccess
	maxSize int
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

const defaultNFTCacheMaxSize = 100000

// NewNFTAccessCache creates a new NFTAccessCache with bounded size and background cleanup.
func NewNFTAccessCache() *NFTAccessCache {
	c := &NFTAccessCache{
		entries: make(map[string]CachedNFTAccess),
		maxSize: defaultNFTCacheMaxSize,
		stopCh:  make(chan struct{}),
	}
	c.wg.Add(1)
	go c.cleanupLoop()
	return c
}

// Stop terminates the background cleanup goroutine.
func (c *NFTAccessCache) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

// cleanupLoop periodically removes expired entries.
func (c *NFTAccessCache) cleanupLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.evictExpired()
		}
	}
}

// evictExpired removes all expired entries.
func (c *NFTAccessCache) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	for k, v := range c.entries {
		if now.After(v.ExpiresAt) {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

// Get retrieves a cached entry.
func (c *NFTAccessCache) Get(key string) (CachedNFTAccess, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return CachedNFTAccess{}, false
	}
	if time.Now().After(entry.ExpiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return CachedNFTAccess{}, false
	}
	return entry, true
}

// Set stores a cached entry. Evicts expired entries if the cache exceeds maxSize.
func (c *NFTAccessCache) Set(key string, entry CachedNFTAccess) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry
	if len(c.entries) > c.maxSize {
		now := time.Now()
		for k, v := range c.entries {
			if now.After(v.ExpiresAt) {
				delete(c.entries, k)
			}
		}
	}
}

// NFTAccessCacheAdapter adapts NFTAccessCache to middleware.NFTAccessCache.
type NFTAccessCacheAdapter struct {
	Cache *NFTAccessCache
}

// Get implements middleware.NFTAccessCache.
func (a *NFTAccessCacheAdapter) Get(key string) (middleware.NFTAccessEntry, bool) {
	entry, ok := a.Cache.Get(key)
	if !ok {
		return middleware.NFTAccessEntry{}, false
	}
	return middleware.NFTAccessEntry{
		HasNFT:  entry.HasNFT,
		Balance: entry.Balance,
		Expires: entry.ExpiresAt,
	}, true
}

// Set implements middleware.NFTAccessCache.
func (a *NFTAccessCacheAdapter) Set(key string, entry middleware.NFTAccessEntry) {
	a.Cache.Set(key, CachedNFTAccess{
		HasNFT:    entry.HasNFT,
		Balance:   entry.Balance,
		ExpiresAt: entry.Expires,
	})
}
