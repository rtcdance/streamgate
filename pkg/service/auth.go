package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
)

// AuthService handles authentication
type AuthService struct {
	jwtSecret []byte
	storage   AuthStorage
}

// AuthStorage defines the interface for user storage
type AuthStorage interface {
	GetUser(username string) (*User, error)
	CreateUser(user *User) error
	UpdateUser(user *User) error
}

// User represents a user
type User struct {
	ID            string
	Username      string
	Password      string // hashed
	Email         string
	WalletAddress string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Claims represents JWT claims
type Claims struct {
	Username      string `json:"username"`
	WalletAddress string `json:"wallet_address,omitempty"`
	jwt.RegisteredClaims
}

// NewAuthService creates a new auth service
func NewAuthService(jwtSecret string, storage AuthStorage) *AuthService {
	return &AuthService{
		jwtSecret: []byte(jwtSecret),
		storage:   storage,
	}
}

// Authenticate authenticates user with username and password
func (s *AuthService) Authenticate(username, password string) (string, error) {
	// 1. Get user from storage
	user, err := s.storage.GetUser(username)
	if err != nil {
		return "", errors.New("invalid username or password")
	}

	// 2. Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", errors.New("invalid username or password")
	}

	// 3. Generate JWT token
	token, err := s.generateToken(user)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	return token, nil
}

// AuthenticateWithWallet authenticates user with wallet signature
func (s *AuthService) AuthenticateWithWallet(walletAddress, signature, message string) (string, error) {
	// TODO: Verify wallet signature
	// This would involve:
	// 1. Recover address from signature
	// 2. Compare with provided wallet address
	// 3. Verify message timestamp to prevent replay attacks

	// For now, create a simple token
	claims := &Claims{
		Username:      walletAddress,
		WalletAddress: walletAddress,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// Verify verifies token and returns claims
func (s *AuthService) Verify(tokenString string) (bool, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return false, err
	}

	// Check if token is expired
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return false, errors.New("token expired")
	}

	return true, nil
}

// ParseToken parses and validates a JWT token
func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

// Register registers a new user
func (s *AuthService) Register(username, password, email string) error {
	// 1. Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 2. Create user
	user := &User{
		ID:        generateID(),
		Username:  username,
		Password:  string(hashedPassword),
		Email:     email,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 3. Save to storage
	if err := s.storage.CreateUser(user); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// ChangePassword changes user password
func (s *AuthService) ChangePassword(username, oldPassword, newPassword string) error {
	// 1. Get user
	user, err := s.storage.GetUser(username)
	if err != nil {
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

	if err := s.storage.UpdateUser(user); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// RefreshToken refreshes an existing token
func (s *AuthService) RefreshToken(tokenString string) (string, error) {
	// 1. Parse existing token
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return "", err
	}

	// 2. Create new token with extended expiration
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(24 * time.Hour))
	claims.IssuedAt = jwt.NewNumericDate(time.Now())

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	newTokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return newTokenString, nil
}

// generateToken generates a JWT token for a user
func (s *AuthService) generateToken(user *User) (string, error) {
	claims := &Claims{
		Username:      user.Username,
		WalletAddress: user.WalletAddress,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   user.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// generateID generates a unique ID
func generateID() string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(hash[:])[:16]
}
