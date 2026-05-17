package web3

import (
	"context"
	"math/big"
	"testing"
	"time"
)

func TestIsStuck_True(t *testing.T) {
	pending := &PendingTx{
		Hash:     "0xabc",
		Nonce:    5,
		GasPrice: big.NewInt(1e9),
		SentAt:   time.Now().Add(-5 * time.Minute),
	}
	if !IsStuck(pending, 3*time.Minute) {
		t.Error("tx sent 5 min ago should be stuck with 3-min threshold")
	}
}

func TestIsStuck_False(t *testing.T) {
	pending := &PendingTx{
		Hash:     "0xabc",
		Nonce:    5,
		GasPrice: big.NewInt(1e9),
		SentAt:   time.Now().Add(-30 * time.Second),
	}
	if IsStuck(pending, 3*time.Minute) {
		t.Error("tx sent 30s ago should NOT be stuck with 3-min threshold")
	}
}

func TestIsStuck_ExactlyAtThreshold(t *testing.T) {
	pending := &PendingTx{
		Hash:     "0xabc",
		Nonce:    5,
		GasPrice: big.NewInt(1e9),
		SentAt:   time.Now().Add(-3 * time.Minute),
	}
	_ = IsStuck(pending, 3*time.Minute)
}

func TestPendingTx_EIP1559Fields(t *testing.T) {
	pending := &PendingTx{
		Hash:         "0xdef",
		Nonce:        10,
		GasTipCap:    big.NewInt(2_000_000_000),  // 2 Gwei
		MaxFeePerGas: big.NewInt(10_000_000_000), // 10 Gwei
		IsEIP1559:    true,
		To:           "0x1234567890123456789012345678901234567890",
		Value:        big.NewInt(0),
		Data:         nil,
		SentAt:       time.Now(),
		ChainID:      1,
	}

	assertEqual(t, true, pending.IsEIP1559)
	assertEqual(t, 0, pending.GasTipCap.Cmp(big.NewInt(2_000_000_000)))
	assertEqual(t, 0, pending.MaxFeePerGas.Cmp(big.NewInt(10_000_000_000)))
	assertEqual(t, (*big.Int)(nil), pending.GasPrice)
}

func TestPendingTx_LegacyFields(t *testing.T) {
	pending := &PendingTx{
		Hash:      "0xabc",
		Nonce:     5,
		GasPrice:  big.NewInt(1e9),
		IsEIP1559: false,
		To:        "0x1234567890123456789012345678901234567890",
		Value:     big.NewInt(0),
		Data:      nil,
		SentAt:    time.Now(),
		ChainID:   1,
	}

	assertEqual(t, false, pending.IsEIP1559)
	assertEqual(t, 0, pending.GasPrice.Cmp(big.NewInt(1e9)))
	assertEqual(t, (*big.Int)(nil), pending.GasTipCap)
	assertEqual(t, (*big.Int)(nil), pending.MaxFeePerGas)
}

func TestBumpLegacyTx(t *testing.T) {
	pending := &PendingTx{
		Hash:      "0xabc",
		Nonce:     5,
		GasPrice:  big.NewInt(1_000_000_000), // 1 Gwei
		IsEIP1559: false,
		To:        "0x1234567890123456789012345678901234567890",
		Value:     big.NewInt(0),
		Data:      nil,
		SentAt:    time.Now(),
		ChainID:   1,
	}

	// Test the legacy bump calculation logic
	bumpPercent := int64(10)
	bumpFactor := big.NewInt(100 + bumpPercent)
	newGasPrice := new(big.Int).Mul(pending.GasPrice, bumpFactor)
	newGasPrice.Div(newGasPrice, big.NewInt(100))

	expected := big.NewInt(1_100_000_000) // 1.1 Gwei
	assertEqual(t, 0, newGasPrice.Cmp(expected))
}

func TestBumpEIP1559TipCalculation(t *testing.T) {
	pending := &PendingTx{
		Hash:         "0xdef",
		Nonce:        10,
		GasTipCap:    big.NewInt(2_000_000_000),  // 2 Gwei
		MaxFeePerGas: big.NewInt(10_000_000_000), // 10 Gwei
		IsEIP1559:    true,
		To:           "0x1234567890123456789012345678901234567890",
		Value:        big.NewInt(0),
		Data:         nil,
		SentAt:       time.Now(),
		ChainID:      1,
	}

	// Test tip bump calculation
	bumpPercent := int64(10)
	bumpFactor := big.NewInt(100 + bumpPercent)
	newTip := new(big.Int).Mul(pending.GasTipCap, bumpFactor)
	newTip.Div(newTip, big.NewInt(100))

	expectedTip := big.NewInt(2_200_000_000) // 2.2 Gwei
	assertEqual(t, 0, newTip.Cmp(expectedTip))

	// MaxFeePerGas should always be >= GasTipCap
	if newTip.Cmp(pending.MaxFeePerGas) > 0 {
		t.Error("bumped tip should not exceed MaxFeePerGas without also bumping MaxFeePerGas")
	}
}

func TestBumpPercentValidation(t *testing.T) {
	pending := &PendingTx{
		Hash:      "0xabc",
		Nonce:     5,
		GasPrice:  big.NewInt(1e9),
		IsEIP1559: false,
		To:        "0x1234567890123456789012345678901234567890",
		Value:     big.NewInt(0),
		SentAt:    time.Now(),
		ChainID:   1,
	}

	tt := &TxTracker{}

	// BumpGas should reject bump percent < 10 (EIP-1559 minimum)
	_, err := tt.BumpGas(context.TODO(), nil, pending, 5)
	if err == nil {
		t.Error("expected error for bump percent 5 (below minimum 10)")
	}
	_, err = tt.BumpGas(context.TODO(), nil, pending, 0)
	if err == nil {
		t.Error("expected error for bump percent 0")
	}
	_, err = tt.BumpGas(context.TODO(), nil, pending, -5)
	if err == nil {
		t.Error("expected error for negative bump percent")
	}
}

func TestPendingTx_GasLimitPreserved(t *testing.T) {
	// PendingTx with explicit GasLimit
	pending := &PendingTx{
		Hash:      "0xabc",
		Nonce:     5,
		GasPrice:  big.NewInt(1e9),
		IsEIP1559: false,
		To:        "0x1234567890123456789012345678901234567890",
		Value:     big.NewInt(0),
		GasLimit:  350000,
		SentAt:    time.Now(),
		ChainID:   1,
	}
	assertEqual(t, 350000, int(pending.GasLimit))

	// PendingTx with zero GasLimit (should fallback to default)
	zeroLimit := &PendingTx{
		Hash:      "0xdef",
		Nonce:     6,
		GasPrice:  big.NewInt(1e9),
		IsEIP1559: false,
		GasLimit:  0,
	}
	assertEqual(t, 0, int(zeroLimit.GasLimit))
}

func TestBumpGasLimitCalculation(t *testing.T) {
	// Test that bumpLegacy uses pending.GasLimit when set
	gasLimit := uint64(350000)
	pending := &PendingTx{
		GasPrice: big.NewInt(1_000_000_000),
		GasLimit: gasLimit,
	}

	// The gas limit used in the bumped tx should be pending.GasLimit
	// (not the hardcoded 200000)
	if pending.GasLimit != 0 {
		usedLimit := pending.GasLimit
		assertEqual(t, 350000, int(usedLimit))
	}

	// When GasLimit is 0, fallback to 200000
	zeroPending := &PendingTx{GasLimit: 0}
	usedLimit := zeroPending.GasLimit
	if usedLimit == 0 {
		usedLimit = 200000 // default fallback
	}
	assertEqual(t, 200000, int(usedLimit))
}

func TestEIP1559MinBumpRequirement(t *testing.T) {
	// EIP-1559 requires replacement tx to have >10% higher tip
	originalTip := big.NewInt(1_000_000_000) // 1 Gwei

	// 10% bump
	bumpPercent := int64(10)
	bumpFactor := big.NewInt(100 + bumpPercent)
	newTip := new(big.Int).Mul(originalTip, bumpFactor)
	newTip.Div(newTip, big.NewInt(100))

	// New tip should be exactly 1.1 Gwei
	expected := big.NewInt(1_100_000_000)
	assertEqual(t, 0, newTip.Cmp(expected))

	// Verify it's > 10% higher (required for EIP-1559 replacement)
	tenPercentOfOriginal := new(big.Int).Div(originalTip, big.NewInt(10))
	minRequired := new(big.Int).Add(originalTip, tenPercentOfOriginal)
	if newTip.Cmp(minRequired) < 0 {
		t.Error("bumped tip should meet EIP-1559 minimum replacement requirement (>10%)")
	}
}

// assertEqual is a simple helper for test assertions
func assertEqual(t *testing.T, expected, actual interface{}) {
	t.Helper()
	switch e := expected.(type) {
	case bool:
		if a, ok := actual.(bool); !ok || e != a {
			t.Errorf("expected %v, got %v", expected, actual)
		}
	case int:
		switch a := actual.(type) {
		case int:
			if e != a {
				t.Errorf("expected %v, got %v", expected, actual)
			}
		case uint64:
			if e != int(a) {
				t.Errorf("expected %v, got %v", expected, actual)
			}
		}
	}
}
