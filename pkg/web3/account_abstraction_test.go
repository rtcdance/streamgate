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

func TestNewAAProvider(t *testing.T) {
	provider := NewAAProvider(nil)
	require.NotNil(t, provider)
	assert.NotNil(t, provider.nonceCache)
	assert.Equal(t, 5*time.Minute, provider.maxAge)
}

func TestAAProvider_ValidateNonce_Nil(t *testing.T) {
	provider := NewAAProvider(nil)
	err := provider.validateNonce(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonce cannot be nil")
}

func TestAAProvider_ValidateNonce_Success(t *testing.T) {
	provider := NewAAProvider(nil)
	err := provider.validateNonce(big.NewInt(1))
	assert.NoError(t, err)
}

func TestAAProvider_ValidateNonce_ReplayProtection(t *testing.T) {
	provider := NewAAProvider(nil)
	err := provider.validateNonce(big.NewInt(42))
	require.NoError(t, err)

	err = provider.validateNonce(big.NewInt(42))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "possible replay attack")
}

func TestAAProvider_ValidateNonce_ExpiredNonce(t *testing.T) {
	provider := NewAAProvider(nil)
	provider.maxAge = 1 * time.Nanosecond

	err := provider.validateNonce(big.NewInt(99))
	require.NoError(t, err)

	time.Sleep(1 * time.Millisecond)

	err = provider.validateNonce(big.NewInt(99))
	assert.NoError(t, err, "expired nonce should be allowed again")
}

func TestAAProvider_CleanupExpiredNonces(t *testing.T) {
	provider := NewAAProvider(nil)
	provider.maxAge = 1 * time.Nanosecond

	err := provider.validateNonce(big.NewInt(1))
	require.NoError(t, err)
	err = provider.validateNonce(big.NewInt(2))
	require.NoError(t, err)

	time.Sleep(1 * time.Millisecond)

	provider.CleanupExpiredNonces()

	count := 0
	provider.nonceCache.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	assert.Equal(t, 0, count, "all expired nonces should be cleaned up")
}

func TestAAProvider_CleanupExpiredNonces_KeepsFresh(t *testing.T) {
	provider := NewAAProvider(nil)
	provider.maxAge = 1 * time.Hour

	err := provider.validateNonce(big.NewInt(1))
	require.NoError(t, err)

	provider.CleanupExpiredNonces()

	count := 0
	provider.nonceCache.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	assert.Equal(t, 1, count, "fresh nonce should be kept")
}

func TestAAProvider_ValidateUserOp_ReplayProtection(t *testing.T) {
	provider := NewAAProvider(nil)
	ctx := context.Background()
	sender := common.HexToAddress("0x1234567890123456789012345678901234567890")
	var userOpHash [32]byte
	copy(userOpHash[:], common.Hex2Bytes("0xabcdef"))

	_, err := provider.ValidateUserOp(ctx, sender, userOpHash, big.NewInt(0), big.NewInt(1))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestAAProvider_ValidateUserOp_NilNonce(t *testing.T) {
	provider := NewAAProvider(nil)
	ctx := context.Background()
	sender := common.HexToAddress("0x1234567890123456789012345678901234567890")
	var userOpHash [32]byte

	_, err := provider.ValidateUserOp(ctx, sender, userOpHash, big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonce cannot be nil")
}

func TestUserOperation_Fields(t *testing.T) {
	userOp := UserOperation{
		Sender:               common.HexToAddress("0x1"),
		Nonce:                big.NewInt(1),
		InitCode:             []byte{0x01},
		CallData:             []byte{0x02},
		CallGasLimit:         big.NewInt(100000),
		VerificationGasLimit: big.NewInt(50000),
		PreVerificationGas:   big.NewInt(21000),
		MaxFeePerGas:         big.NewInt(10_000_000_000),
		MaxPriorityFeePerGas: big.NewInt(2_000_000_000),
		PaymasterAndData:     []byte{},
		Signature:            []byte{0x03},
	}

	assert.Equal(t, "0x0000000000000000000000000000000000000001", userOp.Sender.Hex())
	assert.Equal(t, 0, big.NewInt(1).Cmp(userOp.Nonce))
	assert.Equal(t, uint64(100000), userOp.CallGasLimit.Uint64())
}
