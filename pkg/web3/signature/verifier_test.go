package signature

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSignatureVerifier_SignAndVerify(t *testing.T) {
	sv := NewSignatureVerifier(zap.NewNop())

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	message := "Sign this message to verify your wallet ownership"

	signature, err := sv.SignMessage(message, privateKey)
	require.NoError(t, err)

	valid, err := sv.VerifySignature(context.Background(), address, message, signature)
	require.NoError(t, err)
	assert.True(t, valid, "signature should verify for the correct address")
}

func TestSignatureVerifier_VerifyWrongAddress(t *testing.T) {
	sv := NewSignatureVerifier(zap.NewNop())

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	message := "test message"
	signature, err := sv.SignMessage(message, privateKey)
	require.NoError(t, err)

	wrongAddress := "0x1111111111111111111111111111111111111111"
	valid, err := sv.VerifySignature(context.Background(), wrongAddress, message, signature)
	require.NoError(t, err)
	assert.False(t, valid, "signature should NOT verify for a different address")
}

func TestSignatureVerifier_VerifyInvalidSignature(t *testing.T) {
	sv := NewSignatureVerifier(zap.NewNop())

	t.Run("too short", func(t *testing.T) {
		_, err := sv.VerifySignature(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "msg", "0x1234")
		assert.Error(t, err)
	})

	t.Run("invalid hex", func(t *testing.T) {
		_, err := sv.VerifySignature(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "msg", "0xzzzz")
		assert.Error(t, err)
	})
}

func TestSignatureVerifier_SignMessageFormat(t *testing.T) {
	sv := NewSignatureVerifier(zap.NewNop())

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	signature, err := sv.SignMessage("test", privateKey)
	require.NoError(t, err)

	assert.True(t, len(signature) > 2, "signature should not be empty")

	sigBytes := common.FromHex(signature)
	assert.Equal(t, 65, len(sigBytes), "signature should be 65 bytes")

	recoveryID := sigBytes[64]
	assert.True(t, recoveryID == 27 || recoveryID == 28, "recovery ID should be 27 or 28, got %d", recoveryID)
}

func TestSignatureVerifier_GetAddressFromPrivateKey(t *testing.T) {
	sv := NewSignatureVerifier(zap.NewNop())

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	actualAddr := sv.GetAddressFromPrivateKey(privateKey)

	assert.Equal(t, expectedAddr, actualAddr)
}

func TestSignatureVerifier_Missing0xPrefix(t *testing.T) {
	sv := NewSignatureVerifier(zap.NewNop())

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	message := "test message"

	signature, err := sv.SignMessage(message, privateKey)
	require.NoError(t, err)

	addrNoPrefix := address[2:]
	sigNoPrefix := signature[2:]

	valid, err := sv.VerifySignature(context.Background(), addrNoPrefix, message, sigNoPrefix)
	require.NoError(t, err)
	assert.True(t, valid, "verification should work without 0x prefix")
}

func BenchmarkSignatureVerifier_hashMessage(b *testing.B) {
	sv := NewSignatureVerifier(zap.NewNop())
	msg := "Sign this message to verify your wallet ownership for StreamGate"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sv.hashMessage(msg)
	}
}

func BenchmarkSignatureVerifier_SignMessage(b *testing.B) {
	sv := NewSignatureVerifier(zap.NewNop())
	privateKey, err := crypto.GenerateKey()
	require.NoError(b, err)
	msg := "Sign this message to verify your wallet ownership for StreamGate"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = sv.SignMessage(msg, privateKey)
	}
}

func BenchmarkSignatureVerifier_VerifySignature(b *testing.B) {
	sv := NewSignatureVerifier(zap.NewNop())
	privateKey, err := crypto.GenerateKey()
	require.NoError(b, err)
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	msg := "Sign this message to verify your wallet ownership for StreamGate"
	sig, err := sv.SignMessage(msg, privateKey)
	require.NoError(b, err)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = sv.VerifySignature(context.Background(), address, msg, sig)
	}
}
