package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/web3"
)

func TestIsValidSolanaAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{"valid Solana address", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", true},
		{"short address", "abc", false},
		{"too long", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsUEXTRA", false},
		{"contains 0", "7xKXtg2CW87d97TXJSDpbD5jBkheT0A83TZRuJosgAsU", false},
		{"contains O", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJOsgAsU", false},
		{"contains I", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRIJosgAsU", false},
		{"contains l", "7xKXtg2CW87d97TXJSDpbD5jBkhelqA83TZRuJosgAsU", false},
		{"hex EVM address", "0x1234567890123456789012345678901234567890", false},
		{"empty", "", false},
		{"exactly 32 chars (System Program)", "11111111111111111111111111111111", true},
		{"exactly 44 chars", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsValidSolanaAddress(tt.address))
		})
	}
}

func TestIsSolanaChain(t *testing.T) {
	tests := []struct {
		name    string
		chainID int64
		want    bool
	}{
		{"Solana Mainnet", -1, true},
		{"Solana Devnet", -2, true},
		{"Ethereum Mainnet", 1, false},
		{"Sepolia", 11155111, false},
		{"Polygon", 137, false},
		{"zero", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSolanaChain(tt.chainID))
		})
	}
}

func TestMultiChainSignatureVerifier_EVM(t *testing.T) {
	verifier := NewMultiChainSignatureVerifier(zap.NewNop(), nil)
	assert.NotNil(t, verifier)

	// EVM path should work even without Solana verifier
	// VerifySignature delegates to EVM verifier — invalid input returns false/error
	ok, _ := verifier.VerifySignature(context.Background(), "0xinvalid", "message", "0xbadsig")
	assert.False(t, ok)
}

func TestMultiChainSignatureVerifier_SolanaNotConfigured(t *testing.T) {
	verifier := NewMultiChainSignatureVerifier(zap.NewNop(), nil)

	_, err := verifier.VerifySolanaSignature("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "msg", "sig")
	assert.ErrorIs(t, err, ErrSolanaNotConfigured)
}

func TestMultiChainSignatureVerifier_SolanaWithVerifier(t *testing.T) {
	// Create a Solana verifier (no actual RPC needed for local signature verification)
	solanaVerifier := web3.NewSolanaVerifier(zap.NewNop(), "")
	verifier := NewMultiChainSignatureVerifier(zap.NewNop(), solanaVerifier)

	// Invalid signature format should return error
	_, err := verifier.VerifySolanaSignature("invalid-address", "msg", "invalid-sig")
	assert.Error(t, err)
}

func TestMemoryTokenBlacklist(t *testing.T) {
	blacklist := NewMemoryTokenBlacklist()

	t.Run("not revoked initially", func(t *testing.T) {
		assert.False(t, blacklist.IsRevoked(context.Background(), "jti-1"))
	})

	t.Run("revoke and check", func(t *testing.T) {
		err := blacklist.Revoke(context.Background(), "jti-2", time.Now().Add(time.Hour))
		require.NoError(t, err)
		assert.True(t, blacklist.IsRevoked(context.Background(), "jti-2"))
	})

	t.Run("expired token auto-evicted", func(t *testing.T) {
		err := blacklist.Revoke(context.Background(), "jti-expired", time.Now().Add(-time.Second))
		require.NoError(t, err)
		assert.False(t, blacklist.IsRevoked(context.Background(), "jti-expired"))
	})

	t.Run("different JTIs independent", func(t *testing.T) {
		err := blacklist.Revoke(context.Background(), "jti-3", time.Now().Add(time.Hour))
		require.NoError(t, err)
		assert.True(t, blacklist.IsRevoked(context.Background(), "jti-3"))
		assert.False(t, blacklist.IsRevoked(context.Background(), "jti-4"))
	})
}

func TestSolanaWalletChallenge(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())
	solanaVerifier := web3.NewSolanaVerifier(zap.NewNop(), "")
	sigVerifier := NewMultiChainSignatureVerifier(zap.NewNop(), solanaVerifier)
	auth.signatureVerifier = sigVerifier

	t.Run("generate Solana challenge", func(t *testing.T) {
		solanaAddr := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
		challenge, err := auth.GenerateWalletChallenge(context.Background(), solanaAddr, -1)
		require.NoError(t, err)
		assert.Equal(t, solanaAddr, challenge.WalletAddress)
		assert.Equal(t, int64(-1), challenge.ChainID)
	})

	t.Run("reject invalid Solana address on Solana chain", func(t *testing.T) {
		_, err := auth.GenerateWalletChallenge(context.Background(), "0xbadaddress", -1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Solana wallet address")
	})

	t.Run("reject hex address on Solana chain", func(t *testing.T) {
		_, err := auth.GenerateWalletChallenge(context.Background(), "0x1234567890123456789012345678901234567890", -1)
		assert.Error(t, err)
	})
}
