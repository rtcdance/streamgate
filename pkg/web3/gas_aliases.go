package web3

import "github.com/rtcdance/streamgate/pkg/web3/gas"

type (
	GasPricer           = gas.GasPricer
	FeeHistoryProvider  = gas.FeeHistoryProvider
	FeeHistoryEstimator = gas.FeeHistoryEstimator
	EIP1559Levels       = gas.EIP1559Levels
	GasMonitor          = gas.GasMonitor
	GasEstimate         = gas.GasEstimate
	GasPrice            = gas.GasPrice
	TransactionQueue    = gas.TransactionQueue
	QueuedTransaction   = gas.QueuedTransaction
)

var (
	NewGasMonitor               = gas.NewGasMonitor
	NewGasMonitorWithFeeHistory = gas.NewGasMonitorWithFeeHistory
	NewFeeHistoryEstimator      = gas.NewFeeHistoryEstimator
	NewTransactionQueue         = gas.NewTransactionQueue
)
