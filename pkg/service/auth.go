package service

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"streamgate/pkg/models"
	stg "streamgate/pkg/storage"
	"streamgate/pkg/web3"
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
	validMethods := []string{"HS256"}
	if v.signingType == JWTRS256 {
		validMethods = []string{"RS256"}
	}
	parsed, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return v.keyFunc(token)
	}, jwt.WithValidMethods(validMethods))
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
	signatureVerifier web3.SignatureVerifierInterface
	challengeStore    stg.ChallengeStore
	challengeTTL      time.Duration
	blacklist         stg.TokenBlacklist
	jwtExpiry         time.Duration
	eip712Verifier    web3.EIP712VerifierInterface
	siweDomain        string
	siweURI           string
}

// AuthServiceOption configures an AuthService with optional dependencies.
type AuthServiceOption func(*AuthService)

// WithSignatureVerifier sets the wallet signature verifier.
func WithSignatureVerifier(v web3.SignatureVerifierInterface) AuthServiceOption {
	return func(s *AuthService) { s.signatureVerifier = v }
}

// WithEIP712Verifier sets the EIP-712 typed data verifier.
func WithEIP712Verifier(v web3.EIP712VerifierInterface) AuthServiceOption {
	return func(s *AuthService) { s.eip712Verifier = v }
}

// WithChallengeStore sets the challenge store for wallet authentication.
func WithChallengeStore(cs stg.ChallengeStore) AuthServiceOption {
	return func(s *AuthService) { s.challengeStore = cs }
}

// WithChallengeTTL sets the challenge time-to-live.
func WithChallengeTTL(d time.Duration) AuthServiceOption {
	return func(s *AuthService) { s.challengeTTL = d }
}

// WithTokenBlacklist sets the token blacklist.
func WithTokenBlacklist(b stg.TokenBlacklist) AuthServiceOption {
	return func(s *AuthService) { s.blacklist = b }
}

// WithJWTExpiry sets the JWT token expiry duration.
func WithJWTExpiry(d time.Duration) AuthServiceOption {
	return func(s *AuthService) { s.jwtExpiry = d }
}

// WithSIWEDomain sets the SIWE (EIP-4361) domain and URI for challenge generation.
// Defaults to "streamgate.io" / "https://streamgate.io/login" if not set.
func WithSIWEDomain(domain, uri string) AuthServiceOption {
	return func(s *AuthService) {
		if domain != "" {
			s.siweDomain = domain
		}
		if uri != "" {
			s.siweURI = uri
		}
	}
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

// errSigVerifier returns ErrNotSupported for all verifications.
// It serves as a safe default when no real signature verifier is injected.
type errSigVerifier struct{}

func (errSigVerifier) VerifySignature(_ context.Context, _, _, _ string) (bool, error) {
	return false, ErrNotSupported
}

func NewAuthService(jwtSecret string, storage AuthStorage, opts ...AuthServiceOption) *AuthService {
	if len(jwtSecret) < 32 {
		panic("jwtSecret must be at least 32 characters for HS256 security")
	}
	s := &AuthService{
		jwtSecret:         []byte(jwtSecret),
		storage:           storage,
		signatureVerifier: errSigVerifier{},
		challengeStore:    stg.NewMemoryChallengeStore(),
		challengeTTL:      defaultChallengeTTL,
		jwtExpiry:         2 * time.Hour,
		siweDomain:        "streamgate.io",
		siweURI:           "https://streamgate.io/login",
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
func NewAuthServiceWithDeps(jwtSecret string, as AuthStorage, verifier web3.SignatureVerifierInterface, challengeStore stg.ChallengeStore, challengeTTL time.Duration, blacklist stg.TokenBlacklist) *AuthService {
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
	return NewAuthService(jwtSecret, as, opts...)
}

func (s *AuthService) Close() {
	if s.challengeStore != nil {
		if closer, ok := s.challengeStore.(io.Closer); ok {
			_ = closer.Close()
		}
	}
	if s.blacklist != nil {
		_ = s.blacklist.Close()
	}
}

// Authenticate authenticates user with username and password
func (s *AuthService) Authenticate(ctx context.Context, username, password string) (string, error) {
	if s.storage == nil {
		return "", ErrInvalidCredential
	}
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
	validMethods := []string{"HS256"}
	if s.signingType == JWTRS256 {
		validMethods = []string{"RS256"}
	}

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
	}, jwt.WithValidMethods(validMethods))

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
	if s.storage == nil {
		return fmt.Errorf("user storage not available")
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 12)
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
	if s.storage == nil {
		return ErrInvalidCredential
	}
	user, err := s.storage.GetUser(ctx, username)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if user == nil {
		return ErrNotFound
	}

	// Verify old password using bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return ErrInvalidCredential
	}

	// 3. Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
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

const refreshGracePeriod = 5 * time.Minute

// RefreshToken refreshes an existing token, blacklisting the old one.
// Allows refresh within a grace period after token expiry to improve UX.
// Checks JTI blacklist before refreshing to prevent concurrent refresh attacks.
func (s *AuthService) RefreshToken(ctx context.Context, tokenString string) (string, error) {
	claims, err := s.parseTokenAllowExpired(tokenString, refreshGracePeriod)
	if err != nil {
		return "", err
	}

	if claims.JTI == "" {
		return "", fmt.Errorf("token missing jti claim")
	}

	if s.blacklist != nil && s.blacklist.IsRevoked(ctx, claims.JTI) {
		return "", ErrTokenRevoked
	}

	if s.blacklist != nil && claims.ExpiresAt != nil {
		remainingTTL := time.Until(claims.ExpiresAt.Time)
		if remainingTTL > 0 {
			if err := s.blacklist.Revoke(ctx, claims.JTI, claims.ExpiresAt.Time); err != nil {
				return "", fmt.Errorf("failed to revoke old token: %w", err)
			}
		} else {
			expiresAt := claims.ExpiresAt.Time
			if err := s.blacklist.Revoke(ctx, claims.JTI, expiresAt); err != nil {
				return "", fmt.Errorf("failed to revoke old token: %w", err)
			}
		}
	}

	newClaims := &Claims{
		Username:      claims.Username,
		WalletAddress: claims.WalletAddress,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpiry)),
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

// parseTokenAllowExpired parses a JWT token, allowing tokens that expired
// within the given grace period. This enables token refresh after expiry.
func (s *AuthService) parseTokenAllowExpired(tokenString string, gracePeriod time.Duration) (*Claims, error) {
	claims := &Claims{}
	validMethods := []string{"HS256"}
	if s.signingType == JWTRS256 {
		validMethods = []string{"RS256"}
	}
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
	}, jwt.WithValidMethods(validMethods))
	if err != nil {
		if !token.Valid {
			if claims.ExpiresAt != nil {
				expiredDuration := time.Since(claims.ExpiresAt.Time)
				if expiredDuration > 0 && expiredDuration <= gracePeriod {
					return claims, nil
				}
			}
			return nil, fmt.Errorf("failed to parse token: %w", err)
		}
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	return claims, nil
}

// generateToken generates a JWT token for a user
func (s *AuthService) generateToken(user *models.User) (string, error) {
	claims := &Claims{
		Username:      user.Username,
		WalletAddress: user.WalletAddress,
		JTI:           generateID(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpiry)),
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
