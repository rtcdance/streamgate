package service

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"streamgate/pkg/models"
)

// JWTVerifier validates JWT tokens using either an HMAC secret or RSA public key.
// Use this in streaming/gateway services that only need to verify, not issue, tokens.
type JWTVerifier struct {
	hmacSecret  []byte
	publicKey   *rsa.PublicKey
	signingType JWTSigningType
}

// JWTSigningType specifies the algorithm used for JWT signing.
type JWTSigningType int

const (
	JWTHS256 JWTSigningType = iota
	JWTRS256
)

// NewJWTVerifier creates a verifier-only JWT client for services that do not issue tokens.
func NewJWTVerifier(secret string, opts ...JWTVerifierOption) *JWTVerifier {
	v := &JWTVerifier{hmacSecret: []byte(secret)}
	for _, o := range opts {
		o(v)
	}
	return v
}

// JWTVerifierOption configures a JWTVerifier.
type JWTVerifierOption func(*JWTVerifier)

// WithRSAPublicKey sets the RSA public key for RS256 verification.
func WithRSAPublicKey(key *rsa.PublicKey) JWTVerifierOption {
	return func(v *JWTVerifier) {
		v.publicKey = key
		v.signingType = JWTRS256
	}
}

// ParseToken parses and validates a JWT without issuing capability.
func (v *JWTVerifier) ParseToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return v.keyFunc(token)
	})
	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}
	if !parsed.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (v *JWTVerifier) keyFunc(token *jwt.Token) (interface{}, error) {
	switch v.signingType {
	case JWTRS256:
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.publicKey, nil
	default:
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.hmacSecret, nil
	}
}

// AuthService handles authentication
type AuthService struct {
	jwtSecret         []byte
	privateKey        *rsa.PrivateKey
	publicKey         *rsa.PublicKey
	signingType       JWTSigningType
	storage           AuthStorage
	signatureVerifier WalletSignatureVerifier
	challengeStore    ChallengeStore
	challengeTTL      time.Duration
	blacklist         TokenBlacklist
}

// AuthServiceOption configures an AuthService with optional dependencies.
type AuthServiceOption func(*AuthService)

// WithSignatureVerifier sets the wallet signature verifier.
func WithSignatureVerifier(v WalletSignatureVerifier) AuthServiceOption {
	return func(s *AuthService) { s.signatureVerifier = v }
}

// WithChallengeStore sets the challenge store for wallet authentication.
func WithChallengeStore(cs ChallengeStore) AuthServiceOption {
	return func(s *AuthService) { s.challengeStore = cs }
}

// WithChallengeTTL sets the challenge time-to-live.
func WithChallengeTTL(d time.Duration) AuthServiceOption {
	return func(s *AuthService) { s.challengeTTL = d }
}

// WithTokenBlacklist sets the token blacklist.
func WithTokenBlacklist(b TokenBlacklist) AuthServiceOption {
	return func(s *AuthService) { s.blacklist = b }
}

// AuthStorage defines the interface for user storage
type AuthStorage interface {
	GetUser(ctx context.Context, username string) (*models.User, error)
	CreateUser(ctx context.Context, user *models.User) error
	UpdateUser(ctx context.Context, user *models.User) error
}

// Claims represents JWT claims
type Claims struct {
	Username      string `json:"username"`
	WalletAddress string `json:"wallet_address,omitempty"`
	ContentID     string `json:"content_id,omitempty"`
	Contract      string `json:"contract,omitempty"`
	TokenID       string `json:"token_id,omitempty"`
	ChainID       int64  `json:"chain_id,omitempty"`
	JTI           string `json:"jti,omitempty"`
	jwt.RegisteredClaims
}

// AuthServiceOption applies RSA signing configuration.
type AuthServiceSigningOption func(*AuthService)

// WithRSASigning configures the AuthService to use RS256 instead of HS256.
// The private key is used for signing; the public key is embedded for verification.
func WithRSASigning(privateKey *rsa.PrivateKey) AuthServiceSigningOption {
	return func(s *AuthService) {
		s.privateKey = privateKey
		s.publicKey = &privateKey.PublicKey
		s.signingType = JWTRS256
	}
}

func NewAuthService(jwtSecret string, storage AuthStorage, opts ...AuthServiceOption) *AuthService {
	s := &AuthService{
		jwtSecret:         []byte(jwtSecret),
		storage:           storage,
		signatureVerifier: defaultWalletSignatureVerifier(),
		challengeStore:    NewMemoryChallengeStore(),
		challengeTTL:      defaultChallengeTTL,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewAuthServiceWithDeps creates a new auth service with explicit wallet auth dependencies.
//
// Deprecated: use NewAuthService with functional options instead:
//
//	NewAuthService(jwtSecret, storage,
//	    WithSignatureVerifier(verifier),
//	    WithChallengeStore(store),
//	    WithChallengeTTL(ttl),
//	    WithTokenBlacklist(blacklist),
//	)
func NewAuthServiceWithDeps(jwtSecret string, storage AuthStorage, verifier WalletSignatureVerifier, challengeStore ChallengeStore, challengeTTL time.Duration, blacklist TokenBlacklist) *AuthService {
	var opts []AuthServiceOption
	if verifier != nil {
		opts = append(opts, WithSignatureVerifier(verifier))
	}
	if challengeStore != nil {
		opts = append(opts, WithChallengeStore(challengeStore))
	}
	if challengeTTL > 0 {
		opts = append(opts, WithChallengeTTL(challengeTTL))
	}
	if blacklist != nil {
		opts = append(opts, WithTokenBlacklist(blacklist))
	}
	return NewAuthService(jwtSecret, storage, opts...)
}

// Authenticate authenticates user with username and password
func (s *AuthService) Authenticate(ctx context.Context, username, password string) (string, error) {
	// 1. Get user from storage
	user, err := s.storage.GetUser(ctx, username)
	if err != nil || user == nil {
		return "", ErrInvalidCredential
	}

	// 2. Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", ErrInvalidCredential
	}

	// 3. Generate JWT token
	token, err := s.generateToken(user)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	return token, nil
}

// Verify verifies token and returns claims
func (s *AuthService) Verify(tokenString string) (bool, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return false, err
	}

	// Check if token is expired
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return false, ErrTokenExpired
	}

	return true, nil
}

func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		switch s.signingType {
		case JWTRS256:
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return s.publicKey, nil
		default:
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return s.jwtSecret, nil
		}
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

func (s *AuthService) signToken(claims *Claims) (string, error) {
	switch s.signingType {
	case JWTRS256:
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		return token.SignedString(s.privateKey)
	default:
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		return token.SignedString(s.jwtSecret)
	}
}

// Register registers a new user
func (s *AuthService) Register(ctx context.Context, username, password, email string) error {
	// 1. Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 2. Create user
	user := &models.User{
		ID:        generateID(),
		Username:  username,
		Password:  string(hashedPassword),
		Email:     email,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 3. Save to storage
	if err := s.storage.CreateUser(ctx, user); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// ChangePassword changes user password
func (s *AuthService) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	// 1. Get user
	user, err := s.storage.GetUser(ctx, username)
	if err != nil || user == nil {
		return errors.New("user not found")
	}

	// 2. Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return errors.New("invalid old password")
	}

	// 3. Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 4. Update user
	user.Password = string(hashedPassword)
	user.UpdatedAt = time.Now()

	if err := s.storage.UpdateUser(ctx, user); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// RefreshToken refreshes an existing token, blacklisting the old one.
func (s *AuthService) RefreshToken(ctx context.Context, tokenString string) (string, error) {
	// 1. Parse existing token
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return "", err
	}

	// 2. Reject tokens without JTI (invalid format)
	if claims.JTI == "" {
		return "", fmt.Errorf("token missing jti claim")
	}

	// 3. Blacklist the old token with its remaining TTL
	if s.blacklist != nil && claims.ExpiresAt != nil {
		remainingTTL := time.Until(claims.ExpiresAt.Time)
		if remainingTTL > 0 {
			if err := s.blacklist.Revoke(ctx, claims.JTI, claims.ExpiresAt.Time); err != nil {
				return "", fmt.Errorf("failed to revoke old token: %w", err)
			}
		}
	}

	// 4. Create new token with extended expiration
	newClaims := &Claims{
		Username:      claims.Username,
		WalletAddress: claims.WalletAddress,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   claims.Subject,
		},
	}

	newTokenString, err := s.signToken(newClaims)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}
	return newTokenString, nil
}

// generateToken generates a JWT token for a user
func (s *AuthService) generateToken(user *models.User) (string, error) {
	claims := &Claims{
		Username:      user.Username,
		WalletAddress: user.WalletAddress,
		JTI:           generateID(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   user.ID,
		},
	}

	return s.signToken(claims)
}

func generateID() string {
	return uuid.New().String()
}
