package web3

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"go.uber.org/zap"
)

const (
	solRPCSlotHealthMaxRetries = 2
	solRPCConnectTimeout       = 10 * time.Second
	solRPCRequestTimeout       = 15 * time.Second

	solScoreInitial     = 100.0
	solScorePenalty     = 25.0
	solScoreRecovery    = 5.0
	solScoreMin         = 10.0
	solCoolDownDuration = 30 * time.Second
)

var (
	ErrSolanaNoEndpoints = errors.New("no Solana RPC endpoints configured")
	ErrSolanaAllFailed   = errors.New("all Solana RPC endpoints failed")
)

// solanaEndpointState tracks health and score for a single Solana RPC endpoint.
type solanaEndpointState struct {
	url       string
	client    *rpc.Client
	score     float64
	cooldown  time.Time
	connected bool
}

// SolanaMultiClient provides multi-endpoint failover for Solana RPC calls.
type SolanaMultiClient struct {
	mu         sync.RWMutex
	logger     *zap.Logger
	endpoints  []*solanaEndpointState
}

// NewSolanaMultiClient creates a client with multiple Solana RPC endpoints.
// At least one endpoint is required. Endpoints are tried in score order;
// failed endpoints are penalized and cooled down.
func NewSolanaMultiClient(logger *zap.Logger, rpcURLs []string) (*SolanaMultiClient, error) {
	if len(rpcURLs) == 0 {
		return nil, ErrSolanaNoEndpoints
	}

	endpoints := make([]*solanaEndpointState, 0, len(rpcURLs))
	for _, url := range rpcURLs {
		if url == "" {
			continue
		}
		endpoints = append(endpoints, &solanaEndpointState{
			url:    url,
			client: rpc.New(url),
			score:  solScoreInitial,
		})
	}
	if len(endpoints) == 0 {
		return nil, ErrSolanaNoEndpoints
	}

	c := &SolanaMultiClient{
		logger:    logger,
		endpoints: endpoints,
	}

	// Try to connect to at least one endpoint on startup
	if err := c.connectAny(); err != nil {
		logger.Warn("No Solana RPC endpoint reachable at startup, will retry on demand",
			zap.Error(err))
	}
	return c, nil
}

// GetClient returns a healthy RPC client, failing over if needed.
// Returns the highest-scored connected client.
func (c *SolanaMultiClient) GetClient(ctx context.Context) (*rpc.Client, error) {
	c.mu.RLock()
	// Find the best available endpoint
	best := c.bestEndpoint()
	c.mu.RUnlock()

	if best != nil && best.connected && best.cooldown.Before(time.Now()) {
		return best.client, nil
	}

	// Try to connect to any endpoint
	c.mu.Lock()
	defer c.mu.Unlock()

	// Re-check under write lock
	best = c.bestEndpoint()
	if best != nil && best.connected && best.cooldown.Before(time.Now()) {
		return best.client, nil
	}

	if err := c.connectAnyLocked(); err != nil {
		return nil, err
	}
	best = c.bestEndpoint()
	if best == nil {
		return nil, ErrSolanaAllFailed
	}
	return best.client, nil
}

// RecordSuccess increases score for the given endpoint.
func (c *SolanaMultiClient) RecordSuccess(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ep := range c.endpoints {
		if ep.url == url {
			ep.score = math.Min(solScoreInitial, ep.score+solScoreRecovery)
			ep.connected = true
			return
		}
	}
}

// RecordFailure decreases score and sets cool-down for the given endpoint.
func (c *SolanaMultiClient) RecordFailure(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ep := range c.endpoints {
		if ep.url == url {
			ep.score = math.Max(solScoreMin, ep.score-solScorePenalty)
			ep.cooldown = time.Now().Add(solCoolDownDuration)
			ep.connected = false
			return
		}
	}
}

// bestEndpoint returns the highest-scored connected endpoint not in cool-down.
// Caller must hold at least RLock.
func (c *SolanaMultiClient) bestEndpoint() *solanaEndpointState {
	var best *solanaEndpointState
	now := time.Now()
	for _, ep := range c.endpoints {
		if !ep.connected || ep.cooldown.After(now) {
			continue
		}
		if best == nil || ep.score > best.score {
			best = ep
		}
	}
	return best
}

// connectAny tries all endpoints in random order until one succeeds.
// Caller must hold Lock.
func (c *SolanaMultiClient) connectAny() error {
	return c.connectAnyLocked()
}

// connectAnyLocked tries all endpoints. Caller must hold Lock.
func (c *SolanaMultiClient) connectAnyLocked() error {
	// Shuffle order so we don't always try the same endpoint first
	indices := rand.Perm(len(c.endpoints))

	var lastErr error
	for _, idx := range indices {
		ep := c.endpoints[idx]
		pingCtx, cancel := context.WithTimeout(context.Background(), solRPCConnectTimeout)
		_, err := ep.client.GetRecentBlockhash(pingCtx, rpc.CommitmentFinalized)
		cancel()
		if err == nil {
			ep.connected = true
			ep.score = solScoreInitial
			ep.cooldown = time.Time{}
			c.logger.Info("Connected to Solana RPC endpoint", zap.String("url", ep.url))
			return nil
		}
		ep.connected = false
		ep.cooldown = time.Now().Add(solCoolDownDuration)
		lastErr = err
		c.logger.Warn("Failed to connect to Solana RPC endpoint",
			zap.String("url", ep.url), zap.Error(err))
	}
	return fmt.Errorf("connectAny: %w", lastErr)
}

// EndpointStatus represents the current status of an RPC endpoint.
type SolanaEndpointStatus struct {
	URL       string
	Score     float64
	Connected bool
	Cooldown  time.Time
}

// Statuses returns the current status of all endpoints.
func (c *SolanaMultiClient) Statuses() []SolanaEndpointStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	res := make([]SolanaEndpointStatus, len(c.endpoints))
	for i, ep := range c.endpoints {
		res[i] = SolanaEndpointStatus{
			URL:       ep.url,
			Score:     ep.score,
			Connected: ep.connected,
			Cooldown:  ep.cooldown,
		}
	}
	// Sort by score descending
	sort.Slice(res, func(i, j int) bool {
		return res[i].Score > res[j].Score
	})
	return res
}

// Close releases all connections.
func (c *SolanaMultiClient) Close() {}