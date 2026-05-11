package web3

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// EIP1271MagicValue is the expected return value of isValidSignature per EIP-1271.
var EIP1271MagicValue = [4]byte{0x16, 0x26, 0xba, 0x7e}

// EIP1271ABI is the minimal ABI for the EIP-1271 isValidSignature function.
const EIP1271ABI = `[{"inputs":[{"name":"_hash","type":"bytes32"},{"name":"_signature","type":"bytes"}],"name":"isValidSignature","outputs":[{"name":"magicValue","type":"bytes4"}],"stateMutability":"view","type":"function"}]`

// EIP1271Checker verifies signatures from smart contract wallets (EIP-1271).
// Smart contract wallets like Gnosis Safe implement isValidSignature(bytes32,bytes)
// which returns a magic value (0x1626ba7e) if the signature is valid.
type EIP1271Checker struct {
	caller EthCaller
	logger *zap.Logger
	abi    abi.ABI
}

// NewEIP1271Checker creates a new EIP-1271 signature checker.
func NewEIP1271Checker(caller EthCaller, logger *zap.Logger) *EIP1271Checker {
	return &EIP1271Checker{
		caller: caller,
		logger: logger,
		abi:    mustParseABI("EIP-1271", EIP1271ABI),
	}
}

// IsValidSignature calls the contract's isValidSignature(bytes32,bytes) to verify
// a signature from a smart contract wallet (EIP-1271).
//
// Returns:
//   - true, nil  — signature is valid (magic value matches)
//   - false, nil — signature is invalid (magic value mismatch)
//   - false, err — contract call failed (e.g. contract doesn't implement EIP-1271)
func (c *EIP1271Checker) IsValidSignature(ctx context.Context, contractAddress string, hash [32]byte, signature []byte) (bool, error) {
	contract := common.HexToAddress(contractAddress)

	// Pack the isValidSignature call
	data, err := c.abi.Pack("isValidSignature", hash, signature)
	if err != nil {
		return false, fmt.Errorf("failed to pack isValidSignature: %w", err)
	}

	// Call the contract
	result, err := c.caller.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)
	if err != nil {
		c.logger.Debug("EIP-1271 isValidSignature call failed",
			zap.String("contract", contractAddress),
			zap.Error(err))
		return false, fmt.Errorf("EIP-1271 call failed: %w", err)
	}

	// Unpack the result (bytes4)
	unpacked, err := c.abi.Unpack("isValidSignature", result)
	if err != nil {
		c.logger.Debug("EIP-1271 result unpack failed",
			zap.String("contract", contractAddress),
			zap.Error(err))
		return false, fmt.Errorf("failed to unpack isValidSignature result: %w", err)
	}

	if len(unpacked) == 0 {
		return false, fmt.Errorf("isValidSignature returned no data")
	}

	// The return type is bytes4, check against magic value
	switch v := unpacked[0].(type) {
	case [4]byte:
		return v == EIP1271MagicValue, nil
	case []byte:
		if len(v) >= 4 {
			var magic [4]byte
			copy(magic[:], v[:4])
			return magic == EIP1271MagicValue, nil
		}
		return false, nil
	default:
		return false, fmt.Errorf("unexpected type for isValidSignature result: %T", unpacked[0])
	}
}
