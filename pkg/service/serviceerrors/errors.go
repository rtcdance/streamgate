package serviceerrors

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
	ErrChainIDMismatch  = errors.New("chain id mismatch")

	// Resource errors
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")

	// ErrOriginalImmutable reports that a content's original digest was already
	// sealed and a different digest was presented for the same content. The
	// caller must map it to HTTP 409 Conflict; the original is never replaced.
	ErrOriginalImmutable = errors.New("original media digest is immutable")

	// ErrStagedObjectMissing reports a presigned completion attempted before the
	// client actually wrote the object to its staging key. Map to HTTP 400.
	ErrStagedObjectMissing = errors.New("presigned upload: staged object not found in storage")

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
