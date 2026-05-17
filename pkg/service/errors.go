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
	ErrChallengeExpired  = errors.New("challenge expired")
	ErrChallengeUsed     = errors.New("challenge already used")
	ErrChallengeNotFound = errors.New("challenge not found")

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
	ErrNotSupported  = errors.New("operation not supported")
	ErrInvalidRequest = errors.New("invalid request")
)

// DomainError wraps an error with a machine-readable code for HTTP response mapping.
type DomainError struct {
	Code    string
	Message string
	Cause   error
}

func (e *DomainError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *DomainError) Unwrap() error { return e.Cause }

// NewDomainError creates a DomainError with code, message, and optional cause.
func NewDomainError(code, message string, cause ...error) *DomainError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &DomainError{Code: code, Message: message, Cause: c}
}
