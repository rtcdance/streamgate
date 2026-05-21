package signature

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type mockEIP1271Caller struct {
	result []byte
	err    error
}

func (m *mockEIP1271Caller) CallContract(ctx context.Context, callMsg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func (m *mockEIP1271Caller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	return nil, nil
}

func parseEIP1271ABI() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(EIP1271ABI))
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestEIP1271_IsValidSignature_Valid(t *testing.T) {
	parsedABI := parseEIP1271ABI()
	validResult, _ := parsedABI.Methods["isValidSignature"].Outputs.Pack(EIP1271MagicValue)

	caller := &mockEIP1271Caller{result: validResult}
	checker := NewEIP1271Checker(caller, zap.NewNop())

	var hash [32]byte
	copy(hash[:], common.HexToHash("0xabc").Bytes())
	sig := common.Hex2Bytes("1234")

	valid, err := checker.IsValidSignature(context.Background(), "0x1234567890123456789012345678901234567890", hash, sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected valid signature")
	}
}

func TestEIP1271_IsValidSignature_Invalid(t *testing.T) {
	parsedABI := parseEIP1271ABI()
	invalidMagic := [4]byte{0x00, 0x00, 0x00, 0x00}
	invalidResult, _ := parsedABI.Methods["isValidSignature"].Outputs.Pack(invalidMagic)

	caller := &mockEIP1271Caller{result: invalidResult}
	checker := NewEIP1271Checker(caller, zap.NewNop())

	var hash [32]byte
	sig := common.Hex2Bytes("1234")

	valid, err := checker.IsValidSignature(context.Background(), "0x1234567890123456789012345678901234567890", hash, sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected invalid signature")
	}
}

func TestEIP1271_IsValidSignature_ContractError(t *testing.T) {
	caller := &mockEIP1271Caller{err: errors.New("contract reverted")}
	checker := NewEIP1271Checker(caller, zap.NewNop())

	var hash [32]byte
	sig := common.Hex2Bytes("1234")

	valid, err := checker.IsValidSignature(context.Background(), "0x1234567890123456789012345678901234567890", hash, sig)
	if err == nil {
		t.Error("expected error when contract reverts")
	}
	if valid {
		t.Error("should not be valid on contract error")
	}
}

func TestEIP1271_MagicValue(t *testing.T) {
	expected := [4]byte{0x16, 0x26, 0xba, 0x7e}
	if EIP1271MagicValue != expected {
		t.Errorf("magic value mismatch: got %x, expected %x", EIP1271MagicValue, expected)
	}
}

func TestEIP1271_IsValidSignature_EmptyResult(t *testing.T) {
	caller := &mockEIP1271Caller{result: []byte{}}
	checker := NewEIP1271Checker(caller, zap.NewNop())

	var hash [32]byte
	sig := common.Hex2Bytes("1234")

	_, err := checker.IsValidSignature(context.Background(), "0x1234567890123456789012345678901234567890", hash, sig)
	if err == nil {
		t.Error("expected error for empty result")
	}
}
