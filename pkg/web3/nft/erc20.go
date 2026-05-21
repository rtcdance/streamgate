package nft

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/web3/internal/abiutil"
	"go.uber.org/zap"
)

var ERC20ABI = `[{"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"}],"name":"allowance","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"name","outputs":[{"name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"totalSupply","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"name":"owner","type":"address"}],"name":"nonces","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"},{"name":"value","type":"uint256"},{"name":"deadline","type":"uint256"},{"name":"v","type":"uint8"},{"name":"r","type":"bytes32"},{"name":"s","type":"bytes32"}],"name":"permit","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

const PermitABI = `[{"inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"},{"name":"value","type":"uint256"},{"name":"deadline","type":"uint256"},{"name":"v","type":"uint8"},{"name":"r","type":"bytes32"},{"name":"s","type":"bytes32"}],"name":"permit","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

type ERC20Reader struct {
	caller EthCaller
	logger *zap.Logger
	abi    abi.ABI
}

func NewERC20Reader(caller EthCaller, logger *zap.Logger) *ERC20Reader {
	return &ERC20Reader{
		caller: caller,
		logger: logger,
		abi:    abiutil.MustParseABI("ERC-20", ERC20ABI),
	}
}

type ERC20TokenInfo struct {
	Name        string
	Symbol      string
	Decimals    uint8
	TotalSupply *big.Int
}

func (r *ERC20Reader) GetTokenInfo(ctx context.Context, contractAddress string) (*ERC20TokenInfo, error) {
	contract := common.HexToAddress(contractAddress)

	name, err := r.callString(ctx, contract, "name")
	if err != nil {
		r.logger.Warn("ERC-20 name() failed, using empty", zap.Error(err))
		name = ""
	}

	symbol, err := r.callString(ctx, contract, "symbol")
	if err != nil {
		r.logger.Warn("ERC-20 symbol() failed, using empty", zap.Error(err))
		symbol = ""
	}

	decimals, err := r.callUint8(ctx, contract, "decimals")
	if err != nil {
		r.logger.Warn("ERC-20 decimals() failed, defaulting to 18", zap.Error(err))
		decimals = 18
	}

	totalSupply, err := r.callUint256(ctx, contract, "totalSupply")
	if err != nil {
		r.logger.Warn("ERC-20 totalSupply() failed", zap.Error(err))
		totalSupply = big.NewInt(0)
	}

	return &ERC20TokenInfo{
		Name:        name,
		Symbol:      symbol,
		Decimals:    decimals,
		TotalSupply: totalSupply,
	}, nil
}

func (r *ERC20Reader) GetTokenBalance(ctx context.Context, contractAddress, accountAddress string) (*big.Int, error) {
	contract := common.HexToAddress(contractAddress)
	account := common.HexToAddress(accountAddress)

	data, err := r.abi.Pack("balanceOf", account)
	if err != nil {
		return nil, fmt.Errorf("pack balanceOf: %w", err)
	}

	result, err := r.callContract(ctx, contract, data)
	if err != nil {
		return nil, fmt.Errorf("balanceOf call failed: %w", err)
	}

	balance := new(big.Int)
	if err := r.abi.UnpackIntoInterface(&[]*big.Int{balance}, "balanceOf", result); err != nil {
		out, unpackErr := r.abi.Unpack("balanceOf", result)
		if unpackErr != nil {
			return nil, fmt.Errorf("unpack balanceOf: %w", unpackErr)
		}
		if len(out) > 0 {
			if b, ok := out[0].(*big.Int); ok {
				return b, nil
			}
		}
		return nil, fmt.Errorf("unpack balanceOf: unexpected type")
	}
	return balance, nil
}

func (r *ERC20Reader) GetTokenAllowance(ctx context.Context, contractAddress, ownerAddress, spenderAddress string) (*big.Int, error) {
	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)
	spender := common.HexToAddress(spenderAddress)

	data, err := r.abi.Pack("allowance", owner, spender)
	if err != nil {
		return nil, fmt.Errorf("pack allowance: %w", err)
	}

	result, err := r.callContract(ctx, contract, data)
	if err != nil {
		return nil, fmt.Errorf("allowance call failed: %w", err)
	}

	out, err := r.abi.Unpack("allowance", result)
	if err != nil {
		return nil, fmt.Errorf("unpack allowance: %w", err)
	}
	if len(out) > 0 {
		if a, ok := out[0].(*big.Int); ok {
			return a, nil
		}
	}
	return nil, fmt.Errorf("unpack allowance: unexpected type")
}

func (r *ERC20Reader) callString(ctx context.Context, contract common.Address, method string) (string, error) {
	data, err := r.abi.Pack(method)
	if err != nil {
		return "", err
	}
	result, err := r.callContract(ctx, contract, data)
	if err != nil {
		return "", err
	}
	out, err := r.abi.Unpack(method, result)
	if err != nil {
		return "", err
	}
	if len(out) > 0 {
		if s, ok := out[0].(string); ok {
			return s, nil
		}
	}
	return "", fmt.Errorf("unexpected type for %s", method)
}

func (r *ERC20Reader) callUint8(ctx context.Context, contract common.Address, method string) (uint8, error) {
	data, err := r.abi.Pack(method)
	if err != nil {
		return 0, err
	}
	result, err := r.callContract(ctx, contract, data)
	if err != nil {
		return 0, err
	}
	out, err := r.abi.Unpack(method, result)
	if err != nil {
		return 0, err
	}
	if len(out) > 0 {
		switch v := out[0].(type) {
		case uint8:
			return v, nil
		case *big.Int:
			return uint8(v.Uint64()), nil
		}
	}
	return 0, fmt.Errorf("unexpected type for %s", method)
}

func (r *ERC20Reader) callUint256(ctx context.Context, contract common.Address, method string) (*big.Int, error) {
	data, err := r.abi.Pack(method)
	if err != nil {
		return nil, err
	}
	result, err := r.callContract(ctx, contract, data)
	if err != nil {
		return nil, err
	}
	out, err := r.abi.Unpack(method, result)
	if err != nil {
		return nil, err
	}
	if len(out) > 0 {
		if v, ok := out[0].(*big.Int); ok {
			return v, nil
		}
	}
	return nil, fmt.Errorf("unexpected type for %s", method)
}

func (r *ERC20Reader) callContract(ctx context.Context, contract common.Address, data []byte) ([]byte, error) {
	return r.caller.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)
}

func (r *ERC20Reader) GetPermitNonce(ctx context.Context, contractAddress, ownerAddress string) (*big.Int, error) {
	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)

	data, err := r.abi.Pack("nonces", owner)
	if err != nil {
		return nil, fmt.Errorf("pack nonces: %w", err)
	}

	result, err := r.callContract(ctx, contract, data)
	if err != nil {
		return nil, fmt.Errorf("nonces call failed: %w", err)
	}

	out, err := r.abi.Unpack("nonces", result)
	if err != nil {
		return nil, fmt.Errorf("unpack nonces: %w", err)
	}
	if len(out) > 0 {
		if n, ok := out[0].(*big.Int); ok {
			return n, nil
		}
	}
	return nil, fmt.Errorf("unpack nonces: unexpected type")
}

func PackPermitCall(owner, spender common.Address, value, deadline *big.Int, v uint8, r, s [32]byte) ([]byte, error) {
	parsedABI, err := abi.JSON(strings.NewReader(PermitABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse permit ABI: %w", err)
	}
	data, err := parsedABI.Pack("permit", owner, spender, value, deadline, v, r, s)
	if err != nil {
		return nil, fmt.Errorf("failed to pack permit call: %w", err)
	}
	return data, nil
}
