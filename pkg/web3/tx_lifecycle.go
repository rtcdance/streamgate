package web3

import (
	"context"
	"crypto/ecdsa"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TxStatus represents the lifecycle status of a tracked transaction.
type TxStatus string

const (
	TxStatusPending   TxStatus = "pending"
	TxStatusConfirmed TxStatus = "confirmed"
	TxStatusFailed    TxStatus = "failed"
	TxStatusBumped    TxStatus = "bumped"
	TxStatusCancelled TxStatus = "cancelled"
)

// TrackedTx extends PendingTx with lifecycle metadata for automated monitoring.
type TrackedTx struct {
	*PendingTx
	Status       TxStatus
	Confirmations uint64
	RequiredConf  uint64     // block confirmations required
	BumpCount    int        // how many times gas has been bumped
	MaxBumps     int        // maximum bump attempts before cancelling
	BumpPercent  int64      // gas bump percentage (e.g. 10 = 10%)
	StuckAfter   time.Duration // time before tx is considered stuck
}

// TxLifecycleConfig configures the TxLifecycleManager behavior.
type TxLifecycleConfig struct {
	PollInterval time.Duration // how often to check receipts (default 5s)
	StuckAfter   time.Duration // time before a tx is considered stuck (default 3m)
	MaxBumps     int           // max gas bumps before auto-cancel (default 3)
	BumpPercent  int64         // gas bump percentage (default 10)
	RequiredConf uint64        // block confirmations for "confirmed" (default 3)
}

// DefaultTxLifecycleConfig returns sensible defaults.
func DefaultTxLifecycleConfig() TxLifecycleConfig {
	return TxLifecycleConfig{
		PollInterval: 5 * time.Second,
		StuckAfter:   3 * time.Minute,
		MaxBumps:     3,
		BumpPercent:  10,
		RequiredConf: 3,
	}
}

// KeyProvider abstracts access to the private key for signing replacement txs.
type KeyProvider interface {
	UseKey(fn func(*ecdsa.PrivateKey) error) error
}

// TxLifecycleManager automates the full tx lifecycle: monitor pending txs,
// bump gas for stuck transactions, and cancel after max bumps exceeded.
type TxLifecycleManager struct {
	mu         sync.RWMutex
	tracked    map[string]*TrackedTx // txHash → TrackedTx
	client     *ChainClient
	keyProvider KeyProvider
	config     TxLifecycleConfig
	logger     *zap.Logger
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup // tracks in-flight autoBump/autoCancel goroutines
}

// NewTxLifecycleManager creates a new lifecycle manager.
func NewTxLifecycleManager(client *ChainClient, keyProvider KeyProvider, config TxLifecycleConfig, logger *zap.Logger) *TxLifecycleManager {
	return &TxLifecycleManager{
		tracked:     make(map[string]*TrackedTx),
		client:      client,
		keyProvider:  keyProvider,
		config:      config,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// Track adds a pending transaction for automated lifecycle monitoring.
func (m *TxLifecycleManager) Track(tx *PendingTx) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tracked[tx.Hash] = &TrackedTx{
		PendingTx:   tx,
		Status:      TxStatusPending,
		RequiredConf: m.config.RequiredConf,
		MaxBumps:    m.config.MaxBumps,
		BumpPercent: m.config.BumpPercent,
		StuckAfter:  m.config.StuckAfter,
	}

	m.logger.Info("Tracking transaction",
		zap.String("tx_hash", tx.Hash),
		zap.Int64("chain_id", tx.ChainID),
		zap.Uint64("nonce", tx.Nonce))
}

// GetStatus returns the current status of a tracked transaction.
func (m *TxLifecycleManager) GetStatus(txHash string) (TxStatus, uint64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tx, ok := m.tracked[txHash]
	if !ok {
		return "", 0, false
	}
	return tx.Status, tx.Confirmations, true
}

// ListPending returns all transactions still in pending state.
func (m *TxLifecycleManager) ListPending() []*TrackedTx {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pending []*TrackedTx
	for _, tx := range m.tracked {
		if tx.Status == TxStatusPending || tx.Status == TxStatusBumped {
			pending = append(pending, tx)
		}
	}
	return pending
}

// Start begins the background monitoring goroutine.
func (m *TxLifecycleManager) Start(ctx context.Context) {
	m.logger.Info("Starting TxLifecycleManager",
		zap.Duration("poll_interval", m.config.PollInterval),
		zap.Duration("stuck_after", m.config.StuckAfter))

	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.poll(ctx)
		case <-m.stopCh:
			m.logger.Info("TxLifecycleManager stopped")
			return
		case <-ctx.Done():
			m.logger.Info("TxLifecycleManager context cancelled")
			return
		}
	}
}

// Stop gracefully shuts down the lifecycle manager.
// It signals the polling loop to stop and waits for in-flight
// autoBump/autoCancel goroutines to complete.
func (m *TxLifecycleManager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.wg.Wait()
}

// poll checks all pending transactions and takes action on stuck ones.
func (m *TxLifecycleManager) poll(ctx context.Context) {
	m.mu.RLock()
	pending := make([]*TrackedTx, 0)
	for _, tx := range m.tracked {
		if tx.Status == TxStatusPending || tx.Status == TxStatusBumped {
			pending = append(pending, tx)
		}
	}
	m.mu.RUnlock()

	for _, tx := range pending {
		m.checkTx(ctx, tx)
	}

	// Prune finalized txs older than 10 minutes
	m.pruneFinalized()
}

// checkTx inspects a single tracked transaction and bumps/cancels if needed.
// It snapshots shared state under the lock to avoid data races with
// concurrent autoBump/autoCancel goroutines.
func (m *TxLifecycleManager) checkTx(ctx context.Context, tx *TrackedTx) {
	// Snapshot the fields we need for decision-making under the lock
	m.mu.RLock()
	txHash := tx.Hash
	maxBumps := tx.MaxBumps
	bumpPercent := tx.BumpPercent
	requiredConf := tx.RequiredConf
	m.mu.RUnlock()

	// Check receipt
	receipt, err := m.client.GetTransactionReceipt(ctx, txHash)
	if err == nil && receipt != nil {
		if receipt.Status == 1 {
			// Mined successfully — check confirmations
			blockNum, blockErr := m.client.GetBlockNumber(ctx)
			if blockErr == nil && blockNum >= receipt.BlockNumber+requiredConf {
				m.mu.Lock()
				tx.Status = TxStatusConfirmed
				tx.Confirmations = blockNum - receipt.BlockNumber
				m.mu.Unlock()
				m.logger.Info("Transaction confirmed",
					zap.String("tx_hash", txHash),
					zap.Uint64("confirmations", tx.Confirmations))
			}
		} else {
			// Reverted
			m.mu.Lock()
			tx.Status = TxStatusFailed
			m.mu.Unlock()
			m.logger.Warn("Transaction reverted on-chain",
				zap.String("tx_hash", txHash))
		}
		return
	}

	// No receipt yet — check if stuck
	if !IsStuck(tx.PendingTx, tx.StuckAfter) {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-check bumpCount under write lock (may have changed since snapshot)
	bumpCount := tx.BumpCount
	if bumpCount >= maxBumps {
		// Max bumps exceeded — auto-cancel
		m.logger.Warn("Transaction exceeded max bumps, auto-cancelling",
			zap.String("tx_hash", txHash),
			zap.Int("bump_count", bumpCount))
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.autoCancel(ctx, tx)
		}()
		return
	}

	// Auto-bump
	m.logger.Info("Transaction stuck, auto-bumping gas",
		zap.String("tx_hash", txHash),
		zap.Int("bump_count", bumpCount+1),
		zap.Int64("bump_percent", bumpPercent))
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.autoBump(ctx, tx)
	}()
}

// autoBump bumps gas on a stuck transaction with a per-operation timeout.
func (m *TxLifecycleManager) autoBump(ctx context.Context, tx *TrackedTx) {
	opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tracker := NewTxTracker(m.client, m.logger)

	var newHash string
	err := m.keyProvider.UseKey(func(pk *ecdsa.PrivateKey) error {
		hash, err := tracker.BumpGas(opCtx, pk, tx.PendingTx, tx.BumpPercent)
		if err != nil {
			return err
		}
		newHash = hash
		return nil
	})

	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		m.logger.Error("Failed to auto-bump transaction",
			zap.String("tx_hash", tx.Hash),
			zap.Error(err))
		return
	}

	// Replace the old tx with the bumped one in the tracked map
	delete(m.tracked, tx.Hash)
	tx.BumpCount++
	tx.Hash = newHash
	tx.SentAt = time.Now()
	tx.Status = TxStatusBumped
	m.tracked[newHash] = tx

	m.logger.Info("Transaction gas bumped",
		zap.String("old_hash", tx.PendingTx.Hash),
		zap.String("new_hash", newHash),
		zap.Int("bump_count", tx.BumpCount))
}

// autoCancel cancels a stuck transaction that exceeded max bumps, with a per-operation timeout.
func (m *TxLifecycleManager) autoCancel(ctx context.Context, tx *TrackedTx) {
	opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tracker := NewTxTracker(m.client, m.logger)

	var cancelHash string
	err := m.keyProvider.UseKey(func(pk *ecdsa.PrivateKey) error {
		hash, err := tracker.CancelTx(opCtx, pk, tx.PendingTx, tx.BumpPercent)
		if err != nil {
			return err
		}
		cancelHash = hash
		return nil
	})

	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		m.logger.Error("Failed to auto-cancel transaction",
			zap.String("tx_hash", tx.Hash),
			zap.Error(err))
		return
	}

	tx.Status = TxStatusCancelled
	m.logger.Info("Transaction auto-cancelled",
		zap.String("original_hash", tx.PendingTx.Hash),
		zap.String("cancel_tx_hash", cancelHash))
}

// pruneFinalized removes confirmed/failed/cancelled txs older than 10 minutes.
func (m *TxLifecycleManager) pruneFinalized() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	for hash, tx := range m.tracked {
		if (tx.Status == TxStatusConfirmed || tx.Status == TxStatusFailed || tx.Status == TxStatusCancelled) &&
			tx.SentAt.Before(cutoff) {
			delete(m.tracked, hash)
		}
	}
}
