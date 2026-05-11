package web3

import (
	"crypto/ecdsa"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewWalletManager(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	assert.NotNil(t, wm)
	assert.NotNil(t, wm.logger)
}

func TestWalletManager_CreateWallet(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	wallet, err := wm.CreateWallet()
	require.NoError(t, err)
	assert.NotNil(t, wallet)
	assert.NotEmpty(t, wallet.Address)
	assert.NotNil(t, wallet.PrivateKey)
	assert.NotNil(t, wallet.PublicKey)

	assert.True(t, strings.HasPrefix(wallet.Address, "0x"))
	assert.Len(t, wallet.Address, 42)
}

func TestWalletManager_CreateWallet_Multiple(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	wallet1, err := wm.CreateWallet()
	require.NoError(t, err)
	wallet2, err := wm.CreateWallet()
	require.NoError(t, err)

	assert.NotEqual(t, wallet1.Address, wallet2.Address)
	assert.NotEqual(t, wallet1.PrivateKey, wallet2.PrivateKey)
}

func TestWalletManager_ImportWallet(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	privateKeyHex := fmt.Sprintf("%064x", privateKey.D)

	wallet, err := wm.ImportWallet(privateKeyHex)
	require.NoError(t, err)
	assert.NotNil(t, wallet)
	assert.NotNil(t, wallet.PrivateKey)
	assert.NotNil(t, wallet.PublicKey)

	expectedAddress := crypto.PubkeyToAddress(*wallet.PublicKey).Hex()
	assert.Equal(t, expectedAddress, wallet.Address)
}

func TestWalletManager_ImportWallet_ShortHex(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	// Use %x which can produce <64 chars for keys with leading zero bytes
	shortHex := fmt.Sprintf("%x", privateKey.D)

	wallet, err := wm.ImportWallet(shortHex)
	require.NoError(t, err)
	assert.NotNil(t, wallet)

	expectedAddress := crypto.PubkeyToAddress(*wallet.PublicKey).Hex()
	assert.Equal(t, expectedAddress, wallet.Address)
}

func TestWalletManager_ImportWallet_InvalidKey(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	_, err := wm.ImportWallet("invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse private key")

	// Key >= secp256k1 curve order is invalid
	_, err = wm.ImportWallet("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	assert.Error(t, err)

	// Non-hex characters
	_, err = wm.ImportWallet("0xzzzz")
	assert.Error(t, err)

	_, err = wm.ImportWallet("")
	assert.Error(t, err)
}

func TestWalletManager_ExportPrivateKey(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	wallet, err := wm.CreateWallet()
	require.NoError(t, err)

	// Use SecurePrivateKey.UseKey to verify key access
	secure, err := NewSecurePrivateKey(wallet.PrivateKey)
	require.NoError(t, err)

	var keyHex string
	err = secure.UseKey(func(k *ecdsa.PrivateKey) error {
		keyHex = fmt.Sprintf("0x%x", k.D)
		return nil
	})
	require.NoError(t, err)
	assert.NotEmpty(t, keyHex)
	assert.True(t, strings.HasPrefix(keyHex, "0x"))
}

func TestWalletManager_ValidateAddress(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	t.Run("valid address", func(t *testing.T) {
		wallet, err := wm.CreateWallet()
		require.NoError(t, err)

		assert.True(t, wm.ValidateAddress(wallet.Address))
	})

	t.Run("valid well-known address", func(t *testing.T) {
		assert.True(t, wm.ValidateAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"))
		assert.True(t, wm.ValidateAddress("0x742d35cc6634c0532925a3b844bc9e7595f2bd18"))
		assert.True(t, wm.ValidateAddress("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"))
	})

	t.Run("invalid address - wrong length", func(t *testing.T) {
		assert.False(t, wm.ValidateAddress("0x123"))
		assert.False(t, wm.ValidateAddress("0x1234567890123456789012345678901234567890123456"))
	})

	t.Run("invalid address - missing 0x prefix", func(t *testing.T) {
		assert.False(t, wm.ValidateAddress("123456789012345678901234567890123456789012"))
	})

	t.Run("invalid address - empty", func(t *testing.T) {
		assert.False(t, wm.ValidateAddress(""))
	})

	t.Run("invalid address - non-hex", func(t *testing.T) {
		assert.False(t, wm.ValidateAddress("0xzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"))
	})
}

func TestWalletManager_GetWalletInfo(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	t.Run("valid address", func(t *testing.T) {
		wallet, err := wm.CreateWallet()
		require.NoError(t, err)

		info := wm.GetWalletInfo(wallet.Address)
		assert.NotNil(t, info)
		assert.Equal(t, wallet.Address, info.Address)
		assert.True(t, info.IsValid)
	})

	t.Run("invalid address", func(t *testing.T) {
		info := wm.GetWalletInfo("invalid")
		assert.NotNil(t, info)
		assert.Equal(t, "invalid", info.Address)
		assert.False(t, info.IsValid)
	})
}

func TestWallet_Consistency(t *testing.T) {
	logger := zap.NewNop()
	wm := NewWalletManager(logger)

	wallet, err := wm.CreateWallet()
	require.NoError(t, err)

	address := crypto.PubkeyToAddress(*wallet.PublicKey).Hex()
	assert.Equal(t, address, wallet.Address)

	privateKeyHex := fmt.Sprintf("%064x", wallet.PrivateKey.D)
	importedWallet, err := wm.ImportWallet(privateKeyHex)
	require.NoError(t, err)

	assert.Equal(t, wallet.Address, importedWallet.Address)
}

// --- Benchmarks ---

func BenchmarkWalletManager_CreateWallet(b *testing.B) {
	wm := NewWalletManager(zap.NewNop())
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = wm.CreateWallet()
	}
}

func BenchmarkWalletManager_ImportWallet(b *testing.B) {
	wm := NewWalletManager(zap.NewNop())
	wallet, err := wm.CreateWallet()
	require.NoError(b, err)
	privateKeyHex := fmt.Sprintf("%064x", wallet.PrivateKey.D)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = wm.ImportWallet(privateKeyHex)
	}
}
