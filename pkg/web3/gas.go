package web3

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"go.uber.org/zap"
)

// GasPricer abstracts gas price queries. *ChainClient satisfies this interface.
//
//go:generate mockgen -destination=mocks/mock_gas_pricer.go -package=mocks streamgate/pkg/web3 GasPricer
type GasPricer interface {
	GetGasPrice(ctx context.Context) (*big.Int, error)
}

// FeeHistoryProvider abstracts eth_feeHistory queries. *ChainClient satisfies this interface.
//
//go:generate mockgen -destination=mocks/mock_fee_history_provider.go -package=mocks streamgate/pkg/web3 FeeHistoryProvider
type FeeHistoryProvider interface {
	FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	GetGasPrice(ctx context.Context) (*big.Int, error)
}

// FeeHistoryEstimator uses eth_feeHistory to compute EIP-1559 gas price levels
// with percentile-based priority fee suggestions.
type EIP1559Levels []*GasPrice

type gasCacheEntry struct {
	levels   *EIP1559Levels
	cachedAt time.Time
}

type FeeHistoryEstimator struct {
	provider    FeeHistoryProvider
	logger      *zap.Logger
	blockCount  uint64
	percentiles []float64
	gasCache    atomic.Value
}

// NewFeeHistoryEstimator creates a new estimator.
// blockCount defaults to 10 if 0; percentiles defaults to [25, 50, 75] if empty.
func NewFeeHistoryEstimator(provider FeeHistoryProvider, logger *zap.Logger, blockCount uint64, percentiles []float64) *FeeHistoryEstimator {
	if blockCount == 0 {
		blockCount = 10
	}
	if len(percentiles) == 0 {
		percentiles = []float64{25, 50, 75}
	}
	return &FeeHistoryEstimator{
		provider:    provider,
		logger:      logger,
		blockCount:  blockCount,
		percentiles: percentiles,
	}
}

// EIP1559GasLevels returns gas price levels derived from eth_feeHistory.
// Falls back to simple multiplier-based estimation on error.
func (fe *FeeHistoryEstimator) EIP1559GasLevels(ctx context.Context) ([]*GasPrice, error) {
	if v := fe.gasCache.Load(); v != nil {
		if entry, ok := v.(*gasCacheEntry); ok && time.Since(entry.cachedAt) < 15*time.Second {
			return *entry.levels, nil
		}
	}

	fh, err := fe.provider.FeeHistory(ctx, fe.blockCount, nil, fe.percentiles)
	if err != nil {
		fe.logger.Warn("FeeHistory failed, falling back to SuggestGasPrice", zap.Error(err))
		return fe.fallbackLevels(ctx)
	}

	if len(fh.BaseFee) == 0 || len(fh.GasUsedRatio) == 0 {
		fe.logger.Warn("FeeHistory returned empty data, falling back")
		return fe.fallbackLevels(ctx)
	}

	// FeeHistory returns blockCount+1 base fees: BaseFee[0] is the oldest block
	// in the range, and BaseFee[len-1] is the predicted next-block base fee.
	predictedBaseFee := fh.BaseFee[len(fh.BaseFee)-1]

	// Compute weighted average gasUsedRatio for congestion assessment
	var totalRatio float64
	for _, r := range fh.GasUsedRatio {
		totalRatio += r
	}
	avgRatio := totalRatio / float64(len(fh.GasUsedRatio))

	// Compute priority fee percentiles from Reward array
	var lowTip, medTip, highTip *big.Int
	if len(fh.Reward) > 0 && len(fh.Reward[0]) >= 3 {
		var lowSum, medSum, highSum big.Int
		validBlocks := 0
		for _, rewards := range fh.Reward {
			if len(rewards) >= 3 {
				lowSum.Add(&lowSum, rewards[0])
				medSum.Add(&medSum, rewards[1])
				highSum.Add(&highSum, rewards[2])
				validBlocks++
			}
		}
		if validBlocks > 0 {
			lowTip = new(big.Int).Div(&lowSum, big.NewInt(int64(validBlocks)))
			medTip = new(big.Int).Div(&medSum, big.NewInt(int64(validBlocks)))
			highTip = new(big.Int).Div(&highSum, big.NewInt(int64(validBlocks)))
		}
	}

	// If no reward data, fallback to SuggestGasTipCap
	if lowTip == nil {
		tipCap, err := fe.provider.SuggestGasTipCap(ctx)
		if err != nil {
			return fe.fallbackLevels(ctx)
		}
		lowTip = new(big.Int).Div(tipCap, big.NewInt(2))
		medTip = tipCap
		highTip = new(big.Int).Mul(tipCap, big.NewInt(2))
	}

	// Ensure minimum tip of 1 Gwei
	oneGwei := big.NewInt(1_000_000_000)
	if lowTip.Cmp(oneGwei) < 0 {
		lowTip = new(big.Int).Set(oneGwei)
	}
	if medTip.Cmp(oneGwei) < 0 {
		medTip = new(big.Int).Set(oneGwei)
	}
	if highTip.Cmp(oneGwei) < 0 {
		highTip = new(big.Int).Set(oneGwei)
	}

	// MaxFeePerGas = 2 * baseFee + tip (standard EIP-1559 formula)
	safeMaxFee := new(big.Int).Add(new(big.Int).Mul(predictedBaseFee, big.NewInt(2)), lowTip)
	standardMaxFee := new(big.Int).Add(new(big.Int).Mul(predictedBaseFee, big.NewInt(2)), medTip)
	fastMaxFee := new(big.Int).Add(new(big.Int).Mul(predictedBaseFee, big.NewInt(2)), highTip)

	// Under high congestion (>80% gas usage), boost fast tier
	if avgRatio > 0.8 {
		boost := new(big.Int).Mul(highTip, big.NewInt(2))
		fastMaxFee = new(big.Int).Add(new(big.Int).Mul(predictedBaseFee, big.NewInt(3)), boost)
	}

	estimatedTimes := fe.estimateTimes(avgRatio)

	levels := EIP1559Levels{
		{
			Level:                "safe",
			GasPrice:             safeMaxFee,
			Gwei:                 toGwei(safeMaxFee),
			EstimatedTime:        estimatedTimes[0],
			BaseFee:              predictedBaseFee,
			MaxPriorityFeePerGas: lowTip,
			MaxFeePerGas:         safeMaxFee,
		},
		{
			Level:                "standard",
			GasPrice:             standardMaxFee,
			Gwei:                 toGwei(standardMaxFee),
			EstimatedTime:        estimatedTimes[1],
			BaseFee:              predictedBaseFee,
			MaxPriorityFeePerGas: medTip,
			MaxFeePerGas:         standardMaxFee,
		},
		{
			Level:                "fast",
			GasPrice:             fastMaxFee,
			Gwei:                 toGwei(fastMaxFee),
			EstimatedTime:        estimatedTimes[2],
			BaseFee:              predictedBaseFee,
			MaxPriorityFeePerGas: highTip,
			MaxFeePerGas:         fastMaxFee,
		},
	}

	fe.gasCache.Store(&gasCacheEntry{levels: &levels, cachedAt: time.Now()})

	return levels, nil
}

// fallbackLevels returns simple multiplier-based gas levels when FeeHistory is unavailable.
func (fe *FeeHistoryEstimator) fallbackLevels(ctx context.Context) ([]*GasPrice, error) {
	if v := fe.gasCache.Load(); v != nil {
		if entry, ok := v.(*gasCacheEntry); ok && time.Since(entry.cachedAt) < 15*time.Second {
			return *entry.levels, nil
		}
	}

	gasPrice, err := fe.provider.GetGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price for fallback: %w", err)
	}

	safe := new(big.Int).Mul(gasPrice, big.NewInt(1))
	standard := new(big.Int).Mul(gasPrice, big.NewInt(1))
	fast := new(big.Int).Mul(gasPrice, big.NewInt(2))

	levels := EIP1559Levels{
		{Level: "safe", GasPrice: safe, Gwei: toGwei(safe), EstimatedTime: "> 30 seconds"},
		{Level: "standard", GasPrice: standard, Gwei: toGwei(standard), EstimatedTime: "15-30 seconds"},
		{Level: "fast", GasPrice: fast, Gwei: toGwei(fast), EstimatedTime: "< 15 seconds"},
	}

	fe.gasCache.Store(&gasCacheEntry{levels: &levels, cachedAt: time.Now()})

	return levels, nil
}

// estimateTimes returns estimated confirmation times based on network congestion.
func (fe *FeeHistoryEstimator) estimateTimes(avgRatio float64) [3]string {
	switch {
	case avgRatio < 0.3:
		return [3]string{"> 60 seconds", "30-60 seconds", "< 30 seconds"}
	case avgRatio < 0.6:
		return [3]string{"> 30 seconds", "15-30 seconds", "< 15 seconds"}
	case avgRatio < 0.8:
		return [3]string{"30-60 seconds", "15-30 seconds", "< 15 seconds"}
	default:
		return [3]string{"1-3 minutes", "30-60 seconds", "< 30 seconds"}
	}
}

// GasMonitor monitors gas prices
type GasMonitor struct {
	client         GasPricer
	feeEstimator   *FeeHistoryEstimator // optional: nil = legacy multiplier mode
	logger         *zap.Logger
	currentPrice   *big.Int
	mu             sync.RWMutex
	wg             sync.WaitGroup // waits for background goroutine to exit
	updateTicker   *time.Ticker
	stopChan       chan struct{}
	stopOnce       sync.Once // ensures stopChan is closed only once
	updateInterval time.Duration
}

// NewGasMonitor creates a new gas monitor
func NewGasMonitor(client GasPricer, logger *zap.Logger) *GasMonitor {
	return &GasMonitor{
		client:         client,
		logger:         logger,
		updateInterval: 30 * time.Second,
		stopChan:       make(chan struct{}),
	}
}

// NewGasMonitorWithFeeHistory creates a gas monitor with EIP-1559 FeeHistory support.
func NewGasMonitorWithFeeHistory(provider FeeHistoryProvider, logger *zap.Logger, blockCount uint64, percentiles []float64) *GasMonitor {
	return &GasMonitor{
		client:         provider,
		feeEstimator:   NewFeeHistoryEstimator(provider, logger, blockCount, percentiles),
		logger:         logger,
		updateInterval: 30 * time.Second,
		stopChan:       make(chan struct{}),
	}
}

// Start starts the gas monitor. The ctx parameter applies only to the initial
// gas price fetch; the background goroutine runs until Stop() is called.
func (gm *GasMonitor) Start(ctx context.Context) error {
	gm.logger.Info("Starting gas monitor",
		zap.Duration("update_interval", gm.updateInterval))

	// Get initial gas price (respects caller's timeout/deadline)
	gasPrice, err := gm.client.GetGasPrice(ctx)
	if err != nil {
		gm.logger.Error("Failed to get initial gas price", zap.Error(err))
		return fmt.Errorf("failed to get initial gas price: %w", err)
	}

	gm.mu.Lock()
	gm.currentPrice = gasPrice
	gm.mu.Unlock()

	// Start update ticker
	gm.updateTicker = time.NewTicker(gm.updateInterval)

	gm.wg.Add(1)
	go func() {
		defer gm.wg.Done()
		for {
			select {
			case <-gm.updateTicker.C:
				gm.updateGasPrice(context.Background())
			case <-gm.stopChan:
				gm.logger.Info("Gas monitor stopped")
				return
			}
		}
	}()

	gm.logger.Info("Gas monitor started")
	return nil
}

// Stop stops the gas monitor and waits for the background goroutine to exit.
func (gm *GasMonitor) Stop() {
	gm.logger.Info("Stopping gas monitor")
	if gm.updateTicker != nil {
		gm.updateTicker.Stop()
	}
	gm.stopOnce.Do(func() {
		close(gm.stopChan)
	})
	gm.wg.Wait()
}

// GetGasPrice gets the current gas price
func (gm *GasMonitor) GetGasPrice() *big.Int {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.currentPrice
}

// GetGasPriceInGwei gets the current gas price in Gwei
func (gm *GasMonitor) GetGasPriceInGwei() float64 {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	if gm.currentPrice == nil {
		return 0
	}

	// Convert Wei to Gwei (1 Gwei = 1e9 Wei)
	gwei := new(big.Float).Quo(
		new(big.Float).SetInt(gm.currentPrice),
		big.NewFloat(1e9),
	)

	result, _ := gwei.Float64()
	return result
}

// updateGasPrice updates the current gas price
func (gm *GasMonitor) updateGasPrice(ctx context.Context) {
	gasPrice, err := gm.client.GetGasPrice(ctx)
	if err != nil {
		gm.logger.Error("Failed to update gas price", zap.Error(err))
		return
	}

	gm.mu.Lock()
	gm.currentPrice = gasPrice
	gm.mu.Unlock()

	gm.logger.Debug("Gas price updated",
		zap.String("gas_price_wei", gasPrice.String()),
		zap.Float64("gas_price_gwei", gm.GetGasPriceInGwei()))
}

// GasEstimate contains gas estimation information (EIP-1559 compatible)
type GasEstimate struct {
	StandardGas          uint64
	FastGas              uint64
	InstantGas           uint64
	SafeGasPrice         *big.Int
	StandardGasPrice     *big.Int
	FastGasPrice         *big.Int
	BaseFee              *big.Int
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
}

// EstimateGasCost estimates the cost of a transaction
func (gm *GasMonitor) EstimateGasCost(gasAmount uint64) *big.Int {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	if gm.currentPrice == nil {
		return big.NewInt(0)
	}

	//nolint:gocritic // formula comment
	// Cost = gas * gasPrice
	cost := new(big.Int).Mul(new(big.Int).SetUint64(gasAmount), gm.currentPrice)
	return cost
}

// EstimateGasCostInEther estimates the cost of a transaction in Ether
func (gm *GasMonitor) EstimateGasCostInEther(gasAmount uint64) float64 {
	cost := gm.EstimateGasCost(gasAmount)

	// Convert Wei to Ether (1 Ether = 1e18 Wei)
	ether := new(big.Float).Quo(
		new(big.Float).SetInt(cost),
		big.NewFloat(1e18),
	)

	result, _ := ether.Float64()
	return result
}

// GasPrice represents a gas price level
type GasPrice struct {
	Level                string
	GasPrice             *big.Int
	Gwei                 float64
	EstimatedTime        string
	BaseFee              *big.Int
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
}

// GetGasPriceLevels gets gas price levels.
// If FeeHistoryEstimator is configured, uses eth_feeHistory for EIP-1559 levels.
// Otherwise falls back to simple multiplier-based estimation.
func (gm *GasMonitor) GetGasPriceLevels(ctx context.Context) ([]*GasPrice, error) {
	if gm.feeEstimator != nil {
		levels, err := gm.feeEstimator.EIP1559GasLevels(ctx)
		if err == nil {
			return levels, nil
		}
		gm.logger.Warn("FeeHistory estimation failed, falling back to multiplier", zap.Error(err))
	}

	// Legacy multiplier-based fallback
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	if gm.currentPrice == nil {
		return []*GasPrice{}, nil
	}

	safe := new(big.Int).Mul(gm.currentPrice, big.NewInt(1))
	standard := new(big.Int).Mul(gm.currentPrice, big.NewInt(1))
	fast := new(big.Int).Mul(gm.currentPrice, big.NewInt(2))

	return []*GasPrice{
		{
			Level:         "safe",
			GasPrice:      safe,
			Gwei:          toGwei(safe),
			EstimatedTime: "> 30 seconds",
		},
		{
			Level:         "standard",
			GasPrice:      standard,
			Gwei:          toGwei(standard),
			EstimatedTime: "15-30 seconds",
		},
		{
			Level:         "fast",
			GasPrice:      fast,
			Gwei:          toGwei(fast),
			EstimatedTime: "< 15 seconds",
		},
	}, nil
}

// Helper function to convert Wei to Gwei
func toGwei(wei *big.Int) float64 {
	gwei := new(big.Float).Quo(
		new(big.Float).SetInt(wei),
		big.NewFloat(1e9),
	)
	result, _ := gwei.Float64()
	return result
}

// TransactionQueue represents a queue of pending transactions
type TransactionQueue struct {
	transactions []*QueuedTransaction
	mu           sync.RWMutex
	maxSize      int
}

// QueuedTransaction represents a transaction in the queue
type QueuedTransaction struct {
	ID        string
	From      string
	To        string
	Value     *big.Int
	Data      string
	GasLimit  uint64
	GasPrice  *big.Int
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTransactionQueue creates a new transaction queue
func NewTransactionQueue(maxSize int) *TransactionQueue {
	return &TransactionQueue{
		transactions: make([]*QueuedTransaction, 0, maxSize),
		maxSize:      maxSize,
	}
}

// Enqueue adds a transaction to the queue
func (tq *TransactionQueue) Enqueue(tx *QueuedTransaction) error {
	tq.mu.Lock()
	defer tq.mu.Unlock()

	if len(tq.transactions) >= tq.maxSize {
		return fmt.Errorf("transaction queue is full")
	}

	tx.Status = "pending"
	tx.CreatedAt = time.Now()
	tx.UpdatedAt = time.Now()

	tq.transactions = append(tq.transactions, tx)
	return nil
}

// Dequeue removes and returns the first transaction from the queue
func (tq *TransactionQueue) Dequeue() *QueuedTransaction {
	tq.mu.Lock()
	defer tq.mu.Unlock()

	if len(tq.transactions) == 0 {
		return nil
	}

	tx := tq.transactions[0]
	tq.transactions = tq.transactions[1:]
	return tx
}

// GetQueueSize returns the size of the queue
func (tq *TransactionQueue) GetQueueSize() int {
	tq.mu.RLock()
	defer tq.mu.RUnlock()
	return len(tq.transactions)
}

// GetTransactions returns all transactions in the queue
func (tq *TransactionQueue) GetTransactions() []*QueuedTransaction {
	tq.mu.RLock()
	defer tq.mu.RUnlock()

	result := make([]*QueuedTransaction, len(tq.transactions))
	copy(result, tq.transactions)
	return result
}

// UpdateTransactionStatus updates the status of a transaction
func (tq *TransactionQueue) UpdateTransactionStatus(txID, status string) error {
	tq.mu.Lock()
	defer tq.mu.Unlock()

	for _, tx := range tq.transactions {
		if tx.ID == txID {
			tx.Status = status
			tx.UpdatedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("transaction not found: %s", txID)
}
