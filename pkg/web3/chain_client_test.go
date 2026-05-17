package web3

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- isPermanentRPCError unit tests ---

func TestIsPermanentRPCError_Nil(t *testing.T) {
	assert.False(t, isPermanentRPCError(nil))
}

func TestIsPermanentRPCError_ExecutionReverted(t *testing.T) {
	assert.True(t, isPermanentRPCError(errors.New("execution reverted")))
}

func TestIsPermanentRPCError_Revert(t *testing.T) {
	assert.True(t, isPermanentRPCError(errors.New("revert: caller is not the owner")))
}

func TestIsPermanentRPCError_InvalidOpcode(t *testing.T) {
	assert.True(t, isPermanentRPCError(errors.New("invalid opcode")))
}

func TestIsPermanentRPCError_OutOfGas(t *testing.T) {
	assert.True(t, isPermanentRPCError(errors.New("out of gas")))
}

func TestIsPermanentRPCError_NonceTooLow(t *testing.T) {
	assert.True(t, isPermanentRPCError(errors.New("nonce too low")))
}

func TestIsPermanentRPCError_InsufficientFunds(t *testing.T) {
	assert.True(t, isPermanentRPCError(errors.New("insufficient funds for gas * price + value")))
}

func TestIsPermanentRPCError_AlreadyKnown(t *testing.T) {
	assert.True(t, isPermanentRPCError(errors.New("already known")))
}

func TestIsPermanentRPCError_TransientError(t *testing.T) {
	assert.False(t, isPermanentRPCError(errors.New("upstream timeout")))
}

func TestIsPermanentRPCError_ConnectionRefused(t *testing.T) {
	assert.False(t, isPermanentRPCError(errors.New("connection refused")))
}

func TestIsPermanentRPCError_CaseInsensitive(t *testing.T) {
	assert.True(t, isPermanentRPCError(errors.New("Execution Reverted: insufficient allowance")))
}

// --- withChainClient error classification integration tests ---
// These use the same httptest RPC server pattern from chain_test.go
// but focus on the error classification after all endpoints fail.

func TestWithChainClient_PermanentErrorOnAllFail(t *testing.T) {
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "execution reverted",
				},
			}
		},
	})
	defer first.Close()

	second := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "execution reverted",
				},
			}
		},
	})
	defer second.Close()

	client, err := NewChainClientWithFallback([]string{first.URL, second.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetBlockNumber(context.Background())
	require.Error(t, err)

	var permErr *PermanentError
	assert.True(t, errors.As(err, &permErr), "expected PermanentError, got %T: %v", err, err)
	assert.False(t, permErr.IsRetryable())
}

func TestWithChainClient_RetryableErrorOnAllFail(t *testing.T) {
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "upstream timeout",
				},
			}
		},
	})
	defer first.Close()

	second := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "service unavailable",
				},
			}
		},
	})
	defer second.Close()

	client, err := NewChainClientWithFallback([]string{first.URL, second.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetBlockNumber(context.Background())
	require.Error(t, err)

	var retryErr *RetryableError
	assert.True(t, errors.As(err, &retryErr), "expected RetryableError, got %T: %v", err, err)
	assert.True(t, retryErr.IsRetryable())
}

func TestWithChainClient_SingleRPCSuccess(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x64"} },
	})
	defer srv.Close()

	client, err := NewChainClientWithFallback([]string{srv.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	blockNumber, err := client.GetBlockNumber(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(100), blockNumber)
}

func TestWithChainClient_FailoverToSecondEndpoint(t *testing.T) {
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "temporary failure",
				},
			}
		},
	})
	defer first.Close()

	second := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x2a"} },
	})
	defer second.Close()

	client, err := NewChainClientWithFallback([]string{first.URL, second.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	blockNumber, err := client.GetBlockNumber(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(42), blockNumber)
	assert.Equal(t, second.URL, client.rpcURL)
}

func TestWithChainClient_MixedErrorsPermanentWins(t *testing.T) {
	// First endpoint returns a transient error, second returns a permanent error.
	// Since the last error is permanent, withChainClient should return PermanentError.
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "timeout",
				},
			}
		},
	})
	defer first.Close()

	second := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "nonce too low",
				},
			}
		},
	})
	defer second.Close()

	client, err := NewChainClientWithFallback([]string{first.URL, second.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetBlockNumber(context.Background())
	require.Error(t, err)

	var permErr *PermanentError
	assert.True(t, errors.As(err, &permErr), "expected PermanentError when last RPC fails with permanent error, got %T: %v", err, err)
}

func TestWithChainClient_RateLimitExceeded(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x1"} },
	})
	defer srv.Close()

	client, err := NewChainClientWithFallback([]string{srv.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	// Apply a rate limiter with tiny burst
	limiter := NewRPCRateLimiter(1, 1, zap.NewNop())
	// Exhaust the burst token
	_ = limiter.Wait(context.Background())
	// Set it on the client
	client.mu.Lock()
	client.rateLimiter = limiter
	client.mu.Unlock()

	// Use a cancelled context to make the rate limiter wait fail immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetBlockNumber(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rpc rate limited")
}
