package nft

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type erc20MockCaller struct {
	responses map[string][]byte
}

func (m *erc20MockCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if len(call.Data) < 4 {
		return nil, fmt.Errorf("mock: empty call data")
	}
	selector := common.Bytes2Hex(call.Data[:4])
	if resp, ok := m.responses[selector]; ok {
		return resp, nil
	}
	return nil, fmt.Errorf("mock: unexpected call selector %s", selector)
}

func (m *erc20MockCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	return nil, nil
}

func TestERC20ABI_Valid(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(ERC20ABI))
	if err != nil {
		t.Fatalf("ERC20ABI is not valid: %v", err)
	}
	methods := []string{"balanceOf", "allowance", "decimals", "name", "symbol", "totalSupply"}
	for _, m := range methods {
		if _, ok := parsed.Methods[m]; !ok {
			t.Errorf("ERC20ABI missing method %s", m)
		}
	}
}

func TestERC20Reader_GetTokenBalance_MockError(t *testing.T) {
	logger := zap.NewNop()
	caller := &erc20MockCaller{responses: map[string][]byte{}}
	reader := NewERC20Reader(caller, logger)

	_, err := reader.GetTokenBalance(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000002",
	)
	if err == nil {
		t.Error("expected error from mock with no responses")
	}
}

func TestERC20Reader_GetTokenAllowance_MockError(t *testing.T) {
	logger := zap.NewNop()
	caller := &erc20MockCaller{responses: map[string][]byte{}}
	reader := NewERC20Reader(caller, logger)

	_, err := reader.GetTokenAllowance(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000002",
		"0x0000000000000000000000000000000000000003",
	)
	if err == nil {
		t.Error("expected error from mock")
	}
}

func TestERC20Reader_GetTokenInfo_MockError(t *testing.T) {
	logger := zap.NewNop()
	caller := &erc20MockCaller{responses: map[string][]byte{}}
	reader := NewERC20Reader(caller, logger)

	info, err := reader.GetTokenInfo(context.Background(), "0x0000000000000000000000000000000000000001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info (best-effort)")
	}
	if info.Decimals != 18 {
		t.Errorf("expected default decimals=18, got %d", info.Decimals)
	}
	if info.Name != "" {
		t.Errorf("expected empty name from mock, got %q", info.Name)
	}
}
