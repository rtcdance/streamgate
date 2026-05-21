package tx

import (
	"context"
	"crypto/ecdsa"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type TxStatus string

const (
	TxStatusPending   TxStatus = "pending"
	TxStatusConfirmed TxStatus = "confirmed"
	TxStatusFailed    TxStatus = "failed"
	TxStatusBumped    TxStatus = "bumped"
	TxStatusCancelled TxStatus = "cancelled"
)

type TrackedTx struct {
	*PendingTx
	Status        TxStatus
	Confirmations uint64
	RequiredConf  uint64
	BumpCount     int
	MaxBumps      int
	BumpPercent   int64
	StuckAfter    time.Duration
}

type TxLifecycleConfig struct {
	PollInterval time.Duration
	StuckAfter   time.Duration
	MaxBumps     int
	BumpPercent  int64
	RequiredConf uint64
}

func DefaultTxLifecycleConfig() TxLifecycleConfig {
	return TxLifecycleConfig{
		PollInterval: 5 * time.Second,
		StuckAfter:   3 * time.Minute,
		MaxBumps:     3,
		BumpPercent:  10,
		RequiredConf: 3,
	}
}

type TxLifecycleManager struct {
	mu          sync.RWMutex
	tracked     map[string]*TrackedTx
	client      Client
	keyProvider KeyProvider
	config      TxLifecycleConfig
	logger      *zap.Logger
	stopCh      chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

func NewTxLifecycleManager(client Client, keyProvider KeyProvider, config TxLifecycleConfig, logger *zap.Logger) *TxLifecycleManager {
	return &TxLifecycleManager{
		tracked:     make(map[string]*TrackedTx),
		client:      client,
		keyProvider: keyProvider,
		config:      config,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

func (m *TxLifecycleManager) Track(tx *PendingTx) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tracked[tx.Hash] = &TrackedTx{
		PendingTx:    tx,
		Status:       TxStatusPending,
		RequiredConf: m.config.RequiredConf,
		MaxBumps:     m.config.MaxBumps,
		BumpPercent:  m.config.BumpPercent,
		StuckAfter:   m.config.StuckAfter,
	}

	m.logger.Info("Tracking transaction",
		zap.String("tx_hash", tx.Hash),
		zap.Int64("chain_id", tx.ChainID),
		zap.Uint64("nonce", tx.Nonce))
}

func (m *TxLifecycleManager) GetStatus(txHash string) (TxStatus, uint64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tx, ok := m.tracked[txHash]
	if !ok {
		return "", 0, false
	}
	return tx.Status, tx.Confirmations, true
}

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

func (m *TxLifecycleManager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.wg.Wait()
}

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

	m.pruneFinalized()
}

func (m *TxLifecycleManager) checkTx(ctx context.Context, tx *TrackedTx) {
	m.mu.RLock()
	txHash := tx.Hash
	maxBumps := tx.MaxBumps
	bumpPercent := tx.BumpPercent
	requiredConf := tx.RequiredConf
	m.mu.RUnlock()

	receipt, err := m.client.TransactionReceipt(ctx, common.HexToHash(txHash))
	if err == nil && receipt != nil {
		if receipt.Status == 1 {
			blockNum, blockErr := m.client.GetBlockNumber(ctx)
			if blockErr == nil && blockNum >= receipt.BlockNumber.Uint64()+requiredConf {
				m.mu.Lock()
				tx.Status = TxStatusConfirmed
				tx.Confirmations = blockNum - receipt.BlockNumber.Uint64()
				m.mu.Unlock()
				m.logger.Info("Transaction confirmed",
					zap.String("tx_hash", txHash),
					zap.Uint64("confirmations", tx.Confirmations))
			}
		} else {
			m.mu.Lock()
			tx.Status = TxStatusFailed
			m.mu.Unlock()
			m.logger.Warn("Transaction reverted on-chain",
				zap.String("tx_hash", txHash))
		}
		return
	}

	if !IsStuck(tx.PendingTx, tx.StuckAfter) {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	bumpCount := tx.BumpCount
	if bumpCount >= maxBumps {
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
