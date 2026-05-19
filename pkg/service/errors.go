package service

import "errors"

// Sentinel errors for programmatic matching via errors.Is().
var (
	// Authentication errors
	ErrInvalidCredential = errors.New("invalid credential")
	ErrTokenExpired      = errors.New("token expired")
	ErrTokenRevoked      = errors.New("token revoked")
	ErrInvalidToken      = errors.New("invalid token")

	// Challenge errors
	ErrChallengeExpired = errors.New("challenge expired")

	// Resource errors
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")

	// NFT / Web3 errors
	ErrNFTNotFound         = errors.New("nft not found")
	ErrInsufficientBalance = errors.New("insufficient token balance")
	ErrInvalidAddress      = errors.New("invalid address")

	// Solana errors
	ErrSolanaNotConfigured = errors.New("solana verifier not configured")

	// Operation errors
	ErrNotSupported   = errors.New("operation not supported")
	ErrInvalidRequest = errors.New("invalid request")
)
