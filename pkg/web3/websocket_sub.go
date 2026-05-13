package web3

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// LogSubscriber wraps an ethclient WebSocket subscription with auto-reconnect.
// It delivers real-time logs via a channel and falls back to polling on failure.
type LogSubscriber struct {
	wsURL      string
	logger     *zap.Logger
	client     *ethclient.Client
	sub        ethereum.Subscription
	logs       chan types.Log
	reconnectC chan struct{}
	cancel     context.CancelFunc
	mu         sync.Mutex
	running    bool
}

// NewLogSubscriber creates a new WebSocket log subscriber.
// wsURL must be a WebSocket endpoint (ws:// or wss://).
func NewLogSubscriber(wsURL string, logger *zap.Logger) *LogSubscriber {
	return &LogSubscriber{
		wsURL:      wsURL,
		logger:     logger,
		logs:       make(chan types.Log, 256),
		reconnectC: make(chan struct{}, 1),
	}
}

// Subscribe establishes a WebSocket connection and subscribes to logs matching
// the given filter query. It returns a read-only channel for incoming logs.
// The subscriber auto-reconnects on connection loss.
func (ls *LogSubscriber) Subscribe(ctx context.Context, query ethereum.FilterQuery) (<-chan types.Log, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.running {
		return ls.logs, nil
	}

	subCtx, cancel := context.WithCancel(ctx)
	ls.cancel = cancel

	if err := ls.dialAndSubscribe(subCtx, query); err != nil {
		cancel()
		return nil, fmt.Errorf("initial ws subscription failed: %w", err)
	}

	ls.running = true
	go ls.reconnectLoop(subCtx, query)

	ls.logger.Info("WebSocket log subscriber started",
		zap.String("ws_url", ls.wsURL))
	return ls.logs, nil
}

// Unsubscribe closes the subscription and WebSocket connection.
func (ls *LogSubscriber) Unsubscribe() {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if !ls.running {
		return
	}
	ls.running = false

	if ls.cancel != nil {
		ls.cancel()
	}
	if ls.sub != nil {
		ls.sub.Unsubscribe()
	}
	if ls.client != nil {
		ls.client.Close()
	}
	ls.logger.Info("WebSocket log subscriber stopped")
}

// dialAndSubscribe establishes a WS connection and subscribes to logs.
func (ls *LogSubscriber) dialAndSubscribe(ctx context.Context, query ethereum.FilterQuery) error {
	client, err := ethclient.Dial(ls.wsURL)
	if err != nil {
		return fmt.Errorf("ws dial failed: %w", err)
	}

	// Verify connection is alive
	cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := client.ChainID(cCtx); err != nil {
		client.Close()
		return fmt.Errorf("ws chain id check failed: %w", err)
	}

	sub, err := client.SubscribeFilterLogs(ctx, query, ls.logs)
	if err != nil {
		client.Close()
		return fmt.Errorf("ws subscribe failed: %w", err)
	}

	ls.client = client
	ls.sub = sub
	return nil
}

// reconnectLoop watches for subscription errors and auto-reconnects.
func (ls *LogSubscriber) reconnectLoop(ctx context.Context, query ethereum.FilterQuery) {
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-ls.sub.Err():
			if err != nil {
				ls.logger.Warn("WebSocket subscription error, reconnecting",
					zap.Error(err),
					zap.String("ws_url", ls.wsURL))
			}
			ls.mu.Lock()
			if ls.sub != nil {
				ls.sub.Unsubscribe()
			}
			if ls.client != nil {
				ls.client.Close()
			}
			shouldRun := ls.running
			ls.mu.Unlock()

			if !shouldRun {
				return // Unsubscribe was called
			}

			// Backoff reconnect (ctx is already cancelled by Unsubscribe)
			if err := ctx.Err(); err != nil {
				return
			}
			if !ls.reconnectWithBackoff(ctx, query) {
				return
			}
		}
	}
}

// reconnectWithBackoff attempts to reconnect with exponential backoff.
// Returns false if the context is cancelled.
func (ls *LogSubscriber) reconnectWithBackoff(ctx context.Context, query ethereum.FilterQuery) bool {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for attempt := 1; ; attempt++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		ls.logger.Info("Reconnecting WebSocket",
			zap.Int("attempt", attempt),
			zap.Duration("backoff", backoff))

		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}

		ls.mu.Lock()
		err := ls.dialAndSubscribe(ctx, query)
		ls.mu.Unlock()

		if err == nil {
			ls.logger.Info("WebSocket reconnected",
				zap.Int("attempt", attempt))
			return true
		}

		ls.logger.Warn("WebSocket reconnect failed",
			zap.Int("attempt", attempt),
			zap.Error(err))

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// WSAvailable checks if a WebSocket URL can be derived from the given RPC URL.
// Converts https:// to wss:// and http:// to ws://.
func WSAvailable(rpcURL string) (string, bool) {
	if strings.HasPrefix(rpcURL, "wss://") || strings.HasPrefix(rpcURL, "ws://") {
		return rpcURL, true
	}
	if strings.HasPrefix(rpcURL, "https://") {
		return "wss://" + strings.TrimPrefix(rpcURL, "https://"), true
	}
	if strings.HasPrefix(rpcURL, "http://") {
		return "ws://" + strings.TrimPrefix(rpcURL, "http://"), true
	}
	return "", false
}
