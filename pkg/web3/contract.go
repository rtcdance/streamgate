package web3

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// erc1967ImplementationSlot is the ERC-1967 storage slot for the implementation
// address in proxy contracts: keccak256("eip1967.proxy.implementation") - 1
var erc1967ImplementationSlot = common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")

// proxyCacheTTL is how long a resolved proxy implementation address stays cached.
// This allows cache refresh when a proxy is upgraded to a new implementation.
const proxyCacheTTL = 5 * time.Minute

// proxyCacheEntry holds a cached implementation address with an expiry time.
type proxyCacheEntry struct {
	implAddr string
	expiry   time.Time
}

// ContractInteractor handles smart contract interactions
type ContractInteractor struct {
	client     EthCaller
	logger     *zap.Logger
	proxyMu    sync.RWMutex
	proxyCache map[common.Address]proxyCacheEntry // proxy address → implementation address + TTL
}

// NewContractInteractor creates a new contract interactor
func NewContractInteractor(client EthCaller, logger *zap.Logger) *ContractInteractor {
	return &ContractInteractor{
		client:     client,
		logger:     logger,
		proxyCache: make(map[common.Address]proxyCacheEntry),
	}
}

// InvalidateProxyCache removes the cached implementation address for the given
// proxy. Call this after detecting a proxy upgrade event so subsequent calls
// resolve the new implementation.
func (ci *ContractInteractor) InvalidateProxyCache(proxyAddress string) {
	proxy := common.HexToAddress(proxyAddress)
	ci.proxyMu.Lock()
	delete(ci.proxyCache, proxy)
	ci.proxyMu.Unlock()
	ci.logger.Debug("Invalidated proxy cache", zap.String("proxy", proxyAddress))
}

// ResolveImplementation checks if the given address is an ERC-1967 proxy contract.
// If it is, it reads the implementation address from the proxy storage slot and
// returns it. If not a proxy (slot is zero or unreadable), it returns the original address.
// Results are cached to avoid repeated storage reads.
func (ci *ContractInteractor) ResolveImplementation(ctx context.Context, proxyAddress string) (string, error) {
	proxy := common.HexToAddress(proxyAddress)

	// Check cache first (with TTL)
	ci.proxyMu.RLock()
	if entry, ok := ci.proxyCache[proxy]; ok && time.Now().Before(entry.expiry) {
		ci.proxyMu.RUnlock()
		return entry.implAddr, nil
	}
	ci.proxyMu.RUnlock()

	// Read ERC-1967 implementation slot
	result, err := ci.client.CallContract(ctx, ethereum.CallMsg{
		To:   &proxy,
		Data: erc1967ImplementationSlot.Bytes(),
	}, nil)
	if err != nil {
		// Can't read storage — assume not a proxy
		ci.logger.Debug("Could not read ERC-1967 slot, assuming not a proxy",
			zap.String("address", proxyAddress),
			zap.Error(err))
		return proxyAddress, nil
	}

	// Parse the 32-byte storage slot value as an address
	if len(result) < 32 {
		return proxyAddress, nil
	}

	implAddress := common.BytesToAddress(result[12:32]) // last 20 bytes
	if implAddress == (common.Address{}) {
		// Zero implementation — not a proxy
		return proxyAddress, nil
	}

	implHex := implAddress.Hex()
	ci.logger.Info("Detected ERC-1967 proxy contract",
		zap.String("proxy", proxyAddress),
		zap.String("implementation", implHex))

	// Cache the result with TTL
	ci.proxyMu.Lock()
	ci.proxyCache[proxy] = proxyCacheEntry{implAddr: implHex, expiry: time.Now().Add(proxyCacheTTL)}
	ci.proxyMu.Unlock()
	return implHex, nil
}

// CallContractFunction calls a read-only contract function.
// fromAddress is optional — pass "" to leave msg.From unset (zero address),
// or pass a hex address so that contracts reading msg.sender work correctly.
// If the call fails, it automatically checks for an ERC-1967 proxy and retries
// against the implementation address.
func (ci *ContractInteractor) CallContractFunction(ctx context.Context, contractAddress, abiJSON, functionName, fromAddress string, args ...interface{}) (interface{}, error) {
	ci.logger.Debug("Calling contract function",
		zap.String("contract", contractAddress),
		zap.String("function", functionName))

	result, err := ci.callContractFunction(ctx, contractAddress, abiJSON, functionName, fromAddress, args...)
	if err == nil {
		ci.logger.Debug("Contract function called successfully", zap.String("function", functionName))
		return result, nil
	}

	// Call failed — try resolving as a proxy and retrying against the implementation
	implAddr, resolveErr := ci.ResolveImplementation(ctx, contractAddress)
	if resolveErr != nil || implAddr == contractAddress {
		// Not a proxy or resolution failed — return original error
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

// callContractFunction is the internal implementation that performs the actual contract call.
func (ci *ContractInteractor) callContractFunction(ctx context.Context, contractAddress, abiJSON, functionName, fromAddress string, args ...interface{}) (interface{}, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	contract := common.HexToAddress(contractAddress)

	// Parse ABI
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(abiJSON)))
	if err != nil {
		ci.logger.Error("Failed to parse ABI", zap.Error(err))
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Pack function call
	data, err := parsedABI.Pack(functionName, args...)
	if err != nil {
		ci.logger.Error("Failed to pack function call",
			zap.String("function", functionName),
			zap.Error(err))
		return nil, fmt.Errorf("failed to pack function call: %w", err)
	}

	// Execute call
	msg := ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}

	// Set From address if provided (required for contracts that read msg.sender)
	if fromAddress != "" && common.IsHexAddress(fromAddress) {
		msg.From = common.HexToAddress(fromAddress)
	}

	result, err := ci.client.CallContract(ctx, msg, nil)

	if err != nil {
		// Try to extract and decode revert reason for better diagnostics
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

// GetContractCode gets the bytecode of a contract
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

// IsContractAddress checks if an address is a contract
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

// ContractContentRegistry represents a content registry contract
type ContractContentRegistry struct {
	Address string
	ABI     string
}

// RegisterContent registers content on-chain by packing the ABI call data.
// It returns the encoded call data; the caller is responsible for building,
// signing, and sending the full transaction.
func (cr *ContractContentRegistry) RegisterContent(ctx context.Context, ci *ContractInteractor, contentHash, owner, metadata string) ([]byte, error) {
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(cr.ABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse content registry ABI: %w", err)
	}

	// Convert hex content hash to [32]byte
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

// VerifyContent verifies content on-chain by calling verifyContent(bytes32).
func (cr *ContractContentRegistry) VerifyContent(ctx context.Context, ci *ContractInteractor, contentHash string) (bool, error) {
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(cr.ABI)))
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

	// Unpack bool result
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

// GetContentInfo gets information about registered content via getContentInfo(bytes32).
func (cr *ContractContentRegistry) GetContentInfo(ctx context.Context, ci *ContractInteractor, contentHash string) (*ContentInfo, error) {
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(cr.ABI)))
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

// hexToBytes32 converts a hex string (with or without 0x prefix) to [32]byte.
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

// ContentInfo contains information about registered content
type ContentInfo struct {
	Hash      string
	Owner     string
	Timestamp int64
	Metadata  string
	IsValid   bool
}

// TransactionBuilder builds transactions
type TransactionBuilder struct {
	logger *zap.Logger
}

// NewTransactionBuilder creates a new transaction builder
func NewTransactionBuilder(logger *zap.Logger) *TransactionBuilder {
	return &TransactionBuilder{
		logger: logger,
	}
}

// BuildTransaction builds a transaction
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

// Transaction represents a blockchain transaction
type Transaction struct {
	To       string
	Value    *big.Int
	Data     string
	GasLimit uint64
	GasPrice *big.Int
	Nonce    uint64
}

// EstimateTransactionCost estimates the cost of a transaction
func (tb *TransactionBuilder) EstimateTransactionCost(tx *Transaction) *big.Int {
	//nolint:gocritic // formula comment
	// Cost = gasLimit * gasPrice
	cost := new(big.Int).Mul(new(big.Int).SetUint64(tx.GasLimit), tx.GasPrice)
	return cost
}
