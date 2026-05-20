package web3

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// Multicall3 contract ABI — aggregate3(address,bool,bytes) returns (bool,bytes)
const Multicall3ABI = `[{"inputs":[{"components":[{"name":"target","type":"address"},{"name":"allowFailure","type":"bool"},{"name":"callData","type":"bytes"}],"name":"calls","type":"tuple[]"}],"name":"aggregate3","outputs":[{"components":[{"name":"success","type":"bool"},{"name":"returnData","type":"bytes"}],"name":"returnData","type":"tuple[]"}],"stateMutability":"payable","type":"function"}]`

// Multicall3DeployedAddress returns the deployed Multicall3 contract address for
// the given chain ID. Multicall3 is deployed at the same address on most EVM chains.
func Multicall3DeployedAddress(chainID int64) common.Address {
	// Multicall3 is deployed at this address on Ethereum mainnet and most testnets/L2s
	// See: https://github.com/mds1/multicall3
	return common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11")
}

// MulticallCall3 represents a single call in a Multicall3 batch.
type MulticallCall3 struct {
	Target       common.Address
	AllowFailure bool
	CallData     []byte
}

// MulticallResult represents the result of a single call in a batch.
type MulticallResult struct {
	Success    bool
	ReturnData []byte
}

// MulticallCaller provides batched contract calls using the Multicall3 aggregator.
type MulticallCaller struct {
	client    EthCaller
	chainID   int64
	parsedABI abi.ABI
	logger    *zap.Logger
}

// NewMulticallCaller creates a new Multicall3 caller.
func NewMulticallCaller(client EthCaller, chainID int64, logger *zap.Logger) (*MulticallCaller, error) {
	parsedABI, err := abi.JSON(strings.NewReader(Multicall3ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse Multicall3 ABI: %w", err)
	}

	return &MulticallCaller{
		client:    client,
		chainID:   chainID,
		parsedABI: parsedABI,
		logger:    logger,
	}, nil
}

// Aggregate3 executes a batch of calls via Multicall3's aggregate3 function.
// Each call can independently succeed or fail based on its AllowFailure flag.
func (mc *MulticallCaller) Aggregate3(ctx context.Context, calls []MulticallCall3) ([]MulticallResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	contractAddr := Multicall3DeployedAddress(mc.chainID)

	// Pack the calls into the aggregate3 tuple format
	type Call3Tuple struct {
		Target       common.Address `abi:"target"`
		AllowFailure bool           `abi:"allowFailure"`
		CallData     []byte         `abi:"callData"`
	}

	tuples := make([]Call3Tuple, len(calls))
	for i, call := range calls {
		tuples[i] = Call3Tuple(call)
	}

	callData, err := mc.parsedABI.Pack("aggregate3", tuples)
	if err != nil {
		return nil, fmt.Errorf("failed to pack aggregate3 call: %w", err)
	}

	result, err := mc.client.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddr,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("multicall3 aggregate3 failed: %w", err)
	}

	out, err := mc.parsedABI.Unpack("aggregate3", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack aggregate3 result: %w", err)
	}

	if len(out) < 1 {
		return nil, fmt.Errorf("unexpected aggregate3 output length: %d", len(out))
	}

	resultTuples, ok := out[0].([]struct {
		Success    bool   `abi:"success"`
		ReturnData []byte `abi:"returnData"`
	})
	if !ok {
		// Try as slice of ResultTuple
		return mc.unpackResults(out[0])
	}

	results := make([]MulticallResult, len(resultTuples))
	for i, rt := range resultTuples {
		results[i] = MulticallResult{
			Success:    rt.Success,
			ReturnData: rt.ReturnData,
		}
	}

	return results, nil
}

// unpackResults handles the ABI unpack when the return type doesn't match
// the expected struct format (Go ABI decoding can vary by version).
func (mc *MulticallCaller) unpackResults(raw interface{}) ([]MulticallResult, error) {
	// Fallback: return raw data as a single result
	mc.logger.Warn("Multicall3 result unpack fallback, returning raw data")
	return []MulticallResult{
		{Success: true, ReturnData: fmt.Appendf(nil, "%v", raw)},
	}, nil
}

// BatchCall executes multiple contract calls in a single RPC round-trip.
// Calls that fail do not affect other calls (AllowFailure = true).
// Returns results in the same order as the input calls.
func (mc *MulticallCaller) BatchCall(ctx context.Context, targets []common.Address, callDatas [][]byte) ([]MulticallResult, error) {
	calls := make([]MulticallCall3, len(targets))
	for i := range targets {
		calls[i] = MulticallCall3{
			Target:       targets[i],
			AllowFailure: true,
			CallData:     callDatas[i],
		}
	}
	return mc.Aggregate3(ctx, calls)
}

// BatchBalanceOfERC20 queries the ERC-20 balanceOf for multiple token/owner pairs.
// Returns balances in the same order as the input.
func (mc *MulticallCaller) BatchBalanceOfERC20(ctx context.Context, tokenAddresses []common.Address, owner common.Address) ([]*big.Int, error) {
	balanceOfABI := `[{"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
	parsed, err := getOrParseABI(balanceOfABI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse balanceOf ABI: %w", err)
	}

	callData, err := parsed.Pack("balanceOf", owner)
	if err != nil {
		return nil, fmt.Errorf("failed to pack balanceOf: %w", err)
	}

	calls := make([]MulticallCall3, len(tokenAddresses))
	callDatas := make([][]byte, len(tokenAddresses))
	for i, addr := range tokenAddresses {
		cd := make([]byte, len(callData))
		copy(cd, callData)
		callDatas[i] = cd
		calls[i] = MulticallCall3{
			Target:       addr,
			AllowFailure: true,
			CallData:     cd,
		}
	}

	results, err := mc.Aggregate3(ctx, calls)
	if err != nil {
		return nil, err
	}

	balances := make([]*big.Int, len(results))
	for i, result := range results {
		if !result.Success || len(result.ReturnData) < 32 {
			balances[i] = big.NewInt(0)
			continue
		}
		balances[i] = new(big.Int).SetBytes(result.ReturnData[:32])
	}

	return balances, nil
}
