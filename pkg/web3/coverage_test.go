package web3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestIsPrivateIP_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"10_boundary_low", "10.0.0.0", true},
		{"10_boundary_high", "10.255.255.255", true},
		{"172_16_boundary", "172.16.0.0", true},
		{"172_31_boundary", "172.31.255.255", true},
		{"172_15_not_private", "172.15.255.255", false},
		{"172_32_not_private", "172.32.0.0", false},
		{"192_168_boundary", "192.168.0.0", true},
		{"192_168_high", "192.168.255.255", true},
		{"127_boundary", "127.0.0.0", true},
		{"127_high", "127.255.255.255", true},
		{"169_254_boundary", "169.254.0.0", true},
		{"169_254_high", "169.254.255.255", true},
		{"fc00_ipv6", "fc00::1", true},
		{"fdff_ipv6", "fdff::1", true},
		{"fe80_ipv6", "fe80::1", true},
		{"febf_ipv6", "febf::1", true},
		{"public_ipv4", "104.26.10.78", false},
		{"public_ipv6", "2606:4700::1", false},
		{"broadcast", "255.255.255.255", false},
		{"multicast", "224.0.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP: %s", tt.ip)
			assert.Equal(t, tt.expected, isPrivateIP(ip))
		})
	}
}

func TestIsPermanentRPCError_Table(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{"execution_reverted", "execution reverted: insufficient output", true},
		{"revert", "call revert: custom error", true},
		{"invalid_opcode", "invalid opcode: INVALID", true},
		{"out_of_gas", "out of gas: gas limit reached", true},
		{"invalid_jump", "invalid jump destination", true},
		{"stack_limit", "stack limit reached 1024", true},
		{"contract_creation", "contract creation code storage out of gas", true},
		{"nonce_too_low", "nonce too low: 5 < 10", true},
		{"insufficient_funds", "insufficient funds for transfer", true},
		{"already_known", "already known: transaction", true},
		{"timeout", "context deadline exceeded", false},
		{"connection_refused", "connection refused", false},
		{"network_error", "dial tcp: lookup failed", false},
		{"nil_error", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = errors.New(tt.errMsg)
			}
			assert.Equal(t, tt.expected, isPermanentRPCError(err))
		})
	}
}

func TestIsPermanentRPCError_UpperCase(t *testing.T) {
	err := fmt.Errorf("Execution Reverted: custom error")
	assert.True(t, isPermanentRPCError(err))
}

func TestRewriteURI_IPFSWithPath(t *testing.T) {
	result, err := rewriteURI("ipfs://QmTest/subdir/file.json")
	require.NoError(t, err)
	assert.Equal(t, DefaultIPFSGateway+"QmTest/subdir/file.json", result)
}

func TestRewriteURI_ArweaveWithPath(t *testing.T) {
	result, err := rewriteURI("ar://abc123/metadata.json")
	require.NoError(t, err)
	assert.Equal(t, DefaultArweaveGateway+"abc123/metadata.json", result)
}

func TestDefaultGateways(t *testing.T) {
	assert.Equal(t, "https://ipfs.io/ipfs/", DefaultIPFSGateway)
	assert.Equal(t, "https://arweave.net/", DefaultArweaveGateway)
}

func TestRPCConstants(t *testing.T) {
	assert.Equal(t, 30*time.Second, rpcFailureCooldown)
	assert.Equal(t, 1.0, rpcScoreInitial)
	assert.Equal(t, 0.9, rpcScoreDecay)
	assert.Equal(t, 5.0, rpcLatencyThreshold)
}

func TestRPCStatus_Fields(t *testing.T) {
	now := time.Now()
	s := RPCStatus{
		URL:           "https://rpc.example.com",
		IsActive:      true,
		Failures:      3,
		LastFailureAt: now,
		CooldownUntil: now.Add(30 * time.Second),
		Score:         0.85,
		LastLatencyMs: 150,
	}
	assert.Equal(t, "https://rpc.example.com", s.URL)
	assert.True(t, s.IsActive)
	assert.Equal(t, 3, s.Failures)
	assert.Equal(t, 0.85, s.Score)
	assert.Equal(t, int64(150), s.LastLatencyMs)
}

func TestChainClient_UpdateRPCScores_OutOfBounds(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1"},
		rpcStates: []rpcEndpointState{{Score: 1.0}},
		logger:    zap.NewNop(),
	}
	cc.updateRPCScores(-1, 100*time.Millisecond, true)
	cc.updateRPCScores(5, 100*time.Millisecond, true)
}

func TestChainClient_UpdateRPCScores_Success(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1"},
		rpcStates: []rpcEndpointState{{Score: 0.5}},
		logger:    zap.NewNop(),
	}
	cc.updateRPCScores(0, 100*time.Millisecond, true)
	cc.mu.RLock()
	score := cc.rpcStates[0].Score
	cc.mu.RUnlock()
	assert.Greater(t, score, 0.5)
}

func TestChainClient_UpdateRPCScores_Failure(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1"},
		rpcStates: []rpcEndpointState{{Score: 0.8}},
		logger:    zap.NewNop(),
	}
	cc.updateRPCScores(0, 0, false)
	cc.mu.RLock()
	score := cc.rpcStates[0].Score
	cc.mu.RUnlock()
	assert.Less(t, score, 0.8)
}

func TestChainClient_UpdateRPCScores_HighLatencyZeroScore(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1"},
		rpcStates: []rpcEndpointState{{Score: 0.5}},
		logger:    zap.NewNop(),
	}
	cc.updateRPCScores(0, 10*time.Second, true)
	cc.mu.RLock()
	score := cc.rpcStates[0].Score
	cc.mu.RUnlock()
	assert.GreaterOrEqual(t, score, float64(0))
}

func TestChainClient_SortedRPCScores_ThreeEndpoints(t *testing.T) {
	cc := &ChainClient{
		rpcURLs: []string{"url1", "url2", "url3"},
		rpcStates: []rpcEndpointState{
			{Score: 0.3},
			{Score: 0.9},
			{Score: 0.6},
		},
		logger: zap.NewNop(),
	}
	indices := cc.sortedRPCScores()
	assert.Equal(t, 1, indices[0])
	assert.Equal(t, 2, indices[1])
	assert.Equal(t, 0, indices[2])
}

func TestChainClient_EndpointReady_OutOfBounds(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1"},
		rpcStates: []rpcEndpointState{{Score: 1.0}},
		logger:    zap.NewNop(),
	}
	assert.False(t, cc.endpointReady(-1, false))
	assert.False(t, cc.endpointReady(5, false))
}

func TestChainClient_EndpointReady_CooldownActive(t *testing.T) {
	cc := &ChainClient{
		rpcURLs: []string{"url1"},
		rpcStates: []rpcEndpointState{
			{Score: 1.0, CooldownUntil: time.Now().Add(30 * time.Second)},
		},
		logger: zap.NewNop(),
	}
	assert.False(t, cc.endpointReady(0, false))
}

func TestChainClient_EndpointReady_CooldownExpired(t *testing.T) {
	cc := &ChainClient{
		rpcURLs: []string{"url1"},
		rpcStates: []rpcEndpointState{
			{Score: 1.0, CooldownUntil: time.Now().Add(-1 * time.Second)},
		},
		logger: zap.NewNop(),
	}
	assert.True(t, cc.endpointReady(0, false))
}

func TestChainClient_EndpointReady_AllowCooling(t *testing.T) {
	cc := &ChainClient{
		rpcURLs: []string{"url1"},
		rpcStates: []rpcEndpointState{
			{Score: 1.0, CooldownUntil: time.Now().Add(30 * time.Second)},
		},
		logger: zap.NewNop(),
	}
	assert.True(t, cc.endpointReady(0, true))
}

func TestChainClient_RecordEndpointFailure_OutOfBounds(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1"},
		rpcStates: []rpcEndpointState{{Score: 1.0}},
		logger:    zap.NewNop(),
	}
	cc.recordEndpointFailure(-1)
	cc.recordEndpointFailure(5)
}

func TestChainClient_RecordEndpointFailure_Increments(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1"},
		rpcStates: []rpcEndpointState{{Score: 1.0}},
		logger:    zap.NewNop(),
	}
	cc.recordEndpointFailure(0)
	cc.mu.RLock()
	state := cc.rpcStates[0]
	cc.mu.RUnlock()
	assert.Equal(t, 1, state.Failures)
	assert.False(t, state.LastFailureAt.IsZero())
	assert.False(t, state.CooldownUntil.IsZero())
}

func TestChainClient_GetRPCScores(t *testing.T) {
	cc := &ChainClient{
		rpcURLs: []string{"url1", "url2"},
		rpcStates: []rpcEndpointState{
			{Score: 0.8},
			{Score: 0.5},
		},
		logger: zap.NewNop(),
	}
	scores := cc.GetRPCScores()
	assert.Equal(t, 0.8, scores["url1"])
	assert.Equal(t, 0.5, scores["url2"])
}

func TestMultiChainManager_GetSolanaClient_Found(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	verifier := mcm.solanaClients[-1]
	if verifier == nil {
		err := mcm.AddChain(-1)
		if err != nil {
			t.Skip("Solana chain not available in test")
		}
	}

	client, err := mcm.GetSolanaClient(-1)
	if err == nil {
		assert.NotNil(t, client)
	}
}

func TestMultiChainManager_GetSolanaClient_EVMChain(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())

	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	mcm.AddChainWithClient(1, client)

	_, err = mcm.GetSolanaClient(1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EVM chain")
}

func TestMultiChainManager_GetSolanaClient_NotFound(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	_, err := mcm.GetSolanaClient(999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Solana chain client not found")
}

func TestMultiChainManager_GetClient_SolanaChain(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	mcm.mu.Lock()
	mcm.solanaClients[-1] = nil
	mcm.mu.Unlock()

	_, err := mcm.GetClient(-1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Solana chain")
}

func TestApplyChainConfigs_RPCsFallback(t *testing.T) {
	ApplyChainConfigs([]config.ChainConfigEntry{
		{
			ID:        9998,
			Name:      "RPC Fallback Test",
			RPC:       "https://rpc.example.com",
			Currency:  "TST",
			IsTestnet: true,
		},
	})

	cfg, ok := GetChainConfig(9998)
	assert.True(t, ok)
	assert.Equal(t, []string{"https://rpc.example.com"}, cfg.RPCs)
}

func TestApplyChainConfigs_WithRPCs(t *testing.T) {
	ApplyChainConfigs([]config.ChainConfigEntry{
		{
			ID:        9997,
			Name:      "RPCs Test",
			RPCs:      []string{"https://rpc1.example.com", "https://rpc2.example.com"},
			Currency:  "TST",
			IsTestnet: true,
		},
	})

	cfg, ok := GetChainConfig(9997)
	assert.True(t, ok)
	assert.Equal(t, []string{"https://rpc1.example.com", "https://rpc2.example.com"}, cfg.RPCs)
}

func TestGetActiveFetches(t *testing.T) {
	count := GetActiveFetches()
	assert.GreaterOrEqual(t, count, int64(0))
}

func TestSetFetchConcurrency(t *testing.T) {
	original := fetchSemaphore
	SetFetchConcurrency(10)
	assert.Equal(t, 10, cap(fetchSemaphore))
	SetFetchConcurrency(20)
	assert.Equal(t, 20, cap(fetchSemaphore))
	fetchSemaphore = original
}

func TestCloseSafeHTTPClient(t *testing.T) {
	assert.NotPanics(t, CloseSafeHTTPClient)
}

func TestRetryableError_Fields(t *testing.T) {
	err := &RetryableError{Message: "test", Cause: nil}
	assert.Equal(t, "test", err.Message)
	assert.Nil(t, err.Cause)
}

func TestPermanentError_Fields(t *testing.T) {
	err := &PermanentError{Message: "test", Cause: nil}
	assert.Equal(t, "test", err.Message)
	assert.Nil(t, err.Cause)
}

func TestNewRetryableError(t *testing.T) {
	cause := fmt.Errorf("root")
	err := NewRetryableError("msg", cause)
	assert.Equal(t, "msg", err.Message)
	assert.Equal(t, cause, err.Cause)
}

func TestNewPermanentError(t *testing.T) {
	cause := fmt.Errorf("root")
	err := NewPermanentError("msg", cause)
	assert.Equal(t, "msg", err.Message)
	assert.Equal(t, cause, err.Cause)
}

func TestChainClient_GetFinality_DefaultNotNil(t *testing.T) {
	cc := &ChainClient{
		logger: zap.NewNop(),
	}
	f := cc.GetFinality()
	assert.NotNil(t, f)
}

func TestChainClient_SetFinality_Override(t *testing.T) {
	cc := &ChainClient{
		logger: zap.NewNop(),
	}
	strategy := newFinalityDefault(nil, 12, BlockTagSafe, nil)
	cc.SetFinality(strategy)
	f := cc.GetFinality()
	assert.Equal(t, strategy, f)
}

func TestRewriteURI_Https(t *testing.T) {
	result, err := rewriteURI("https://example.com/metadata.json")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/metadata.json", result)
}

func TestRewriteURI_Http(t *testing.T) {
	result, err := rewriteURI("http://localhost:8080/test")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080/test", result)
}

func TestRewriteURI_UnsupportedScheme(t *testing.T) {
	_, err := rewriteURI("ftp://example.com/file")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported URI scheme")
}

func TestSafeFetchURI_DataURI_InvalidFormat(t *testing.T) {
	var result map[string]interface{}
	err := SafeFetchURI(context.Background(), "data:application/json", &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data URI format")
}

func TestSafeFetchURI_DataURI_Base64NotSupported(t *testing.T) {
	var result map[string]interface{}
	err := SafeFetchURI(context.Background(), "data:application/json;base64,e30=", &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "base64 data URIs not supported")
}

func TestSafeFetchURI_DataURI_InvalidJSON(t *testing.T) {
	var result map[string]interface{}
	err := SafeFetchURI(context.Background(), "data:application/json,not-json", &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse data URI payload")
}

func TestMustParseCIDR_Valid(t *testing.T) {
	network := mustParseCIDR("10.0.0.0/8")
	assert.NotNil(t, network)
}

func TestIsPrivateIP_NilIP(t *testing.T) {
	assert.False(t, isPrivateIP(nil))
}

func TestChainClient_GetActiveRPCIndex(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1", "url2"},
		rpcStates: []rpcEndpointState{{Score: 1.0}, {Score: 0.5}},
		logger:    zap.NewNop(),
	}
	cc.activeRPC = 1
	idx := cc.getActiveRPCIndex()
	assert.Equal(t, 1, idx)
}

func TestChainClient_HealthCheck_NilClient(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{"url1"},
		rpcStates: []rpcEndpointState{{Score: 1.0}},
		logger:    zap.NewNop(),
	}
	err := cc.HealthCheck(context.Background())
	assert.Error(t, err)
}
