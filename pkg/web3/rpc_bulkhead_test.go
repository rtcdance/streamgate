package web3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestChainClient_BoundsConcurrentRPCCalls pins the bulkhead. Throughput on a
// CPU-throttled endpoint falls as concurrency rises, so an unbounded viewer
// fan-out queues behind a forwarder that is idle in aggregate; this test keeps
// the bound from being removed as a "simplification".
func TestChainClient_BoundsConcurrentRPCCalls(t *testing.T) {
	var inFlight, peak int64
	var mu sync.Mutex
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "eth_chainId" {
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x7a69"})
			return
		}
		current := atomic.AddInt64(&inFlight, 1)
		mu.Lock()
		if current > peak {
			peak = current
		}
		mu.Unlock()
		<-release
		atomic.AddInt64(&inFlight, -1)
		_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x"})
	}))
	defer server.Close()

	client, err := NewChainClient(server.URL, 31337, zap.NewNop())
	require.NoError(t, err)

	const callers = 64
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_, _ = client.CallContract(context.Background(), ethereum.CallMsg{To: &common.Address{}}, nil)
		}()
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		observed := peak
		mu.Unlock()
		if observed >= int64(rpcMaxConcurrentCalls) || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	observed := peak
	mu.Unlock()
	close(release)
	wg.Wait()

	require.Equal(t, int64(rpcMaxConcurrentCalls), observed,
		"the chain client must keep exactly %d RPC calls in flight, not %d", rpcMaxConcurrentCalls, observed)
}
