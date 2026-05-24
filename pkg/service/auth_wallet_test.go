package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	stg "github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockChainAwareVerifier struct {
	verifySignatureFunc     func(ctx context.Context, address, message, signature string) (bool, error)
	verifySolanaFunc        func(ctx context.Context, address, message, signature string) (bool, error)
	verifyOffchainFunc      func(ctx context.Context, address, message, signature string) (bool, error)
}

func (m *mockChainAwareVerifier) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	if m.verifySignatureFunc != nil {
		return m.verifySignatureFunc(ctx, address, message, signature)
	}
	return false, nil
}

func (m *mockChainAwareVerifier) VerifySolanaSignature(ctx context.Context, address, message, signature string) (bool, error) {
	if m.verifySolanaFunc != nil {
		return m.verifySolanaFunc(ctx, address, message, signature)
	}
	return false, nil
}

func (m *mockChainAwareVerifier) VerifyOffchainMessage(ctx context.Context, address, message, signature string) (bool, error) {
	if m.verifyOffchainFunc != nil {
		return m.verifyOffchainFunc(ctx, address, message, signature)
	}
	return false, nil
}

type mockEIP712Verifier struct {
	verifyFunc func(address string, typedData *web3.EIP712TypedData, signature string) (bool, error)
}

func (m *mockEIP712Verifier) VerifyTypedData(address string, typedData *web3.EIP712TypedData, signature string) (bool, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(address, typedData, signature)
	}
	return false, nil
}

func TestGenerateWalletChallenge_EVM_SIWE(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	addr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"
	challenge, err := auth.GenerateWalletChallenge(context.Background(), addr, 1)
	require.NoError(t, err)
	assert.NotEmpty(t, challenge.WalletAddress)
	assert.Equal(t, int64(1), challenge.ChainID)
	assert.Equal(t, "siwe", challenge.SigningType)
	assert.NotEmpty(t, challenge.Nonce)
	assert.NotEmpty(t, challenge.Message)
}

func TestGenerateWalletChallenge_EVM_PersonalSign(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1, "personal_sign")
	require.NoError(t, err)
	assert.Equal(t, "personal_sign", challenge.SigningType)
	assert.Contains(t, challenge.Message, "Sign this message")
	assert.Contains(t, challenge.Message, challenge.WalletAddress)
}

func TestGenerateWalletChallenge_EVM_EIP712(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1, "eip712")
	require.NoError(t, err)
	assert.Equal(t, "eip712", challenge.SigningType)

	var typedData web3.EIP712TypedData
	err = json.Unmarshal([]byte(challenge.Message), &typedData)
	require.NoError(t, err)
	assert.Equal(t, "Authentication", typedData.PrimaryType)
	assert.Equal(t, "StreamGate", typedData.Domain.Name)
}

func TestGenerateWalletChallenge_Solana(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "11111111111111111111111111111111", -1)
	require.NoError(t, err)
	assert.Equal(t, "11111111111111111111111111111111", challenge.WalletAddress)
	assert.Equal(t, int64(-1), challenge.ChainID)
	assert.Equal(t, "siwe", challenge.SigningType)
}

func TestGenerateWalletChallenge_Solana_InvalidAddress(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	_, err := auth.GenerateWalletChallenge(context.Background(), "short", -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Solana wallet address")
}

func TestGenerateWalletChallenge_EVM_InvalidAddress(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	_, err := auth.GenerateWalletChallenge(context.Background(), "not-an-address", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid wallet address")
}

func TestGenerateWalletChallenge_AddressNormalization(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35cc6634c0532925a3b844bc9e7595f2bd18", 1)
	require.NoError(t, err)
	assert.True(t, challenge.WalletAddress == "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18" || challenge.WalletAddress == "0x742D35CC6634C0532925a3B844Bc9E7595F2bD18")
}

func TestGenerateWalletChallenge_ChallengeStoreError(t *testing.T) {
	cs := &failingChallengeStore{}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	_, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to store challenge")
}

type failingChallengeStore struct{}

func (f *failingChallengeStore) SaveChallenge(_ context.Context, _ *stg.WalletChallenge) error {
	return errors.New("store failed")
}
func (f *failingChallengeStore) GetChallenge(_ context.Context, _ string) (*stg.WalletChallenge, error) {
	return nil, errors.New("not found")
}
func (f *failingChallengeStore) MarkChallengeUsed(_ context.Context, _ string, _ time.Time) error {
	return errors.New("not found")
}
func (f *failingChallengeStore) Close() error { return nil }

func TestAuthenticateWithWallet_EIP712_Success(t *testing.T) {
	cs := newMockChallengeStore()
	eip712 := &mockEIP712Verifier{
		verifyFunc: func(_ string, _ *web3.EIP712TypedData, _ string) (bool, error) {
			return true, nil
		},
	}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithEIP712Verifier(eip712),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1, "eip712")
	require.NoError(t, err)

	token, err := auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestAuthenticateWithWallet_EIP712_NoVerifier(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1, "eip712")
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestAuthenticateWithWallet_EIP712_InvalidSignature(t *testing.T) {
	cs := newMockChallengeStore()
	eip712 := &mockEIP712Verifier{
		verifyFunc: func(_ string, _ *web3.EIP712TypedData, _ string) (bool, error) {
			return false, nil
		},
	}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithEIP712Verifier(eip712),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1, "eip712")
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	assert.ErrorIs(t, err, ErrInvalidCredential)
}

func TestAuthenticateWithWallet_EIP712_VerifierError(t *testing.T) {
	cs := newMockChallengeStore()
	eip712 := &mockEIP712Verifier{
		verifyFunc: func(_ string, _ *web3.EIP712TypedData, _ string) (bool, error) {
			return false, errors.New("verification error")
		},
	}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithEIP712Verifier(eip712),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1, "eip712")
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to verify wallet signature")
}

func TestAuthenticateWithWallet_Solana_Success(t *testing.T) {
	cs := newMockChallengeStore()
	verifier := &mockChainAwareVerifier{
		verifyOffchainFunc: func(_ context.Context, _, _, _ string) (bool, error) {
			return true, nil
		},
	}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithSignatureVerifier(verifier),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "11111111111111111111111111111111", -1, "personal_sign")
	require.NoError(t, err)

	token, err := auth.AuthenticateWithWallet(context.Background(), "11111111111111111111111111111111", challenge.ID, "solSig", -1)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestAuthenticateWithWallet_Solana_NoChainAwareVerifier(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "11111111111111111111111111111111", -1, "personal_sign")
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "11111111111111111111111111111111", challenge.ID, "solSig", -1)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestAuthenticateWithWallet_Solana_InvalidSignature(t *testing.T) {
	cs := newMockChallengeStore()
	verifier := &mockChainAwareVerifier{
		verifyOffchainFunc: func(_ context.Context, _, _, _ string) (bool, error) {
			return false, nil
		},
	}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithSignatureVerifier(verifier),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "11111111111111111111111111111111", -1, "personal_sign")
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "11111111111111111111111111111111", challenge.ID, "badSig", -1)
	assert.ErrorIs(t, err, ErrInvalidCredential)
}

func TestAuthenticateWithWallet_AddressMismatch(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1)
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x1111111111111111111111111111111111111111", challenge.ID, "0xsig", 1)
	assert.ErrorIs(t, err, ErrInvalidCredential)
}

func TestAuthenticateWithWallet_ChainIDMismatch(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1)
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 999)
	assert.ErrorIs(t, err, ErrChainIDMismatch)
}

func TestAuthenticateWithWallet_ChallengeAlreadyUsed(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1)
	require.NoError(t, err)

	err = cs.MarkChallengeUsed(context.Background(), challenge.ID, time.Now().UTC())
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	assert.Error(t, err)
}

func TestAuthenticateWithWallet_ExpiredChallenge(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithChallengeTTL(1*time.Nanosecond),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	assert.ErrorIs(t, err, ErrChallengeExpired)
}

func TestAuthenticateWithWallet_NoSignatureVerifier(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1)
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestAuthenticateWithWallet_ChallengeNotFound(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	_, err := auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "nonexistent-id", "0xsig", 1)
	assert.Error(t, err)
}

func TestAuthenticateWithWallet_Solana_InvalidAddress(t *testing.T) {
	cs := newMockChallengeStore()
	challenge := &stg.WalletChallenge{
		ID:            "sol-challenge",
		WalletAddress: "11111111111111111111111111111111",
		ChainID:       -1,
		SigningType:   "siwe",
		Nonce:         "nonce",
		Message:       "msg",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}
	err := cs.SaveChallenge(context.Background(), challenge)
	require.NoError(t, err)

	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	_, err = auth.AuthenticateWithWallet(context.Background(), "short", "sol-challenge", "sig", -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Solana wallet address")
}

func TestAuthenticateWithWallet_EVM_InvalidAddress(t *testing.T) {
	cs := newMockChallengeStore()
	challenge := &stg.WalletChallenge{
		ID:            "evm-challenge",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		SigningType:   "siwe",
		Nonce:         "nonce",
		Message:       "msg",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}
	err := cs.SaveChallenge(context.Background(), challenge)
	require.NoError(t, err)

	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)

	_, err = auth.AuthenticateWithWallet(context.Background(), "not-an-address", "evm-challenge", "sig", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid wallet address")
}

func TestAuthenticateWithWallet_AuditLogger(t *testing.T) {
	cs := newMockChallengeStore()
	al := &mockAuditLogger{}
	verifier := &mockChainAwareVerifier{
		verifySignatureFunc: func(_ context.Context, _, _, _ string) (bool, error) {
			return true, nil
		},
	}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithSignatureVerifier(verifier),
		WithAuditLogger(al),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1, "personal_sign")
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	require.NoError(t, err)
	assert.Len(t, al.logs, 1)
	assert.Equal(t, "auth.wallet_login", al.logs[0].action)
	assert.True(t, al.logs[0].success)
}

func TestAuthenticateWithWallet_AuditLogger_Failure(t *testing.T) {
	cs := newMockChallengeStore()
	al := &mockAuditLogger{}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithAuditLogger(al),
	)

	_, err := auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "nonexistent", "0xsig", 1)
	assert.Error(t, err)
	assert.Len(t, al.logs, 1)
	assert.False(t, al.logs[0].success)
}

func TestAuthenticateWithWallet_MarkChallengeUsedError(t *testing.T) {
	cs := &markFailingChallengeStore{challenges: make(map[string]*stg.WalletChallenge)}
	verifier := &mockChainAwareVerifier{
		verifySignatureFunc: func(_ context.Context, _, _, _ string) (bool, error) {
			return true, nil
		},
	}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
		WithSignatureVerifier(verifier),
	)

	challenge, err := auth.GenerateWalletChallenge(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", 1, "personal_sign")
	require.NoError(t, err)

	_, err = auth.AuthenticateWithWallet(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", challenge.ID, "0xsig", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to consume challenge")
}

type markFailingChallengeStore struct {
	challenges map[string]*stg.WalletChallenge
}

func (m *markFailingChallengeStore) SaveChallenge(_ context.Context, c *stg.WalletChallenge) error {
	m.challenges[c.ID] = c
	return nil
}
func (m *markFailingChallengeStore) GetChallenge(_ context.Context, id string) (*stg.WalletChallenge, error) {
	c, ok := m.challenges[id]
	if !ok {
		return nil, stg.ErrChallengeNotFound
	}
	return c, nil
}
func (m *markFailingChallengeStore) MarkChallengeUsed(_ context.Context, _ string, _ time.Time) error {
	return errors.New("mark failed")
}
func (m *markFailingChallengeStore) Close() error { return nil }

func TestIsValidSolanaAddress_WalletPkg(t *testing.T) {
	tests := []struct {
		addr  string
		valid bool
	}{
		{"11111111111111111111111111111111", true},
		{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", false},
		{"short", false},
		{"", false},
		{"a", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.valid, IsValidSolanaAddress(tt.addr), "IsValidSolanaAddress(%q)", tt.addr)
	}
}

func TestIsSolanaChain_WalletPkg(t *testing.T) {
	assert.True(t, isSolanaChain(-1))
	assert.True(t, isSolanaChain(-2))
	assert.False(t, isSolanaChain(0))
	assert.False(t, isSolanaChain(1))
	assert.False(t, isSolanaChain(11155111))
}

func TestGenerateNonce_WalletPkg(t *testing.T) {
	n1, err := generateNonce()
	require.NoError(t, err)
	assert.Len(t, n1, 32)

	n2, err := generateNonce()
	require.NoError(t, err)
	assert.NotEqual(t, n1, n2)
}

func TestGeneratePlaybackToken_DefaultTTL(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.GeneratePlaybackToken(
		context.Background(),
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"content-1",
		"0xcontract",
		"7",
		1,
		0,
		"fp-abc",
	)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := auth.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "content-1", claims.ContentID)
	assert.Equal(t, "0xcontract", claims.Contract)
	assert.Equal(t, "7", claims.TokenID)
	assert.Equal(t, int64(1), claims.ChainID)
	assert.Equal(t, "fp-abc", claims.ClientFingerprint)
}

func TestValidatePlaybackToken_ContentMismatch(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.GeneratePlaybackToken(
		context.Background(),
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"content-1",
		"0xcontract",
		"7",
		1,
		time.Minute,
		"fp-abc",
	)
	require.NoError(t, err)

	_, err = auth.ValidatePlaybackToken(context.Background(), token, "content-2", "fp-abc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "content mismatch")
}

func TestValidatePlaybackToken_WalletMismatch(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.GeneratePlaybackToken(
		context.Background(),
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"content-1",
		"0xcontract",
		"7",
		1,
		time.Minute,
		"fp-abc",
	)
	require.NoError(t, err)

	_, err = auth.ValidatePlaybackToken(context.Background(), token, "content-1", "fp-abc", "0xdifferent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wallet mismatch")
}

func TestValidatePlaybackToken_FingerprintMismatch(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.GeneratePlaybackToken(
		context.Background(),
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"content-1",
		"0xcontract",
		"7",
		1,
		time.Minute,
		"fp-correct",
	)
	require.NoError(t, err)

	_, err = auth.ValidatePlaybackToken(context.Background(), token, "content-1", "fp-wrong")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fingerprint mismatch")
}

func TestValidatePlaybackToken_InvalidToken(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	_, err := auth.ValidatePlaybackToken(context.Background(), "invalid.token", "content-1", "fp-abc")
	assert.Error(t, err)
}

func TestVerifyToken_ExpiredToken(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username:      "expired-user",
		WalletAddress: "0xWallet",
		JTI:           "jti-expired",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	result, err := auth.VerifyToken(context.Background(), token)
	assert.ErrorIs(t, err, ErrTokenExpired)
	assert.False(t, result.Valid)
}

func TestRevokeToken_NoBlacklist(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username: "user",
		JTI:      "jti-123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	err = auth.RevokeToken(context.Background(), token)
	assert.NoError(t, err)
}

func TestRevokeToken_NoJTI(t *testing.T) {
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)

	claims := &Claims{
		Username: "user",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	err = auth.RevokeToken(context.Background(), token)
	assert.NoError(t, err)
}

func TestRevokeToken_BlacklistError(t *testing.T) {
	bl := &errorTokenBlacklist{}
	al := &mockAuditLogger{}
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
		WithAuditLogger(al),
	)

	claims := &Claims{
		Username:      "user",
		WalletAddress: "0xWallet",
		JTI:           "jti-err",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	err = auth.RevokeToken(context.Background(), token)
	assert.Error(t, err)
	assert.Len(t, al.logs, 1)
	assert.False(t, al.logs[0].success)
}

type errorTokenBlacklist struct{}

func (e *errorTokenBlacklist) Revoke(_ context.Context, _ string, _ time.Time) error {
	return errors.New("blacklist error")
}
func (e *errorTokenBlacklist) IsRevoked(_ context.Context, _ string) bool { return false }
func (e *errorTokenBlacklist) Close() error                                { return nil }

func TestIsTokenRevoked_NoBlacklist(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())
	assert.False(t, auth.IsTokenRevoked(context.Background(), "any-jti"))
}

func TestIsTokenRevoked_EmptyJTI(t *testing.T) {
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)
	assert.False(t, auth.IsTokenRevoked(context.Background(), ""))
}

func TestBuildEIP712Challenge(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithSIWEDomain("test.io", "https://test.io/login"),
	)

	challenge := &stg.WalletChallenge{
		ID:            "test-id",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		Nonce:         "test-nonce",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	typedData := auth.buildEIP712Challenge(challenge)
	assert.Equal(t, "StreamGate", typedData.Domain.Name)
	assert.Equal(t, "1", typedData.Domain.Version)
	assert.Equal(t, "Authentication", typedData.PrimaryType)
	assert.NotNil(t, typedData.Domain.ChainId)
	assert.Equal(t, int64(1), typedData.Domain.ChainId.Int64())
}
