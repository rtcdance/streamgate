package web3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestChainClient_WalletDeadlineIncludesFailoverHandshake(t *testing.T) {
	primary := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(31337),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32005, "message": "rate limited"}}
		},
	})
	defer primary.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			return
		}
		once.Do(func() { close(entered) })
		select {
		case <-r.Context().Done():
			return
		case <-release:
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x7a69"}); err != nil {
			return
		}
	}))
	defer fallback.Close()
	client, err := NewChainClientWithFallback([]string{primary.URL, fallback.URL}, 31337, zap.NewNop())
	require.NoError(t, err)
	defer func() { close(release); client.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, callErr := client.CallContract(ctx, ethereum.CallMsg{To: &common.Address{}}, nil)
		done <- callErr
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("failover handshake not reached")
	}
	cancel()
	select {
	case callErr := <-done:
		require.Error(t, callErr)
	case <-time.After(time.Second):
		t.Fatal("failover handshake detached from caller context")
	}
}
