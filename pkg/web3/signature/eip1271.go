package signature

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/web3/internal/abiutil"
	"go.uber.org/zap"
)

var EIP1271MagicValue = [4]byte{0x16, 0x26, 0xba, 0x7e}

const EIP1271ABI = `[{"inputs":[{"name":"_hash","type":"bytes32"},{"name":"_signature","type":"bytes"}],"name":"isValidSignature","outputs":[{"name":"magicValue","type":"bytes4"}],"stateMutability":"view","type":"function"}]`

type ContractCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

type EIP1271Checker struct {
	caller ContractCaller
	logger *zap.Logger
	abi    abi.ABI
}

func NewEIP1271Checker(caller ContractCaller, logger *zap.Logger) *EIP1271Checker {
	return &EIP1271Checker{
		caller: caller,
		logger: logger,
		abi:    abiutil.MustParseABI("EIP-1271", EIP1271ABI),
	}
}

func (c *EIP1271Checker) IsValidSignature(ctx context.Context, contractAddress string, hash [32]byte, signature []byte) (bool, error) {
	contract := common.HexToAddress(contractAddress)

	data, err := c.abi.Pack("isValidSignature", hash, signature)
	if err != nil {
		return false, fmt.Errorf("failed to pack isValidSignature: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

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
