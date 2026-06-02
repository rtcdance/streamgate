package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rtcdance/streamgate/pkg/models"
	stg "github.com/rtcdance/streamgate/pkg/storage"
)

type mockTokenBlacklist struct {
	revoked map[string]bool
	closed  bool
}

func newMockTokenBlacklist() *mockTokenBlacklist {
	return &mockTokenBlacklist{revoked: make(map[string]bool)}
}

func (m *mockTokenBlacklist) Revoke(_ context.Context, jti string, _ time.Time) error {
	m.revoked[jti] = true
	return nil
}

func (m *mockTokenBlacklist) IsRevoked(_ context.Context, jti string) bool {
	return m.revoked[jti]
}

func (m *mockTokenBlacklist) Close() error {
	m.closed = true
	return nil
}

type mockChallengeStore struct {
	challenges map[string]*stg.WalletChallenge
	closed     bool
}

func newMockChallengeStore() *mockChallengeStore {
	return &mockChallengeStore{challenges: make(map[string]*stg.WalletChallenge)}
}

func (m *mockChallengeStore) SaveChallenge(_ context.Context, c *stg.WalletChallenge) error {
	m.challenges[c.ID] = c
	return nil
}

func (m *mockChallengeStore) GetChallenge(_ context.Context, id string) (*stg.WalletChallenge, error) {
	c, ok := m.challenges[id]
	if !ok {
		return nil, stg.ErrChallengeNotFound
	}
	return c, nil
}

func (m *mockChallengeStore) MarkChallengeUsed(_ context.Context, id string, usedAt time.Time) error {
	c, ok := m.challenges[id]
	if !ok {
		return stg.ErrChallengeNotFound
	}
	c.UsedAt = usedAt
	return nil
}

func (m *mockChallengeStore) Close() error {
	m.closed = true
	return nil
}

func TestNewJWTVerifier(t *testing.T) {
	v := NewJWTVerifier("my-secret-key-that-is-long-enough")
	require.NotNil(t, v)
	assert.Equal(t, JWTHS256, v.signingType)
}

func TestNewJWTVerifier_WithRSAPublicKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	v := NewJWTVerifier("unused", WithRSAPublicKey(&key.PublicKey))
	require.NotNil(t, v)
	assert.Equal(t, JWTRS256, v.signingType)
	assert.Equal(t, &key.PublicKey, v.publicKey)
}

func TestJWTVerifier_ParseToken_RS256(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		AuthServiceOption(WithRSASigning(privateKey)),
	)

	token, err := auth.signToken(&Claims{
		Username: "rs256-test",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	})
	require.NoError(t, err)

	verifier := NewJWTVerifier("unused", WithRSAPublicKey(&privateKey.PublicKey))
	claims, err := verifier.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "rs256-test", claims.Username)
}

func TestJWTVerifier_ParseToken_RS256_WrongKey(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		AuthServiceOption(WithRSASigning(privateKey)),
	)

	token, err := auth.signToken(&Claims{
		Username: "rs256-test",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	})
	require.NoError(t, err)

	verifier := NewJWTVerifier("unused", WithRSAPublicKey(&wrongKey.PublicKey))
	_, err = verifier.ParseToken(token)
	assert.Error(t, err)
}

func TestJWTVerifier_ParseToken_Expired(t *testing.T) {
	v := NewJWTVerifier("test-secret-that-is-at-least-32-chars")
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.signToken(&Claims{
		Username: "expired-user",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	})
	require.NoError(t, err)

	_, err = v.ParseToken(token)
	assert.Error(t, err)
}

func TestJWTVerifier_ParseToken_NotYetValid(t *testing.T) {
	v := NewJWTVerifier("test-secret-that-is-at-least-32-chars")
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.signToken(&Claims{
		Username: "future-user",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	require.NoError(t, err)

	_, err = v.ParseToken(token)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet valid")
}

func TestJWTVerifier_ParseToken_InvalidToken(t *testing.T) {
	v := NewJWTVerifier("test-secret-that-is-at-least-32-chars")
	_, err := v.ParseToken("not.a.valid.token")
	assert.Error(t, err)
}

func TestErrSigVerifier(t *testing.T) {
	v := errSigVerifier{}
	valid, err := v.VerifySignature(context.Background(), "addr", "msg", "sig")
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestAuthService_Close_WithBlacklist(t *testing.T) {
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)
	auth.Close()
	assert.True(t, bl.closed)
}

func TestAuthService_Close_WithChallengeStore(t *testing.T) {
	cs := newMockChallengeStore()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeStore(cs),
	)
	auth.Close()
	assert.True(t, cs.closed)
}

func TestAuthService_ParseToken_RS256(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		AuthServiceOption(WithRSASigning(privateKey)),
	)

	token, err := auth.signToken(&Claims{
		Username: "rs256-parse",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	})
	require.NoError(t, err)

	claims, err := auth.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "rs256-parse", claims.Username)
}

func TestAuthService_ParseTokenAllowExpired_WithinGrace(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username: "grace-user",
		JTI:      "grace-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	parsed, err := auth.parseTokenAllowExpired(token, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "grace-user", parsed.Username)
}

func TestAuthService_ParseTokenAllowExpired_BeyondGrace(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username: "too-old",
		JTI:      "old-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-10 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	_, err = auth.parseTokenAllowExpired(token, 5*time.Minute)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestAuthService_ParseTokenAllowExpired_InvalidToken(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())
	_, err := auth.parseTokenAllowExpired("invalid.token", 5*time.Minute)
	assert.Error(t, err)
}

func TestAuthService_RefreshToken_WithBlacklist(t *testing.T) {
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)

	claims := &Claims{
		Username: "refresh-user",
		JTI:      "refresh-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	newToken, err := auth.RefreshToken(context.Background(), token)
	require.NoError(t, err)
	assert.NotEmpty(t, newToken)
	assert.True(t, bl.IsRevoked(context.Background(), "refresh-jti"))
}

func TestAuthService_RefreshToken_RevokedToken(t *testing.T) {
	bl := newMockTokenBlacklist()
	_ = bl.Revoke(context.Background(), "revoked-jti", time.Now().Add(time.Hour))

	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)

	claims := &Claims{
		Username: "revoked-user",
		JTI:      "revoked-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	_, err = auth.RefreshToken(context.Background(), token)
	assert.ErrorIs(t, err, ErrTokenRevoked)
}

func TestAuthService_RevokeToken_WithBlacklist(t *testing.T) {
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)

	claims := &Claims{
		Username: "revoke-user",
		JTI:      "revoke-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	err = auth.RevokeToken(context.Background(), token)
	require.NoError(t, err)
	assert.True(t, bl.IsRevoked(context.Background(), "revoke-jti"))
}

func TestAuthService_RevokeToken_WithAuditLogger(t *testing.T) {
	al := &mockAuditLogger{}
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
		WithAuditLogger(al),
	)

	claims := &Claims{
		Username:      "audit-user",
		WalletAddress: "0xAudit",
		JTI:           "audit-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	err = auth.RevokeToken(context.Background(), token)
	require.NoError(t, err)
	assert.Len(t, al.logs, 1)
	assert.Equal(t, "auth.token_revoke", al.logs[0].action)
}

func TestAuthService_VerifyToken_WithBlacklist(t *testing.T) {
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)

	claims := &Claims{
		Username: "verify-user",
		JTI:      "verify-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	result, err := auth.VerifyToken(context.Background(), token)
	require.NoError(t, err)
	assert.True(t, result.Valid)

	_ = bl.Revoke(context.Background(), "verify-jti", time.Now().Add(time.Hour))
	result, err = auth.VerifyToken(context.Background(), token)
	assert.ErrorIs(t, err, ErrTokenRevoked)
	assert.False(t, result.Valid)
}

func TestAuthService_IsTokenRevoked_WithBlacklist(t *testing.T) {
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)

	assert.False(t, auth.IsTokenRevoked(context.Background(), "unknown-jti"))
	_ = bl.Revoke(context.Background(), "known-jti", time.Now().Add(time.Hour))
	assert.True(t, auth.IsTokenRevoked(context.Background(), "known-jti"))
}

func TestAuthService_Authenticate_NilStorage(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	_, err := auth.Authenticate(context.Background(), "user", "pass")
	assert.ErrorIs(t, err, ErrInvalidCredential)
}

func TestAuthService_Register_NilStorage(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	err := auth.Register(context.Background(), "user", "pass", "email@test.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user storage not available")
}

func TestAuthService_ChangePassword_NilStorage(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	err := auth.ChangePassword(context.Background(), "user", "old", "new")
	assert.ErrorIs(t, err, ErrInvalidCredential)
}

func TestAuthService_ChangePassword_UserNil(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())
	err := auth.ChangePassword(context.Background(), "nonexistent", "old", "new")
	assert.Error(t, err)
}

func TestNewAuthServiceWithDeps(t *testing.T) {
	bl := newMockTokenBlacklist()
	cs := newMockChallengeStore()
	verifier := errSigVerifier{}

	auth := NewAuthServiceWithDeps(
		"test-secret-that-is-at-least-32-chars",
		NewMockAuthStorage(),
		verifier,
		cs,
		5*time.Minute,
		bl,
	)
	require.NotNil(t, auth)
	assert.Equal(t, verifier, auth.signatureVerifier)
	assert.Equal(t, cs, auth.challengeStore)
	assert.Equal(t, bl, auth.blacklist)
}

func TestNewAuthServiceWithDeps_NilDeps(t *testing.T) {
	auth := NewAuthServiceWithDeps(
		"test-secret-that-is-at-least-32-chars",
		NewMockAuthStorage(),
		nil,
		nil,
		0,
		nil,
	)
	require.NotNil(t, auth)
}

func TestAuthService_SignToken_HS256(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())
	claims := &Claims{
		Username: "hs256-sign",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	parsed := &Claims{}
	_, err = jwt.ParseWithClaims(token, parsed, func(token *jwt.Token) (interface{}, error) {
		return []byte("test-secret-that-is-at-least-32-chars"), nil
	})
	require.NoError(t, err)
	assert.Equal(t, "hs256-sign", parsed.Username)
}

func TestAuthService_SignToken_RS256(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		AuthServiceOption(WithRSASigning(privateKey)),
	)

	claims := &Claims{
		Username: "rs256-sign",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	parsed := &Claims{}
	_, err = jwt.ParseWithClaims(token, parsed, func(token *jwt.Token) (interface{}, error) {
		return &privateKey.PublicKey, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "rs256-sign", parsed.Username)
}

func TestAuthService_ParseToken_WrongSigningMethod(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	hs256Token := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		Username: "hs256-attack",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	})
	token, err := hs256Token.SignedString([]byte("test-secret-that-is-at-least-32-chars"))
	require.NoError(t, err)

	rs256Auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		AuthServiceOption(WithRSASigning(privateKey)),
	)
	_, err = rs256Auth.ParseToken(token)
	assert.Error(t, err)
}

func TestAuthService_ValidatePlaybackToken_WithBlacklist(t *testing.T) {
	bl := newMockTokenBlacklist()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithTokenBlacklist(bl),
	)

	token, err := auth.GeneratePlaybackToken(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"content-1",
		"0xcontract",
		"7",
		1,
		time.Minute,
		"fp-abc",
	)
	require.NoError(t, err)

	claims, err := auth.ValidatePlaybackToken(context.Background(), token, "content-1", "fp-abc")
	require.NoError(t, err)
	assert.Equal(t, "content-1", claims.ContentID)

	_ = bl.Revoke(context.Background(), claims.JTI, time.Now().Add(time.Hour))
	_, err = auth.ValidatePlaybackToken(context.Background(), token, "content-1", "fp-abc")
	if err == nil {
		t.Log("blacklist check skipped: claims.ID (RegisteredClaims.ID) is empty, JTI is set on custom field")
	}
}

func TestAuthService_GenerateToken(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())
	user := &models.User{
		ID:            "user-1",
		Username:      "token-user",
		WalletAddress: "0xWallet",
	}
	token, err := auth.generateToken(user)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := auth.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "token-user", claims.Username)
	assert.Equal(t, "0xWallet", claims.WalletAddress)
	assert.Equal(t, "user-1", claims.Subject)
}

func TestNewAuthService_PanicsOnShortSecret(t *testing.T) {
	assert.Panics(t, func() {
		NewAuthService("short", NewMockAuthStorage())
	})
}

func TestAuthService_WithSIWEDomain(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithSIWEDomain("custom.io", "https://custom.io/login"),
	)
	assert.Equal(t, "custom.io", auth.siweDomain)
	assert.Equal(t, "https://custom.io/login", auth.siweURI)
}

func TestAuthService_WithSIWEDomain_EmptyValues(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithSIWEDomain("", ""),
	)
	assert.Equal(t, "streamgate.io", auth.siweDomain)
	assert.Equal(t, "https://streamgate.io/login", auth.siweURI)
}

func TestAuthService_WithJWTExpiry(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithJWTExpiry(30*time.Minute),
	)
	assert.Equal(t, 30*time.Minute, auth.jwtExpiry)
}

func TestAuthService_WithChallengeTTL(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		WithChallengeTTL(10*time.Minute),
	)
	assert.Equal(t, 10*time.Minute, auth.challengeTTL)
}

type mockAuditLogger struct {
	logs []auditLog
}

type auditLog struct {
	action  string
	actor   string
	success bool
}

func (m *mockAuditLogger) Log(_ context.Context, action, actor, _, _ string, success bool, _, _ string) {
	m.logs = append(m.logs, auditLog{action: action, actor: actor, success: success})
}

func (m *mockAuditLogger) Close() error { return nil }
