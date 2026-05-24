package web3

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAAProvider_ValidateNonce_TableDriven(t *testing.T) {
	provider := NewAAProvider(nil)

	tests := []struct {
		name      string
		nonce     *big.Int
		expectErr bool
	}{
		{"nil nonce", nil, true},
		{"valid nonce", big.NewInt(1), false},
		{"zero nonce", big.NewInt(0), false},
		{"large nonce", big.NewInt(1e18), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := provider.validateNonce(tc.nonce)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAAProvider_ValidateNonce_MultipleNonces(t *testing.T) {
	provider := NewAAProvider(nil)

	err := provider.validateNonce(big.NewInt(1))
	require.NoError(t, err)

	err = provider.validateNonce(big.NewInt(2))
	require.NoError(t, err)

	err = provider.validateNonce(big.NewInt(3))
	require.NoError(t, err)
}

func TestAAProvider_ValidateUserOp_TableDriven(t *testing.T) {
	ctx := context.Background()
	sender := common.HexToAddress("0x1234567890123456789012345678901234567890")
	var userOpHash [32]byte
	copy(userOpHash[:], common.Hex2Bytes("0xabcdef"))

	tests := []struct {
		name      string
		nonce     *big.Int
		expectErr string
	}{
		{"nil nonce returns nonce error", nil, "nonce cannot be nil"},
		{"valid nonce returns not implemented", big.NewInt(1), "not implemented"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewAAProvider(nil)
			_, err := p.ValidateUserOp(ctx, sender, userOpHash, big.NewInt(0), tc.nonce)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectErr)
		})
	}
}

func TestAAProvider_ValidateUserOp_ReplayThenNew(t *testing.T) {
	p := NewAAProvider(nil)
	ctx := context.Background()
	sender := common.HexToAddress("0x1234567890123456789012345678901234567890")
	var userOpHash [32]byte

	_, err := p.ValidateUserOp(ctx, sender, userOpHash, big.NewInt(0), big.NewInt(1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")

	_, err = p.ValidateUserOp(ctx, sender, userOpHash, big.NewInt(0), big.NewInt(2))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestAAProvider_CleanupExpiredNonces_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		maxAge     time.Duration
		nonces     []*big.Int
		expectLeft int
	}{
		{"all expired", 1 * time.Nanosecond, []*big.Int{big.NewInt(1), big.NewInt(2)}, 0},
		{"all fresh", 1 * time.Hour, []*big.Int{big.NewInt(1), big.NewInt(2)}, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider := NewAAProvider(nil)
			provider.maxAge = tc.maxAge

			for _, n := range tc.nonces {
				_ = provider.validateNonce(n)
			}

			if tc.maxAge < time.Second {
				time.Sleep(1 * time.Millisecond)
			}

			provider.CleanupExpiredNonces()

			count := 0
			provider.nonceCache.Range(func(key, value interface{}) bool {
				count++
				return true
			})
			assert.Equal(t, tc.expectLeft, count)
		})
	}
}

func TestUserOperation_AllFields(t *testing.T) {
	userOp := UserOperation{
		Sender:               common.HexToAddress("0x1"),
		Nonce:                big.NewInt(5),
		InitCode:             []byte{0x01, 0x02},
		CallData:             []byte{0x03, 0x04},
		CallGasLimit:         big.NewInt(100000),
		VerificationGasLimit: big.NewInt(50000),
		PreVerificationGas:   big.NewInt(21000),
		MaxFeePerGas:         big.NewInt(10_000_000_000),
		MaxPriorityFeePerGas: big.NewInt(2_000_000_000),
		PaymasterAndData:     []byte{0x05},
		Signature:            []byte{0x06, 0x07},
	}

	assert.Equal(t, "0x0000000000000000000000000000000000000001", userOp.Sender.Hex())
	assert.Equal(t, 0, big.NewInt(5).Cmp(userOp.Nonce))
	assert.Equal(t, []byte{0x01, 0x02}, userOp.InitCode)
	assert.Equal(t, []byte{0x03, 0x04}, userOp.CallData)
	assert.Equal(t, uint64(100000), userOp.CallGasLimit.Uint64())
	assert.Equal(t, uint64(50000), userOp.VerificationGasLimit.Uint64())
	assert.Equal(t, uint64(21000), userOp.PreVerificationGas.Uint64())
	assert.Equal(t, []byte{0x05}, userOp.PaymasterAndData)
	assert.Equal(t, []byte{0x06, 0x07}, userOp.Signature)
}

func TestIAccount_Interface(t *testing.T) {
	var _ IAccount = &mockIAccount{}
}

type mockIAccount struct{}

func (m *mockIAccount) ValidateUserOp(_ context.Context, _ common.Address, _ [32]byte, _ *big.Int, _ *big.Int) ([]byte, error) {
	return nil, nil
}
