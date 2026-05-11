package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v4"
	"github.com/gagliardetto/solana-go"
	"go.uber.org/zap"
	"streamgate/pkg/web3"
)

const defaultChallengeTTL = 5 * time.Minute

// IsValidSolanaAddress checks if the address is a valid Solana base58 public key.
// Uses the solana SDK for proper base58 validation.
func IsValidSolanaAddress(address string) bool {
	if len(address) < 32 || len(address) > 44 {
		return false
	}
	if strings.HasPrefix(address, "0x") {
		return false
	}
	_, err := solana.PublicKeyFromBase58(address)
	return err == nil
}

// isSolanaChain returns true for Solana chain IDs (negative values).
func isSolanaChain(chainID int64) bool {
	return chainID < 0
}

// TokenBlacklist stores revoked JWT IDs.
//go:generate mockgen -destination=mocks/mock_token_blacklist.go -package=mocks streamgate/pkg/service TokenBlacklist
type TokenBlacklist interface {
	Revoke(ctx context.Context, jti string, expiresAt time.Time) error
	IsRevoked(ctx context.Context, jti string) bool
	Close() error
}

// MemoryTokenBlacklist is an in-memory token blacklist with lazy expiry eviction.
type MemoryTokenBlacklist struct {
	mu      sync.RWMutex
	entries map[string]time.Time // jti → expiresAt
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewMemoryTokenBlacklist creates a new in-memory token blacklist with periodic cleanup.
func NewMemoryTokenBlacklist() *MemoryTokenBlacklist {
	b := &MemoryTokenBlacklist{
		entries: make(map[string]time.Time),
		stopCh:  make(chan struct{}),
	}
	b.wg.Add(1)
	go b.cleanupLoop()
	return b
}

// Close stops the background cleanup goroutine.
func (b *MemoryTokenBlacklist) Close() error {
	close(b.stopCh)
	b.wg.Wait()
	return nil
}

// cleanupLoop periodically removes expired entries.
func (b *MemoryTokenBlacklist) cleanupLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.evictExpired()
		}
	}
}

// evictExpired removes all expired entries.
func (b *MemoryTokenBlacklist) evictExpired() {
	now := time.Now()
	b.mu.Lock()
	for jti, expiresAt := range b.entries {
		if now.After(expiresAt) {
			delete(b.entries, jti)
		}
	}
	b.mu.Unlock()
}

// Revoke adds a JTI to the blacklist.
func (b *MemoryTokenBlacklist) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[jti] = expiresAt
	return nil
}

// IsRevoked checks if a JTI is blacklisted. Lazily evicts expired entries.
func (b *MemoryTokenBlacklist) IsRevoked(ctx context.Context, jti string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if expiresAt, ok := b.entries[jti]; ok {
		if time.Now().After(expiresAt) {
			delete(b.entries, jti)
			return false
		}
		return true
	}
	return false
}

// TokenVerifyResult contains the result of a token verification.
type TokenVerifyResult struct {
	Valid        bool
	ExpiresAt    string
	WalletAddress string
}

// WalletSignatureVerifier verifies wallet signatures.
// Implementations must handle chain-specific verification:
// EVM chains use secp256k1/EIP-191, Solana uses ed25519.
//go:generate mockgen -destination=mocks/mock_wallet_sig_verifier.go -package=mocks streamgate/pkg/service WalletSignatureVerifier
type WalletSignatureVerifier interface {
	VerifySignature(ctx context.Context, address, message, signature string) (bool, error)
}

// ChainAwareSignatureVerifier extends WalletSignatureVerifier with chain routing.
// If available, the auth service will use this interface to route signatures
// to the correct verification algorithm based on chain ID.
type ChainAwareSignatureVerifier interface {
	WalletSignatureVerifier
	VerifySolanaSignature(address, message, signature string) (bool, error)
}

// WalletChallenge represents a one-time wallet login challenge.
type WalletChallenge struct {
	ID            string    `json:"id"`
	WalletAddress string    `json:"wallet_address"`
	ChainID       int64     `json:"chain_id"`
	SigningType   string    `json:"signing_type"` // "personal_sign" or "eip712"
	Nonce         string    `json:"nonce"`
	Message       string    `json:"message"`
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	UsedAt        time.Time `json:"used_at,omitempty"`
}

// ChallengeStore stores wallet login challenges.
//go:generate mockgen -destination=mocks/mock_challenge_store.go -package=mocks streamgate/pkg/service ChallengeStore
type ChallengeStore interface {
	SaveChallenge(ctx context.Context, challenge *WalletChallenge) error
	GetChallenge(ctx context.Context, id string) (*WalletChallenge, error)
	MarkChallengeUsed(ctx context.Context, id string, usedAt time.Time) error
}

// RedisChallengeStore stores challenges in Redis.
type RedisChallengeStore struct {
	client *redis.Client
	ttl    time.Duration
}

// redisChallengeStoreConfig holds Redis connection options.
type redisChallengeStoreConfig struct {
	password     string
	db           int
	poolSize     int
	dialTimeout  time.Duration
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// RedisChallengeStoreOption configures a RedisChallengeStore.
type RedisChallengeStoreOption func(*redisChallengeStoreConfig)

// WithRedisPassword sets the Redis password.
func WithRedisPassword(password string) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.password = password }
}

// WithRedisDB sets the Redis database index.
func WithRedisDB(db int) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.db = db }
}

// WithRedisPoolSize sets the connection pool size.
func WithRedisPoolSize(size int) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.poolSize = size }
}

// WithRedisDialTimeout sets the dial timeout.
func WithRedisDialTimeout(d time.Duration) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.dialTimeout = d }
}

// WithRedisReadTimeout sets the read timeout.
func WithRedisReadTimeout(d time.Duration) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.readTimeout = d }
}

// WithRedisWriteTimeout sets the write timeout.
func WithRedisWriteTimeout(d time.Duration) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.writeTimeout = d }
}

// NewRedisChallengeStore creates a Redis-backed challenge store.
func NewRedisChallengeStore(addr string, ttl time.Duration, opts ...RedisChallengeStoreOption) (*RedisChallengeStore, error) {
	cfg := redisChallengeStoreConfig{
		poolSize:     100,
		dialTimeout:  5 * time.Second,
		readTimeout:  3 * time.Second,
		writeTimeout: 3 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.password,
		DB:           cfg.db,
		PoolSize:     cfg.poolSize,
		DialTimeout:  cfg.dialTimeout,
		ReadTimeout:  cfg.readTimeout,
		WriteTimeout: cfg.writeTimeout,
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
func (m *MemoryChallengeStore) SaveChallenge(ctx context.Context, challenge *WalletChallenge) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	challengeCopy := *challenge
	m.challenges[challenge.ID] = &challengeCopy
	return nil
}

// GetChallenge retrieves a challenge by ID.
func (m *MemoryChallengeStore) GetChallenge(ctx context.Context, id string) (*WalletChallenge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	challenge, ok := m.challenges[id]
	if !ok {
		return nil, errors.New("challenge not found")
	}

	challengeCopy := *challenge
	return &challengeCopy, nil
}

// MarkChallengeUsed atomically checks that the challenge has not been used and
// marks it as consumed in a single write-lock scope to prevent TOCTOU replay.
// Returns an error if the challenge is not found or has already been used.
func (m *MemoryChallengeStore) MarkChallengeUsed(ctx context.Context, id string, usedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	challenge, ok := m.challenges[id]
	if !ok {
		return errors.New("challenge not found")
	}

	// Fast-fail: skip expensive signature verification for already-used challenges.
	// The atomic MarkChallengeUsed below provides the definitive TOCTOU-safe check.
	if !challenge.UsedAt.IsZero() {
		return errors.New("challenge already used")
	}

	challenge.UsedAt = usedAt
	return nil
}

// SaveChallenge stores a challenge in Redis.
func (r *RedisChallengeStore) SaveChallenge(ctx context.Context, challenge *WalletChallenge) error {
	data, err := json.Marshal(challenge)
	if err != nil {
		return fmt.Errorf("failed to marshal challenge: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return r.client.Set(ctx, challenge.ID, string(data), r.ttl).Err()
}

// GetChallenge retrieves a challenge from Redis.
func (r *RedisChallengeStore) GetChallenge(ctx context.Context, id string) (*WalletChallenge, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
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

// markChallengeUsedLua is a Redis Lua script that atomically checks if a
// challenge is unused and marks it used. Returns the previous used_at value
// (empty string if unused, non-empty if already used). This prevents the
// TOCTOU race where two concurrent requests both read a challenge as unused
// and both receive valid JWTs.
var markChallengeUsedLua = redis.NewScript(`
local data = redis.call('GET', KEYS[1])
if not data then
  return 'NOT_FOUND'
end
local decoded = cjson.decode(data)
if decoded.used_at and decoded.used_at ~= '' and decoded.used_at ~= '0001-01-01T00:00:00Z' then
  return 'ALREADY_USED'
end
decoded.used_at = ARGV[1]
local encoded = cjson.encode(decoded)
redis.call('SET', KEYS[1], encoded, 'PX', ARGV[2])
return 'OK'
`)

// MarkChallengeUsed marks a challenge used in Redis atomically via Lua script.
func (r *RedisChallengeStore) MarkChallengeUsed(ctx context.Context, id string, usedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Calculate remaining TTL in milliseconds
	ttlMs := int64(r.ttl / time.Millisecond)

	result, err := markChallengeUsedLua.Run(ctx, r.client, []string{id},
		usedAt.UTC().Format(time.RFC3339Nano), ttlMs).Result()
	if err != nil {
		return fmt.Errorf("failed to mark challenge used: %w", err)
	}

	str, ok := result.(string)
	if !ok {
		return fmt.Errorf("unexpected Lua script result type: %T", result)
	}

	switch str {
	case "OK":
		return nil
	case "ALREADY_USED":
		return errors.New("challenge already used")
	case "NOT_FOUND":
		return errors.New("challenge not found")
	default:
		return fmt.Errorf("unexpected Lua script result: %s", str)
	}
}

// Close closes the Redis connection.
func (r *RedisChallengeStore) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

func defaultWalletSignatureVerifier() WalletSignatureVerifier {
	return web3.NewSignatureVerifier(zap.NewNop())
}

// GenerateWalletChallenge creates and stores a one-time wallet login challenge.
// Supports both EVM (hex addresses) and Solana (base58 addresses) chains.
// signType controls the signing method: "personal_sign" (default) or "eip712".
// Solana chains ignore signType and always use Ed25519 off-chain verification.
func (s *AuthService) GenerateWalletChallenge(ctx context.Context, walletAddress string, chainID int64, signType ...string) (*WalletChallenge, error) {
	st := "personal_sign"
	if len(signType) > 0 && signType[0] != "" {
		st = signType[0]
	}
	var normalizedAddr string
	if isSolanaChain(chainID) {
		if !IsValidSolanaAddress(walletAddress) {
			return nil, fmt.Errorf("invalid Solana wallet address: %s", walletAddress)
		}
		normalizedAddr = walletAddress
	} else {
		if !common.IsHexAddress(walletAddress) {
			return nil, fmt.Errorf("invalid wallet address: %s", walletAddress)
		}
		normalizedAddr = common.HexToAddress(walletAddress).Hex()
	}

	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.challengeTTL)
	challenge := &WalletChallenge{
		ID:            generateID(),
		WalletAddress: normalizedAddr,
		ChainID:       chainID,
		SigningType:   st,
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

	// SIWE (EIP-4361) standard message format
	if st == "siwe" {
		siweMsg := web3.NewSIWEMessage(
			"streamgate.io",
			challenge.WalletAddress,
			"https://streamgate.io/login",
			challenge.ChainID,
			challenge.Nonce,
			challenge.IssuedAt,
			web3.WithSIWEExpirationTime(challenge.ExpiresAt),
		)
		challenge.Message = web3.BuildSIWEMessage(siweMsg)
	}

	if err := s.challengeStore.SaveChallenge(ctx, challenge); err != nil {
		return nil, fmt.Errorf("failed to store challenge: %w", err)
	}

	return challenge, nil
}

// AuthenticateWithWallet verifies a challenge-based wallet login and issues a JWT.
// Supports both EVM (secp256k1/EIP-191) and Solana (ed25519) signature verification.
func (s *AuthService) AuthenticateWithWallet(ctx context.Context, walletAddress, challengeID, signature string) (string, error) {
	if challengeID == "" {
		return "", errors.New("challenge id is required")
	}
	if signature == "" {
		return "", errors.New("signature is required")
	}

	challenge, err := s.challengeStore.GetChallenge(ctx, challengeID)
	if err != nil {
		return "", err
	}

	var normalizedAddress string
	if isSolanaChain(challenge.ChainID) {
		// Solana path: base58 address, no hex normalization
		if !IsValidSolanaAddress(walletAddress) {
			return "", fmt.Errorf("invalid Solana wallet address: %s", walletAddress)
		}
		normalizedAddress = walletAddress
	} else {
		// EVM path: hex address normalization
		if !common.IsHexAddress(walletAddress) {
			return "", fmt.Errorf("invalid wallet address: %s", walletAddress)
		}
		normalizedAddress = common.HexToAddress(walletAddress).Hex()
	}

	if challenge.WalletAddress != normalizedAddress {
		return "", ErrInvalidCredential
	}
	// Fast-fail: skip expensive signature verification for already-used challenges.
	// The atomic MarkChallengeUsed below provides the definitive TOCTOU-safe check.
	if !challenge.UsedAt.IsZero() {
		return "", ErrChallengeUsed
	}
	if time.Now().UTC().After(challenge.ExpiresAt) {
		return "", ErrChallengeExpired
	}

	// Route to the correct signature verifier based on chain type and signing type
	var valid bool
	if isSolanaChain(challenge.ChainID) {
		verifier, ok := s.signatureVerifier.(ChainAwareSignatureVerifier)
		if !ok {
			return "", errors.New("solana signature verification not supported")
		}
		valid, err = verifier.VerifySolanaSignature(normalizedAddress, challenge.Message, signature)
	} else if challenge.SigningType == "eip712" {
		// EIP-712 typed data verification: reconstruct the typed data from the challenge
		eip712Verifier := web3.NewEIP712Verifier(zap.NewNop())
		typedData := s.buildEIP712Challenge(challenge)
		valid, err = eip712Verifier.VerifyTypedData(normalizedAddress, typedData, signature)
	} else {
		// Default: EIP-191 personal_sign
		valid, err = s.signatureVerifier.VerifySignature(ctx, normalizedAddress, challenge.Message, signature)
	}
	if err != nil {
		return "", fmt.Errorf("failed to verify wallet signature: %w", err)
	}
	if !valid {
		return "", ErrInvalidCredential
	}

	if err := s.challengeStore.MarkChallengeUsed(ctx, challengeID, time.Now().UTC()); err != nil {
		// This catches the TOCTOU race: if a concurrent request consumed the challenge
		// between the fast-fail check above and this atomic mark, the error here
		// prevents token issuance.
		return "", fmt.Errorf("failed to consume challenge: %w", err)
	}

	return s.generateWalletToken(normalizedAddress)
}

// buildEIP712Challenge constructs an EIP-712 typed data structure from a wallet challenge.
// This allows wallets to sign a structured message instead of a plain-text string,
// providing better user experience and security in MetaMask and similar wallets.
func (s *AuthService) buildEIP712Challenge(challenge *WalletChallenge) *web3.EIP712TypedData {
	domain := web3.EIP712Domain{
		Name:              "StreamGate",
		Version:           "1",
		ChainId:           big.NewInt(challenge.ChainID),
		VerifyingContract: "",
	}

	return &web3.EIP712TypedData{
		Types: web3.EIP712Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
			},
			"Authentication": {
				{Name: "wallet", Type: "address"},
				{Name: "nonce", Type: "string"},
				{Name: "issuedAt", Type: "string"},
				{Name: "expiresAt", Type: "string"},
				{Name: "domain", Type: "string"},
				{Name: "uri", Type: "string"},
				{Name: "version", Type: "string"},
			},
		},
		PrimaryType: "Authentication",
		Domain:      domain,
		Message: map[string]interface{}{
			"wallet":    challenge.WalletAddress,
			"nonce":     challenge.Nonce,
			"issuedAt":  challenge.IssuedAt.Format(time.RFC3339),
			"expiresAt": challenge.ExpiresAt.Format(time.RFC3339),
			"domain":    "streamgate.io",
			"uri":       "https://streamgate.io/login",
			"version":   "1",
		},
	}
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

// RevokeToken parses a JWT and adds its JTI to the blacklist.
// This is best-effort: expired or invalid tokens are silently accepted.
func (s *AuthService) RevokeToken(ctx context.Context, tokenString string) error {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		// Token already invalid, nothing to revoke
		return nil
	}
	if s.blacklist == nil {
		return nil
	}
	jti := claims.JTI
	if jti == "" {
		return nil
	}
	expiresAt := time.Now()
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}
	return s.blacklist.Revoke(ctx, jti, expiresAt)
}

// VerifyToken checks if a token is valid, not expired, and not revoked.
func (s *AuthService) VerifyToken(ctx context.Context, tokenString string) (*TokenVerifyResult, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return &TokenVerifyResult{Valid: false}, err
	}
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return &TokenVerifyResult{Valid: false}, ErrTokenExpired
	}
	if s.blacklist != nil && claims.JTI != "" && s.blacklist.IsRevoked(ctx, claims.JTI) {
		return &TokenVerifyResult{Valid: false}, ErrTokenRevoked
	}
	expiresAtStr := ""
	if claims.ExpiresAt != nil {
		expiresAtStr = claims.ExpiresAt.Format(time.RFC3339)
	}
	return &TokenVerifyResult{
		Valid:         true,
		ExpiresAt:     expiresAtStr,
		WalletAddress: claims.WalletAddress,
	}, nil
}

// IsTokenRevoked checks if a token's JTI is in the blacklist.
func (s *AuthService) IsTokenRevoked(ctx context.Context, jti string) bool {
	if s.blacklist == nil || jti == "" {
		return false
	}
	return s.blacklist.IsRevoked(ctx, jti)
}
