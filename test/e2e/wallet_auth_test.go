package e2e_test

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/service"
	"streamgate/pkg/web3"
)

// newTestAuthService creates an AuthService wired with a real SignatureVerifier
// and in-memory stores — no external dependencies required.
func newTestAuthService() (*service.AuthService, *ecdsa.PrivateKey, string) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		panic(err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	storage := NewMockAuthStorage()
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	challengeStore := service.NewMemoryChallengeStore()
	blacklist := service.NewMemoryTokenBlacklist()

	authSvc := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-32bytes!",
		storage,
		verifier,
		challengeStore,
		5*time.Minute,
		blacklist,
	)

	return authSvc, privateKey, address
}

// signPersonalMessage signs a message using EIP-191 personal_sign via the
// SignatureVerifier helper — this produces the exact same signature format
// that MetaMask would produce.
func signPersonalMessage(message string, privateKey *ecdsa.PrivateKey) (string, error) {
	sv := web3.NewSignatureVerifier(zap.NewNop())
	return sv.SignMessage(message, privateKey)
}

func TestWalletAuth_PersonalSign_FullFlow(t *testing.T) {
	authSvc, privateKey, address := newTestAuthService()

	// Step 1: Generate challenge (personal_sign — default)
	challenge, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
	require.NoError(t, err)
	require.NotNil(t, challenge)
	assert.Equal(t, address, challenge.WalletAddress)
	assert.Equal(t, "personal_sign", challenge.SigningType)
	assert.False(t, challenge.ExpiresAt.IsZero())

	// Step 2: Sign the challenge message with the private key
	signature, err := signPersonalMessage(challenge.Message, privateKey)
	require.NoError(t, err)
	assert.NotEmpty(t, signature)

	// Step 3: Authenticate with the signed challenge
	token, err := authSvc.AuthenticateWithWallet(context.Background(), address, challenge.ID, signature)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	// Step 4: Verify the token
	valid, err := authSvc.Verify(token)
	require.NoError(t, err)
	assert.True(t, valid)

	// Step 5: Parse token and check claims
	claims, err := authSvc.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, address, claims.WalletAddress)

	// Step 6: Token should contain a JTI for revocation
	assert.NotEmpty(t, claims.JTI)
}

func TestWalletAuth_WrongSignature_Rejected(t *testing.T) {
	authSvc, _, address := newTestAuthService()

	// Generate a different key for wrong-signature test
	wrongKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	challenge, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
	require.NoError(t, err)

	// Sign with the WRONG private key
	wrongSig, err := signPersonalMessage(challenge.Message, wrongKey)
	require.NoError(t, err)

	// Authentication should fail
	token, err := authSvc.AuthenticateWithWallet(context.Background(), address, challenge.ID, wrongSig)
	assert.Error(t, err)
	assert.Empty(t, token)
}

func TestWalletAuth_ChallengeReuse_Rejected(t *testing.T) {
	authSvc, privateKey, address := newTestAuthService()

	challenge, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
	require.NoError(t, err)

	signature, err := signPersonalMessage(challenge.Message, privateKey)
	require.NoError(t, err)

	// First use should succeed
	token, err := authSvc.AuthenticateWithWallet(context.Background(), address, challenge.ID, signature)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	// Second use of same challenge should fail
	token2, err := authSvc.AuthenticateWithWallet(context.Background(), address, challenge.ID, signature)
	assert.Error(t, err)
	assert.Empty(t, token2)
}

func TestWalletAuth_ExpiredChallenge_Rejected(t *testing.T) {
	storage := NewMockAuthStorage()
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	challengeStore := service.NewMemoryChallengeStore()
	blacklist := service.NewMemoryTokenBlacklist()

	// Create service with very short TTL
	authSvc := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-32bytes!",
		storage,
		verifier,
		challengeStore,
		1*time.Nanosecond, // immediate expiry
		blacklist,
	)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	challenge, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
	require.NoError(t, err)

	// Wait for challenge to expire
	time.Sleep(10 * time.Millisecond)

	signature, err := signPersonalMessage(challenge.Message, privateKey)
	require.NoError(t, err)

	token, err := authSvc.AuthenticateWithWallet(context.Background(), address, challenge.ID, signature)
	assert.Error(t, err)
	assert.Empty(t, token)
}

func TestWalletAuth_WrongAddress_Rejected(t *testing.T) {
	authSvc, privateKey, address := newTestAuthService()

	challenge, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
	require.NoError(t, err)

	signature, err := signPersonalMessage(challenge.Message, privateKey)
	require.NoError(t, err)

	// Use a different address than the one the challenge was issued for
	wrongAddress := "0x0000000000000000000000000000000000000001"
	token, err := authSvc.AuthenticateWithWallet(context.Background(), wrongAddress, challenge.ID, signature)
	assert.Error(t, err)
	assert.Empty(t, token)
}

func TestWalletAuth_TokenRevocation(t *testing.T) {
	authSvc, privateKey, address := newTestAuthService()

	challenge, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
	require.NoError(t, err)

	signature, err := signPersonalMessage(challenge.Message, privateKey)
	require.NoError(t, err)

	token, err := authSvc.AuthenticateWithWallet(context.Background(), address, challenge.ID, signature)
	require.NoError(t, err)

	// Token should be valid
	valid, err := authSvc.Verify(token)
	require.NoError(t, err)
	assert.True(t, valid)

	// Revoke the token
	err = authSvc.RevokeToken(context.Background(), token)
	require.NoError(t, err)

	// Token should no longer be valid
	result, err := authSvc.VerifyToken(context.Background(), token)
	assert.ErrorIs(t, err, service.ErrTokenRevoked)
	assert.False(t, result.Valid)
}

func TestWalletAuth_MultipleChallenges(t *testing.T) {
	authSvc, privateKey, address := newTestAuthService()

	// Generate multiple challenges
	ch1, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
	require.NoError(t, err)
	ch2, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
	require.NoError(t, err)
	assert.NotEqual(t, ch1.ID, ch2.ID)
	assert.NotEqual(t, ch1.Nonce, ch2.Nonce)

	// Only the second challenge should be usable (each has unique nonce)
	sig2, err := signPersonalMessage(ch2.Message, privateKey)
	require.NoError(t, err)

	token, err := authSvc.AuthenticateWithWallet(context.Background(), address, ch2.ID, sig2)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestWalletAuth_InvalidAddress_Rejected(t *testing.T) {
	authSvc, _, _ := newTestAuthService()

	_, err := authSvc.GenerateWalletChallenge(context.Background(), "not-an-address", 1)
	assert.Error(t, err)
}

func TestWalletAuth_SigningTypeEIP712(t *testing.T) {
	authSvc, _, address := newTestAuthService()

	// Request EIP-712 signing type
	challenge, err := authSvc.GenerateWalletChallenge(context.Background(), address, 1, "eip712")
	require.NoError(t, err)
	assert.Equal(t, "eip712", challenge.SigningType)

	// The challenge should still have a message field for personal_sign fallback
	assert.NotEmpty(t, challenge.Message)
}

func TestWalletAuth_FullLifecycle(t *testing.T) {
	authSvc, privateKey, address := newTestAuthService()

	// 1. Challenge
	challenge, err := authSvc.GenerateWalletChallenge(context.Background(), address, 11155111) // Sepolia
	require.NoError(t, err)
	assert.Equal(t, int64(11155111), challenge.ChainID)

	// 2. Sign
	signature, err := signPersonalMessage(challenge.Message, privateKey)
	require.NoError(t, err)

	// 3. Authenticate
	token, err := authSvc.AuthenticateWithWallet(context.Background(), address, challenge.ID, signature)
	require.NoError(t, err)

	// 4. Verify
	valid, err := authSvc.Verify(token)
	require.NoError(t, err)
	assert.True(t, valid)

	// 5. Parse claims
	claims, err := authSvc.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, address, claims.WalletAddress)

	// 6. Revoke
	err = authSvc.RevokeToken(context.Background(), token)
	require.NoError(t, err)

	// 7. Verify revoked
	result, err := authSvc.VerifyToken(context.Background(), token)
	assert.ErrorIs(t, err, service.ErrTokenRevoked)
	assert.False(t, result.Valid)

	// 8. New challenge + new token (fresh login)
	ch2, err := authSvc.GenerateWalletChallenge(context.Background(), address, 11155111)
	require.NoError(t, err)
	sig2, err := signPersonalMessage(ch2.Message, privateKey)
	require.NoError(t, err)
	token2, err := authSvc.AuthenticateWithWallet(context.Background(), address, ch2.ID, sig2)
	require.NoError(t, err)
	valid2, err := authSvc.Verify(token2)
	require.NoError(t, err)
	assert.True(t, valid2)
}

// Benchmark to ensure signing/verification is fast enough for production
func BenchmarkWalletAuth_FullFlow(b *testing.B) {
	privateKey, _ := crypto.GenerateKey()
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	storage := NewMockAuthStorage()
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	challengeStore := service.NewMemoryChallengeStore()
	blacklist := service.NewMemoryTokenBlacklist()

	authSvc := service.NewAuthServiceWithDeps(
		"bench-jwt-secret-key-32bytes!",
		storage,
		verifier,
		challengeStore,
		5*time.Minute,
		blacklist,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := authSvc.GenerateWalletChallenge(context.Background(), address, 1)
		sig, _ := signPersonalMessage(ch.Message, privateKey)
		token, _ := authSvc.AuthenticateWithWallet(context.Background(), address, ch.ID, sig)
		if token == "" {
			b.Fatal("expected non-empty token")
		}
	}
}

// Example for documentation
// ExampleWalletAuth demonstrates the full wallet authentication flow.
func Example_fullWalletAuth() {
	// 1. Generate a wallet (in production, the user's wallet does this)
	privateKey, _ := crypto.GenerateKey()
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	// 2. Server-side: create AuthService and generate challenge
	storage := NewMockAuthStorage()
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	authSvc := service.NewAuthServiceWithDeps(
		"jwt-secret",
		storage,
		verifier,
		service.NewMemoryChallengeStore(),
		5*time.Minute,
		service.NewMemoryTokenBlacklist(),
	)

	challenge, _ := authSvc.GenerateWalletChallenge(context.Background(), address, 1)

	// 3. Client-side: sign the challenge
	sv := web3.NewSignatureVerifier(zap.NewNop())
	signature, _ := sv.SignMessage(challenge.Message, privateKey)

	// 4. Server-side: verify and issue JWT
	token, err := authSvc.AuthenticateWithWallet(context.Background(), address, challenge.ID, signature)
	if err != nil {
		fmt.Println("auth failed:", err)
		return
	}
	fmt.Println("token issued:", len(token) > 0)
	// Output: token issued: true
}
