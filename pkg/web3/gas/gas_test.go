package gas

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockFeeHistoryProvider implements FeeHistoryProvider for testing.
type mockFeeHistoryProvider struct {
	feeHistory    *ethereum.FeeHistory
	feeHistoryErr error
	tipCap        *big.Int
	tipCapErr     error
	gasPrice      *big.Int
	gasPriceErr   error
}

func (m *mockFeeHistoryProvider) FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error) {
	return m.feeHistory, m.feeHistoryErr
}

func (m *mockFeeHistoryProvider) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return m.tipCap, m.tipCapErr
}

func (m *mockFeeHistoryProvider) GetGasPrice(ctx context.Context) (*big.Int, error) {
	return m.gasPrice, m.gasPriceErr
}

func TestFeeHistoryEstimator_EIP1559GasLevels(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(2_000_000_000), big.NewInt(2_500_000_000)}, // 2 Gwei, predicted 2.5 Gwei
			GasUsedRatio: []float64{0.5, 0.6, 0.4, 0.55, 0.5},
			Reward: [][]*big.Int{
				{big.NewInt(500_000_000), big.NewInt(1_000_000_000), big.NewInt(2_000_000_000)},
				{big.NewInt(600_000_000), big.NewInt(1_100_000_000), big.NewInt(2_100_000_000)},
			},
		},
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 5, []float64{25, 50, 75})
	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)

	// Check that levels are populated
	assert.Equal(t, "safe", levels[0].Level)
	assert.Equal(t, "standard", levels[1].Level)
	assert.Equal(t, "fast", levels[2].Level)

	// EIP-1559 fields should be populated
	for _, level := range levels {
		assert.NotNil(t, level.BaseFee, "BaseFee should be set")
		assert.NotNil(t, level.MaxPriorityFeePerGas, "MaxPriorityFeePerGas should be set")
		assert.NotNil(t, level.MaxFeePerGas, "MaxFeePerGas should be set")
		assert.Greater(t, level.Gwei, 0.0, "Gwei should be positive")
		assert.NotEmpty(t, level.EstimatedTime, "EstimatedTime should be set")
	}

	// fast should have higher MaxFeePerGas than standard
	assert.True(t, levels[2].MaxFeePerGas.Cmp(levels[1].MaxFeePerGas) > 0,
		"fast MaxFeePerGas should be higher than standard")
	// standard should have higher MaxFeePerGas than safe
	assert.True(t, levels[1].MaxFeePerGas.Cmp(levels[0].MaxFeePerGas) > 0,
		"standard MaxFeePerGas should be higher than safe")
}

func TestFeeHistoryEstimator_HighCongestion(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(10_000_000_000), big.NewInt(12_000_000_000)},
			GasUsedRatio: []float64{0.9, 0.95, 0.85, 0.92}, // high congestion > 0.8
			Reward: [][]*big.Int{
				{big.NewInt(1_000_000_000), big.NewInt(2_000_000_000), big.NewInt(5_000_000_000)},
				{big.NewInt(1_200_000_000), big.NewInt(2_200_000_000), big.NewInt(5_500_000_000)},
			},
		},
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 4, []float64{25, 50, 75})
	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)

	// Under high congestion, fast tier should be boosted (3x baseFee + 2x tip)
	assert.Equal(t, "1-3 minutes", levels[0].EstimatedTime)
	assert.Equal(t, "30-60 seconds", levels[1].EstimatedTime)
	assert.Equal(t, "< 30 seconds", levels[2].EstimatedTime)
}

func TestFeeHistoryEstimator_LowCongestion(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(1_000_000_000), big.NewInt(900_000_000)},
			GasUsedRatio: []float64{0.1, 0.2, 0.15}, // low congestion < 0.3
			Reward: [][]*big.Int{
				{big.NewInt(100_000_000), big.NewInt(500_000_000), big.NewInt(1_000_000_000)},
			},
		},
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 3, []float64{25, 50, 75})
	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "> 60 seconds", levels[0].EstimatedTime)
	assert.Equal(t, "30-60 seconds", levels[1].EstimatedTime)
	assert.Equal(t, "< 30 seconds", levels[2].EstimatedTime)
}

func TestFeeHistoryEstimator_FallbackOnFeeHistoryError(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistoryErr: assert.AnError,
		gasPrice:      big.NewInt(3_000_000_000), // 3 Gwei
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 5, []float64{25, 50, 75})
	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)

	// Fallback: simple multipliers (1x, 1x, 2x), no EIP-1559 fields
	assert.Equal(t, "safe", levels[0].Level)
	assert.Equal(t, "standard", levels[1].Level)
	assert.Equal(t, "fast", levels[2].Level)
	assert.Nil(t, levels[0].MaxPriorityFeePerGas, "fallback should not set EIP-1559 fields")
	assert.Nil(t, levels[0].MaxFeePerGas)
	assert.Nil(t, levels[0].BaseFee)
}

func TestFeeHistoryEstimator_FallbackOnEmptyData(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{},
			GasUsedRatio: []float64{},
		},
		gasPrice: big.NewInt(2_000_000_000),
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 5, []float64{25, 50, 75})
	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)
	assert.Equal(t, "safe", levels[0].Level)
}

func TestFeeHistoryEstimator_NoRewardData(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(2_000_000_000), big.NewInt(2_000_000_000)},
			GasUsedRatio: []float64{0.5},
			Reward:       nil, // no reward data
		},
		tipCap:   big.NewInt(1_500_000_000), // 1.5 Gwei
		gasPrice: big.NewInt(3_000_000_000),
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 1, []float64{25, 50, 75})
	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)

	// Should use SuggestGasTipCap as fallback: low=0.75, med=1.5, high=3.0 Gwei
	assert.NotNil(t, levels[0].MaxPriorityFeePerGas)
	assert.NotNil(t, levels[1].MaxPriorityFeePerGas)
	assert.NotNil(t, levels[2].MaxPriorityFeePerGas)
}

func TestFeeHistoryEstimator_MinTipOneGwei(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(2_000_000_000), big.NewInt(2_000_000_000)},
			GasUsedRatio: []float64{0.3},
			Reward: [][]*big.Int{
				{big.NewInt(100_000), big.NewInt(500_000), big.NewInt(1_000_000)}, // all < 1 Gwei
			},
		},
	}

	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 1, []float64{25, 50, 75})
	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)

	oneGwei := big.NewInt(1_000_000_000)
	for _, level := range levels {
		assert.True(t, level.MaxPriorityFeePerGas.Cmp(oneGwei) >= 0,
			"tip should be at least 1 Gwei, got %s", level.MaxPriorityFeePerGas.String())
	}
}

func TestFeeHistoryEstimator_Defaults(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(2_000_000_000)},
			GasUsedRatio: []float64{0.5},
			Reward:       [][]*big.Int{{big.NewInt(1_000_000_000), big.NewInt(1_500_000_000), big.NewInt(2_000_000_000)}},
		},
	}

	// Test defaults: blockCount=0 → 10, percentiles=nil → [25,50,75]
	estimator := NewFeeHistoryEstimator(provider, zap.NewNop(), 0, nil)
	assert.Equal(t, uint64(10), estimator.blockCount)
	assert.Equal(t, []float64{25, 50, 75}, estimator.percentiles)

	levels, err := estimator.EIP1559GasLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)
}

func TestGasMonitor_GetGasPriceLevels_LegacyFallback(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		gasPrice: big.NewInt(3_000_000_000),
	}

	gm := NewGasMonitor(provider, zap.NewNop())
	gm.currentPrice = big.NewInt(3_000_000_000)

	levels, err := gm.GetGasPriceLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)

	assert.Equal(t, "safe", levels[0].Level)
	assert.Equal(t, "standard", levels[1].Level)
	assert.Equal(t, "fast", levels[2].Level)

	// Legacy mode: 1x, 1x, 2x
	assert.Equal(t, 0, levels[0].GasPrice.Cmp(big.NewInt(3_000_000_000)))
	assert.Equal(t, 0, levels[1].GasPrice.Cmp(big.NewInt(3_000_000_000)))
	assert.Equal(t, 0, levels[2].GasPrice.Cmp(big.NewInt(6_000_000_000)))
}

func TestGasMonitor_GetGasPriceLevels_NilPrice(t *testing.T) {
	provider := &mockFeeHistoryProvider{}
	gm := NewGasMonitor(provider, zap.NewNop())

	levels, err := gm.GetGasPriceLevels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, levels)
}

func TestGasMonitor_WithFeeHistory(t *testing.T) {
	provider := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(2_000_000_000), big.NewInt(2_500_000_000)},
			GasUsedRatio: []float64{0.5},
			Reward:       [][]*big.Int{{big.NewInt(1_000_000_000), big.NewInt(1_500_000_000), big.NewInt(2_000_000_000)}},
		},
	}

	gm := NewGasMonitorWithFeeHistory(provider, zap.NewNop(), 5, []float64{25, 50, 75})
	require.NotNil(t, gm.feeEstimator)

	levels, err := gm.GetGasPriceLevels(context.Background())
	require.NoError(t, err)
	require.Len(t, levels, 3)

	// Should have EIP-1559 fields
	for _, level := range levels {
		assert.NotNil(t, level.BaseFee)
		assert.NotNil(t, level.MaxPriorityFeePerGas)
		assert.NotNil(t, level.MaxFeePerGas)
	}
}

func TestGasEstimate_EIP1559Fields(t *testing.T) {
	est := GasEstimate{
		StandardGas:          21000,
		FastGas:              21000,
		SafeGasPrice:         big.NewInt(1_000_000_000),
		StandardGasPrice:     big.NewInt(2_000_000_000),
		FastGasPrice:         big.NewInt(4_000_000_000),
		BaseFee:              big.NewInt(1_500_000_000),
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(4_000_000_000),
	}

	assert.NotNil(t, est.BaseFee)
	assert.NotNil(t, est.MaxPriorityFeePerGas)
	assert.NotNil(t, est.MaxFeePerGas)
}

func TestGasPrice_EIP1559Fields(t *testing.T) {
	gp := GasPrice{
		Level:                "standard",
		GasPrice:             big.NewInt(4_000_000_000),
		Gwei:                 4.0,
		EstimatedTime:        "15-30 seconds",
		BaseFee:              big.NewInt(1_500_000_000),
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(4_000_000_000),
	}

	assert.Equal(t, "standard", gp.Level)
	assert.Equal(t, 4.0, gp.Gwei)
	assert.NotNil(t, gp.BaseFee)
	assert.NotNil(t, gp.MaxPriorityFeePerGas)
	assert.NotNil(t, gp.MaxFeePerGas)
}

// --- Benchmarks ---

func BenchmarkToGwei(b *testing.B) {
	wei := big.NewInt(2_500_000_000) // 2.5 Gwei
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		toGwei(wei)
	}
}

func BenchmarkFeeHistoryEstimator_EIP1559GasLevels(b *testing.B) {
	reward := []*big.Int{big.NewInt(1_000_000_000)}
	mock := &mockFeeHistoryProvider{
		feeHistory: &ethereum.FeeHistory{
			OldestBlock:  big.NewInt(100),
			BaseFee:      []*big.Int{big.NewInt(1_500_000_000), big.NewInt(1_600_000_000)},
			GasUsedRatio: []float64{0.5, 0.6},
			Reward:       [][]*big.Int{reward, reward},
		},
		tipCap:   big.NewInt(1_000_000_000),
		gasPrice: big.NewInt(3_000_000_000),
	}
	estimator := NewFeeHistoryEstimator(mock, zap.NewNop(), 10, []float64{25, 50, 75})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = estimator.EIP1559GasLevels(context.Background())
	}
}
