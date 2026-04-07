package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
	"streamgate/pkg/web3"
)

const defaultChallengeTTL = 5 * time.Minute

// WalletSignatureVerifier verifies wallet signatures.
type WalletSignatureVerifier interface {
	VerifySignature(address, message, signature string) (bool, error)
}

// WalletChallenge represents a one-time wallet login challenge.
type WalletChallenge struct {
	ID            string    `json:"id"`
	WalletAddress string    `json:"wallet_address"`
	ChainID       int64     `json:"chain_id"`
	Nonce         string    `json:"nonce"`
	Message       string    `json:"message"`
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	UsedAt        time.Time `json:"used_at,omitempty"`
}

// ChallengeStore stores wallet login challenges.
type ChallengeStore interface {
	SaveChallenge(challenge *WalletChallenge) error
	GetChallenge(id string) (*WalletChallenge, error)
	MarkChallengeUsed(id string, usedAt time.Time) error
}

// RedisChallengeStore stores challenges in Redis.
type RedisChallengeStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisChallengeStore creates a Redis-backed challenge store.
func NewRedisChallengeStore(addr string, ttl time.Duration) (*RedisChallengeStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     "",
		DB:           0,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisChallengeStore{
		client: client,
		ttl:    ttl,
	}, nil
}

// MemoryChallengeStore stores challenges in-memory for local development and tests.
type MemoryChallengeStore struct {
	mu         sync.RWMutex
	challenges map[string]*WalletChallenge
}

// NewMemoryChallengeStore creates a new in-memory challenge store.
func NewMemoryChallengeStore() *MemoryChallengeStore {
	return &MemoryChallengeStore{
		challenges: make(map[string]*WalletChallenge),
	}
}

// SaveChallenge stores a challenge.
func (m *MemoryChallengeStore) SaveChallenge(challenge *WalletChallenge) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	challengeCopy := *challenge
	m.challenges[challenge.ID] = &challengeCopy
	return nil
}

// GetChallenge retrieves a challenge by ID.
func (m *MemoryChallengeStore) GetChallenge(id string) (*WalletChallenge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	challenge, ok := m.challenges[id]
	if !ok {
		return nil, errors.New("challenge not found")
	}

	challengeCopy := *challenge
	return &challengeCopy, nil
}

// MarkChallengeUsed marks a challenge as consumed.
func (m *MemoryChallengeStore) MarkChallengeUsed(id string, usedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	challenge, ok := m.challenges[id]
	if !ok {
		return errors.New("challenge not found")
	}

	challenge.UsedAt = usedAt
	return nil
}

// SaveChallenge stores a challenge in Redis.
func (r *RedisChallengeStore) SaveChallenge(challenge *WalletChallenge) error {
	data, err := json.Marshal(challenge)
	if err != nil {
		return fmt.Errorf("failed to marshal challenge: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return r.client.Set(ctx, challenge.ID, string(data), r.ttl).Err()
}

// GetChallenge retrieves a challenge from Redis.
func (r *RedisChallengeStore) GetChallenge(id string) (*WalletChallenge, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := r.client.Get(ctx, id).Result()
	if err == redis.Nil {
		return nil, errors.New("challenge not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load challenge: %w", err)
	}

	var challenge WalletChallenge
	if err := json.Unmarshal([]byte(raw), &challenge); err != nil {
		return nil, fmt.Errorf("failed to unmarshal challenge: %w", err)
	}

	return &challenge, nil
}

// MarkChallengeUsed marks a challenge used in Redis.
func (r *RedisChallengeStore) MarkChallengeUsed(id string, usedAt time.Time) error {
	challenge, err := r.GetChallenge(id)
	if err != nil {
		return err
	}

	challenge.UsedAt = usedAt
	data, err := json.Marshal(challenge)
	if err != nil {
		return fmt.Errorf("failed to marshal challenge: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return r.client.Set(ctx, challenge.ID, string(data), r.ttl).Err()
}

func defaultWalletSignatureVerifier() WalletSignatureVerifier {
	return web3.NewSignatureVerifier(zap.NewNop())
}

func parseChallengeTTL(raw string) time.Duration {
	if raw == "" {
		return defaultChallengeTTL
	}

	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl <= 0 {
		return defaultChallengeTTL
	}

	return ttl
}

// GenerateWalletChallenge creates and stores a one-time wallet login challenge.
func (s *AuthService) GenerateWalletChallenge(walletAddress string, chainID int64) (*WalletChallenge, error) {
	if !common.IsHexAddress(walletAddress) {
		return nil, fmt.Errorf("invalid wallet address: %s", walletAddress)
	}

	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.challengeTTL)
	challenge := &WalletChallenge{
		ID:            generateID(),
		WalletAddress: common.HexToAddress(walletAddress).Hex(),
		ChainID:       chainID,
		Nonce:         nonce,
		IssuedAt:      now,
		ExpiresAt:     expiresAt,
	}
	challenge.Message = fmt.Sprintf(
		"Sign this message to authenticate with StreamGate.\nAddress: %s\nChain ID: %d\nNonce: %s\nIssued At: %s\nExpires At: %s",
		challenge.WalletAddress,
		challenge.ChainID,
		challenge.Nonce,
		challenge.IssuedAt.Format(time.RFC3339),
		challenge.ExpiresAt.Format(time.RFC3339),
	)

	if err := s.challengeStore.SaveChallenge(challenge); err != nil {
		return nil, fmt.Errorf("failed to store challenge: %w", err)
	}

	return challenge, nil
}

// AuthenticateWithWallet verifies a challenge-based wallet login and issues a JWT.
func (s *AuthService) AuthenticateWithWallet(walletAddress, challengeID, signature string) (string, error) {
	if !common.IsHexAddress(walletAddress) {
		return "", fmt.Errorf("invalid wallet address: %s", walletAddress)
	}
	if challengeID == "" {
		return "", errors.New("challenge id is required")
	}
	if signature == "" {
		return "", errors.New("signature is required")
	}

	challenge, err := s.challengeStore.GetChallenge(challengeID)
	if err != nil {
		return "", err
	}

	normalizedAddress := common.HexToAddress(walletAddress).Hex()
	if challenge.WalletAddress != normalizedAddress {
		return "", errors.New("challenge wallet mismatch")
	}
	if !challenge.UsedAt.IsZero() {
		return "", errors.New("challenge already used")
	}
	if time.Now().UTC().After(challenge.ExpiresAt) {
		return "", errors.New("challenge expired")
	}

	valid, err := s.signatureVerifier.VerifySignature(normalizedAddress, challenge.Message, signature)
	if err != nil {
		return "", fmt.Errorf("failed to verify wallet signature: %w", err)
	}
	if !valid {
		return "", errors.New("invalid wallet signature")
	}

	if err := s.challengeStore.MarkChallengeUsed(challengeID, time.Now().UTC()); err != nil {
		return "", fmt.Errorf("failed to consume challenge: %w", err)
	}

	return s.generateWalletToken(normalizedAddress)
}

func (s *AuthService) generateWalletToken(walletAddress string) (string, error) {
	claims := &Claims{
		Username:      walletAddress,
		WalletAddress: walletAddress,
		JTI:           generateID(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   walletAddress,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// GeneratePlaybackToken creates a short-lived token for segment access after manifest authorization.
func (s *AuthService) GeneratePlaybackToken(walletAddress, contentID, contract, tokenID string, chainID int64, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}

	claims := &Claims{
		Username:      walletAddress,
		WalletAddress: walletAddress,
		ContentID:     contentID,
		Contract:      contract,
		TokenID:       tokenID,
		ChainID:       chainID,
		JTI:           generateID(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   contentID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign playback token: %w", err)
	}

	return tokenString, nil
}

// ValidatePlaybackToken validates a playback token and ensures it matches the requested content.
func (s *AuthService) ValidatePlaybackToken(tokenString, contentID string) (*Claims, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.Subject != contentID {
		return nil, errors.New("playback token content mismatch")
	}
	return claims, nil
}

func generateNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}
