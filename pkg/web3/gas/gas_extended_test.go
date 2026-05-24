package gas

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockGasPricer struct {
	price    *big.Int
	err      error
	callCount int
}

func (m *mockGasPricer) GetGasPrice(ctx context.Context) (*big.Int, error) {
	m.callCount++
	return m.price, m.err
}

func TestTransactionQueue_NewTransactionQueue(t *testing.T) {
	tq := NewTransactionQueue(10)
	require.NotNil(t, tq)
	assert.Equal(t, 0, tq.GetQueueSize())
	assert.Equal(t, 10, tq.maxSize)
}

func TestTransactionQueue_Enqueue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tq := NewTransactionQueue(3)
		tx := &QueuedTransaction{ID: "tx1", From: "0x1", To: "0x2"}
		err := tq.Enqueue(tx)
		require.NoError(t, err)
		assert.Equal(t, "pending", tx.Status)
		assert.False(t, tx.CreatedAt.IsZero())
		assert.False(t, tx.UpdatedAt.IsZero())
		assert.Equal(t, 1, tq.GetQueueSize())
	})

	t.Run("queue_full", func(t *testing.T) {
		tq := NewTransactionQueue(2)
		require.NoError(t, tq.Enqueue(&QueuedTransaction{ID: "1"}))
		require.NoError(t, tq.Enqueue(&QueuedTransaction{ID: "2"}))
		err := tq.Enqueue(&QueuedTransaction{ID: "3"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "full")
	})
}

func TestTransactionQueue_Dequeue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tq := NewTransactionQueue(10)
		_ = tq.Enqueue(&QueuedTransaction{ID: "tx1"})
		_ = tq.Enqueue(&QueuedTransaction{ID: "tx2"})

		first := tq.Dequeue()
		require.NotNil(t, first)
		assert.Equal(t, "tx1", first.ID)
		assert.Equal(t, 1, tq.GetQueueSize())

		second := tq.Dequeue()
		require.NotNil(t, second)
		assert.Equal(t, "tx2", second.ID)
		assert.Equal(t, 0, tq.GetQueueSize())
	})

	t.Run("empty_queue", func(t *testing.T) {
		tq := NewTransactionQueue(10)
		result := tq.Dequeue()
		assert.Nil(t, result)
	})
}

func TestTransactionQueue_GetTransactions(t *testing.T) {
	tq := NewTransactionQueue(10)
	_ = tq.Enqueue(&QueuedTransaction{ID: "tx1"})
	_ = tq.Enqueue(&QueuedTransaction{ID: "tx2"})

	txs := tq.GetTransactions()
	assert.Len(t, txs, 2)
	assert.Equal(t, "tx1", txs[0].ID)
	assert.Equal(t, "tx2", txs[1].ID)

	txs[0].ID = "modified"
	original := tq.GetTransactions()
	assert.Equal(t, "modified", original[0].ID)
}

func TestTransactionQueue_UpdateTransactionStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tq := NewTransactionQueue(10)
		_ = tq.Enqueue(&QueuedTransaction{ID: "tx1"})
		err := tq.UpdateTransactionStatus("tx1", "confirmed")
		require.NoError(t, err)
		txs := tq.GetTransactions()
		assert.Equal(t, "confirmed", txs[0].Status)
	})

	t.Run("not_found", func(t *testing.T) {
		tq := NewTransactionQueue(10)
		err := tq.UpdateTransactionStatus("nonexistent", "confirmed")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGasMonitor_NewGasMonitor(t *testing.T) {
	mock := &mockGasPricer{price: big.NewInt(1e9)}
	gm := NewGasMonitor(mock, zap.NewNop())
	require.NotNil(t, gm)
	assert.Equal(t, 30*time.Second, gm.updateInterval)
	assert.Nil(t, gm.feeEstimator)
}

func TestGasMonitor_NewGasMonitorWithFeeHistory(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		gasPrice: big.NewInt(1e9),
	}
	gm := NewGasMonitorWithFeeHistory(provider, zap.NewNop(), 5, []float64{25, 50, 75})
	require.NotNil(t, gm)
	assert.NotNil(t, gm.feeEstimator)
}

func TestGasMonitor_StartAndStop(t *testing.T) {
	mock := &mockGasPricer{price: big.NewInt(2_000_000_000)}
	gm := NewGasMonitor(mock, zap.NewNop())
	gm.updateInterval = 50 * time.Millisecond

	ctx := context.Background()
	err := gm.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, big.NewInt(2_000_000_000).Cmp(gm.GetGasPrice()))

	gm.Stop()
}

func TestGasMonitor_Start_Error(t *testing.T) {
	mock := &mockGasPricer{err: assert.AnError}
	gm := NewGasMonitor(mock, zap.NewNop())

	err := gm.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "initial gas price")
}

func TestGasMonitor_GetGasPriceInGwei(t *testing.T) {
	mock := &mockGasPricer{price: big.NewInt(3_000_000_000)}
	gm := NewGasMonitor(mock, zap.NewNop())
	gm.currentPrice = big.NewInt(3_000_000_000)

	gwei := gm.GetGasPriceInGwei()
	assert.InDelta(t, 3.0, gwei, 0.01)
}

func TestGasMonitor_GetGasPriceInGwei_Nil(t *testing.T) {
	mock := &mockGasPricer{}
	gm := NewGasMonitor(mock, zap.NewNop())

	gwei := gm.GetGasPriceInGwei()
	assert.Equal(t, 0.0, gwei)
}

func TestGasMonitor_EstimateGasCost(t *testing.T) {
	mock := &mockGasPricer{price: big.NewInt(2_000_000_000)}
	gm := NewGasMonitor(mock, zap.NewNop())
	gm.currentPrice = big.NewInt(2_000_000_000)

	cost := gm.EstimateGasCost(21000)
	expected := new(big.Int).Mul(big.NewInt(21000), big.NewInt(2_000_000_000))
	assert.Equal(t, 0, expected.Cmp(cost))
}

func TestGasMonitor_EstimateGasCost_NilPrice(t *testing.T) {
	mock := &mockGasPricer{}
	gm := NewGasMonitor(mock, zap.NewNop())

	cost := gm.EstimateGasCost(21000)
	assert.Equal(t, 0, big.NewInt(0).Cmp(cost))
}

func TestGasMonitor_EstimateGasCostInEther(t *testing.T) {
	mock := &mockGasPricer{price: big.NewInt(10_000_000_000)}
	gm := NewGasMonitor(mock, zap.NewNop())
	gm.currentPrice = big.NewInt(10_000_000_000)

	ether := gm.EstimateGasCostInEther(21000)
	assert.Greater(t, ether, 0.0)
}

func TestFeeHistoryEstimator_EstimateTimes(t *testing.T) {
	tests := []struct {
		name      string
		ratio     float64
		expected  [3]string
	}{
		{"very_low", 0.1, [3]string{"> 60 seconds", "30-60 seconds", "< 30 seconds"}},
		{"low", 0.4, [3]string{"> 30 seconds", "15-30 seconds", "< 15 seconds"}},
		{"medium", 0.7, [3]string{"30-60 seconds", "15-30 seconds", "< 15 seconds"}},
		{"high", 0.9, [3]string{"1-3 minutes", "30-60 seconds", "< 30 seconds"}},
	}

	estimator := NewFeeHistoryEstimator(nil, zap.NewNop(), 10, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := estimator.estimateTimes(tc.ratio)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFeeHistoryEstimator_FallbackAllRetriesFail(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistoryErr: assert.AnError,
		gasPriceErr:   assert.AnError,
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 5, []float64{25, 50, 75})
	_, err := estimator.EIP1559GasLevels(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get gas price for fallback")
}

func TestFeeHistoryEstimator_CacheHit(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(2_000_000_000), big.NewInt(2_500_000_000)},
			GasUsedRatio: []float64{0.5},
			Reward:       [][]*big.Int{{big.NewInt(1_000_000_000), big.NewInt(1_500_000_000), big.NewInt(2_000_000_000)}},
		},
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 5, []float64{25, 50, 75})
	levels1, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)

	levels2, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)

	assert.Equal(t, levels1[0].GasPrice.String(), levels2[0].GasPrice.String())
}

func TestFeeHistoryEstimator_TipCapFallbackFails(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(2_000_000_000), big.NewInt(2_000_000_000)},
			GasUsedRatio: []float64{0.5},
			Reward:       nil,
		},
		tipCapErr:   assert.AnError,
		gasPrice:    big.NewInt(3_000_000_000),
		gasPriceErr: nil,
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 1, []float64{25, 50, 75})
	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)
	assert.Nil(t, levels[0].MaxPriorityFeePerGas)
}

func TestGasMonitor_GetGasPriceLevels_FeeHistoryError(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistoryErr: assert.AnError,
		gasPrice:      big.NewInt(3_000_000_000),
	}

	gm := NewGasMonitorWithFeeHistory(provider, zap.NewNop(), 5, []float64{25, 50, 75})
	gm.currentPrice = big.NewInt(3_000_000_000)

	levels, err := gm.GetGasPriceLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)
}
