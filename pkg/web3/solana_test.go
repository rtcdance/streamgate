package web3

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSolanaVerifier_VerifySignature_Local(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	// Generate ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	// Convert public key to Solana base58 address
	solanaPubKey := solana.PublicKeyFromBytes(pubKey)
	address := solanaPubKey.String()

	t.Run("valid signature", func(t *testing.T) {
		message := "test message"
		sig := ed25519.Sign(privKey, []byte(message))
		sigB64 := base64.StdEncoding.EncodeToString(sig)

		ok, err := sv.VerifySignature(address, message, sigB64)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("valid signature with base64 message", func(t *testing.T) {
		message := base64.StdEncoding.EncodeToString([]byte("hello solana"))
		sig := ed25519.Sign(privKey, []byte("hello solana"))
		sigB64 := base64.StdEncoding.EncodeToString(sig)

		ok, err := sv.VerifySignature(address, message, sigB64)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("wrong message", func(t *testing.T) {
		sig := ed25519.Sign(privKey, []byte("original"))
		sigB64 := base64.StdEncoding.EncodeToString(sig)

		ok, err := sv.VerifySignature(address, "tampered", sigB64)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("invalid address", func(t *testing.T) {
		sig := ed25519.Sign(privKey, []byte("msg"))
		sigB64 := base64.StdEncoding.EncodeToString(sig)

		_, err := sv.VerifySignature("invalid-address!", "msg", sigB64)
		assert.Error(t, err)
	})

	t.Run("invalid signature format", func(t *testing.T) {
		_, err := sv.VerifySignature(address, "msg", "not-base64!!!")
		assert.Error(t, err)
	})

	t.Run("wrong signature length", func(t *testing.T) {
		shortSig := base64.StdEncoding.EncodeToString([]byte("short"))
		_, err := sv.VerifySignature(address, "msg", shortSig)
		assert.Error(t, err)
	})
}

func TestMultiChainManager_SolanaChains(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())

	t.Run("add Solana chain with negative ID", func(t *testing.T) {
		err := mcm.AddChain(-1)
		require.NoError(t, err)

		client, err := mcm.GetSolanaClient(-1)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("add Solana Devnet", func(t *testing.T) {
		err := mcm.AddChain(-2)
		require.NoError(t, err)

		client, err := mcm.GetSolanaClient(-2)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("EVM client not found for Solana chain ID", func(t *testing.T) {
		_, err := mcm.GetClient(-1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "EVM chain client not found")
	})

	t.Run("Solana client not found for EVM chain ID", func(t *testing.T) {
		_, err := mcm.GetSolanaClient(1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Solana chain client not found")
	})

	t.Run("unsupported chain ID", func(t *testing.T) {
		err := mcm.AddChain(-999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chain not supported")
	})

	t.Run("remove Solana chain", func(t *testing.T) {
		mcm.RemoveChain(-2)
		_, err := mcm.GetSolanaClient(-2)
		assert.Error(t, err)
	})

	mcm.Close()
}

func TestSolanaVerifier_DeriveTokenAccountAddress(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	t.Run("valid addresses", func(t *testing.T) {
		wallet := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
		mint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC mint

		ata, err := sv.DeriveTokenAccountAddress(wallet, mint)
		require.NoError(t, err)
		assert.NotEmpty(t, ata)
		// Should be a valid base58 address
		assert.GreaterOrEqual(t, len(ata), 32)
		assert.LessOrEqual(t, len(ata), 44)
	})

	t.Run("invalid wallet address", func(t *testing.T) {
		_, err := sv.DeriveTokenAccountAddress("invalid", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
		assert.Error(t, err)
	})

	t.Run("invalid mint address", func(t *testing.T) {
		_, err := sv.DeriveTokenAccountAddress("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "invalid")
		assert.Error(t, err)
	})
}

func TestSolanaVerifier_IsValidAddress(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	t.Run("valid Solana address", func(t *testing.T) {
		assert.True(t, sv.IsValidAddress("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"))
	})

	t.Run("invalid address", func(t *testing.T) {
		assert.False(t, sv.IsValidAddress("not-a-valid-address!"))
	})

	t.Run("empty address", func(t *testing.T) {
		assert.False(t, sv.IsValidAddress(""))
	})
}
