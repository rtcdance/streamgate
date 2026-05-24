package tx

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTxLifecycleManager_Track_MultipleTx(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	for i := 0; i < 5; i++ {
		m.Track(&PendingTx{
			Hash:    fmt.Sprintf("0x%02d", i),
			Nonce:   uint64(i),
			SentAt:  time.Now(),
			ChainID: 1,
		})
	}

	for i := 0; i < 5; i++ {
		status, _, ok := m.GetStatus(fmt.Sprintf("0x%02d", i))
		require.True(t, ok)
		assert.Equal(t, TxStatusPending, status)
	}
}

func TestTxLifecycleManager_ListPending_IncludesBumped(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now(), ChainID: 1})
	m.Track(&PendingTx{Hash: "0x02", Nonce: 2, SentAt: time.Now(), ChainID: 1})

	m.mu.Lock()
	if tx, ok := m.tracked["0x02"]; ok {
		tx.Status = TxStatusBumped
	}
	m.mu.Unlock()

	pending := m.ListPending()
	assert.Len(t, pending, 2)
}

func TestTxLifecycleManager_ListPending_ExcludesFailed(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now(), ChainID: 1})
	m.Track(&PendingTx{Hash: "0x02", Nonce: 2, SentAt: time.Now(), ChainID: 1})

	m.mu.Lock()
	if tx, ok := m.tracked["0x02"]; ok {
		tx.Status = TxStatusFailed
	}
	m.mu.Unlock()

	pending := m.ListPending()
	assert.Len(t, pending, 1)
}

func TestTxLifecycleManager_PruneFinalized_RemovesOld(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now().Add(-15 * time.Minute), ChainID: 1})
	m.Track(&PendingTx{Hash: "0x02", Nonce: 2, SentAt: time.Now().Add(-15 * time.Minute), ChainID: 1})

	m.mu.Lock()
	if tx, ok := m.tracked["0x01"]; ok {
		tx.Status = TxStatusConfirmed
	}
	if tx, ok := m.tracked["0x02"]; ok {
		tx.Status = TxStatusFailed
	}
	m.mu.Unlock()

	m.pruneFinalized()

	_, _, ok1 := m.GetStatus("0x01")
	assert.False(t, ok1)
	_, _, ok2 := m.GetStatus("0x02")
	assert.False(t, ok2)
}

func TestTxLifecycleManager_PruneFinalized_KeepsPending(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now().Add(-15 * time.Minute), ChainID: 1})

	m.pruneFinalized()

	_, _, ok := m.GetStatus("0x01")
	assert.True(t, ok)
}

func TestTxLifecycleManager_PruneFinalized_KeepsCancelled(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())

	m.Track(&PendingTx{Hash: "0x01", Nonce: 1, SentAt: time.Now().Add(-15 * time.Minute), ChainID: 1})

	m.mu.Lock()
	if tx, ok := m.tracked["0x01"]; ok {
		tx.Status = TxStatusCancelled
	}
	m.mu.Unlock()

	m.pruneFinalized()

	_, _, ok := m.GetStatus("0x01")
	assert.False(t, ok)
}

func TestTxLifecycleManager_Stop_Idempotent(t *testing.T) {
	m := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())
	assert.NotPanics(t, func() {
		m.Stop()
		m.Stop()
	})
}

func TestTxLifecycleManager_checkTx_Confirmed(t *testing.T) {
	crypto.GenerateKey()

	client := &mockTxClient{
		receiptFn: func(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
			return &types.Receipt{
				Status:      1,
				BlockNumber: big.NewInt(90),
			}, nil
		},
		blockNumberFn: func(ctx context.Context) (uint64, error) {
			return 95, nil
		},
	}

	m := NewTxLifecycleManager(client, nil, DefaultTxLifecycleConfig(), zap.NewNop())
	m.Track(&PendingTx{Hash: "0xabc123", Nonce: 1, SentAt: time.Now(), ChainID: 1})

	tx := m.tracked["0xabc123"]
	m.checkTx(context.Background(), tx)

	assert.Equal(t, TxStatusConfirmed, tx.Status)
	assert.Equal(t, uint64(5), tx.Confirmations)
}

func TestTxLifecycleManager_checkTx_Failed(t *testing.T) {
	client := &mockTxClient{
		receiptFn: func(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
			return &types.Receipt{
				Status:      0,
				BlockNumber: big.NewInt(90),
			}, nil
		},
	}

	m := NewTxLifecycleManager(client, nil, DefaultTxLifecycleConfig(), zap.NewNop())
	m.Track(&PendingTx{Hash: "0xabc123", Nonce: 1, SentAt: time.Now(), ChainID: 1})

	tx := m.tracked["0xabc123"]
	m.checkTx(context.Background(), tx)

	assert.Equal(t, TxStatusFailed, tx.Status)
}

func TestTxLifecycleManager_checkTx_NotStuck(t *testing.T) {
	client := &mockTxClient{
		receiptFn: func(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
			return nil, fmt.Errorf("not found")
		},
	}

	m := NewTxLifecycleManager(client, nil, DefaultTxLifecycleConfig(), zap.NewNop())
	m.Track(&PendingTx{Hash: "0xabc123", Nonce: 1, SentAt: time.Now(), ChainID: 1})

	tx := m.tracked["0xabc123"]
	m.checkTx(context.Background(), tx)

	assert.Equal(t, TxStatusPending, tx.Status)
}

func TestTxLifecycleManager_checkTx_Stuck_AutoBump(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()

	client := &mockTxClient{
		receiptFn: func(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
			return nil, fmt.Errorf("not found")
		},
		headerByNumFn: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return &types.Header{BaseFee: big.NewInt(2_000_000_000)}, nil
		},
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	kp := &mockKeyProvider{key: privateKey}

	m := NewTxLifecycleManager(client, kp, TxLifecycleConfig{
		PollInterval: 1 * time.Second,
		StuckAfter:   1 * time.Nanosecond,
		MaxBumps:     3,
		BumpPercent:  10,
		RequiredConf: 3,
	}, zap.NewNop())

	m.Track(&PendingTx{
		Hash:         "0xabc123",
		Nonce:        1,
		GasTipCap:    big.NewInt(2_000_000_000),
		MaxFeePerGas: big.NewInt(10_000_000_000),
		IsEIP1559:    true,
		To:           crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:        big.NewInt(0),
		GasLimit:     200000,
		SentAt:       time.Now().Add(-5 * time.Minute),
		ChainID:      1,
	})

	tx := m.tracked["0xabc123"]
	m.checkTx(context.Background(), tx)

	time.Sleep(100 * time.Millisecond)
	m.wg.Wait()

	m.mu.RLock()
	newStatus := tx.Status
	m.mu.RUnlock()
	assert.Equal(t, TxStatusBumped, newStatus)
}

func TestTxLifecycleManager_checkTx_Stuck_MaxBumps_AutoCancel(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()

	client := &mockTxClient{
		receiptFn: func(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
			return nil, fmt.Errorf("not found")
		},
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	kp := &mockKeyProvider{key: privateKey}

	m := NewTxLifecycleManager(client, kp, TxLifecycleConfig{
		PollInterval: 1 * time.Second,
		StuckAfter:   1 * time.Nanosecond,
		MaxBumps:     0,
		BumpPercent:  10,
		RequiredConf: 3,
	}, zap.NewNop())

	m.Track(&PendingTx{
		Hash:      "0xabc123",
		Nonce:     1,
		GasPrice:  big.NewInt(1_000_000_000),
		IsEIP1559: false,
		To:        crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:     big.NewInt(0),
		GasLimit:  200000,
		SentAt:    time.Now().Add(-5 * time.Minute),
		ChainID:   1,
	})

	tx := m.tracked["0xabc123"]
	m.checkTx(context.Background(), tx)

	time.Sleep(100 * time.Millisecond)
	m.wg.Wait()

	m.mu.RLock()
	newStatus := tx.Status
	m.mu.RUnlock()
	assert.Equal(t, TxStatusCancelled, newStatus)
}

func TestTxTracker_BumpGas_InvalidBumpPercent(t *testing.T) {
	tt := NewTxTracker(nil, zap.NewNop())
	pending := &PendingTx{
		Hash:      "0xabc",
		Nonce:     1,
		GasPrice:  big.NewInt(1e9),
		IsEIP1559: false,
		To:        "0x1234567890123456789012345678901234567890",
		SentAt:    time.Now(),
		ChainID:   1,
	}

	_, err := tt.BumpGas(context.Background(), nil, pending, 5)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bump percent must be at least 10")
}

func TestTxTracker_BumpGas_SendError(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return fmt.Errorf("send failed")
		},
	}

	tt := NewTxTracker(client, zap.NewNop())
	pending := &PendingTx{
		Hash:      "0xold",
		Nonce:     5,
		GasPrice:  big.NewInt(1_000_000_000),
		IsEIP1559: false,
		To:        crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:     big.NewInt(0),
		GasLimit:  200000,
		SentAt:    time.Now(),
		ChainID:   1,
	}

	_, err = tt.BumpGas(context.Background(), privateKey, pending, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send bumped tx")
}

func TestTxTracker_BumpGas_EIP1559_SendError(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		headerByNumFn: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return &types.Header{BaseFee: big.NewInt(2_000_000_000)}, nil
		},
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return fmt.Errorf("send failed")
		},
	}

	tt := NewTxTracker(client, zap.NewNop())
	pending := &PendingTx{
		Hash:         "0xold",
		Nonce:        5,
		GasTipCap:    big.NewInt(2_000_000_000),
		MaxFeePerGas: big.NewInt(10_000_000_000),
		IsEIP1559:    true,
		To:           crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:        big.NewInt(0),
		GasLimit:     200000,
		SentAt:       time.Now(),
		ChainID:      1,
	}

	_, err = tt.BumpGas(context.Background(), privateKey, pending, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send bumped eip-1559 tx")
}

func TestTxTracker_CancelTx_EIP1559_HeaderError(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		headerByNumFn: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return nil, fmt.Errorf("header error")
		},
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	tt := NewTxTracker(client, zap.NewNop())
	pending := &PendingTx{
		Hash:         "0xold",
		Nonce:        5,
		GasTipCap:    big.NewInt(2_000_000_000),
		MaxFeePerGas: big.NewInt(10_000_000_000),
		IsEIP1559:    true,
		To:           crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:        big.NewInt(0),
		GasLimit:     200000,
		SentAt:       time.Now(),
		ChainID:      1,
	}

	cancelHash, err := tt.CancelTx(context.Background(), privateKey, pending, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, cancelHash)
}

func TestPendingTx_Fields(t *testing.T) {
	pending := &PendingTx{
		Hash:         "0xabc",
		Nonce:        5,
		GasPrice:     big.NewInt(1e9),
		GasTipCap:    big.NewInt(2e9),
		MaxFeePerGas: big.NewInt(10e9),
		IsEIP1559:    true,
		To:           "0x1234567890123456789012345678901234567890",
		Value:        big.NewInt(0),
		Data:         []byte{0x01, 0x02},
		GasLimit:     200000,
		SentAt:       time.Now(),
		ChainID:      1,
	}
	assert.Equal(t, "0xabc", pending.Hash)
	assert.Equal(t, uint64(5), pending.Nonce)
	assert.True(t, pending.IsEIP1559)
	assert.Equal(t, uint64(200000), pending.GasLimit)
}

func TestTrackedTx_Fields(t *testing.T) {
	tx := &TrackedTx{
		PendingTx: &PendingTx{
			Hash:    "0xabc",
			Nonce:   5,
			SentAt:  time.Now(),
			ChainID: 1,
		},
		Status:        TxStatusPending,
		Confirmations: 0,
		RequiredConf:  3,
		BumpCount:     0,
		MaxBumps:      3,
		BumpPercent:   10,
		StuckAfter:    3 * time.Minute,
	}
	assert.Equal(t, TxStatusPending, tx.Status)
	assert.Equal(t, uint64(0), tx.Confirmations)
	assert.Equal(t, 0, tx.BumpCount)
}

func TestTxStatus_Constants(t *testing.T) {
	assert.Equal(t, TxStatus("pending"), TxStatusPending)
	assert.Equal(t, TxStatus("confirmed"), TxStatusConfirmed)
	assert.Equal(t, TxStatus("failed"), TxStatusFailed)
	assert.Equal(t, TxStatus("bumped"), TxStatusBumped)
	assert.Equal(t, TxStatus("cancelled"), TxStatusCancelled)
}

func TestDefaultTxLifecycleConfig_Values(t *testing.T) {
	cfg := DefaultTxLifecycleConfig()
	assert.Equal(t, 5*time.Second, cfg.PollInterval)
	assert.Equal(t, 3*time.Minute, cfg.StuckAfter)
	assert.Equal(t, 3, cfg.MaxBumps)
	assert.Equal(t, int64(10), cfg.BumpPercent)
	assert.Equal(t, uint64(3), cfg.RequiredConf)
}

func TestIsStuck_TableDriven(t *testing.T) {
	tests := []struct {
		name      string
		sentAgo   time.Duration
		threshold time.Duration
		stuck     bool
	}{
		{"just sent", 0, 3 * time.Minute, false},
		{"1 min ago", 1 * time.Minute, 3 * time.Minute, false},
		{"5 min ago", 5 * time.Minute, 3 * time.Minute, true},
		{"10 min ago", 10 * time.Minute, 3 * time.Minute, true},
		{"exactly at threshold", 3 * time.Minute, 3 * time.Minute, true},
	}

	now := time.Now()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pending := &PendingTx{
				Hash:    "0x01",
				SentAt:  now.Add(-tt.sentAgo),
				ChainID: 1,
			}
			assert.Equal(t, tt.stuck, IsStuck(pending, tt.threshold))
		})
	}
}

func TestContractWriter_SendTx_EIP1559_WithBaseFee(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		headerByNumFn: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return &types.Header{BaseFee: big.NewInt(1_500_000_000)}, nil
		},
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         &mockKeyProvider{key: privateKey},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	parsedABI := getTestABI(t)
	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, result.TxHash)
	assert.NotNil(t, result.MaxFeePerGas)
	assert.NotNil(t, result.TipCap)
}

func TestContractWriter_SendTx_EstimateGasSuccess(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		estimateGasFn: func(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
			return 150000, nil
		},
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         &mockKeyProvider{key: privateKey},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	parsedABI := getTestABI(t)
	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  0,
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(180000), result.GasLimit)
}

func TestNonceManager_NextNonce_SequentialWithNetwork(t *testing.T) {
	var callCount int
	mockClient := &mockNonceClient{
		getNonceFn: func(ctx context.Context, addr string) (uint64, error) {
			callCount++
			return 10, nil
		},
	}

	nm := NewNonceManager(mockClient, zap.NewNop())

	n1, err := nm.NextNonce(context.Background(), "0xaddr")
	require.NoError(t, err)
	assert.Equal(t, uint64(10), n1)

	n2, err := nm.NextNonce(context.Background(), "0xaddr")
	require.NoError(t, err)
	assert.Equal(t, uint64(11), n2)
}

func TestNonceManager_Rollback_NonExistent(t *testing.T) {
	nm := NewNonceManager(nil, zap.NewNop())
	assert.NotPanics(t, func() {
		nm.Rollback("0xnonexistent", 5)
	})
}

func TestNonceManager_Reset_NonExistent(t *testing.T) {
	nm := NewNonceManager(nil, zap.NewNop())
	assert.NotPanics(t, func() {
		nm.Reset("0xnonexistent")
	})
}

func TestNonceManager_ResetAll_Clears(t *testing.T) {
	mockClient := &mockNonceClient{nonce: 0}
	nm := NewNonceManager(mockClient, zap.NewNop())
	nm.states = map[string]*nonceState{
		"0xa": {next: 10, pending: make(map[uint64]struct{})},
		"0xb": {next: 20, pending: make(map[uint64]struct{})},
	}
	nm.lastSync = map[string]time.Time{
		"0xa": time.Now(),
		"0xb": time.Now(),
	}

	nm.ResetAll()

	nm.mu.Lock()
	count := len(nm.states)
	nm.mu.Unlock()
	assert.Equal(t, 0, count)
}

func TestNonceManager_NextNonce_NetworkFallback(t *testing.T) {
	mockClient := &mockNonceClient{
		getNonceFn: func(ctx context.Context, addr string) (uint64, error) {
			return 42, nil
		},
	}
	nm := NewNonceManager(mockClient, zap.NewNop())

	nm.states = map[string]*nonceState{
		"0xaddr": {next: 10, pending: make(map[uint64]struct{})},
	}
	nm.lastSync = map[string]time.Time{
		"0xaddr": time.Now().Add(-2 * time.Hour),
	}

	nonce, err := nm.NextNonce(context.Background(), "0xaddr")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, nonce, uint64(42))
}

func TestNonceManager_NextNonce_WithPendingGaps(t *testing.T) {
	mockClient := &mockNonceClient{nonce: 0}
	nm := NewNonceManager(mockClient, zap.NewNop())

	nm.states = map[string]*nonceState{
		"0xaddr": {next: 15, pending: map[uint64]struct{}{12: {}, 13: {}}},
	}
	nm.lastSync = map[string]time.Time{
		"0xaddr": time.Now(),
	}

	nonce, err := nm.NextNonce(context.Background(), "0xaddr")
	require.NoError(t, err)
	assert.Equal(t, uint64(12), nonce)
}
