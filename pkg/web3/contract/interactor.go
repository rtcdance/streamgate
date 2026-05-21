package contract

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/web3/internal/abiutil"
	"github.com/rtcdance/streamgate/pkg/web3/nft"
	"go.uber.org/zap"
)

var erc1967ImplementationSlot = common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")

const proxyCacheTTL = 5 * time.Minute

type proxyCacheEntry struct {
	implAddr string
	expiry   time.Time
}

type ContractInteractor struct {
	client     nft.EthCaller
	logger     *zap.Logger
	proxyMu    sync.RWMutex
	proxyCache map[common.Address]proxyCacheEntry
}

func NewContractInteractor(client nft.EthCaller, logger *zap.Logger) *ContractInteractor {
	return &ContractInteractor{
		client:     client,
		logger:     logger,
		proxyCache: make(map[common.Address]proxyCacheEntry),
	}
}

func (ci *ContractInteractor) InvalidateProxyCache(proxyAddress string) {
	proxy := common.HexToAddress(proxyAddress)
	ci.proxyMu.Lock()
	delete(ci.proxyCache, proxy)
	ci.proxyMu.Unlock()
	ci.logger.Debug("Invalidated proxy cache", zap.String("proxy", proxyAddress))
}

func (ci *ContractInteractor) ResolveImplementation(ctx context.Context, proxyAddress string) (string, error) {
	proxy := common.HexToAddress(proxyAddress)

	ci.proxyMu.RLock()
	if entry, ok := ci.proxyCache[proxy]; ok && time.Now().Before(entry.expiry) {
		ci.proxyMu.RUnlock()
		return entry.implAddr, nil
	}
	ci.proxyMu.RUnlock()

	result, err := ci.client.CallContract(ctx, ethereum.CallMsg{
		To:   &proxy,
		Data: erc1967ImplementationSlot.Bytes(),
	}, nil)
	if err != nil {
		ci.logger.Debug("Could not read ERC-1967 slot, assuming not a proxy",
			zap.String("address", proxyAddress),
			zap.Error(err))
		return proxyAddress, nil
	}

	if len(result) < 32 {
		return proxyAddress, nil
	}

	implAddress := common.BytesToAddress(result[12:32])
	if implAddress == (common.Address{}) {
		return proxyAddress, nil
	}

	implHex := implAddress.Hex()
	ci.logger.Info("Detected ERC-1967 proxy contract",
		zap.String("proxy", proxyAddress),
		zap.String("implementation", implHex))

	ci.proxyMu.Lock()
	ci.proxyCache[proxy] = proxyCacheEntry{implAddr: implHex, expiry: time.Now().Add(proxyCacheTTL)}
	ci.proxyMu.Unlock()
	return implHex, nil
}

func (ci *ContractInteractor) CallContractFunction(ctx context.Context, contractAddress, abiJSON, functionName, fromAddress string, args ...interface{}) (interface{}, error) {
	ci.logger.Debug("Calling contract function",
		zap.String("contract", contractAddress),
		zap.String("function", functionName))

	result, err := ci.callContractFunction(ctx, contractAddress, abiJSON, functionName, fromAddress, args...)
	if err == nil {
		ci.logger.Debug("Contract function called successfully", zap.String("function", functionName))
		return result, nil
	}

	implAddr, resolveErr := ci.ResolveImplementation(ctx, contractAddress)
	if resolveErr != nil || implAddr == contractAddress {
		return nil, err
	}

	ci.logger.Info("Retrying contract call against resolved implementation",
		zap.String("proxy", contractAddress),
		zap.String("implementation", implAddr),
		zap.String("function", functionName))

	retryResult, retryErr := ci.callContractFunction(ctx, implAddr, abiJSON, functionName, fromAddress, args...)
	if retryErr != nil {
		return nil, &DualError{Primary: err, Secondary: retryErr}
	}

	return retryResult, nil
}

func (ci *ContractInteractor) callContractFunction(ctx context.Context, contractAddress, abiJSON, functionName, fromAddress string, args ...interface{}) (interface{}, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	contract := common.HexToAddress(contractAddress)

	parsedABI, err := abiutil.GetOrParseABI(abiJSON)
	if err != nil {
		ci.logger.Error("Failed to parse ABI", zap.Error(err))
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	data, err := parsedABI.Pack(functionName, args...)
	if err != nil {
		ci.logger.Error("Failed to pack function call",
			zap.String("function", functionName),
			zap.Error(err))
		return nil, fmt.Errorf("failed to pack function call: %w", err)
	}

	msg := ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}

	if fromAddress != "" && common.IsHexAddress(fromAddress) {
		msg.From = common.HexToAddress(fromAddress)
	}

	result, err := ci.client.CallContract(ctx, msg, nil)

	if err != nil {
		errMsg := err.Error()
		if revertData := ExtractRevertData(errMsg); revertData != nil {
			if revert := ParseRevertReason(revertData); revert != nil {
				ci.logger.Error("Contract call reverted",
					zap.String("function", functionName),
					zap.String("reason", revert.Reason),
					zap.Bool("is_panic", revert.IsPanic))
				return nil, fmt.Errorf("contract call %s reverted: %w", functionName, revert)
			}
		}
		ci.logger.Error("Failed to call contract function",
			zap.String("function", functionName),
			zap.Error(err))
		return nil, fmt.Errorf("failed to call contract function: %w", err)
	}

	return result, nil
}

func (ci *ContractInteractor) GetContractCode(ctx context.Context, contractAddress string) (string, error) {
	ci.logger.Debug("Getting contract code", zap.String("contract", contractAddress))

	contract := common.HexToAddress(contractAddress)
	code, err := ci.client.CodeAt(ctx, contract, nil)
	if err != nil {
		ci.logger.Error("Failed to get contract code",
			zap.String("contract", contractAddress),
			zap.Error(err))
		return "", fmt.Errorf("failed to get contract code: %w", err)
	}

	ci.logger.Debug("Contract code retrieved",
		zap.String("contract", contractAddress),
		zap.Int("size", len(code)))
	return fmt.Sprintf("0x%x", code), nil
}

func (ci *ContractInteractor) IsContractAddress(ctx context.Context, address string) (bool, error) {
	ci.logger.Debug("Checking if address is contract", zap.String("address", address))

	addr := common.HexToAddress(address)
	code, err := ci.client.CodeAt(ctx, addr, nil)
	if err != nil {
		ci.logger.Error("Failed to check contract address",
			zap.String("address", address),
			zap.Error(err))
		return false, fmt.Errorf("failed to check contract address: %w", err)
	}

	isContract := len(code) > 0
	ci.logger.Debug("Contract address check completed",
		zap.String("address", address),
		zap.Bool("is_contract", isContract))
	return isContract, nil
}

type ContractContentRegistry struct {
	Address string
	ABI     string
}

func (cr *ContractContentRegistry) RegisterContent(ctx context.Context, ci *ContractInteractor, contentHash, owner, metadata string) ([]byte, error) {
	parsedABI, err := abiutil.GetOrParseABI(cr.ABI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse content registry ABI: %w", err)
	}

	hashBytes, err := hexToBytes32(contentHash)
	if err != nil {
		return nil, fmt.Errorf("invalid content hash: %w", err)
	}

	data, err := parsedABI.Pack("registerContent", hashBytes, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to pack registerContent call: %w", err)
	}

	return data, nil
}

func (cr *ContractContentRegistry) VerifyContent(ctx context.Context, ci *ContractInteractor, contentHash string) (bool, error) {
	parsedABI, err := abiutil.GetOrParseABI(cr.ABI)
	if err != nil {
		return false, fmt.Errorf("failed to parse content registry ABI: %w", err)
	}

	hashBytes, err := hexToBytes32(contentHash)
	if err != nil {
		return false, fmt.Errorf("invalid content hash: %w", err)
	}

	contract := common.HexToAddress(cr.Address)
	callData, err := parsedABI.Pack("verifyContent", hashBytes)
	if err != nil {
		return false, fmt.Errorf("failed to pack verifyContent call: %w", err)
	}

	result, err := ci.client.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: callData,
	}, nil)
	if err != nil {
		return false, fmt.Errorf("verifyContent call failed: %w", err)
	}

	out, err := parsedABI.Unpack("verifyContent", result)
	if err != nil {
		return false, fmt.Errorf("failed to unpack verifyContent result: %w", err)
	}
	if len(out) > 0 {
		if valid, ok := out[0].(bool); ok {
			return valid, nil
		}
	}
	return false, nil
}

func (cr *ContractContentRegistry) GetContentInfo(ctx context.Context, ci *ContractInteractor, contentHash string) (*ContentInfo, error) {
	parsedABI, err := abiutil.GetOrParseABI(cr.ABI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse content registry ABI: %w", err)
	}

	hashBytes, err := hexToBytes32(contentHash)
	if err != nil {
		return nil, fmt.Errorf("invalid content hash: %w", err)
	}

	contract := common.HexToAddress(cr.Address)
	callData, err := parsedABI.Pack("getContentInfo", hashBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to pack getContentInfo call: %w", err)
	}

	result, err := ci.client.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("getContentInfo call failed: %w", err)
	}

	type getContentInfoResult struct {
		Owner     common.Address
		Timestamp *big.Int
		Metadata  string
	}
	var out getContentInfoResult
	if err := parsedABI.UnpackIntoInterface(&out, "getContentInfo", result); err != nil {
		return nil, fmt.Errorf("failed to unpack getContentInfo result: %w", err)
	}

	return &ContentInfo{
		Hash:      contentHash,
		Owner:     out.Owner.Hex(),
		Timestamp: out.Timestamp.Int64(),
		Metadata:  out.Metadata,
		IsValid:   out.Owner != common.Address{},
	}, nil
}

func hexToBytes32(hexStr string) ([32]byte, error) {
	var out [32]byte
	if strings.HasPrefix(hexStr, "0x") || strings.HasPrefix(hexStr, "0X") {
		hexStr = hexStr[2:]
	}
	if len(hexStr) != 64 {
		return out, fmt.Errorf("expected 64 hex chars for bytes32, got %d", len(hexStr))
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return out, fmt.Errorf("invalid hex: %w", err)
	}
	copy(out[:], b)
	return out, nil
}

type ContentInfo struct {
	Hash      string
	Owner     string
	Timestamp int64
	Metadata  string
	IsValid   bool
}

type TransactionBuilder struct {
	logger *zap.Logger
}

func NewTransactionBuilder(logger *zap.Logger) *TransactionBuilder {
	return &TransactionBuilder{
		logger: logger,
	}
}

func (tb *TransactionBuilder) BuildTransaction(to string, value *big.Int, data string, gasLimit uint64, gasPrice *big.Int) *Transaction {
	tb.logger.Debug("Building transaction",
		zap.String("to", to),
		zap.String("value", value.String()),
		zap.Uint64("gas_limit", gasLimit))

	return &Transaction{
		To:       to,
		Value:    value,
		Data:     data,
		GasLimit: gasLimit,
		GasPrice: gasPrice,
	}
}

type Transaction struct {
	To       string
	Value    *big.Int
	Data     string
	GasLimit uint64
	GasPrice *big.Int
	Nonce    uint64
}

func (tb *TransactionBuilder) EstimateTransactionCost(tx *Transaction) *big.Int {
	cost := new(big.Int).Mul(new(big.Int).SetUint64(tx.GasLimit), tx.GasPrice)
	return cost
}
