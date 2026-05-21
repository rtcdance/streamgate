package web3

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rtcdance/streamgate/pkg/monitoring"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// ChainClient handles blockchain interactions
type ChainClient struct {
	mu          sync.RWMutex
	client      atomic.Pointer[ethclient.Client]
	rpcURL      string
	rpcURLs     []string
	rpcStates   []rpcEndpointState
	activeRPC   int
	chainID     int64
	logger      *zap.Logger
	rateLimiter *RPCRateLimiter
	finality    FinalityStrategy
	wg          sync.WaitGroup
	closed      atomic.Bool

	nftVerifier   atomic.Pointer[NFTVerifier]
	nftVerifierMu sync.Mutex
}

// CallContract implements EthCaller, delegating to the active RPC client.
// This allows ChainClient to be used directly as an NFTVerifier client,
// enabling verifier reuse and proper block tag support.
func (cc *ChainClient) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return withChainClient(ctx, cc, "ChainClient.CallContract", func(client *ethclient.Client) ([]byte, error) {
		return client.CallContract(ctx, msg, blockNumber)
	})
}

// CodeAt implements EthCaller, delegating to the active RPC client.
func (cc *ChainClient) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	return withChainClient(ctx, cc, "ChainClient.CodeAt", func(client *ethclient.Client) ([]byte, error) {
		return client.CodeAt(ctx, contract, blockNumber)
	})
}

// getNFTVerifier returns a cached NFTVerifier that uses ChainClient as its
// EthCaller. Since ChainClient handles RPC failover internally, the verifier
// remains valid across connection changes and its standard cache is preserved.
func (cc *ChainClient) getNFTVerifier() *NFTVerifier {
	if v := cc.nftVerifier.Load(); v != nil {
		return v
	}
	cc.nftVerifierMu.Lock()
	defer cc.nftVerifierMu.Unlock()
	if v := cc.nftVerifier.Load(); v != nil {
		return v
	}
	v := NewNFTVerifier(cc, cc.logger).WithBlockTag(cc.GetFinality().BlockTag())
	cc.nftVerifier.Store(v)
	return v
}

type rpcEndpointState struct {
	Failures      int
	LastFailureAt time.Time
	CooldownUntil time.Time
	Score         float64
	LastLatency   time.Duration
}

// RPCStatus describes the current runtime status of an RPC endpoint.
type RPCStatus struct {
	URL           string    `json:"url"`
	IsActive      bool      `json:"is_active"`
	Failures      int       `json:"failures"`
	LastFailureAt time.Time `json:"last_failure_at,omitempty"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
	Score         float64   `json:"score"`
	LastLatencyMs int64     `json:"last_latency_ms,omitempty"`
}

const (
	rpcFailureCooldown  = 30 * time.Second
	rpcScoreInitial     = 1.0
	rpcScoreDecay       = 0.9
	rpcLatencyThreshold = 5.0
)

// NewChainClient creates a new chain client
func NewChainClient(rpcURL string, chainID int64, logger *zap.Logger) (*ChainClient, error) {
	return NewChainClientWithFallback([]string{rpcURL}, chainID, logger)
}

// NewChainClientWithFallback creates a chain client with multiple RPC candidates.
func NewChainClientWithFallback(rpcURLs []string, chainID int64, logger *zap.Logger) (*ChainClient, error) {
	normalizedRPCs := make([]string, 0, len(rpcURLs))
	for _, rpcURL := range rpcURLs {
		rpcURL = strings.TrimSpace(rpcURL)
		if rpcURL != "" {
			normalizedRPCs = append(normalizedRPCs, rpcURL)
		}
	}
	if len(normalizedRPCs) == 0 {
		return nil, fmt.Errorf("no rpc urls configured for chain %d", chainID)
	}

	states := make([]rpcEndpointState, len(normalizedRPCs))
	for i := range states {
		states[i].Score = rpcScoreInitial
	}
	cc := &ChainClient{
		rpcURL:    normalizedRPCs[0],
		rpcURLs:   normalizedRPCs,
		rpcStates: states,
		activeRPC: 0,
		chainID:   chainID,
		logger:    logger,
	}

	if err := cc.connectAny(); err != nil {
		return nil, err
	}

	return cc, nil
}

// GetEthClient returns the underlying ethclient.Client
func (cc *ChainClient) GetEthClient() *ethclient.Client {
	return cc.client.Load()
}

// SetRateLimiter sets the RPC rate limiter. Pass nil to disable rate limiting.
func (cc *ChainClient) SetRateLimiter(rl *RPCRateLimiter) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.rateLimiter = rl
	if rl != nil {
		cc.logger.Info("RPC rate limiter configured",
			zap.Float64("rate", rl.rate),
			zap.Float64("burst", rl.maxTokens))
	}
}

func (cc *ChainClient) SetFinality(f FinalityStrategy) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.finality = f
}

func (cc *ChainClient) GetFinality() FinalityStrategy {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if cc.finality == nil {
		return newFinalityDefault(nil, 12, BlockTagSafe, nil)
	}
	return cc.finality
}

func (cc *ChainClient) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	if cc.closed.Load() {
		return nil, fmt.Errorf("chain client closed")
	}
	cc.wg.Add(1)
	defer cc.wg.Done()

	client := cc.client.Load()
	if client == nil {
		return nil, fmt.Errorf("chain client not connected")
	}
	return client.SubscribeNewHead(ctx, ch)
}

// Close closes the client connection
func (cc *ChainClient) Close() {
	cc.closed.Store(true)

	cc.wg.Wait()

	old := cc.client.Swap(nil)
	if old != nil {
		old.Close()
	}
	cc.logger.Info("Chain client closed")
}

// GetRPCStatuses returns the runtime status of all configured RPC endpoints.
func (cc *ChainClient) GetRPCStatuses() []RPCStatus {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	statuses := make([]RPCStatus, 0, len(cc.rpcURLs))
	for idx, rpcURL := range cc.rpcURLs {
		state := cc.rpcStates[idx]
		statuses = append(statuses, RPCStatus{
			URL:           rpcURL,
			IsActive:      idx == cc.activeRPC,
			Failures:      state.Failures,
			LastFailureAt: state.LastFailureAt,
			CooldownUntil: state.CooldownUntil,
			Score:         state.Score,
			LastLatencyMs: state.LastLatency.Milliseconds(),
		})
	}
	return statuses
}

// updateRPCScores updates the weighted score for an RPC endpoint.
// Uses exponential moving average: recent results weigh more.
// Success latency: measured against the threshold (5s).
// Failure: halves the current score.
func (cc *ChainClient) updateRPCScores(idx int, latency time.Duration, success bool) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if idx < 0 || idx >= len(cc.rpcStates) {
		return
	}
	state := cc.rpcStates[idx]
	state.LastLatency = latency
	if success {
		latencyScore := 1.0 - (latency.Seconds() / rpcLatencyThreshold)
		if latencyScore < 0 {
			latencyScore = 0
		}
		state.Score = state.Score*rpcScoreDecay + latencyScore*(1.0-rpcScoreDecay)
		if state.Failures > 0 {
			state.Failures--
		}
	} else {
		state.Score *= 0.5
	}
	if state.Score < 0 {
		state.Score = 0
	}
	cc.rpcStates[idx] = state
}

// sortedRPCScores returns endpoint indices sorted by score descending.
func (cc *ChainClient) sortedRPCScores() []int {
	indices := make([]int, len(cc.rpcURLs))
	for i := range indices {
		indices[i] = i
	}
	cc.mu.RLock()
	scores := make([]float64, len(cc.rpcURLs))
	for i, st := range cc.rpcStates {
		scores[i] = st.Score
	}
	cc.mu.RUnlock()

	for i := 1; i < len(indices); i++ {
		key := indices[i]
		j := i - 1
		for j >= 0 && scores[indices[j]] < scores[key] {
			indices[j+1] = indices[j]
			j--
		}
		indices[j+1] = key
	}
	return indices
}

// GetRPCScores returns a map of RPC URL to score for monitoring.
func (cc *ChainClient) GetRPCScores() map[string]float64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	scores := make(map[string]float64, len(cc.rpcURLs))
	for i, url := range cc.rpcURLs {
		scores[url] = cc.rpcStates[i].Score
	}
	return scores
}

// HealthCheck performs a health check on the blockchain connection
func (cc *ChainClient) HealthCheck(ctx context.Context) error {
	cc.logger.Debug("Performing health check")

	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	blockNumber, err := withChainClient(healthCtx, cc, "HealthCheck.BlockNumber", func(client *ethclient.Client) (uint64, error) {
		return client.BlockNumber(healthCtx)
	})
	if err != nil {
		cc.logger.Error("Health check failed: cannot get block number", zap.Error(err))
		return fmt.Errorf("health check failed: %w", err)
	}

	chainID, err := withChainClient(healthCtx, cc, "HealthCheck.ChainID", func(client *ethclient.Client) (*big.Int, error) {
		return client.ChainID(healthCtx)
	})
	if err != nil {
		cc.logger.Error("Health check failed: cannot get chain ID", zap.Error(err))
		return fmt.Errorf("health check failed: %w", err)
	}

	if cc.chainID != 0 && chainID.Int64() != cc.chainID {
		cc.logger.Error("Health check failed: chain ID mismatch",
			zap.Int64("configured_chain_id", cc.chainID),
			zap.Int64("rpc_chain_id", chainID.Int64()))
		return fmt.Errorf("chain ID mismatch: configured=%d, rpc=%d", cc.chainID, chainID.Int64())
	}

	cc.logger.Info("Health check passed",
		zap.Uint64("block_number", blockNumber),
		zap.Int64("chain_id", chainID.Int64()),
		zap.String("rpc_url", cc.rpcURL))

	return nil
}

func (cc *ChainClient) connectAny() error {
	var lastErr error
	for _, idx := range cc.sortedRPCScores() {
		if !cc.endpointReady(idx, false) {
			continue
		}
		client, chainIDFromRPC, err := cc.connectAt(idx)
		if err != nil {
			cc.recordEndpointFailure(idx)
			lastErr = err
			continue
		}
		cc.setActiveClient(idx, client, true)
		cc.logger.Info("Connected to blockchain",
			zap.Int64("configured_chain_id", cc.chainID),
			zap.Int64("rpc_chain_id", chainIDFromRPC.Int64()),
			zap.String("rpc_url", cc.rpcURL))
		return nil
	}
	for _, idx := range cc.sortedRPCScores() {
		client, chainIDFromRPC, err := cc.connectAt(idx)
		if err != nil {
			cc.recordEndpointFailure(idx)
			lastErr = err
			continue
		}
		cc.setActiveClient(idx, client, true)
		cc.logger.Info("Connected to blockchain after cooldown bypass",
			zap.Int64("configured_chain_id", cc.chainID),
			zap.Int64("rpc_chain_id", chainIDFromRPC.Int64()),
			zap.String("rpc_url", cc.rpcURL))
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no rpc urls available")
	}
	cc.logger.Error("Failed to connect to blockchain", zap.Error(lastErr))
	return fmt.Errorf("failed to connect to blockchain: %w", lastErr)
}

func (cc *ChainClient) connectAt(idx int) (*ethclient.Client, *big.Int, error) {
	rpcURL := cc.rpcURLs[idx]
	cc.logger.Info("Connecting to blockchain",
		zap.String("rpc_url", rpcURL),
		zap.Int64("chain_id", cc.chainID))

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chainIDFromRPC, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	if cc.chainID != 0 && chainIDFromRPC.Int64() != cc.chainID {
		client.Close()
		return nil, nil, fmt.Errorf("unexpected chain id from %s: got %d want %d", rpcURL, chainIDFromRPC.Int64(), cc.chainID)
	}

	return client, chainIDFromRPC, nil
}

func (cc *ChainClient) setActiveClient(idx int, client *ethclient.Client, resetFailures bool) {
	oldClient := cc.client.Swap(client)
	cc.mu.Lock()
	cc.activeRPC = idx
	cc.rpcURL = cc.rpcURLs[idx]
	if resetFailures {
		cc.rpcStates[idx] = rpcEndpointState{Score: rpcScoreInitial}
	}
	cc.mu.Unlock()

	if oldClient != nil {
		go func(cl *ethclient.Client) {
			time.Sleep(30 * time.Second)
			cl.Close()
		}(oldClient)
	}
}

func (cc *ChainClient) failover() error {
	var lastErr error
	active := cc.getActiveRPCIndex()
	for _, idx := range cc.sortedRPCScores() {
		if idx == active {
			continue
		}
		if !cc.endpointReady(idx, false) {
			continue
		}
		client, chainIDFromRPC, err := cc.connectAt(idx)
		if err != nil {
			cc.recordEndpointFailure(idx)
			lastErr = err
			continue
		}
		cc.setActiveClient(idx, client, true)
		cc.logger.Warn("Switched blockchain RPC endpoint (scored)",
			zap.String("rpc_url", cc.rpcURL),
			zap.Int64("rpc_chain_id", chainIDFromRPC.Int64()))
		return nil
	}
	for _, idx := range cc.sortedRPCScores() {
		if idx == active {
			continue
		}
		client, chainIDFromRPC, err := cc.connectAt(idx)
		if err != nil {
			cc.recordEndpointFailure(idx)
			lastErr = err
			continue
		}
		cc.setActiveClient(idx, client, true)
		cc.logger.Warn("Switched blockchain RPC endpoint after cooldown bypass (scored)",
			zap.String("rpc_url", cc.rpcURL),
			zap.Int64("rpc_chain_id", chainIDFromRPC.Int64()))
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no failover rpc available")
	}
	return lastErr
}

func withChainClient[T any](ctx context.Context, cc *ChainClient, op string, fn func(*ethclient.Client) (T, error)) (T, error) {
	var zero T

	if cc.closed.Load() {
		return zero, NewPermanentError("chain client closed", nil)
	}
	cc.wg.Add(1)
	defer cc.wg.Done()

	cc.mu.RLock()
	limiter := cc.rateLimiter
	cc.mu.RUnlock()
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return zero, NewRetryableError("rpc rate limited", err)
		}
	}

	client := cc.client.Load()
	total := len(cc.rpcURLs)
	cc.mu.RLock()
	fromProvider := monitoring.RPCProviderFromURL(cc.rpcURL)
	cc.mu.RUnlock()

	if client == nil {
		if err := cc.connectAny(); err != nil {
			return zero, err
		}
		client = cc.client.Load()
		cc.mu.RLock()
		fromProvider = monitoring.RPCProviderFromURL(cc.rpcURL)
		cc.mu.RUnlock()
	}

	start := time.Now()
	result, err := fn(client)
	latency := time.Since(start)
	monitoring.RPCLatencySeconds.WithLabelValues(op, fromProvider).Observe(latency.Seconds())
	cc.updateRPCScores(cc.getActiveRPCIndex(), latency, err == nil)

	if err == nil || total <= 1 {
		if err != nil && isPermanentRPCError(err) {
			return zero, NewPermanentError(fmt.Sprintf("%s failed", op), err)
		}
		return result, err
	}

	cc.logger.Warn("RPC operation failed, attempting failover",
		zap.String("operation", op),
		zap.String("rpc_url", cc.rpcURL),
		zap.Error(err))
	cc.recordEndpointFailure(cc.getActiveRPCIndex())

	lastErr := err
	for attempts := 1; attempts < total; attempts++ {
		if failoverErr := cc.failover(); failoverErr != nil {
			lastErr = failoverErr
			continue
		}
		client = cc.client.Load()
		cc.mu.RLock()
		toProvider := monitoring.RPCProviderFromURL(cc.rpcURL)
		cc.mu.RUnlock()

		monitoring.RPCFailoverTotal.WithLabelValues(op, fromProvider, toProvider).Inc()

		start = time.Now()
		result, err = fn(client)
		latency = time.Since(start)
		monitoring.RPCLatencySeconds.WithLabelValues(op, toProvider).Observe(latency.Seconds())
		cc.updateRPCScores(cc.getActiveRPCIndex(), latency, err == nil)

		if err == nil {
			return result, nil
		}
		lastErr = err
		cc.logger.Warn("RPC operation failed on fallback endpoint",
			zap.String("operation", op),
			zap.String("rpc_url", cc.rpcURL),
			zap.Error(err))
	}

	if isPermanentRPCError(lastErr) {
		return zero, NewPermanentError(fmt.Sprintf("%s failed after rpc failover attempts", op), lastErr)
	}
	return zero, NewRetryableError(fmt.Sprintf("%s failed after rpc failover attempts", op), lastErr)
}

// isPermanentRPCError inspects an RPC error to determine if it represents a
// permanent failure that will not succeed on retry (e.g. contract revert,
// invalid opcode, out of gas). When uncertain, returns false so the caller
// defaults to retryable — this is the safer choice for transient RPC issues.
func isPermanentRPCError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	permanentPatterns := []string{
		"execution reverted",
		"revert",
		"invalid opcode",
		"out of gas",
		"invalid jump destination",
		"stack limit reached",
		"contract creation code storage out of gas",
		"nonce too low",
		"insufficient funds",
		"already known",
	}
	for _, pattern := range permanentPatterns {
		if strings.Contains(strings.ToLower(msg), pattern) {
			return true
		}
	}
	return false
}

func (cc *ChainClient) getActiveRPCIndex() int {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.activeRPC
}

func (cc *ChainClient) recordEndpointFailure(idx int) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if idx < 0 || idx >= len(cc.rpcStates) {
		return
	}
	state := cc.rpcStates[idx]
	state.Failures++
	state.LastFailureAt = time.Now()
	state.CooldownUntil = state.LastFailureAt.Add(rpcFailureCooldown)
	cc.rpcStates[idx] = state
}

func (cc *ChainClient) endpointReady(idx int, allowCooling bool) bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if idx < 0 || idx >= len(cc.rpcStates) {
		return false
	}
	if allowCooling {
		return true
	}
	state := cc.rpcStates[idx]
	return state.CooldownUntil.IsZero() || time.Now().After(state.CooldownUntil)
}
