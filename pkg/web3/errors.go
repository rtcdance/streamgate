package web3

import (
	"errors"
	"fmt"
)

// RetryableError indicates an operation failed but may succeed if retried.
// Examples: RPC timeout, rate limit (429), temporary network failure.
type RetryableError struct {
	Message string
	Cause   error
}

func (e *RetryableError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *RetryableError) Unwrap() error { return e.Cause }

// IsRetryable returns true for RetryableError.
func (e *RetryableError) IsRetryable() bool { return true }

// PermanentError indicates an operation failed and will not succeed if retried.
// Examples: invalid parameters, contract revert, insufficient funds.
type PermanentError struct {
	Message string
	Cause   error
}

func (e *PermanentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *PermanentError) Unwrap() error { return e.Cause }

// IsRetryable returns false for PermanentError.
func (e *PermanentError) IsRetryable() bool { return false }

// Retryable is an interface for errors that can report whether they are retryable.
// This interface is defined here so that any package can implement it without
// importing pkg/web3 — just copy the interface definition.
type Retryable interface {
	IsRetryable() bool
}

// IsRetryable checks if an error (or any error in its chain) is retryable.
// It walks the error chain using errors.As to find a Retryable implementation.
// This function is safe to call from any package.
func IsRetryable(err error) bool {
	var r Retryable
	if errors.As(err, &r) {
		return r.IsRetryable()
	}
	return false
}

// NewRetryableError creates a RetryableError wrapping the given cause.
func NewRetryableError(msg string, cause error) *RetryableError {
	return &RetryableError{Message: msg, Cause: cause}
}

// NewPermanentError creates a PermanentError wrapping the given cause.
func NewPermanentError(msg string, cause error) *PermanentError {
	return &PermanentError{Message: msg, Cause: cause}
}

// DualError.Unwrap() []error requires Go 1.20+ for correct errors.Is/errors.As traversal.
type DualError struct {
	Primary   error
	Secondary error
}

func (e *DualError) Error() string {
	return fmt.Sprintf("call failed on both proxy (%v) and implementation (%v)", e.Primary, e.Secondary)
}

func (e *DualError) Unwrap() []error { return []error{e.Primary, e.Secondary} }

func (e *DualError) IsRetryable() bool {
	var r Retryable
	if errors.As(e.Primary, &r) && !r.IsRetryable() {
		return false
	}
	if errors.As(e.Secondary, &r) && !r.IsRetryable() {
		return false
	}
	if errors.As(e.Primary, &r) || errors.As(e.Secondary, &r) {
		return true
	}
	return false
}
