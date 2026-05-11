package web3

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestTxLifecycleConfig_Defaults(t *testing.T) {
	cfg := DefaultTxLifecycleConfig()
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("expected PollInterval 5s, got %v", cfg.PollInterval)
	}
	if cfg.StuckAfter != 3*time.Minute {
		t.Errorf("expected StuckAfter 3m, got %v", cfg.StuckAfter)
	}
	if cfg.MaxBumps != 3 {
		t.Errorf("expected MaxBumps 3, got %d", cfg.MaxBumps)
	}
	if cfg.BumpPercent != 10 {
		t.Errorf("expected BumpPercent 10, got %d", cfg.BumpPercent)
	}
	if cfg.RequiredConf != 3 {
		t.Errorf("expected RequiredConf 3, got %d", cfg.RequiredConf)
	}
}

func TestTxLifecycleManager_TrackAndGetStatus(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	tx := &PendingTx{
		Hash:    "0xabc123",
		Nonce:   5,
		SentAt:  time.Now(),
		ChainID: 1,
	}
	m.Track(tx)

	status, conf, ok := m.GetStatus("0xabc123")
	if !ok {
		t.Fatal("expected to find tracked tx")
	}
	if status != TxStatusPending {
		t.Errorf("expected status pending, got %s", status)
	}
	if conf != 0 {
		t.Errorf("expected 0 confirmations, got %d", conf)
	}
}

func TestTxLifecycleManager_GetStatus_NotFound(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())
	_, _, ok := m.GetStatus("0xnonexistent")
	if ok {
		t.Error("expected not found for untracked tx")
	}
}

func TestTxLifecycleManager_ListPending(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now(), ChainID: 1})
	m.Track(&PendingTx{Hash: "0x02", Nonce: 2, SentAt: time.Now(), ChainID: 1})

	pending := m.ListPending()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending txs, got %d", len(pending))
	}
}

func TestTxLifecycleManager_ListPending_ExcludesFinalized(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now(), ChainID: 1})

	// Manually set status to confirmed
	m.mu.Lock()
	if tx, ok := m.tracked["0x01"]; ok {
		tx.Status = TxStatusConfirmed
	}
	m.mu.Unlock()

	pending := m.ListPending()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after confirmation, got %d", len(pending))
	}
}

func TestTxLifecycleManager_PruneFinalized(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	// Add a confirmed tx sent 15 minutes ago (beyond 10-min prune cutoff)
	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now().Add(-15 * time.Minute), ChainID: 1})
	m.mu.Lock()
	if tx, ok := m.tracked["0x01"]; ok {
		tx.Status = TxStatusConfirmed
	}
	m.mu.Unlock()

	m.pruneFinalized()

	_, _, ok := m.GetStatus("0x01")
	if ok {
		t.Error("expected old confirmed tx to be pruned")
	}
}

func TestTxLifecycleManager_PruneFinalized_KeepsRecent(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	// Add a confirmed tx sent 2 minutes ago (within 10-min cutoff)
	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now().Add(-2 * time.Minute), ChainID: 1})
	m.mu.Lock()
	if tx, ok := m.tracked["0x01"]; ok {
		tx.Status = TxStatusConfirmed
	}
	m.mu.Unlock()

	m.pruneFinalized()

	_, _, ok := m.GetStatus("0x01")
	if !ok {
		t.Error("expected recent confirmed tx to be kept")
	}
}

func TestIsStuck(t *testing.T) {
	pending := &PendingTx{
		Hash:   "0x01",
		SentAt: time.Now().Add(-5 * time.Minute),
	}

	if !IsStuck(pending, 3*time.Minute) {
		t.Error("expected tx sent 5m ago to be stuck with 3m threshold")
	}
	if IsStuck(pending, 10*time.Minute) {
		t.Error("expected tx sent 5m ago to NOT be stuck with 10m threshold")
	}
}

func TestTxStatus_Values(t *testing.T) {
	statuses := map[TxStatus]bool{
		TxStatusPending:   true,
		TxStatusConfirmed: true,
		TxStatusFailed:    true,
		TxStatusBumped:    true,
		TxStatusCancelled: true,
	}
	if len(statuses) != 5 {
		t.Errorf("expected 5 distinct TxStatus values, got %d", len(statuses))
	}
}
