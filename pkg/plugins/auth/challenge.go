package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ChallengeResponseAuth handles challenge-response authentication
type ChallengeResponseAuth struct {
	logger     *zap.Logger
	challenges map[string]*Challenge
	mu         sync.RWMutex
	config     *AuthConfig
}

// Challenge represents an authentication challenge
type Challenge struct {
	ID          string
	Nonce       string
	Timestamp   time.Time
	ExpiresAt   time.Time
	Used        bool
	Attempts    int
	MaxAttempts int
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	ChallengeTTL     time.Duration
	MaxAttempts      int
	RequireSignature bool
}

// NewChallengeResponseAuth creates a new challenge-response authentication handler
func NewChallengeResponseAuth(logger *zap.Logger, config *AuthConfig) *ChallengeResponseAuth {
	if config == nil {
		config = &AuthConfig{
			ChallengeTTL:     5 * time.Minute,
			MaxAttempts:      3,
			RequireSignature: true,
		}
	}

	return &ChallengeResponseAuth{
		logger:     logger,
		challenges: make(map[string]*Challenge),
		config:     config,
	}
}

// GenerateChallenge generates a new authentication challenge
func (cra *ChallengeResponseAuth) GenerateChallenge(ctx context.Context, clientID string) (*Challenge, error) {
	cra.logger.Debug("Generating challenge",
		zap.String("client_id", clientID))

	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	challengeID := generateChallengeID()

	challenge := &Challenge{
		ID:          challengeID,
		Nonce:       nonce,
		Timestamp:   time.Now(),
		ExpiresAt:   time.Now().Add(cra.config.ChallengeTTL),
		Used:        false,
		Attempts:    0,
		MaxAttempts: cra.config.MaxAttempts,
	}

	cra.mu.Lock()
	cra.challenges[challengeID] = challenge
	cra.mu.Unlock()

	cra.logger.Debug("Challenge generated",
		zap.String("challenge_id", challengeID),
		zap.String("client_id", clientID))

	return challenge, nil
}

// VerifyResponse verifies a challenge response
func (cra *ChallengeResponseAuth) VerifyResponse(ctx context.Context, challengeID, response string, verifier ResponseVerifier) (bool, error) {
	cra.logger.Debug("Verifying response",
		zap.String("challenge_id", challengeID))

	cra.mu.Lock()
	defer cra.mu.Unlock()

	challenge, exists := cra.challenges[challengeID]
	if !exists {
		return false, fmt.Errorf("challenge not found: %s", challengeID)
	}

	if challenge.Used {
		return false, fmt.Errorf("challenge already used: %s", challengeID)
	}

	if time.Now().After(challenge.ExpiresAt) {
		delete(cra.challenges, challengeID)
		return false, fmt.Errorf("challenge expired: %s", challengeID)
	}

	if challenge.Attempts >= challenge.MaxAttempts {
		delete(cra.challenges, challengeID)
		return false, fmt.Errorf("max attempts exceeded: %s", challengeID)
	}

	challenge.Attempts++

	expectedResponse, err := verifier.ComputeResponse(challenge.Nonce)
	if err != nil {
		return false, fmt.Errorf("failed to compute expected response: %w", err)
	}

	if response != expectedResponse {
		cra.logger.Warn("Invalid response",
			zap.String("challenge_id", challengeID),
			zap.Int("attempt", challenge.Attempts))
		return false, nil
	}

	challenge.Used = true

	cra.logger.Debug("Response verified",
		zap.String("challenge_id", challengeID))

	return true, nil
}

// VerifySignature verifies a signature-based challenge response
func (cra *ChallengeResponseAuth) VerifySignature(ctx context.Context, challengeID, signature, publicKey string, verifier SignatureVerifier) (bool, error) {
	cra.logger.Debug("Verifying signature",
		zap.String("challenge_id", challengeID),
		zap.String("public_key", publicKey))

	cra.mu.Lock()
	defer cra.mu.Unlock()

	challenge, exists := cra.challenges[challengeID]
	if !exists {
		return false, fmt.Errorf("challenge not found: %s", challengeID)
	}

	if challenge.Used {
		return false, fmt.Errorf("challenge already used: %s", challengeID)
	}

	if time.Now().After(challenge.ExpiresAt) {
		delete(cra.challenges, challengeID)
		return false, fmt.Errorf("challenge expired: %s", challengeID)
	}

	if challenge.Attempts >= challenge.MaxAttempts {
		delete(cra.challenges, challengeID)
		return false, fmt.Errorf("max attempts exceeded: %s", challengeID)
	}

	challenge.Attempts++

	valid, err := verifier.VerifySignature(publicKey, challenge.Nonce, signature)
	if err != nil {
		return false, fmt.Errorf("signature verification failed: %w", err)
	}

	if !valid {
		cra.logger.Warn("Invalid signature",
			zap.String("challenge_id", challengeID),
			zap.Int("attempt", challenge.Attempts))
		return false, nil
	}

	challenge.Used = true

	cra.logger.Debug("Signature verified",
		zap.String("challenge_id", challengeID))

	return true, nil
}

// GetChallenge retrieves a challenge by ID
func (cra *ChallengeResponseAuth) GetChallenge(ctx context.Context, challengeID string) (*Challenge, error) {
	cra.mu.RLock()
	defer cra.mu.RUnlock()

	challenge, exists := cra.challenges[challengeID]
	if !exists {
		return nil, fmt.Errorf("challenge not found: %s", challengeID)
	}

	return challenge, nil
}

// CleanupExpiredChallenges removes expired challenges
func (cra *ChallengeResponseAuth) CleanupExpiredChallenges(ctx context.Context) error {
	cra.logger.Debug("Cleaning up expired challenges")

	cra.mu.Lock()
	defer cra.mu.Unlock()

	now := time.Now()
	for challengeID, challenge := range cra.challenges {
		if now.After(challenge.ExpiresAt) || challenge.Used {
			delete(cra.challenges, challengeID)
		}
	}

	return nil
}

// ResponseVerifier defines the interface for verifying challenge responses
type ResponseVerifier interface {
	ComputeResponse(nonce string) (string, error)
}

// SignatureVerifier defines the interface for verifying signatures
type SignatureVerifier interface {
	VerifySignature(publicKey, message, signature string) (bool, error)
}

// SHA256Verifier verifies SHA256-based responses
type SHA256Verifier struct {
	secret string
}

// NewSHA256Verifier creates a new SHA256 verifier
func NewSHA256Verifier(secret string) *SHA256Verifier {
	return &SHA256Verifier{
		secret: secret,
	}
}

// ComputeResponse computes the expected response
func (sv *SHA256Verifier) ComputeResponse(nonce string) (string, error) {
	hash := sha256.Sum256([]byte(sv.secret + nonce))
	return hex.EncodeToString(hash[:]), nil
}

// HMACVerifier verifies HMAC-based responses
type HMACVerifier struct {
	secret string
}

// NewHMACVerifier creates a new HMAC verifier
func NewHMACVerifier(secret string) *HMACVerifier {
	return &HMACVerifier{
		secret: secret,
	}
}

// ComputeResponse computes the expected response
func (hv *HMACVerifier) ComputeResponse(nonce string) (string, error) {
	hash := sha256.Sum256([]byte(hv.secret + nonce))
	return base64.StdEncoding.EncodeToString(hash[:]), nil
}

// generateNonce generates a random nonce
func generateNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// generateChallengeID generates a unique challenge ID
func generateChallengeID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// Session represents an authenticated session
type Session struct {
	ID           string
	ClientID     string
	PublicKey    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastActivity time.Time
}

// SessionManager manages authenticated sessions
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	logger   *zap.Logger
	config   *SessionConfig
}

// SessionConfig represents session configuration
type SessionConfig struct {
	SessionTTL      time.Duration
	CleanupInterval time.Duration
}

// NewSessionManager creates a new session manager
func NewSessionManager(logger *zap.Logger, config *SessionConfig) *SessionManager {
	if config == nil {
		config = &SessionConfig{
			SessionTTL:      24 * time.Hour,
			CleanupInterval: 1 * time.Hour,
		}
	}

	sm := &SessionManager{
		sessions: make(map[string]*Session),
		logger:   logger,
		config:   config,
	}

	go sm.startCleanup()

	return sm
}

// CreateSession creates a new session
func (sm *SessionManager) CreateSession(ctx context.Context, clientID, publicKey string) (*Session, error) {
	sm.logger.Debug("Creating session",
		zap.String("client_id", clientID),
		zap.String("public_key", publicKey))

	sessionID := generateSessionID()

	session := &Session{
		ID:           sessionID,
		ClientID:     clientID,
		PublicKey:    publicKey,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(sm.config.SessionTTL),
		LastActivity: time.Now(),
	}

	sm.mu.Lock()
	sm.sessions[sessionID] = session
	sm.mu.Unlock()

	sm.logger.Debug("Session created",
		zap.String("session_id", sessionID))

	return session, nil
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("session expired: %s", sessionID)
	}

	return session, nil
}

// ValidateSession validates a session
func (sm *SessionManager) ValidateSession(ctx context.Context, sessionID string) (bool, error) {
	session, err := sm.GetSession(ctx, sessionID)
	if err != nil {
		return false, err
	}

	if time.Now().After(session.ExpiresAt) {
		return false, nil
	}

	return true, nil
}

// RefreshSession refreshes a session
func (sm *SessionManager) RefreshSession(ctx context.Context, sessionID string) error {
	sm.logger.Debug("Refreshing session",
		zap.String("session_id", sessionID))

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	session.ExpiresAt = time.Now().Add(sm.config.SessionTTL)
	session.LastActivity = time.Now()

	sm.logger.Debug("Session refreshed",
		zap.String("session_id", sessionID))

	return nil
}

// RevokeSession revokes a session
func (sm *SessionManager) RevokeSession(ctx context.Context, sessionID string) error {
	sm.logger.Debug("Revoking session",
		zap.String("session_id", sessionID))

	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, sessionID)

	sm.logger.Debug("Session revoked",
		zap.String("session_id", sessionID))

	return nil
}

// startCleanup starts the cleanup goroutine
func (sm *SessionManager) startCleanup() {
	ticker := time.NewTicker(sm.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		sm.cleanupExpiredSessions()
	}
}

// cleanupExpiredSessions removes expired sessions
func (sm *SessionManager) cleanupExpiredSessions() {
	sm.logger.Debug("Cleaning up expired sessions")

	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for sessionID, session := range sm.sessions {
		if now.After(session.ExpiresAt) {
			delete(sm.sessions, sessionID)
		}
	}
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// AuthMiddleware provides authentication middleware
type AuthMiddleware struct {
	auth        *ChallengeResponseAuth
	sessionMgr  *SessionManager
	requireAuth bool
	logger      *zap.Logger
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(auth *ChallengeResponseAuth, sessionMgr *SessionManager, requireAuth bool, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		auth:        auth,
		sessionMgr:  sessionMgr,
		requireAuth: requireAuth,
		logger:      logger,
	}
}

// Authenticate authenticates a request
func (am *AuthMiddleware) Authenticate(ctx context.Context, sessionID string) (*Session, error) {
	if !am.requireAuth {
		return nil, nil
	}

	session, err := am.sessionMgr.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if err := am.sessionMgr.RefreshSession(ctx, sessionID); err != nil {
		return nil, fmt.Errorf("failed to refresh session: %w", err)
	}

	return session, nil
}

// ChallengeRequest represents a challenge request
type ChallengeRequest struct {
	ClientID string `json:"client_id"`
}

// ChallengeResponse represents a challenge response
type ChallengeResponse struct {
	ChallengeID string `json:"challenge_id"`
	Nonce       string `json:"nonce"`
	ExpiresAt   string `json:"expires_at"`
}

// AuthRequest represents an authentication request
type AuthRequest struct {
	ChallengeID string `json:"challenge_id"`
	Response    string `json:"response"`
	PublicKey   string `json:"public_key,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// AuthResponse represents an authentication response
type AuthResponse struct {
	SessionID string `json:"session_id"`
	ExpiresAt string `json:"expires_at"`
}

// HandleChallenge handles a challenge request
func (am *AuthMiddleware) HandleChallenge(ctx context.Context, req *ChallengeRequest) (*ChallengeResponse, error) {
	challenge, err := am.auth.GenerateChallenge(ctx, req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate challenge: %w", err)
	}

	return &ChallengeResponse{
		ChallengeID: challenge.ID,
		Nonce:       challenge.Nonce,
		ExpiresAt:   challenge.ExpiresAt.Format(time.RFC3339),
	}, nil
}

// HandleAuth handles an authentication request
func (am *AuthMiddleware) HandleAuth(ctx context.Context, req *AuthRequest, verifier ResponseVerifier) (*AuthResponse, error) {
	if req.Signature != "" && req.PublicKey != "" {
		sigVerifier, ok := verifier.(SignatureVerifier)
		if !ok {
			return nil, fmt.Errorf("verifier does not support signature verification")
		}

		valid, err := am.auth.VerifySignature(ctx, req.ChallengeID, req.Signature, req.PublicKey, sigVerifier)
		if err != nil {
			return nil, fmt.Errorf("signature verification failed: %w", err)
		}

		if !valid {
			return nil, fmt.Errorf("invalid signature")
		}
	} else {
		valid, err := am.auth.VerifyResponse(ctx, req.ChallengeID, req.Response, verifier)
		if err != nil {
			return nil, fmt.Errorf("response verification failed: %w", err)
		}

		if !valid {
			return nil, fmt.Errorf("invalid response")
		}
	}

	session, err := am.sessionMgr.CreateSession(ctx, "", req.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &AuthResponse{
		SessionID: session.ID,
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339),
	}, nil
}
