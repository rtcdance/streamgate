package web3

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func TestMulticall3DeployedAddress(t *testing.T) {
	addr := Multicall3DeployedAddress(1) // Ethereum mainnet
	expected := common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11")
	if addr != expected {
		t.Errorf("expected %s, got %s", expected.Hex(), addr.Hex())
	}
}

func TestNewMulticallCaller(t *testing.T) {
	mc, err := NewMulticallCaller(nil, 1, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc == nil {
		t.Error("expected non-nil MulticallCaller")
	}
}

func TestMulticallCaller_Aggregate3_Empty(t *testing.T) {
	mc, _ := NewMulticallCaller(nil, 1, zap.NewNop())
	results, err := mc.Aggregate3(context.TODO(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Error("expected nil results for empty calls")
	}
}

func TestMulticall3ABI_Valid(t *testing.T) {
	_, err := abi.JSON(strings.NewReader(Multicall3ABI))
	if err != nil {
		t.Fatalf("Multicall3 ABI should be valid: %v", err)
	}
}

func TestMulticallResult(t *testing.T) {
	result := MulticallResult{
		Success:    true,
		ReturnData: []byte{0x00, 0x01, 0x02},
	}
	if !result.Success {
		t.Error("expected success")
	}
	if len(result.ReturnData) != 3 {
		t.Errorf("expected 3 bytes, got %d", len(result.ReturnData))
	}
}

func TestMulticallCall3_Fields(t *testing.T) {
	call := MulticallCall3{
		Target:       common.HexToAddress("0x1234"),
		AllowFailure: true,
		CallData:     []byte{0x01, 0x02},
	}
	if call.Target == (common.Address{}) {
		t.Error("target should not be zero address")
	}
	if !call.AllowFailure {
		t.Error("allowFailure should be true")
	}
}

func TestBigIntZero(t *testing.T) {
	zero := big.NewInt(0)
	if zero.Sign() != 0 {
		t.Error("expected zero")
	}
}
