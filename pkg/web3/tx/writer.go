package tx

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

type ContractWriteResult struct {
	TxHash       string
	Nonce        uint64
	GasLimit     uint64
	GasPrice     *big.Int
	MaxFeePerGas *big.Int
	TipCap       *big.Int
	SentAt       time.Time
}

type ContractTxOpts struct {
	To        string
	Method    string
	ParsedABI interface{ Pack(string, ...interface{}) ([]byte, error) }
	Args      []interface{}
	Value     *big.Int
	GasLimit  uint64
}

type KeyProvider interface {
	UseKey(fn func(*ecdsa.PrivateKey) error) error
}

type ContractWriter struct {
	client      Client
	key         KeyProvider
	nonceMgr    NonceProvider
	tracker     *TxTracker
	logger      *zap.Logger
	fromAddress common.Address
	chainID     *big.Int
}

type ContractWriterConfig struct {
	Client      Client
	Key         KeyProvider
	NonceMgr    NonceProvider
	FromAddress string
	ChainID     int64
	Logger      *zap.Logger
}

func NewContractWriter(cfg ContractWriterConfig) *ContractWriter {
	return &ContractWriter{
		client:      cfg.Client,
		key:         cfg.Key,
		nonceMgr:    cfg.NonceMgr,
		logger:      cfg.Logger,
		fromAddress: common.HexToAddress(cfg.FromAddress),
		chainID:     big.NewInt(cfg.ChainID),
	}
}

func (cw *ContractWriter) WithTracker(tracker *TxTracker) *ContractWriter {
	cw.tracker = tracker
	return cw
}

func (cw *ContractWriter) SendTx(ctx context.Context, opts ContractTxOpts) (*ContractWriteResult, error) {
	callData, err := opts.ParsedABI.Pack(opts.Method, opts.Args...)
	if err != nil {
		return nil, fmt.Errorf("contract_writer: pack %s: %w", opts.Method, err)
	}

	contractAddr := common.HexToAddress(opts.To)

	nonce, err := cw.nonceMgr.NextNonce(ctx, cw.fromAddress.Hex())
	if err != nil {
		return nil, fmt.Errorf("contract_writer: get nonce: %w", err)
	}

	gasLimit := opts.GasLimit
	if gasLimit == 0 {
		gasLimit = cw.estimateGas(ctx, cw.fromAddress, contractAddr, callData, opts.Value, opts.Method)
	}

	var signedTx *types.Transaction
	var pending *PendingTx
	err = cw.key.UseKey(func(privKey *ecdsa.PrivateKey) error {
		signedTx, pending, err = cw.buildAndSignTx(ctx, privKey, nonce, contractAddr, callData, gasLimit, opts)
		return err
	})
	if err != nil {
		return nil, err
	}

	if err := cw.client.SendTransaction(ctx, signedTx); err != nil {
		cw.nonceMgr.Rollback(cw.fromAddress.Hex(), nonce)
		return nil, fmt.Errorf("contract_writer: send tx: %w", err)
	}

	cw.logger.Info("Contract write tx sent",
		zap.String("method", opts.Method),
		zap.String("tx_hash", pending.Hash),
		zap.Uint64("nonce", nonce),
		zap.Uint64("gas_limit", gasLimit),
		zap.String("to", opts.To))

	if cw.tracker != nil {
		cw.logger.Debug("Tracking pending tx", zap.String("tx_hash", pending.Hash))
	}

	return buildResult(pending, nonce, gasLimit), nil
}

func (cw *ContractWriter) estimateGas(ctx context.Context, from, to common.Address, callData []byte, value *big.Int, method string) uint64 {
	if value == nil {
		value = new(big.Int)
	}
	estimated, err := cw.client.EstimateGas(ctx, ethereum.CallMsg{
		From:  from,
		To:    &to,
		Data:  callData,
		Value: value,
	})
	if err != nil {
		cw.logger.Warn("Gas estimation failed, using default",
			zap.String("method", method),
			zap.Uint64("gas_limit", uint64(300_000)),
			zap.Error(err))
		return 300_000
	}
	return estimated * 120 / 100
}

func (cw *ContractWriter) buildAndSignTx(ctx context.Context, privKey *ecdsa.PrivateKey, nonce uint64, contractAddr common.Address, callData []byte, gasLimit uint64, opts ContractTxOpts) (*types.Transaction, *PendingTx, error) {
	tipCap, err := cw.client.SuggestGasTipCap(ctx)
	if err != nil {
		gasPrice, err2 := cw.client.GetGasPrice(ctx)
		if err2 != nil {
			return nil, nil, fmt.Errorf("contract_writer: get gas price: %w (tipcap err: %v)", err2, err)
		}

		legacyTx := types.NewTransaction(nonce, contractAddr, opts.Value, gasLimit, gasPrice, callData)
		signedTx, err := types.SignTx(legacyTx, types.NewEIP155Signer(cw.chainID), privKey)
		if err != nil {
			return nil, nil, fmt.Errorf("contract_writer: sign legacy tx: %w", err)
		}

		pending := &PendingTx{
			Hash:     signedTx.Hash().Hex(),
			Nonce:    nonce,
			GasPrice: gasPrice,
			To:       opts.To,
			Value:    opts.Value,
			Data:     callData,
			GasLimit: gasLimit,
			SentAt:   time.Now(),
			ChainID:  cw.chainID.Int64(),
		}
		return signedTx, pending, nil
	}

	head, headErr := cw.client.HeaderByNumber(ctx, nil)
	var maxFee *big.Int
	if headErr == nil && head.BaseFee != nil {
		maxFee = new(big.Int).Add(
			new(big.Int).Mul(head.BaseFee, big.NewInt(2)),
			tipCap,
		)
	} else {
		maxFee = new(big.Int).Add(
			new(big.Int).Mul(tipCap, big.NewInt(3)),
			tipCap,
		)
	}

	dynamicTx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   cw.chainID,
		Nonce:     nonce,
		GasTipCap: tipCap,
		GasFeeCap: maxFee,
		Gas:       gasLimit,
		To:        &contractAddr,
		Value:     opts.Value,
		Data:      callData,
	})
	signedTx, err := types.SignTx(dynamicTx, types.NewLondonSigner(cw.chainID), privKey)
	if err != nil {
		return nil, nil, fmt.Errorf("contract_writer: sign EIP-1559 tx: %w", err)
	}

	pending := &PendingTx{
		Hash:         signedTx.Hash().Hex(),
		Nonce:        nonce,
		GasTipCap:    tipCap,
		MaxFeePerGas: maxFee,
		IsEIP1559:    true,
		To:           opts.To,
		Value:        opts.Value,
		Data:         callData,
		GasLimit:     gasLimit,
		SentAt:       time.Now(),
		ChainID:      cw.chainID.Int64(),
	}
	return signedTx, pending, nil
}

func buildResult(pending *PendingTx, nonce, gasLimit uint64) *ContractWriteResult {
	result := &ContractWriteResult{
		TxHash:   pending.Hash,
		Nonce:    nonce,
		GasLimit: gasLimit,
		SentAt:   pending.SentAt,
	}
	if pending.IsEIP1559 {
		result.MaxFeePerGas = pending.MaxFeePerGas
		result.TipCap = pending.GasTipCap
	} else {
		result.GasPrice = pending.GasPrice
	}
	return result
}
