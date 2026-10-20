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

	"github.com/ethereum/go-ethereum/ethclient"
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
		defer func() { _ = r.Body.Close() }()
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

// A dead endpoint that HANGS — it accepts the request and never answers — must
// not spend the caller's whole budget, because the failover loop then runs on an
// already cancelled context and the read fails for a legitimate caller. This is
// the perf matrix's `rpc_primary_down@100` failure: one manifest request took
// the whole 30s budget against the stopped primary endpoint and answered 503 for
// a real NFT holder, with the fallback reporting "context canceled" instead of
// carrying the read.
func TestChainClient_AHungEndpointCannotStarveTheFailover(t *testing.T) {
	restore := rpcRequestTimeout
	rpcRequestTimeout = 200 * time.Millisecond
	defer func() { rpcRequestTimeout = restore }()

	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "eth_chainId" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x1"})
			return
		}
		// Accept the connection, answer nothing: the client's own bound must end
		// the attempt, not the caller's deadline.
		<-r.Context().Done()
	}))
	defer hung.Close()

	healthy := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(1),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x64"} },
	})
	defer healthy.Close()

	client, err := NewChainClientWithFallback([]string{hung.URL, healthy.URL}, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	blockNumber, err := client.GetBlockNumber(ctx)
	elapsed := time.Since(start)

	require.NoError(t, err, "the same-chain fallback must carry the read")
	assert.Equal(t, uint64(100), blockNumber)
	assert.Equal(t, healthy.URL, client.rpcURL)
	assert.Less(t, elapsed, time.Second, "the hung endpoint must not consume the caller's budget")
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
		"eth_chainId":     chainIDHandler(11155111),
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
		"eth_chainId":     chainIDHandler(11155111),
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
		"eth_chainId":     chainIDHandler(11155111),
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

// A failure must be charged to the endpoint that actually served the request.
// Reading the active index separately from the client lets a concurrent
// failover move the index between the two reads, so a peer's failure can be
// charged to a HEALTHY endpoint and put it into cooldown — which removes it from
// the failover candidates and turns a recoverable read into a hard failure.
func TestChainClient_FailureIsChargedToTheEndpointThatServedIt(t *testing.T) {
	healthy := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x2a"} },
	})
	defer healthy.Close()

	dead := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32000, "message": "upstream failure"}}
		},
	})
	defer dead.Close()

	// Endpoint 0 is healthy, endpoint 1 fails; make the FAILING one active.
	client, err := NewChainClientWithFallback([]string{healthy.URL, dead.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()
	deadClient, _, err := client.connectAt(context.Background(), 1)
	require.NoError(t, err)
	client.setActiveClient(1, deadClient, true)

	// While this operation is in flight on endpoint 1, a peer switches the active
	// endpoint to the healthy one; then this operation itself fails.
	calls := 0
	result, err := withChainClient(context.Background(), client, "SimulatedOp", func(*ethclient.Client) (int, error) {
		calls++
		if calls == 1 {
			healthyClient, _, cerr := client.connectAt(context.Background(), 0)
			require.NoError(t, cerr)
			client.setActiveClient(0, healthyClient, true)
			return 0, fmt.Errorf("simulated transport failure")
		}
		return 42, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 42, result)

	client.mu.RLock()
	healthyState := client.rpcStates[0]
	failingState := client.rpcStates[1]
	client.mu.RUnlock()
	assert.True(t, healthyState.CooldownUntil.IsZero(),
		"a healthy endpoint must not be charged a failure that happened on another endpoint")
	assert.Equal(t, 1, failingState.Failures, "the endpoint that served the request must be the one charged")
}
