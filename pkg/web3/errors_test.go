package web3

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryableError_Error(t *testing.T) {
	err := NewRetryableError("rpc timeout", fmt.Errorf("connection refused"))
	assert.Equal(t, "rpc timeout: connection refused", err.Error())
	assert.True(t, err.IsRetryable())
}

func TestRetryableError_NoCause(t *testing.T) {
	err := NewRetryableError("rate limited", nil)
	assert.Equal(t, "rate limited", err.Error())
}

func TestPermanentError_Error(t *testing.T) {
	err := NewPermanentError("invalid params", fmt.Errorf("bad input"))
	assert.Equal(t, "invalid params: bad input", err.Error())
	assert.False(t, err.IsRetryable())
}

func TestPermanentError_NoCause(t *testing.T) {
	err := NewPermanentError("contract revert", nil)
	assert.Equal(t, "contract revert", err.Error())
}

func TestIsRetryable_RetryableError(t *testing.T) {
	err := NewRetryableError("timeout", nil)
	assert.True(t, IsRetryable(err))
}

func TestIsRetryable_PermanentError(t *testing.T) {
	err := NewPermanentError("invalid", nil)
	assert.False(t, IsRetryable(err))
}

func TestIsRetryable_PlainError(t *testing.T) {
	err := fmt.Errorf("some error")
	assert.False(t, IsRetryable(err))
}

func TestIsRetryable_WrappedRetryable(t *testing.T) {
	inner := NewRetryableError("rpc timeout", nil)
	wrapped := fmt.Errorf("operation failed: %w", inner)
	assert.True(t, IsRetryable(wrapped))
}

func TestIsRetryable_WrappedPermanent(t *testing.T) {
	inner := NewPermanentError("contract revert", nil)
	wrapped := fmt.Errorf("tx failed: %w", inner)
	assert.False(t, IsRetryable(wrapped))
}

func TestIsRetryable_DoubleWrapped(t *testing.T) {
	inner := NewRetryableError("429 too many requests", nil)
	wrapped1 := fmt.Errorf("rpc call failed: %w", inner)
	wrapped2 := fmt.Errorf("operation error: %w", wrapped1)
	assert.True(t, IsRetryable(wrapped2))
}

func TestRetryableError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("root cause")
	err := NewRetryableError("wrapper", cause)
	unwrapped := err.Unwrap()
	require.NotNil(t, unwrapped)
	assert.Equal(t, "root cause", unwrapped.Error())
}

func TestPermanentError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("root cause")
	err := NewPermanentError("wrapper", cause)
	unwrapped := err.Unwrap()
	require.NotNil(t, unwrapped)
	assert.Equal(t, "root cause", unwrapped.Error())
}

func TestIsRetryable_WithErrorsAs(t *testing.T) {
	err := NewRetryableError("timeout", nil)
	var r Retryable
	assert.True(t, errors.As(err, &r))
}

func TestIsRetryable_NilError(t *testing.T) {
	assert.False(t, IsRetryable(nil))
}
