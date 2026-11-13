package signature

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
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
	abi    abi.ABI
}

func NewEIP1271Checker(caller ContractCaller, _ *zap.Logger) *EIP1271Checker {
	return &EIP1271Checker{caller: caller, abi: abiutil.MustParseABI("EIP-1271", EIP1271ABI)}
}

// IsValidSignature resolves and rechecks a safe anchor. Clients that cannot
// supply safe headers are refused; a nil block must never imply latest.
func (c *EIP1271Checker) IsValidSignature(ctx context.Context, address string, hash [32]byte, signature []byte) (bool, error) {
	reader, ok := c.caller.(interface {
		HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
	})
	if !ok {
		return false, ErrWalletDependency
	}
	ctx, cancel := context.WithTimeout(ctx, WalletVerificationTimeout)
	defer cancel()
	header, err := reader.HeaderByNumber(ctx, big.NewInt(int64(rpc.SafeBlockNumber)))
	if err != nil || header == nil || header.Number == nil || !header.Number.IsUint64() {
		return false, ErrWalletDependency
	}
	number := new(big.Int).Set(header.Number)
	valid, err := c.IsValidSignatureAt(ctx, address, hash, signature, number)
	if err != nil {
		return false, err
	}
	recheck, err := reader.HeaderByNumber(ctx, number)
	if err != nil || recheck == nil || recheck.Number == nil || recheck.Number.Cmp(number) != 0 || recheck.Hash() != header.Hash() || ctx.Err() != nil {
		return false, ErrWalletDependency
	}
	return valid, nil
}

// IsValidSignatureAt uses opaque signature bytes at the caller's concrete
// anchor. It never performs EOA recovery and never retries at latest.
func (c *EIP1271Checker) IsValidSignatureAt(ctx context.Context, address string, hash [32]byte, signature []byte, number *big.Int) (bool, error) {
	return c.isValidSignatureAt(ctx, address, hash, signature, number, WalletCallGasLimit)
}

func (c *EIP1271Checker) isValidSignatureAt(ctx context.Context, address string, hash [32]byte, signature []byte, number *big.Int, gas uint64) (bool, error) {
	if !common.IsHexAddress(address) || number == nil || number.Sign() < 0 || len(signature) == 0 || len(signature) > MaxWalletSignatureBytes {
		return false, ErrWalletFormat
	}
	if bytes.HasSuffix(signature, wrapped6492Suffix) {
		return false, ErrWalletUnsupported
	}
	if c.caller == nil {
		return false, ErrWalletDependency
	}
	ctx, cancel := context.WithTimeout(ctx, WalletVerificationTimeout)
	defer cancel()
	data, err := c.abi.Pack("isValidSignature", hash, signature)
	if err != nil {
		return false, fmt.Errorf("%w: signature encoding", ErrWalletFormat)
	}
	contract := common.HexToAddress(address)
	result, err := walletCall(ctx, c.caller, ethereum.CallMsg{To: &contract, Data: data, Gas: gas}, new(big.Int).Set(number))
	if err != nil {
		return false, err
	}
	if ctx.Err() != nil {
		return false, ErrWalletDependency
	}
	// bytes4 has exactly one ABI word, left-aligned with zero padding.
	// ABI Unpack alone accepts dirty padding or trailing words in some versions.
	if len(result) != 32 || !bytes.Equal(result[4:], make([]byte, 28)) {
		return false, ErrWalletSignature
	}
	return bytes.Equal(result[:4], EIP1271MagicValue[:]), nil
}
