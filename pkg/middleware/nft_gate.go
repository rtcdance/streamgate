package middleware

import (
	"context"
	"errors"
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
	// GetNFTOwnershipQuantity is the ONLY signal the gate authorizes on: the
	// collection balance when tokenID is empty, otherwise the token quantity
	// (ERC-721 → 0/1, ERC-1155 → on-chain uint256). It MUST return an error —
	// never a permissive value — for unknown contracts, unsupported standards,
	// or an unreadable chain. A boolean "holds some NFT" answer is deliberately
	// absent here: it cannot express a min_balance greater than 1.
	//
	// block pins every read behind the answer (ERC-165 probe included) to one
	// exact block. The gate always passes a resolved block: an implementation
	// that cannot read at a specific block must fail rather than answer from
	// "latest", because two reads at different heights are not one authorization.
	GetNFTOwnershipQuantity(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string, block *big.Int) (*big.Int, error)
}

type NFTMetadata struct {
	Name            string
	TokenURI        string
	ContractAddress string
	TokenID         string
}

// BlockProver supplies the block evidence an authorization is anchored to.
//
// The chain is an explicit parameter rather than an implicit "default chain":
// evidence read from chain A must never be used to validate a rule that targets
// chain B, and two chains at the same height have different hashes.
type BlockProver interface {
	HeaderByNumber(ctx context.Context, chainID int64, number *big.Int) (*BlockHeaderInfo, error)
	// HeaderByTag resolves a finality tag ("latest", "safe", "finalized") to a
	// concrete header. This is how the gate picks the ONE block an
	// authorization reads at, instead of letting the node re-resolve a tag on
	// every individual call.
	HeaderByTag(ctx context.Context, chainID int64, tag string) (*BlockHeaderInfo, error)
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

// invalidGatingRuleError marks a rule that is malformed, as opposed to a rule
// whose verification could not be performed. The gate answers 400 for the
// former and 503 for the latter, so the two must never be conflated — a
// misconfigured rule must not be reported as "the chain is down", and an RPC
// outage must not be reported as "no NFT".
type invalidGatingRuleError struct{ reason string }

func (e *invalidGatingRuleError) Error() string { return "invalid gating rule: " + e.reason }

type NFTGateConfig struct {
	Verifier         NFTOwnershipChecker
	BlockProver      BlockProver
	Cache            NFTAccessCache
	RuleResolver     GatingRuleResolver
	AuditLogger      storage.AuditLogger
	CircuitBreaker   *CircuitBreaker
	BlockVerifyCache *BlockHashCache
	DefaultChainID   int64
	CacheTTL         time.Duration
	ReorgTTL         time.Duration
	BlockVerifyTTL   time.Duration
	// MaxEvidenceAge caps how old the on-chain evidence behind a decision may
	// be, measured from when the query started. Zero means the 5s default.
	MaxEvidenceAge     time.Duration
	MarketplaceURL     string
	BlockTag           web3.BlockTag
	AutoDetectStandard bool
	// GatingMode decides what happens when NO active gating rules resolve for a
	// content: "closed" (default) denies the request (fail-closed — an unlisted
	// content is protected by default), "open" allows it (for explicitly public
	// deployments). Empty means "closed".
	GatingMode      string
	Enabled         atomic.Bool // atomic for safe concurrent runtime toggling
	reorgActive     atomic.Bool
	reorgDetectedAt atomic.Int64
}

type blockHashEntry struct {
	hash      string
	expiresAt time.Time
}

// blockHashKey scopes a block hash to the chain it came from. A height alone is
// not an identity: two chains routinely have a block at the same height with
// different hashes, and sharing an entry between them would let evidence from
// one chain validate an authorization on another.
type blockHashKey struct {
	chainID int64
	number  uint64
}

type BlockHashCache struct {
	mu         sync.RWMutex
	entries    map[blockHashKey]blockHashEntry
	ttl        time.Duration
	maxEntries int
}

func NewBlockHashCache(ttl ...time.Duration) *BlockHashCache {
	d := 5 * time.Minute
	if len(ttl) > 0 && ttl[0] > 0 {
		d = ttl[0]
	}
	return &BlockHashCache{entries: make(map[blockHashKey]blockHashEntry), ttl: d, maxEntries: 2048}
}

func (bhc *BlockHashCache) Get(chainID int64, blockNumber uint64) (string, bool) {
	bhc.mu.RLock()
	entry, ok := bhc.entries[blockHashKey{chainID: chainID, number: blockNumber}]
	bhc.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.hash, true
}

func (bhc *BlockHashCache) Set(chainID int64, blockNumber uint64, hash string) {
	bhc.mu.Lock()
	defer bhc.mu.Unlock()
	if bhc.entries == nil {
		bhc.entries = make(map[blockHashKey]blockHashEntry)
	}
	bhc.entries[blockHashKey{chainID: chainID, number: blockNumber}] = blockHashEntry{hash: hash, expiresAt: time.Now().Add(bhc.ttl)}
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
	// The freshness bound on on-chain evidence. Whatever the cache TTLs say,
	// evidence older than this is re-read rather than trusted.
	if config.MaxEvidenceAge <= 0 {
		config.MaxEvidenceAge = defaultMaxEvidenceAge
	}
	if config.BlockVerifyCache == nil {
		config.BlockVerifyCache = NewBlockHashCache()
	}
	// Gating is fail-closed by default: a content with no resolvable rules is
	// DENIED unless the operator explicitly opted into "open".
	if config.GatingMode == "" {
		config.GatingMode = "closed"
	}
	config.Enabled.Store(true)

	return func(c *gin.Context) {
		if !config.Enabled.Load() {
			// Fail closed: the gate being toggled off must not become a bypass.
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "nft gating is disabled; protected content is unavailable",
				"code":  "GATING_DISABLED",
			})
			return
		}

		if _, ok := c.Get("playback_auth"); ok {
			c.Set("nft_verified", true)
			c.Set("nft_bypass_reason", "playback_token")
			c.Next()
			return
		}

		walletAddress := GetWalletAddress(c)
		if walletAddress == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required", "code": "UNAUTHORIZED"})
			return
		}

		contentID := c.Param("id")

		rules, errStatus, errResp := resolveGatingRules(c, config, logger, contentID)
		if errResp != nil {
			if errStatus == 0 {
				errStatus = http.StatusBadRequest
			}
			c.AbortWithStatusJSON(errStatus, errResp)
			return
		}

		if len(rules) == 0 {
			if config.GatingMode == "open" {
				c.Set("nft_verified", true)
				c.Set("nft_bypass_reason", "no_gating_rules")
				c.Next()
				return
			}
			// Fail closed: no rules means the content is gated by default.
			// (An explicitly public deployment sets GatingMode "open".)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "content is gated; no active gating rules are configured for it",
				"code":  "GATING_REQUIRED",
			})
			return
		}

		// Reported subject for the denial body / audit trail.
		contract := rules[0].ContractAddress
		tokenID := rules[0].TokenID
		chainID := rules[0].ChainID

		if !common.IsHexAddress(contract) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "invalid contract address format",
				"code":  "INVALID_CONTRACT",
			})
			return
		}

		allowed, anchor, evalErr := evaluateGatingRules(c.Request.Context(), config, logger, rules, walletAddress)
		if evalErr != nil {
			var invalid *invalidGatingRuleError
			if errors.As(evalErr, &invalid) {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": invalid.Error(),
					"code":  "INVALID_GATING_RULE",
				})
				return
			}
			// No rule verified true AND at least one rule could not be verified:
			// this is a service failure, not a denial. Answering 403 here would
			// disguise an RPC outage as "you hold no NFT".
			logger.Error("NFT verification failed", zap.Error(evalErr))
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":      "verification service unavailable",
				"code":       "NFT_VERIFY_ERROR",
				"chain_id":   chainID,
				"chain_name": chainName(chainID),
			})
			return
		}

		if !allowed {
			nftGateDenied(c, config, walletAddress, contract, tokenID, chainID, contentID)
			return
		}

		c.Set("nft_verified", true)
		c.Set("wallet_address", walletAddress)
		c.Set("nft_contract", contract)
		c.Set("nft_chain_id", chainID)
		// Report WHICH chain state authorized the request, so every allow can be
		// traced to a specific chain and block rather than "some recent state".
		if anchor != nil {
			c.Set("nft_block_number", anchor.Number)
			c.Set("nft_block_hash", anchor.Hash)
			c.Header("X-NFT-Chain-Id", fmt.Sprintf("%d", chainID))
			c.Header("X-NFT-Block-Number", fmt.Sprintf("%d", anchor.Number))
			c.Header("X-NFT-Block-Hash", anchor.Hash)
		}
		if config.AuditLogger != nil {
			config.AuditLogger.Log(c.Request.Context(), "nft.gate_passed", walletAddress, "content", contentID, true, "", fmt.Sprintf("%s:%d", contract, chainID))
		}
		c.Next()
	}
}

// resolveGatingRules loads the server-side rule set. The authorization subject
// MUST come from server-side gating rules, never from the request: a
// caller-supplied ?contract= would let anyone pass the gate by naming a
// contract they happen to hold an NFT in.
func resolveGatingRules(c *gin.Context, config *NFTGateConfig, logger *zap.Logger, contentID string) (rules []GatingRule, errStatus int, errorBody gin.H) {
	if contentID == "" || config.RuleResolver == nil {
		// No resolver (or no content ID): there are no server-side rules to
		// authorize against. An empty rule set hands the decision to the
		// GatingMode check (closed denies, open allows).
		return nil, 0, nil
	}

	resolved, err := config.RuleResolver.GetActiveRulesForContent(c.Request.Context(), contentID)
	if err != nil {
		// Fail closed: an unresolvable rule set must not become a bypass.
		logger.Error("failed to resolve gating rules; denying request",
			zap.String("content_id", contentID), zap.Error(err))
		return nil, http.StatusInternalServerError, gin.H{
			"error": "failed to resolve gating rules",
			"code":  "GATING_RULES_ERROR",
		}
	}

	c.Set("gating_rules", resolved)
	c.Set("gating_rule_id", contentID)
	c.Set("gating_rules_count", len(resolved))
	return resolved, 0, nil
}

// validateGatingRule rejects rules that cannot be evaluated meaningfully, so a
// malformed rule surfaces as 400 instead of being defaulted into a laxer rule.
func validateGatingRule(rule GatingRule) error {
	if !common.IsHexAddress(rule.ContractAddress) {
		return &invalidGatingRuleError{reason: "contract address is not a valid hex address"}
	}
	if rule.ChainID == 0 {
		return &invalidGatingRuleError{reason: "chain_id is required"}
	}
	if _, err := normalizedMinBalance(rule.MinBalance); err != nil {
		return err
	}
	if rule.TokenID != "" {
		if _, ok := new(big.Int).SetString(rule.TokenID, 10); !ok {
			return &invalidGatingRuleError{reason: "token_id must be a decimal uint256"}
		}
	} else if strings.EqualFold(rule.Standard, "erc1155") {
		// ERC-1155 has no collection-wide quantity, so "holds any 1" can never
		// satisfy min_balance > 1. Reject at evaluation time rather than
		// inventing a total.
		return &invalidGatingRuleError{reason: "ERC-1155 rules require a token_id"}
	}
	return nil
}

// normalizedMinBalance applies the default of 1 and rejects negatives.
func normalizedMinBalance(minBalance int) (int64, error) {
	switch {
	case minBalance == 0:
		return 1, nil
	case minBalance < 0:
		return 0, &invalidGatingRuleError{reason: "min_balance must not be negative"}
	default:
		return int64(minBalance), nil
	}
}

// evaluateGatingRules applies OR semantics over the active rules: any rule that
// verifies to a sufficient quantity grants access. If no rule grants access, a
// rule that could not be verified at all produces a service error so the caller
// answers 503 rather than a misleading 403.
//
// On success it also returns the block the granting rule was read at, so the
// caller can report which chain state authorized the request.
func evaluateGatingRules(ctx context.Context, config *NFTGateConfig, logger *zap.Logger, rules []GatingRule, walletAddress string) (bool, *BlockHeaderInfo, error) {
	var firstUnverifiable error

	for _, rule := range rules {
		if err := validateGatingRule(rule); err != nil {
			return false, nil, err
		}
		minBalance, _ := normalizedMinBalance(rule.MinBalance)

		cacheKey := nftCacheKey(rule.ChainID, walletAddress, rule.ContractAddress, rule.TokenID, rule.Standard)
		quantity, anchor, err := resolveQuantity(ctx, config, logger, cacheKey, rule.ChainID, rule.ContractAddress, rule.TokenID, walletAddress)
		if err != nil {
			logger.Warn("gating rule could not be verified",
				zap.String("contract", rule.ContractAddress),
				zap.Int64("chain_id", rule.ChainID),
				zap.Error(err))
			if firstUnverifiable == nil {
				firstUnverifiable = err
			}
			continue
		}

		if quantity.Cmp(big.NewInt(minBalance)) >= 0 {
			return true, anchor, nil
		}
	}

	if firstUnverifiable != nil {
		return false, nil, firstUnverifiable
	}
	return false, nil, nil
}

// resolveQuantity returns the holder's quantity EVIDENCE for one subject, along
// with the block it was read at. The cache stores the evidence (the balance),
// never the verdict: the comparison against min_balance happens per request, so
// two rules with different thresholds over the same subject can never share a
// stale decision.
//
// InvalidateNFTEvidence drops cached evidence for one chain and marks the gate
// as having observed a reorg, so the next request on that chain re-reads at a
// fresh anchor instead of serving evidence from a chain state that no longer
// exists.
//
// The whole chain is flushed rather than the affected addresses alone: cache
// keys are "<chainID>:<wallet>:<contract>:<tokenID>:<standard>", so a wallet
// sits between the chain and the contract and a prefix cannot select by
// contract. A reorg is a chain-level event anyway, so dropping all of that
// chain's evidence is both simpler and safer.
func InvalidateNFTEvidence(config *NFTGateConfig, chainID int64) {
	if config == nil {
		return
	}
	config.reorgActive.Store(true)
	config.reorgDetectedAt.Store(time.Now().UnixNano())
	if config.Cache == nil {
		return
	}
	config.Cache.DeleteByPrefix(context.Background(), fmt.Sprintf("%d:", chainID))
}

// resolveAnchor picks the ONE block an authorization reads at: the header at the
// configured finality tag, resolved once. Every decision query in the request
// then names that block number, so the node never gets to re-resolve a tag
// mid-read and hand back two different chain states.
func resolveAnchor(ctx context.Context, config *NFTGateConfig, chainID int64) (*BlockHeaderInfo, error) {
	if config.BlockProver == nil {
		return nil, fmt.Errorf("no block prover configured; refusing to authorize without block evidence")
	}
	tag := strings.TrimSpace(string(config.BlockTag))
	if tag == "" {
		// Fail-closed default: the safe tag, never "latest".
		tag = "safe"
	}
	header, err := config.BlockProver.HeaderByTag(ctx, chainID, tag)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s block on chain %d: %w", tag, chainID, err)
	}
	if header == nil || header.Hash == "" {
		return nil, fmt.Errorf("resolved %s block on chain %d carries no hash", tag, chainID)
	}
	return header, nil
}

// verifyAnchorUnchanged re-reads the anchor after the decision queries and
// refuses if its hash moved. A block that changed under the read means the
// evidence was assembled from a chain state that no longer exists.
func verifyAnchorUnchanged(ctx context.Context, config *NFTGateConfig, chainID int64, anchor *BlockHeaderInfo) error {
	after, err := config.BlockProver.HeaderByNumber(ctx, chainID, big.NewInt(int64(anchor.Number)))
	if err != nil {
		return fmt.Errorf("failed to re-read block %d on chain %d: %w", anchor.Number, chainID, err)
	}
	if after == nil {
		return fmt.Errorf("block %d on chain %d disappeared during the read", anchor.Number, chainID)
	}
	if after.Hash != anchor.Hash {
		return fmt.Errorf("block %d on chain %d changed during the read (%s -> %s)",
			anchor.Number, chainID, anchor.Hash, after.Hash)
	}
	return nil
}

// entryAnchor reconstructs the block a cached entry was read at. An entry with
// no recorded block carries no anchor to report.
func entryAnchor(entry NFTAccessEntry) *BlockHeaderInfo {
	if entry.BlockHash == "" {
		return nil
	}
	return &BlockHeaderInfo{Number: entry.BlockNumber, Hash: entry.BlockHash}
}

// quantityEvidence pairs a quantity with the block it was read at, so the caller
// can report WHICH chain state authorized the request.
type quantityEvidence struct {
	quantity *big.Int
	anchor   *BlockHeaderInfo
}

func resolveQuantity(ctx context.Context, config *NFTGateConfig, logger *zap.Logger, cacheKey string, chainID int64, contract, tokenID, walletAddress string) (*big.Int, *BlockHeaderInfo, error) {
	start := time.Now()
	// The freshness clock for whatever evidence this request ends up using
	// starts HERE, before any cache is consulted, so a cache write further down
	// the stack cannot make the evidence look younger than it is.
	observedAt := start

	if config.Cache != nil {
		if entry, ok := config.Cache.Get(ctx, cacheKey); ok && entry.Expires.After(time.Now()) {
			if quantity, usable := cachedQuantity(config, logger, cacheKey, chainID, entry); usable {
				nftCacheHits.WithLabelValues("l1_l2").Inc()
				return quantity, entryAnchor(entry), nil
			}
		}
	}

	v, err, _ := nftSF.Do(cacheKey, func() (interface{}, error) {
		if config.CircuitBreaker != nil && !config.CircuitBreaker.Allow() {
			return nil, fmt.Errorf("nft verification circuit breaker is open for chain %d", chainID)
		}

		// One block for the whole authorization. If it cannot be resolved, the
		// request fails: reading at "latest" instead would silently reintroduce
		// the mixed-height evidence this exists to prevent.
		anchor, anchorErr := resolveAnchor(ctx, config, chainID)
		if anchorErr != nil {
			nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), "false").Inc()
			if config.CircuitBreaker != nil {
				config.CircuitBreaker.RecordFailure()
			}
			return nil, anchorErr
		}
		pinnedBlock := new(big.Int).SetUint64(anchor.Number)

		quantity, verifyErr := config.Verifier.GetNFTOwnershipQuantity(ctx, chainID, contract, tokenID, walletAddress, pinnedBlock)
		nftVerifyDuration.WithLabelValues(fmt.Sprintf("%d", chainID)).Observe(time.Since(start).Seconds())

		if verifyErr != nil {
			nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), "false").Inc()
			if config.CircuitBreaker != nil {
				config.CircuitBreaker.RecordFailure()
			}
			return nil, verifyErr
		}
		if quantity == nil || quantity.Sign() < 0 {
			// A nil or negative quantity is not evidence of ownership; treat it
			// as an unusable answer rather than a permissive one.
			nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), "false").Inc()
			return nil, fmt.Errorf("verifier returned no usable quantity for chain %d contract %s", chainID, contract)
		}

		// The anchor must not have moved while we were reading it.
		if reorgErr := verifyAnchorUnchanged(ctx, config, chainID, anchor); reorgErr != nil {
			nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), "false").Inc()
			config.reorgActive.Store(true)
			config.reorgDetectedAt.Store(time.Now().UnixNano())
			logger.Warn("Block changed under an NFT authorization read",
				zap.String("cache_key", cacheKey),
				zap.Error(reorgErr))
			return nil, reorgErr
		}

		nftVerifyTotal.WithLabelValues(fmt.Sprintf("%d", chainID), "true").Inc()
		if config.CircuitBreaker != nil {
			config.CircuitBreaker.RecordSuccess()
		}
		if config.Cache != nil {
			setCachedEntry(ctx, config, logger, cacheKey, observedAt, anchor, quantity)
		}
		return &quantityEvidence{quantity: quantity, anchor: anchor}, nil
	})
	if err != nil {
		return nil, nil, err
	}

	evidence, ok := v.(*quantityEvidence)
	if !ok || evidence == nil || evidence.quantity == nil {
		return nil, nil, fmt.Errorf("unexpected quantity result for chain %d", chainID)
	}
	return evidence.quantity, evidence.anchor, nil
}

// defaultMaxEvidenceAge is the freshness bound applied to on-chain evidence
// when the operator has not chosen one.
const defaultMaxEvidenceAge = 5 * time.Second

// evidenceAgeBound returns the configured freshness bound. A zero or negative
// value means "use the default" rather than "no bound at all", so a config that
// never passed through NFTGateMiddleware cannot silently disable the check.
func evidenceAgeBound(config *NFTGateConfig) time.Duration {
	if config.MaxEvidenceAge <= 0 {
		return defaultMaxEvidenceAge
	}
	return config.MaxEvidenceAge
}

// cachedQuantity decides whether a cached entry can be trusted and, if so,
// returns a copy of its balance evidence. A nil balance is never usable.
func cachedQuantity(config *NFTGateConfig, logger *zap.Logger, cacheKey string, chainID int64, entry NFTAccessEntry) (*big.Int, bool) {
	if entry.Balance == nil {
		return nil, false
	}
	// Evidence has a freshness bound that is independent of the cache TTL, and
	// it is measured from when the query STARTED. An entry written at T and read
	// at T+10s is re-read, and re-writing the entry into a lower cache does not
	// reset that clock.
	bound := evidenceAgeBound(config)
	if entry.ObservedAt.IsZero() || time.Since(entry.ObservedAt) > bound {
		logger.Debug("NFT evidence past its freshness bound, re-reading",
			zap.String("cache_key", cacheKey),
			zap.Duration("age", time.Since(entry.ObservedAt)),
			zap.Duration("max_age", bound))
		return nil, false
	}
	if entry.BlockHash == "" || config.BlockProver == nil {
		return new(big.Int).Set(entry.Balance), true
	}

	if cachedHash, ok := config.BlockVerifyCache.Get(chainID, entry.BlockNumber); ok && cachedHash == entry.BlockHash {
		nftBlockHashCacheHits.Inc()
		return new(big.Int).Set(entry.Balance), true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	header, err := config.BlockProver.HeaderByNumber(ctx, chainID, big.NewInt(int64(entry.BlockNumber)))
	if err == nil && header != nil {
		config.BlockVerifyCache.Set(chainID, entry.BlockNumber, header.Hash)
		if header.Hash != entry.BlockHash {
			nftVerifyReorgInvalidated.Inc()
			logger.Warn("NFT cache entry invalidated by reorg",
				zap.String("cache_key", cacheKey),
				zap.Uint64("block", entry.BlockNumber),
				zap.String("cached_hash", entry.BlockHash),
				zap.String("actual_hash", header.Hash))
			config.reorgActive.Store(true)
			config.reorgDetectedAt.Store(time.Now().UnixNano())
			return nil, false
		}
		return new(big.Int).Set(entry.Balance), true
	}

	// Block prover unavailable: serve the entry only inside a degraded window.
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
		return new(big.Int).Set(entry.Balance), true
	}
	logger.Warn("Degraded cache TTL expired, refusing to serve stale NFT verification",
		zap.String("cache_key", cacheKey))
	return nil, false
}

func setCachedEntry(ctx context.Context, config *NFTGateConfig, logger *zap.Logger, key string, observedAt time.Time, anchor *BlockHeaderInfo, quantity *big.Int) {
	// The evidence anchor is the block the authorization actually read at, not
	// whatever "latest" happens to be by the time the entry is written.
	var blockNumber uint64
	var blockHash string
	if anchor != nil {
		blockNumber = anchor.Number
		blockHash = anchor.Hash
	} else {
		logger.Debug("Caching NFT evidence without a block anchor")
	}

	hasNFT := quantity != nil && quantity.Sign() > 0
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

	var balance *big.Int
	if quantity != nil {
		balance = new(big.Int).Set(quantity)
	}
	config.Cache.Set(ctx, key, NFTAccessEntry{
		HasNFT:      hasNFT,
		Balance:     balance,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		Expires:     time.Now().Add(ttl),
		// The query's own start time, not "now": writing the entry must not
		// reset the evidence's freshness clock.
		ObservedAt: observedAt,
	})
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

// nftCacheKey isolates every axis that changes what a cached balance means:
// chain, subject wallet, contract, token and standard. min_balance is
// deliberately absent — the cached value is the balance itself, so a different
// threshold is a different comparison over the same evidence, not a different
// cache entry.
func nftCacheKey(chainID int64, wallet, contract, tokenID, standard string) string {
	if tokenID == "" {
		tokenID = "__collection__"
	}
	if standard == "" {
		standard = "__auto__"
	}
	return fmt.Sprintf("%d:%s:%s:%s:%s", chainID, wallet, contract, tokenID, strings.ToLower(standard))
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
