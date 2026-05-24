package service

import "github.com/rtcdance/streamgate/pkg/service/serviceerrors"

// Backward-compatible error aliases for types moved to service/serviceerrors.
var (
	ErrInvalidCredential   = serviceerrors.ErrInvalidCredential
	ErrTokenExpired        = serviceerrors.ErrTokenExpired
	ErrTokenRevoked        = serviceerrors.ErrTokenRevoked
	ErrInvalidToken        = serviceerrors.ErrInvalidToken
	ErrChallengeExpired    = serviceerrors.ErrChallengeExpired
	ErrChainIDMismatch     = serviceerrors.ErrChainIDMismatch
	ErrNotFound            = serviceerrors.ErrNotFound
	ErrAlreadyExists       = serviceerrors.ErrAlreadyExists
	ErrNFTNotFound         = serviceerrors.ErrNFTNotFound
	ErrInsufficientBalance = serviceerrors.ErrInsufficientBalance
	ErrInvalidAddress      = serviceerrors.ErrInvalidAddress
	ErrSolanaNotConfigured = serviceerrors.ErrSolanaNotConfigured
	ErrNotSupported        = serviceerrors.ErrNotSupported
	ErrInvalidRequest      = serviceerrors.ErrInvalidRequest
)
