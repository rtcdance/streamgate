package web3

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type rpcRequest struct {
	ID     interface{}       `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func newRPCServer(t *testing.T, handlers map[string]func(req rpcRequest) rpcResponse) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		handler, ok := handlers[req.Method]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32601,
					"message": "method not found",
				},
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(handler(req))
	}))
}

func chainIDHandler(id int64) func(req rpcRequest) rpcResponse {
	return func(req rpcRequest) rpcResponse {
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  fmt.Sprintf("0x%x", id),
		}
	}
}

func TestChainClient_FailoverOnBlockNumber(t *testing.T) {
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "upstream failure",
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
				Result:  "0x2a",
			}
		},
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

func TestChainClient_FailoverOnEthCall(t *testing.T) {
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "eth_call failed",
				},
			}
		},
	})
	defer first.Close()

	second := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  "0x" + strings.Repeat("0", 63) + "2",
			}
		},
	})
	defer second.Close()

	client, err := NewChainClientWithFallback([]string{first.URL, second.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	balance, err := client.GetNFTBalance(context.Background(), "0x1234567890123456789012345678901234567890", "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f")
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(2), balance)
	assert.Equal(t, second.URL, client.rpcURL)
}

func TestChainClient_StartupFallbackSkipsDeadEndpoint(t *testing.T) {
	deadListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	deadURL := "http://" + deadListener.Addr().String()
	require.NoError(t, deadListener.Close())

	healthy := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":    chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x1"} },
	})
	defer healthy.Close()

	client, err := NewChainClientWithFallback([]string{deadURL, healthy.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	blockNumber, err := client.GetBlockNumber(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), blockNumber)
	assert.Equal(t, healthy.URL, client.rpcURL)
}

func TestChainClient_FailsWhenAllRPCsFail(t *testing.T) {
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "chain id failure",
				},
			}
		},
	})
	defer first.Close()

	second := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "chain id failure",
				},
			}
		},
	})
	defer second.Close()

	client, err := NewChainClientWithFallback([]string{first.URL, second.URL}, 11155111, zap.NewNop())
	require.Nil(t, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to blockchain")
}

func TestChainClient_FailedEndpointEntersCooldown(t *testing.T) {
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "temporary upstream failure",
				},
			}
		},
	})
	defer first.Close()

	second := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":    chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x10"} },
	})
	defer second.Close()

	client, err := NewChainClientWithFallback([]string{first.URL, second.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetBlockNumber(context.Background())
	require.NoError(t, err)

	client.mu.RLock()
	state := client.rpcStates[0]
	client.mu.RUnlock()
	assert.Equal(t, 1, state.Failures)
	assert.False(t, state.CooldownUntil.IsZero())
	assert.True(t, state.CooldownUntil.After(time.Now()))
	assert.False(t, client.endpointReady(0, false))
}

func TestChainClient_RPCStatusesReflectFailover(t *testing.T) {
	first := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32000,
					"message": "temporary upstream failure",
				},
			}
		},
	})
	defer first.Close()

	second := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":    chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x20"} },
	})
	defer second.Close()

	client, err := NewChainClientWithFallback([]string{first.URL, second.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetBlockNumber(context.Background())
	require.NoError(t, err)

	statuses := client.GetRPCStatuses()
	require.Len(t, statuses, 2)
	assert.Equal(t, first.URL, statuses[0].URL)
	assert.Equal(t, second.URL, statuses[1].URL)
	assert.False(t, statuses[0].IsActive)
	assert.True(t, statuses[1].IsActive)
	assert.Equal(t, 1, statuses[0].Failures)
}
