package web3

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// ContractWriteResult contains the result of a contract write operation.
type ContractWriteResult struct {
	TxHash      string
	Nonce       uint64
	GasLimit    uint64
	GasPrice    *big.Int // nil for EIP-1559
	MaxFeePerGas *big.Int
	TipCap      *big.Int
	SentAt      time.Time
}

// ContractWriter sends state-changing transactions to smart contracts.
// It handles ABI encoding, nonce management, gas estimation, signing via
// SecurePrivateKey, and sending via ChainClient (no failover on writes to
// prevent duplicate submission).  Optionally tracks the tx with TxTracker.
type ContractWriter struct {
	client       *ChainClient
	key          KeyProvider
	nonceMgr     NonceProvider
	tracker      *TxTracker // optional: set via WithTracker
	logger       *zap.Logger
	fromAddress  common.Address // derived from key at construction
	chainID      *big.Int
}

// ContractWriterConfig holds configuration for creating a ContractWriter.
type ContractWriterConfig struct {
	Client      *ChainClient
	Key         KeyProvider
	NonceMgr    NonceProvider
	FromAddress string // hex address of the signer
	ChainID     int64
	Logger      *zap.Logger
}

// NewContractWriter creates a new ContractWriter.
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

// WithTracker sets the TxTracker for post-send monitoring.
func (cw *ContractWriter) WithTracker(tracker *TxTracker) *ContractWriter {
	cw.tracker = tracker
	return cw
}

// encodeCallData encodes contract method arguments using the parsed ABI.
func (cw *ContractWriter) encodeCallData(opts ContractTxOpts) ([]byte, error) {
	if opts.ParsedABI == nil {
		return nil, fmt.Errorf("contract_writer: ParsedABI is required")
	}
	callData, err := opts.ParsedABI.Pack(opts.Method, opts.Args...)
	if err != nil {
		return nil, fmt.Errorf("contract_writer: pack %s: %w", opts.Method, err)
	}
	return callData, nil
}

// estimateGas determines gas limit for a contract call, adding a 20% buffer
// for auto-estimated values. Falls back to 300,000 when estimation fails.
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

// buildAndSignTx creates and signs either an EIP-1559 or legacy transaction
// depending on what the RPC endpoint supports. Returns the signed tx and a
// PendingTx metadata struct for tracking.
func (cw *ContractWriter) buildAndSignTx(ctx context.Context, privKey *ecdsa.PrivateKey, nonce uint64, contractAddr common.Address, callData []byte, gasLimit uint64, opts ContractTxOpts) (*types.Transaction, *PendingTx, error) {
	// Try EIP-1559 first
	tipCap, err := cw.client.SuggestGasTipCap(ctx)
	if err != nil {
		// Fallback to legacy
		gasPrice, err2 := cw.client.GetGasPrice(ctx)
		if err2 != nil {
			return nil, nil, fmt.Errorf("contract_writer: get gas price: %w (tipcap err: %w)", err2, err)
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

	// EIP-1559: maxFee = 2 * baseFee + tipCap
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

// buildResult constructs a ContractWriteResult from a PendingTx.
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

// SendTx builds, signs, and sends a contract write transaction.
// It orchestrates: ABI-encode calldata → nonce acquisition → gas estimation →
// EIP-1559 or legacy signing → send → optional tracking.
func (cw *ContractWriter) SendTx(ctx context.Context, opts ContractTxOpts) (*ContractWriteResult, error) {
	callData, err := cw.encodeCallData(opts)
	if err != nil {
		return nil, err
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

// ContractTxOpts specifies the parameters for a contract write transaction.
type ContractTxOpts struct {
	To        string      // contract address (hex)
	Method    string      // function name, e.g. "registerContent"
	ParsedABI *abi.ABI   // pre-parsed ABI (avoids re-parsing on every call)
	Args      []interface{} // positional arguments
	Value     *big.Int   // ETH value to send (nil = 0)
	GasLimit  uint64     // 0 = auto-estimate
}
