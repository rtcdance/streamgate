package tx

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

type PendingTx struct {
	Hash         string
	Nonce        uint64
	GasPrice     *big.Int
	GasTipCap    *big.Int
	MaxFeePerGas *big.Int
	IsEIP1559    bool
	To           string
	Value        *big.Int
	Data         []byte
	GasLimit     uint64
	SentAt       time.Time
	ChainID      int64
}

type TxTracker struct {
	client Client
	logger *zap.Logger
}

func NewTxTracker(client Client, logger *zap.Logger) *TxTracker {
	return &TxTracker{client: client, logger: logger}
}

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

func (tt *TxTracker) bumpLegacy(privateKey *ecdsa.PrivateKey, pending *PendingTx, bumpPercent int64, toAddr common.Address, chainIDBig *big.Int) (*types.Transaction, error) {
	bumpFactor := big.NewInt(100 + bumpPercent)
	newGasPrice := new(big.Int).Mul(pending.GasPrice, bumpFactor)
	newGasPrice.Div(newGasPrice, big.NewInt(100))

	gasLimit := pending.GasLimit
	if gasLimit == 0 {
		gasLimit = 200000
	}

	unsignedTx := types.NewTransaction(pending.Nonce, toAddr, pending.Value, gasLimit, newGasPrice, pending.Data)
	signedTx, err := types.SignTx(unsignedTx, types.NewEIP155Signer(chainIDBig), privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign bumped tx: %w", err)
	}
	return signedTx, nil
}

func (tt *TxTracker) bumpEIP1559(ctx context.Context, privateKey *ecdsa.PrivateKey, pending *PendingTx, bumpPercent int64, toAddr common.Address, chainIDBig *big.Int) (*types.Transaction, error) {
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

	gasLimit := pending.GasLimit
	if gasLimit == 0 {
		gasLimit = 200000
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

func (tt *TxTracker) CancelTx(ctx context.Context, privateKey *ecdsa.PrivateKey, pending *PendingTx, bumpPercent int64) (string, error) {
	if bumpPercent <= 0 {
		bumpPercent = 10
	}

	publicKey := privateKey.Public().(*ecdsa.PublicKey)
	fromAddress := crypto.PubkeyToAddress(*publicKey)
	chainIDBig := big.NewInt(pending.ChainID)

	var signedTx *types.Transaction

	if pending.IsEIP1559 {
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

func IsStuck(pending *PendingTx, threshold time.Duration) bool {
	return time.Since(pending.SentAt) > threshold
}
