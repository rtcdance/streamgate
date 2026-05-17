package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// WalletSignatureVerifier verifies EVM/Solana wallet signatures.
type WalletSignatureVerifier interface {
	VerifySignature(ctx context.Context, address, message, signature string) (bool, error)
}

// NFTOwnershipVerifier checks NFT ownership on-chain.
type NFTOwnershipVerifier interface {
	VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
}

// JWTTokenVerifier validates JWT tokens.
type JWTTokenVerifier interface {
	VerifyToken(tokenString string) (bool, error)
}

// AuthServer handles authentication and authorization
type AuthServer struct {
	config   *config.Config
	logger   *zap.Logger
	kernel   *core.Microkernel
	server   *http.Server
	verifier *AuthVerifier
}

// NewAuthServer creates a new auth server
func NewAuthServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*AuthServer, error) {
	verifier := NewAuthVerifier(logger)

	return &AuthServer{
		config:   cfg,
		logger:   logger,
		kernel:   kernel,
		verifier: verifier,
	}, nil
}

// Start starts the auth server
func (s *AuthServer) Start(ctx context.Context) error {
	handler := NewAuthHandler(s.verifier, s.logger, s.kernel)

	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	// Auth endpoints
	mux.HandleFunc("/api/v1/auth/verify-signature", handler.VerifySignatureHandler)
	mux.HandleFunc("/api/v1/auth/verify-nft", handler.VerifyNFTHandler)
	mux.HandleFunc("/api/v1/auth/verify-token", handler.VerifyTokenHandler)
	mux.HandleFunc("/api/v1/auth/challenge", handler.GetChallengeHandler)

	// Catch-all for 404
	mux.HandleFunc("/", handler.NotFoundHandler)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Auth server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the auth server
func (s *AuthServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down auth server", zap.Error(err))
			return err
		}
	}

	return nil
}

// Health checks the health of the auth server
func (s *AuthServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("auth server not started")
	}

	return nil
}

// AuthVerifier handles authentication verification.
// When injected verifier interfaces are set, it delegates to real implementations.
// When absent, methods return explicit "not configured" errors.
type AuthVerifier struct {
	logger        *zap.Logger
	sigVerifier   WalletSignatureVerifier
	nftVerifier   NFTOwnershipVerifier
	jwtVerifier   JWTTokenVerifier
	challengeAuth *ChallengeResponseAuth
}

// NewAuthVerifier creates a new auth verifier without backend verifiers.
func NewAuthVerifier(logger *zap.Logger) *AuthVerifier {
	return &AuthVerifier{
		logger:        logger,
		challengeAuth: NewChallengeResponseAuth(logger, nil),
	}
}

// NewAuthVerifierWithVerifiers creates an AuthVerifier wired to real verification backends.
func NewAuthVerifierWithVerifiers(
	logger *zap.Logger,
	sigVerifier WalletSignatureVerifier,
	nftVerifier NFTOwnershipVerifier,
	jwtVerifier JWTTokenVerifier,
) *AuthVerifier {
	return &AuthVerifier{
		logger:        logger,
		sigVerifier:   sigVerifier,
		nftVerifier:   nftVerifier,
		jwtVerifier:   jwtVerifier,
		challengeAuth: NewChallengeResponseAuth(logger, nil),
	}
}

// VerifySignature verifies a wallet signature.
// Returns an error if no WalletSignatureVerifier is configured.
func (v *AuthVerifier) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	if v.sigVerifier == nil {
		return false, errors.New("signature verification not configured: inject WalletSignatureVerifier via NewAuthVerifierWithVerifiers")
	}
	return v.sigVerifier.VerifySignature(ctx, address, message, signature)
}

// VerifyNFT verifies NFT ownership.
// Returns an error if no NFTOwnershipVerifier is configured.
func (v *AuthVerifier) VerifyNFT(ctx context.Context, address, contractAddress, tokenID string) (bool, error) {
	if v.nftVerifier == nil {
		return false, errors.New("NFT verification not configured: inject NFTOwnershipVerifier via NewAuthVerifierWithVerifiers")
	}
	// Default to chain ID 1 (Ethereum mainnet) — callers should use the service layer directly for multi-chain.
	return v.nftVerifier.VerifyNFTOwnership(ctx, 1, contractAddress, tokenID, address)
}

// VerifyToken verifies a JWT token.
// Returns an error if no JWTTokenVerifier is configured.
func (v *AuthVerifier) VerifyToken(ctx context.Context, token string) (bool, error) {
	if v.jwtVerifier == nil {
		return false, errors.New("token verification not configured: inject JWTTokenVerifier via NewAuthVerifierWithVerifiers")
	}
	return v.jwtVerifier.VerifyToken(token)
}

// GetChallenge generates a nonce-based challenge for wallet signing.
func (v *AuthVerifier) GetChallenge(ctx context.Context, address string) (string, error) {
	challenge, err := v.challengeAuth.GenerateChallenge(ctx, address)
	if err != nil {
		return "", fmt.Errorf("failed to generate challenge: %w", err)
	}
	return fmt.Sprintf("Sign this message to authenticate with StreamGate.\nAddress: %s\nNonce: %s\nIssued At: %s\nExpires At: %s",
		address, challenge.Nonce, challenge.Timestamp.Format(time.RFC3339), challenge.ExpiresAt.Format(time.RFC3339)), nil
}
