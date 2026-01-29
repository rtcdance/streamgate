package web3

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// GasMonitor monitors gas prices
type GasMonitor struct {
	client         *ethclient.Client
	logger         *zap.Logger
	currentPrice   *big.Int
	mu             sync.RWMutex
	updateTicker   *time.Ticker
	stopChan       chan struct{}
	updateInterval time.Duration
}

// NewGasMonitor creates a new gas monitor
func NewGasMonitor(client *ethclient.Client, logger *zap.Logger) *GasMonitor {
	return &GasMonitor{
		client:         client,
		logger:         logger,
		updateInterval: 30 * time.Second,
		stopChan:       make(chan struct{}),
	}
}

// Start starts the gas monitor
func (gm *GasMonitor) Start(ctx context.Context) error {
	gm.logger.Info("Starting gas monitor",
		zap.Duration("update_interval", gm.updateInterval))

	// Get initial gas price
	gasPrice, err := gm.client.SuggestGasPrice(ctx)
	if err != nil {
		gm.logger.Error("Failed to get initial gas price", zap.Error(err))
		return fmt.Errorf("failed to get initial gas price: %w", err)
	}

	gm.mu.Lock()
	gm.currentPrice = gasPrice
	gm.mu.Unlock()

	// Start update ticker
	gm.updateTicker = time.NewTicker(gm.updateInterval)

	go func() {
		for {
			select {
			case <-gm.updateTicker.C:
				gm.updateGasPrice(ctx)
			case <-gm.stopChan:
				gm.logger.Info("Gas monitor stopped")
				return
			}
		}
	}()

	gm.logger.Info("Gas monitor started")
	return nil
}

// Stop stops the gas monitor
func (gm *GasMonitor) Stop() {
	gm.logger.Info("Stopping gas monitor")
	if gm.updateTicker != nil {
		gm.updateTicker.Stop()
	}
	close(gm.stopChan)
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
	gasPrice, err := gm.client.SuggestGasPrice(ctx)
	if err != nil {
		gm.logger.Error("Failed to update gas price", zap.Error(err))
		return
	}

	gm.mu.Lock()
	gm.currentPrice = gasPrice
	gm.mu.Unlock()

	gm.logger.Debug("Gas price updated", zap.String("gas_price_wei", gasPrice.String()), "gas_price_gwei", gm.GetGasPriceInGwei())
}

// GasEstimate contains gas estimation information
type GasEstimate struct {
	StandardGas      uint64
	FastGas          uint64
	InstantGas       uint64
	SafeGasPrice     *big.Int
	StandardGasPrice *big.Int
	FastGasPrice     *big.Int
}

// EstimateGasCost estimates the cost of a transaction
func (gm *GasMonitor) EstimateGasCost(gasAmount uint64) *big.Int {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	if gm.currentPrice == nil {
		return big.NewInt(0)
	}

	// Cost = gas * gasPrice
	cost := new(big.Int).Mul(big.NewInt(int64(gasAmount)), gm.currentPrice)
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
	Level         string
	GasPrice      *big.Int
	Gwei          float64
	EstimatedTime string
}

// GetGasPriceLevels gets gas price levels
func (gm *GasMonitor) GetGasPriceLevels() []*GasPrice {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	if gm.currentPrice == nil {
		return []*GasPrice{}
	}

	// Calculate different gas price levels
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
	}
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
func (tq *TransactionQueue) UpdateTransactionStatus(txID string, status string) error {
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
