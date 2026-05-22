package solana

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

type solanaEndpointState struct {
	url       string
	client    *rpc.Client
	score     float64
	cooldown  time.Time
	connected bool
}

type SolanaMultiClient struct {
	mu        sync.RWMutex
	logger    *zap.Logger
	endpoints []*solanaEndpointState
}

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

	if err := c.connectAny(); err != nil {
		logger.Warn("No Solana RPC endpoint reachable at startup, will retry on demand",
			zap.Error(err))
	}
	return c, nil
}

func (c *SolanaMultiClient) GetClient(ctx context.Context) (*rpc.Client, error) {
	c.mu.RLock()
	best := c.bestEndpoint()
	c.mu.RUnlock()

	if best != nil && best.connected && best.cooldown.Before(time.Now()) {
		return best.client, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	best = c.bestEndpoint()
	if best != nil && best.connected && best.cooldown.Before(time.Now()) {
		return best.client, nil
	}

	if err := c.connectAnyLocked(ctx); err != nil {
		return nil, err
	}
	best = c.bestEndpoint()
	if best == nil {
		return nil, ErrSolanaAllFailed
	}
	return best.client, nil
}

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

func (c *SolanaMultiClient) connectAny() error {
	return c.connectAnyLocked(context.Background())
}

func (c *SolanaMultiClient) connectAnyLocked(ctx context.Context) error {
	indices := rand.Perm(len(c.endpoints))

	var lastErr error
	for _, idx := range indices {
		ep := c.endpoints[idx]
		pingCtx, cancel := context.WithTimeout(ctx, solRPCConnectTimeout)
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

type SolanaEndpointStatus struct {
	URL       string
	Score     float64
	Connected bool
	Cooldown  time.Time
}

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
	sort.Slice(res, func(i, j int) bool {
		return res[i].Score > res[j].Score
	})
	return res
}

func (c *SolanaMultiClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ep := range c.endpoints {
		if ep.client != nil {
			_ = ep.client.Close()
		}
	}
}
