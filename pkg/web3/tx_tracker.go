package web3

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// PendingTx tracks a sent transaction for stuck-tx detection and replacement.
type PendingTx struct {
	Hash         string
	Nonce        uint64
	GasPrice     *big.Int // legacy gas price (nil for EIP-1559)
	GasTipCap    *big.Int // EIP-1559 max priority fee per gas
	MaxFeePerGas *big.Int // EIP-1559 max fee per gas
	IsEIP1559    bool
	To           string
	Value        *big.Int
	Data         []byte
	GasLimit     uint64 // original gas limit (0 = use default)
	SentAt       time.Time
	ChainID      int64
}

// TxTracker manages pending transactions and provides gas-bump / cancel operations.
type TxTracker struct {
	client *ChainClient
	logger *zap.Logger
}

// NewTxTracker creates a new TxTracker backed by the given ChainClient.
func NewTxTracker(client *ChainClient, logger *zap.Logger) *TxTracker {
	return &TxTracker{client: client, logger: logger}
}

// BumpGas replaces a pending transaction with the same nonce but higher gas price.
// The bumpPercent parameter specifies how much to increase the gas price (e.g. 10 = 10%).
// For EIP-1559 transactions, it bumps GasTipCap and MaxFeePerGas.
// Returns the new transaction hash on success.
func (tt *TxTracker) BumpGas(ctx context.Context, privateKey *ecdsa.PrivateKey, pending *PendingTx, bumpPercent int64) (string, error) {
	if bumpPercent < 10 {
		return "", fmt.Errorf("bump percent must be at least 10%% for EIP-1559 replacement, got %d", bumpPercent)
	}

	toAddr := common.HexToAddress(pending.To)
	chainIDBig := big.NewInt(pending.ChainID)

	var signedTx *types.Transaction

	if pending.IsEIP1559 {
		signedTx, err := tt.bumpEIP1559(ctx, privateKey, pending, bumpPercent, toAddr, chainIDBig)
		if err != nil {
			return "", err
		}
		if err := tt.client.SendTransaction(ctx, signedTx); err != nil {
			return "", fmt.Errorf("failed to send bumped eip-1559 tx: %w", err)
		}
		tt.logger.Info("EIP-1559 transaction gas bumped",
			zap.String("old_hash", pending.Hash),
			zap.String("new_hash", signedTx.Hash().Hex()),
			zap.Uint64("nonce", pending.Nonce),
			zap.String("old_tip", pending.GasTipCap.String()),
			zap.String("new_tip", signedTx.GasTipCap().String()))
		return signedTx.Hash().Hex(), nil
	}

	// Legacy path
	signedTx, err := tt.bumpLegacy(privateKey, pending, bumpPercent, toAddr, chainIDBig)
	if err != nil {
		return "", err
	}
	if err := tt.client.SendTransaction(ctx, signedTx); err != nil {
		return "", fmt.Errorf("failed to send bumped tx: %w", err)
	}

	tt.logger.Info("Legacy transaction gas bumped",
		zap.String("old_hash", pending.Hash),
		zap.String("new_hash", signedTx.Hash().Hex()),
		zap.Uint64("nonce", pending.Nonce),
		zap.String("old_gas_price", pending.GasPrice.String()),
		zap.String("new_gas_price", signedTx.GasPrice().String()))

	return signedTx.Hash().Hex(), nil
}

// bumpLegacy builds a bumped legacy transaction.
func (tt *TxTracker) bumpLegacy(privateKey *ecdsa.PrivateKey, pending *PendingTx, bumpPercent int64, toAddr common.Address, chainIDBig *big.Int) (*types.Transaction, error) {
	bumpFactor := big.NewInt(100 + bumpPercent)
	newGasPrice := new(big.Int).Mul(pending.GasPrice, bumpFactor)
	newGasPrice.Div(newGasPrice, big.NewInt(100))

	gasLimit := pending.GasLimit
	if gasLimit == 0 {
		gasLimit = 200000 // default fallback
	}

	unsignedTx := types.NewTransaction(pending.Nonce, toAddr, pending.Value, gasLimit, newGasPrice, pending.Data)
	signedTx, err := types.SignTx(unsignedTx, types.NewEIP155Signer(chainIDBig), privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign bumped tx: %w", err)
	}
	return signedTx, nil
}

// bumpEIP1559 builds a bumped EIP-1559 transaction.
// GasTipCap is bumped by bumpPercent. MaxFeePerGas is recalculated as 2*baseFee + newTip.
func (tt *TxTracker) bumpEIP1559(ctx context.Context, privateKey *ecdsa.PrivateKey, pending *PendingTx, bumpPercent int64, toAddr common.Address, chainIDBig *big.Int) (*types.Transaction, error) {
	// Bump GasTipCap
	bumpFactor := big.NewInt(100 + bumpPercent)
	newTip := new(big.Int).Mul(pending.GasTipCap, bumpFactor)
	newTip.Div(newTip, big.NewInt(100))

	// Get fresh base fee for MaxFeePerGas calculation
	var newMaxFee *big.Int
	if header, err := tt.client.HeaderByNumber(ctx, nil); err == nil && header.BaseFee != nil {
		// MaxFeePerGas = 2 * baseFee + tip (standard EIP-1559 formula)
		twoBaseFee := new(big.Int).Mul(header.BaseFee, big.NewInt(2))
		calculated := new(big.Int).Add(twoBaseFee, newTip)
		// Use the higher of calculated vs bumped existing MaxFeePerGas
		bumpedMaxFee := new(big.Int).Mul(pending.MaxFeePerGas, bumpFactor)
		bumpedMaxFee.Div(bumpedMaxFee, big.NewInt(100))
		if calculated.Cmp(bumpedMaxFee) > 0 {
			newMaxFee = calculated
		} else {
			newMaxFee = bumpedMaxFee
		}
	} else {
		// Fallback: just bump existing MaxFeePerGas
		newMaxFee = new(big.Int).Mul(pending.MaxFeePerGas, bumpFactor)
		newMaxFee.Div(newMaxFee, big.NewInt(100))
	}

	// Ensure MaxFeePerGas >= GasTipCap
	if newMaxFee.Cmp(newTip) < 0 {
		newMaxFee = new(big.Int).Set(newTip)
	}

	gasLimit := pending.GasLimit
	if gasLimit == 0 {
		gasLimit = 200000 // default fallback
	}

	unsignedTx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainIDBig,
		Nonce:     pending.Nonce,
		GasTipCap: newTip,
		GasFeeCap: newMaxFee,
		Gas:       gasLimit,
		To:        &toAddr,
		Value:     pending.Value,
		Data:      pending.Data,
	})

	signedTx, err := types.SignTx(unsignedTx, types.LatestSignerForChainID(chainIDBig), privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign bumped eip-1559 tx: %w", err)
	}
	return signedTx, nil
}

// CancelTx cancels a pending transaction by sending a zero-value self-transfer
// with the same nonce but higher gas price. This is the standard on-chain
// mechanism for cancelling a stuck transaction.
func (tt *TxTracker) CancelTx(ctx context.Context, privateKey *ecdsa.PrivateKey, pending *PendingTx, bumpPercent int64) (string, error) {
	if bumpPercent <= 0 {
		bumpPercent = 10
	}

	publicKey := privateKey.Public().(*ecdsa.PublicKey)
	fromAddress := crypto.PubkeyToAddress(*publicKey)
	chainIDBig := big.NewInt(pending.ChainID)

	var signedTx *types.Transaction

	if pending.IsEIP1559 {
		// EIP-1559 cancel: self-transfer with 0 value + bumped tip
		bumpFactor := big.NewInt(100 + bumpPercent)
		newTip := new(big.Int).Mul(pending.GasTipCap, bumpFactor)
		newTip.Div(newTip, big.NewInt(100))

		var newMaxFee *big.Int
		if header, err := tt.client.HeaderByNumber(ctx, nil); err == nil && header.BaseFee != nil {
			twoBaseFee := new(big.Int).Mul(header.BaseFee, big.NewInt(2))
			calculated := new(big.Int).Add(twoBaseFee, newTip)
			bumpedMaxFee := new(big.Int).Mul(pending.MaxFeePerGas, bumpFactor)
			bumpedMaxFee.Div(bumpedMaxFee, big.NewInt(100))
			if calculated.Cmp(bumpedMaxFee) > 0 {
				newMaxFee = calculated
			} else {
				newMaxFee = bumpedMaxFee
			}
		} else {
			newMaxFee = new(big.Int).Mul(pending.MaxFeePerGas, bumpFactor)
			newMaxFee.Div(newMaxFee, big.NewInt(100))
		}

		if newMaxFee.Cmp(newTip) < 0 {
			newMaxFee = new(big.Int).Set(newTip)
		}

		unsignedTx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainIDBig,
			Nonce:     pending.Nonce,
			GasTipCap: newTip,
			GasFeeCap: newMaxFee,
			Gas:       21000,
			To:        &fromAddress,
			Value:     big.NewInt(0),
			Data:      nil,
		})
		var err error
		signedTx, err = types.SignTx(unsignedTx, types.LatestSignerForChainID(chainIDBig), privateKey)
		if err != nil {
			return "", fmt.Errorf("failed to sign eip-1559 cancel tx: %w", err)
		}
	} else {
		// Legacy cancel
		bumpFactor := big.NewInt(100 + bumpPercent)
		newGasPrice := new(big.Int).Mul(pending.GasPrice, bumpFactor)
		newGasPrice.Div(newGasPrice, big.NewInt(100))

		unsignedTx := types.NewTransaction(pending.Nonce, fromAddress, big.NewInt(0), 21000, newGasPrice, nil)
		var err error
		signedTx, err = types.SignTx(unsignedTx, types.NewEIP155Signer(chainIDBig), privateKey)
		if err != nil {
			return "", fmt.Errorf("failed to sign cancel tx: %w", err)
		}
	}

	if err := tt.client.SendTransaction(ctx, signedTx); err != nil {
		return "", fmt.Errorf("failed to send cancel tx: %w", err)
	}

	tt.logger.Info("Transaction cancelled via self-transfer",
		zap.Bool("eip1559", pending.IsEIP1559),
		zap.String("cancelled_hash", pending.Hash),
		zap.String("cancel_tx_hash", signedTx.Hash().Hex()),
		zap.Uint64("nonce", pending.Nonce))

	return signedTx.Hash().Hex(), nil
}

// IsStuck checks if a transaction has been pending longer than the given threshold.
func IsStuck(pending *PendingTx, threshold time.Duration) bool {
	return time.Since(pending.SentAt) > threshold
}
