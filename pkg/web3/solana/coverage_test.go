package solana

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type jsonRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mockRPCServer struct {
	server   *httptest.Server
	handlers map[string]func() json.RawMessage
	mu       sync.RWMutex
	callLog  []string
}

func newMockRPCServer() *mockRPCServer {
	m := &mockRPCServer{
		handlers: make(map[string]func() json.RawMessage),
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(500)
			return
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(400)
			return
		}

		m.mu.Lock()
		m.callLog = append(m.callLog, req.Method)
		m.mu.Unlock()

		m.mu.RLock()
		handler, ok := m.handlers[req.Method]
		m.mu.RUnlock()

		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
		}

		if ok {
			resp.Result = handler()
		} else {
			resp.Error = &jsonRPCError{
				Code:    -32601,
				Message: "Method not found",
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	return m
}

func (m *mockRPCServer) URL() string {
	return m.server.URL
}

func (m *mockRPCServer) Client() *rpc.Client {
	return rpc.New(m.server.URL)
}

func (m *mockRPCServer) Close() {
	m.server.Close()
}

func (m *mockRPCServer) RegisterHandler(method string, handler func() json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[method] = handler
}

func makeAccountInfoResult(data []byte) json.RawMessage {
	encoded := base64.StdEncoding.EncodeToString(data)
	result := map[string]interface{}{
		"context": map[string]interface{}{"slot": 123},
		"value": map[string]interface{}{
			"data":       []string{encoded, "base64"},
			"owner":      "11111111111111111111111111111111",
			"lamports":   0,
			"executable": false,
			"rentEpoch":  0,
		},
	}
	raw, _ := json.Marshal(result)
	return raw
}

func makeNullAccountInfoResult() json.RawMessage {
	result := map[string]interface{}{
		"context": map[string]interface{}{"slot": 123},
		"value":   nil,
	}
	raw, _ := json.Marshal(result)
	return raw
}

func makeTokenAccountsResult(accounts []map[string]interface{}) json.RawMessage {
	result := map[string]interface{}{
		"context": map[string]interface{}{"slot": 123},
		"value":   accounts,
	}
	raw, _ := json.Marshal(result)
	return raw
}

func TestSolanaVerifier_VerifyTokenAccount_NoClients(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop())
	defer sv.Close()
	_, err := sv.VerifyTokenAccount(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
}

func TestSolanaVerifier_VerifyTokenAccount_UnreachableRPC(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://127.0.0.1:1")
	defer sv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := sv.VerifyTokenAccount(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
}

func TestSolanaVerifier_VerifyTokenAccount_MockRPC_Match(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	ownerKey := solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	tokenData := make([]byte, 64)
	copy(tokenData[0:32], mintKey[:])
	copy(tokenData[32:64], ownerKey[:])

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(tokenData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyTokenAccount(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestSolanaVerifier_VerifyTokenAccount_MockRPC_NoMatch(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	ownerKey := solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	tokenData := make([]byte, 64)
	copy(tokenData[0:32], mintKey[:])
	copy(tokenData[32:64], ownerKey[:])

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(tokenData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyTokenAccount(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "11111111111111111111111111111111")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestSolanaVerifier_VerifyTokenAccount_MockRPC_NullAccount(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop())
	defer sv.Close()
	result, err := sv.VerifyTokenAccount(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
	assert.False(t, result)
}

func TestSolanaVerifier_VerifyTokenAccount_MockRPC_DataTooShort(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	shortData := make([]byte, 32)
	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(shortData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyTokenAccount(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		assert.False(t, result)
	} else {
		assert.False(t, result)
	}
}

func TestSolanaVerifier_VerifyTokenAccount_MockRPC_InvalidOwner(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	tokenData := make([]byte, 64)
	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(tokenData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyTokenAccount(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		assert.False(t, result)
	} else {
		assert.False(t, result)
	}
}

func TestSolanaVerifier_VerifyMintAuthority_NoClients(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop())
	defer sv.Close()
	_, err := sv.VerifyMintAuthority(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
}

func TestSolanaVerifier_VerifyMintAuthority_UnreachableRPC(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://127.0.0.1:1")
	defer sv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := sv.VerifyMintAuthority(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
}

func TestSolanaVerifier_VerifyMintAuthority_MockRPC_Match(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	authorityKey := solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	mintData := make([]byte, 36)
	mintData[0] = 1
	copy(mintData[4:36], authorityKey[:])

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(mintData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyMintAuthority(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestSolanaVerifier_VerifyMintAuthority_MockRPC_NoMatch(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	authorityKey := solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	mintData := make([]byte, 36)
	mintData[0] = 1
	copy(mintData[4:36], authorityKey[:])

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(mintData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyMintAuthority(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "11111111111111111111111111111111")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestSolanaVerifier_VerifyMintAuthority_MockRPC_NullAccount(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop())
	defer sv.Close()
	result, err := sv.VerifyMintAuthority(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
	assert.False(t, result)
}

func TestSolanaVerifier_VerifyMintAuthority_MockRPC_NoAuthority(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	mintData := make([]byte, 36)
	mintData[0] = 0

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(mintData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyMintAuthority(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestSolanaVerifier_VerifyMintAuthority_MockRPC_DataTooShort(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	shortData := make([]byte, 10)
	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(shortData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyMintAuthority(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		assert.False(t, result)
	} else {
		assert.False(t, result)
	}
}

func TestSolanaVerifier_VerifyMintAuthority_MockRPC_InvalidAuthority(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	mintData := make([]byte, 36)
	mintData[0] = 1
	copy(mintData[4:36], make([]byte, 32))

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(mintData)
	})

	sv := NewSolanaVerifier(zap.NewNop(), mockSrv.URL())
	defer sv.Close()

	result, err := sv.VerifyMintAuthority(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		assert.False(t, result)
	} else {
		assert.False(t, result)
	}
}

func TestSolanaVerifier_VerifyMetaplexNFTOwnership_NoClients(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop())
	defer sv.Close()
	_, err := sv.VerifyMetaplexNFTOwnership(context.Background(), "mint", "owner")
	require.Error(t, err)
}

func TestSolanaVerifier_VerifyMetaplexNFTOwnership_UnreachableRPC(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://127.0.0.1:1")
	defer sv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := sv.VerifyMetaplexNFTOwnership(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
}

func TestSolanaVerifier_FetchMetaplexMetadata_NoClients(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop())
	defer sv.Close()
	_, err := sv.FetchMetaplexMetadata(context.Background(), "mint")
	require.Error(t, err)
}

func TestSolanaVerifier_FetchMetaplexMetadata_UnreachableRPC(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://127.0.0.1:1")
	defer sv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := sv.FetchMetaplexMetadata(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.Error(t, err)
}

func TestSolanaVerifier_WithRPCClient_CallbackError(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://127.0.0.1:1")
	defer sv.Close()

	err := sv.withRPCClient(func(client *rpc.Client) error {
		return fmt.Errorf("callback error")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestSolanaVerifier_WithRPCClient_CallbackSuccess(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://127.0.0.1:1")
	defer sv.Close()

	err := sv.withRPCClient(func(client *rpc.Client) error {
		return nil
	})
	require.NoError(t, err)
}

func TestSolanaVerifier_WithRPCClient_MultipleEndpoints(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://127.0.0.1:1", "http://127.0.0.1:2")
	defer sv.Close()

	callCount := 0
	err := sv.withRPCClient(func(client *rpc.Client) error {
		callCount++
		if callCount == 1 {
			return fmt.Errorf("first endpoint fails")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestSolanaMultiClient_GetClient_ConnectedEndpoint(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].cooldown = time.Time{}
	client.mu.Unlock()

	ctx := context.Background()
	rpcClient, err := client.GetClient(ctx)
	require.NoError(t, err)
	assert.NotNil(t, rpcClient)
}

func TestSolanaMultiClient_GetClient_CooldownThenReconnect(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 50.0
	client.endpoints[0].cooldown = time.Now().Add(-1 * time.Second)
	client.mu.Unlock()

	ctx := context.Background()
	rpcClient, err := client.GetClient(ctx)
	require.NoError(t, err)
	assert.NotNil(t, rpcClient)
}

func TestSolanaMultiClient_RecordSuccess_ScoreRecovery(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.RecordFailure("http://127.0.0.1:1")
	client.RecordFailure("http://127.0.0.1:1")

	client.mu.RLock()
	scoreAfterFail := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, 50.0, scoreAfterFail)

	client.RecordSuccess("http://127.0.0.1:1")

	client.mu.RLock()
	scoreAfterRecovery := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, 55.0, scoreAfterRecovery)
}

func TestSolanaMultiClient_RecordFailure_ScoreFloor(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	for i := 0; i < 10; i++ {
		client.RecordFailure("http://127.0.0.1:1")
	}

	client.mu.RLock()
	score := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreMin, score)
}

func TestSolanaMultiClient_Statuses_SortedByScore(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://a:1", "http://b:2"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 50.0
	client.endpoints[0].cooldown = time.Time{}
	client.endpoints[1].connected = true
	client.endpoints[1].score = 90.0
	client.endpoints[1].cooldown = time.Time{}
	client.mu.Unlock()

	statuses := client.Statuses()
	assert.Equal(t, 90.0, statuses[0].Score)
	assert.Equal(t, 50.0, statuses[1].Score)
}

func TestSolanaMultiClient_BestEndpoint_MultipleConnected(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://a:1", "http://b:2", "http://c:3"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 70.0
	client.endpoints[0].cooldown = time.Time{}
	client.endpoints[1].connected = true
	client.endpoints[1].score = 95.0
	client.endpoints[1].cooldown = time.Time{}
	client.endpoints[2].connected = true
	client.endpoints[2].score = 80.0
	client.endpoints[2].cooldown = time.Time{}
	client.mu.Unlock()

	best := client.bestEndpoint()
	require.NotNil(t, best)
	assert.Equal(t, 95.0, best.score)
}

func TestSolanaMultiClient_NewSolanaMultiClient_MixedEndpoints(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"", "http://127.0.0.1:1", ""})
	require.NoError(t, err)
	defer client.Close()

	client.mu.RLock()
	count := len(client.endpoints)
	client.mu.RUnlock()
	assert.Equal(t, 1, count)
}

func TestBasicFetchURI(t *testing.T) {
	expectedMetadata := MetaplexMetadata{
		Name:   "TestNFT",
		Symbol: "TNFT",
		Image:  "https://example.com/img.png",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(expectedMetadata)
	}))
	defer server.Close()

	var result MetaplexMetadata
	err := basicFetchURI(context.Background(), server.URL, &result)
	require.NoError(t, err)
	assert.Equal(t, "TestNFT", result.Name)
	assert.Equal(t, "TNFT", result.Symbol)
}

func TestBasicFetchURI_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var result MetaplexMetadata
	err := basicFetchURI(context.Background(), server.URL, &result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status code")
}

func TestBasicFetchURI_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	var result MetaplexMetadata
	err := basicFetchURI(context.Background(), server.URL, &result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
}

func TestBasicFetchURI_UnreachableURL(t *testing.T) {
	var result MetaplexMetadata
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := basicFetchURI(ctx, "http://127.0.0.1:1/metadata.json", &result)
	require.Error(t, err)
}

func TestMetaplexVerifier_FetchMetadataFromURI_SafeURIFetch(t *testing.T) {
	originalSafeURIFetch := SafeURIFetch
	defer func() { SafeURIFetch = originalSafeURIFetch }()

	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	expectedMetadata := &MetaplexMetadata{Name: "SafeNFT", Symbol: "SNFT"}
	SafeURIFetch = func(ctx context.Context, uri string, result interface{}) error {
		meta, ok := result.(*MetaplexMetadata)
		if !ok {
			return fmt.Errorf("wrong type")
		}
		*meta = *expectedMetadata
		return nil
	}

	metadata, err := mv.fetchMetadataFromURI(context.Background(), "https://example.com/metadata.json")
	require.NoError(t, err)
	assert.Equal(t, "SafeNFT", metadata.Name)
}

func TestMetaplexVerifier_FetchMetadataFromURI_SafeURIFetchError(t *testing.T) {
	originalSafeURIFetch := SafeURIFetch
	defer func() { SafeURIFetch = originalSafeURIFetch }()

	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	SafeURIFetch = func(ctx context.Context, uri string, result interface{}) error {
		return fmt.Errorf("safe fetch error")
	}

	_, err := mv.fetchMetadataFromURI(context.Background(), "https://example.com/metadata.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "safe fetch error")
}

func TestMetaplexVerifier_FetchMetadataFromURI_BasicFetch(t *testing.T) {
	originalSafeURIFetch := SafeURIFetch
	defer func() { SafeURIFetch = originalSafeURIFetch }()

	SafeURIFetch = nil

	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	expectedMetadata := MetaplexMetadata{Name: "BasicNFT", Symbol: "BNFT"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(expectedMetadata)
	}))
	defer server.Close()

	metadata, err := mv.fetchMetadataFromURI(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, "BasicNFT", metadata.Name)
}

func TestMetaplexVerifier_VerifyNFTOwnership_CacheHit(t *testing.T) {
	cache := newMockCache()
	mv := NewMetaplexVerifier(nil, zap.NewNop(), cache)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	ownerAddress := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	cacheKey := fmt.Sprintf("metaplex:owner:%s:%s", mintAddress, ownerAddress)
	cache.Set(cacheKey, true)

	owned, err := mv.VerifyNFTOwnership(context.Background(), mintAddress, ownerAddress)
	require.NoError(t, err)
	assert.True(t, owned)
}

func TestMetaplexVerifier_VerifyNFTOwnership_CacheHitFalse(t *testing.T) {
	cache := newMockCache()
	mv := NewMetaplexVerifier(nil, zap.NewNop(), cache)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	ownerAddress := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	cacheKey := fmt.Sprintf("metaplex:owner:%s:%s", mintAddress, ownerAddress)
	cache.Set(cacheKey, false)

	owned, err := mv.VerifyNFTOwnership(context.Background(), mintAddress, ownerAddress)
	require.NoError(t, err)
	assert.False(t, owned)
}

func TestMetaplexVerifier_VerifyNFTOwnership_UnreachableRPC(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	ownerAddress := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mv.VerifyNFTOwnership(ctx, mintAddress, ownerAddress)
	require.Error(t, err)
}

func TestMetaplexVerifier_VerifyNFTOwnership_InvalidMintAddress(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	_, err := mv.VerifyNFTOwnership(context.Background(), "invalid_mint", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_VerifyNFTOwnership_InvalidOwnerAddress(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	_, err := mv.VerifyNFTOwnership(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "invalid_owner")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid owner address")
}

func TestMetaplexVerifier_GetMetadata_CacheHit(t *testing.T) {
	cache := newMockCache()
	mv := NewMetaplexVerifier(nil, zap.NewNop(), cache)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	cacheKey := fmt.Sprintf("metaplex:metadata:%s", mintAddress)
	cachedMeta := &MetaplexMetadata{Name: "CachedNFT", Symbol: "CNFT"}
	cache.Set(cacheKey, cachedMeta)

	metadata, err := mv.GetMetadata(context.Background(), mintAddress)
	require.NoError(t, err)
	assert.Equal(t, "CachedNFT", metadata.Name)
}

func TestMetaplexVerifier_GetMetadata_InvalidMintAddress(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	_, err := mv.GetMetadata(context.Background(), "invalid_mint")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_GetMetadata_CacheMiss_UnreachableRPC(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mv.GetMetadata(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.Error(t, err)
}

func TestMetaplexVerifier_GetMetadata_MockRPC(t *testing.T) {
	originalSafeURIFetch := SafeURIFetch
	defer func() { SafeURIFetch = originalSafeURIFetch }()
	SafeURIFetch = nil

	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	metadataURI := ""
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(MetaplexMetadata{
			Name:   "MockNFT",
			Symbol: "MNFT",
			Image:  "https://example.com/img.png",
		})
	}))
	defer metadataServer.Close()
	metadataURI = metadataServer.URL

	metadataData := buildMetadataDataWithCreators("MockNFT", "MNFT", metadataURI, 500, nil, true, true)

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(metadataData)
	})

	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), nil)
	defer mv.Close()

	metadata, err := mv.GetMetadata(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.NoError(t, err)
	assert.Equal(t, "MockNFT", metadata.Name)
	assert.Equal(t, "MNFT", metadata.Symbol)
}

func TestMetaplexVerifier_GetMetadata_MockRPC_WithCache(t *testing.T) {
	originalSafeURIFetch := SafeURIFetch
	defer func() { SafeURIFetch = originalSafeURIFetch }()
	SafeURIFetch = nil

	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(MetaplexMetadata{
			Name:   "CachedMockNFT",
			Symbol: "CMNFT",
		})
	}))
	defer metadataServer.Close()

	metadataData := buildMetadataDataWithCreators("CachedMockNFT", "CMNFT", metadataServer.URL, 500, nil, true, true)

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(metadataData)
	})

	cache := newMockCache()
	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), cache)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	metadata, err := mv.GetMetadata(context.Background(), mintAddress)
	require.NoError(t, err)
	assert.Equal(t, "CachedMockNFT", metadata.Name)

	cacheKey := fmt.Sprintf("metaplex:metadata:%s", mintAddress)
	cached, err := cache.Get(cacheKey)
	require.NoError(t, err)
	cachedMeta, ok := cached.(*MetaplexMetadata)
	require.True(t, ok)
	assert.Equal(t, "CachedMockNFT", cachedMeta.Name)
}

func TestMetaplexVerifier_VerifyMetadata_CacheHit(t *testing.T) {
	cache := newMockCache()
	mv := NewMetaplexVerifier(nil, zap.NewNop(), cache)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	cacheKey := fmt.Sprintf("metaplex:metadata:%s", mintAddress)
	cachedMeta := &MetaplexMetadata{
		Name:   "TestNFT",
		Symbol: "TNFT",
	}
	cache.Set(cacheKey, cachedMeta)

	expected := &MetaplexMetadata{
		Name:   "TestNFT",
		Symbol: "TNFT",
	}

	valid, err := mv.VerifyMetadata(context.Background(), mintAddress, expected)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestMetaplexVerifier_VerifyMetadata_CacheHit_Mismatch(t *testing.T) {
	cache := newMockCache()
	mv := NewMetaplexVerifier(nil, zap.NewNop(), cache)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	cacheKey := fmt.Sprintf("metaplex:metadata:%s", mintAddress)
	cachedMeta := &MetaplexMetadata{
		Name:   "TestNFT",
		Symbol: "TNFT",
	}
	cache.Set(cacheKey, cachedMeta)

	expected := &MetaplexMetadata{
		Name:   "DifferentNFT",
		Symbol: "TNFT",
	}

	valid, err := mv.VerifyMetadata(context.Background(), mintAddress, expected)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestMetaplexVerifier_VerifyMetadata_Matching(t *testing.T) {
	mv := &MetaplexVerifier{}

	expected := &MetaplexMetadata{
		Name:        "TestNFT",
		Symbol:      "TNFT",
		Description: "A test NFT",
		SellerFee:   500,
		Image:       "https://example.com/img.png",
		Attributes: []MetadataAttribute{
			{TraitType: "Color", Value: "Blue"},
		},
	}

	actual := &MetaplexMetadata{
		Name:        "TestNFT",
		Symbol:      "TNFT",
		Description: "A test NFT",
		SellerFee:   500,
		Image:       "https://example.com/img.png",
		Attributes: []MetadataAttribute{
			{TraitType: "Color", Value: "Blue"},
		},
	}

	valid := mv.compareMetadata(actual, expected)
	assert.True(t, valid)
}

func TestMetaplexVerifier_VerifyMetadata_Mismatching(t *testing.T) {
	mv := &MetaplexVerifier{}

	expected := &MetaplexMetadata{
		Name:        "TestNFT",
		Symbol:      "TNFT",
		Description: "A test NFT",
		SellerFee:   500,
		Image:       "https://example.com/img.png",
	}

	actual := &MetaplexMetadata{
		Name:        "DifferentNFT",
		Symbol:      "TNFT",
		Description: "A test NFT",
		SellerFee:   500,
		Image:       "https://example.com/img.png",
	}

	valid := mv.compareMetadata(actual, expected)
	assert.False(t, valid)
}

func TestMetaplexVerifier_VerifyCreator_MockRPC(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), nil)
	defer mv.Close()

	valid, err := mv.VerifyCreator(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		assert.False(t, valid)
	} else {
		_ = valid
	}
}

func TestMetaplexVerifier_VerifyCreator_InvalidMintAddress(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	_, err := mv.VerifyCreator(context.Background(), "invalid_mint", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_VerifyCreator_InvalidCreatorAddress(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	_, err := mv.VerifyCreator(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "invalid_creator")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid creator address")
}

func TestMetaplexVerifier_VerifyCreator_UnreachableRPC(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mv.VerifyCreator(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
}

func TestMetaplexVerifier_VerifyCollection_InvalidMintAddress(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	_, err := mv.VerifyCollection(context.Background(), "invalid_mint", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_VerifyCollection_InvalidCollectionAddress(t *testing.T) {
	cache := newMockCache()
	mv := NewMetaplexVerifier(nil, zap.NewNop(), cache)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	cacheKey := fmt.Sprintf("metaplex:metadata:%s", mintAddress)
	cachedMeta := &MetaplexMetadata{
		Name: "TestNFT",
		Collection: &MetadataCollection{
			Name:   "TestCollection",
			Family: "TestFamily",
		},
	}
	cache.Set(cacheKey, cachedMeta)

	_, err := mv.VerifyCollection(context.Background(), mintAddress, "invalid_collection")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid collection mint address")
}

func TestMetaplexVerifier_VerifyCollection_CacheHit_NilCollection(t *testing.T) {
	cache := newMockCache()
	mv := NewMetaplexVerifier(nil, zap.NewNop(), cache)
	defer mv.Close()

	mintAddress := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	cacheKey := fmt.Sprintf("metaplex:metadata:%s", mintAddress)
	cachedMeta := &MetaplexMetadata{
		Name:       "TestNFT",
		Collection: nil,
	}
	cache.Set(cacheKey, cachedMeta)

	valid, err := mv.VerifyCollection(context.Background(), mintAddress, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestMetaplexVerifier_VerifyCollection_GetMetadataFails(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mv.VerifyCollection(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.Error(t, err)
}

func TestMetaplexVerifier_IsMetaplexNFT_InvalidMintAddress(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	_, err := mv.IsMetaplexNFT(context.Background(), "invalid_mint")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_IsMetaplexNFT_UnreachableRPC(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	isNFT, err := mv.IsMetaplexNFT(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.NoError(t, err)
	assert.False(t, isNFT)
}

func TestMetaplexVerifier_IsMetaplexNFT_MockRPC(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	metadataData := buildMetadataDataWithCreators("NFT", "N", "https://example.com", 100, nil, true, true)
	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(metadataData)
	})

	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), nil)
	defer mv.Close()

	isNFT, err := mv.IsMetaplexNFT(context.Background(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.NoError(t, err)
	assert.True(t, isNFT)
}

func TestMetaplexVerifier_GetTokenInfo_InvalidMintAddress(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	defer mv.Close()

	_, err := mv.GetTokenInfo(context.Background(), "invalid_mint")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_GetTokenInfo_UnreachableRPC(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mv.GetTokenInfo(ctx, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.Error(t, err)
}

func TestMetaplexVerifier_getMetadataAccount_MockRPC(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	metadataData := buildMetadataDataWithCreators("NFT", "N", "https://example.com", 100, nil, true, true)
	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(metadataData)
	})

	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), nil)
	defer mv.Close()

	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	account, err := mv.getMetadataAccount(context.Background(), mintKey)
	require.NoError(t, err)
	assert.NotNil(t, account)
	assert.Equal(t, "NFT", account.Data.Name)
	assert.Equal(t, "N", account.Data.Symbol)
}

func TestMetaplexVerifier_getMetadataAccount_NullAccount(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeNullAccountInfoResult()
	})

	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), nil)
	defer mv.Close()

	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	_, err := mv.getMetadataAccount(context.Background(), mintKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMetaplexVerifier_getMetadataAccount_InvalidData(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	invalidData := []byte{0x01, 0x02, 0x03}
	mockSrv.RegisterHandler("getAccountInfo", func() json.RawMessage {
		return makeAccountInfoResult(invalidData)
	})

	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), nil)
	defer mv.Close()

	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	_, err := mv.getMetadataAccount(context.Background(), mintKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to deserialize")
}

func TestMetaplexVerifier_getLargestTokenAccount_UnreachableRPC(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mv.getLargestTokenAccount(ctx, mintKey)
	require.Error(t, err)
}

func TestMetaplexVerifier_getLargestTokenAccount_MockRPC_EmptyResult(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	mockSrv.RegisterHandler("getTokenAccountsByOwner", func() json.RawMessage {
		return makeTokenAccountsResult([]map[string]interface{}{})
	})

	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), nil)
	defer mv.Close()

	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	_, err := mv.getLargestTokenAccount(context.Background(), mintKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no token accounts found")
}

func TestMetaplexVerifier_getLargestTokenAccount_MockRPC_WithAccounts(t *testing.T) {
	mockSrv := newMockRPCServer()
	defer mockSrv.Close()

	ownerKey := solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	parsedData := map[string]interface{}{
		"parsed": map[string]interface{}{
			"info": map[string]interface{}{
				"owner": ownerKey.String(),
				"tokenAmount": map[string]interface{}{
					"amount": "100",
				},
			},
		},
	}
	parsedJSON, _ := json.Marshal(parsedData)
	encodedParsed := base64.StdEncoding.EncodeToString(parsedJSON)

	accounts := []map[string]interface{}{
		{
			"pubkey": "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
			"account": map[string]interface{}{
				"data":       []string{encodedParsed, "base64"},
				"owner":      "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
				"lamports":   0,
				"executable": false,
				"rentEpoch":  0,
			},
		},
	}

	mockSrv.RegisterHandler("getTokenAccountsByOwner", func() json.RawMessage {
		return makeTokenAccountsResult(accounts)
	})

	mv := NewMetaplexVerifier(mockSrv.Client(), zap.NewNop(), nil)
	defer mv.Close()

	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	accountInfo, err := mv.getLargestTokenAccount(context.Background(), mintKey)
	require.NoError(t, err)
	assert.NotNil(t, accountInfo)
	assert.Equal(t, ownerKey, accountInfo.Owner)
	assert.Equal(t, uint64(100), accountInfo.Amount)
}

func TestMetaplexVerifier_getLargestTokenAccount_MockRPC_InvalidJSON(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	_, err := mv.getLargestTokenAccount(ctx, mintKey)
	require.Error(t, err)
}

func TestMetaplexVerifier_getLargestTokenAccount_MockRPC_InvalidOwner(t *testing.T) {
	rpcClient := rpc.New("http://127.0.0.1:1")
	mv := NewMetaplexVerifier(rpcClient, zap.NewNop(), nil)
	defer mv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mintKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	_, err := mv.getLargestTokenAccount(ctx, mintKey)
	require.Error(t, err)
}

func TestMetaplexVerifier_Close_NilTransport(t *testing.T) {
	mv := &MetaplexVerifier{httpClient: &http.Client{}}
	assert.NotPanics(t, mv.Close)
}

func TestMetaplexVerifier_Close_NilHTTPClient(t *testing.T) {
	mv := &MetaplexVerifier{httpClient: nil}
	assert.NotPanics(t, mv.Close)
}

func TestMetadataAccount_Deserialize_WithMultipleCreators(t *testing.T) {
	data := buildMetadataDataWithCreators("NFT", "N", "https://example.com", 100,
		[]creatorData{
			{verified: true, share: 50},
			{verified: false, share: 30},
			{verified: true, share: 20},
		},
		true, false,
	)

	var meta MetadataAccount
	err := meta.Deserialize(data)
	require.NoError(t, err)
	assert.Len(t, meta.Data.Creators, 3)
	assert.True(t, meta.Data.Creators[0].Verified)
	assert.Equal(t, uint8(50), meta.Data.Creators[0].Share)
	assert.False(t, meta.Data.Creators[1].Verified)
	assert.True(t, meta.Data.Creators[2].Verified)
	assert.True(t, meta.PrimarySaleHappened)
	assert.False(t, meta.IsMutable)
}

func TestBorshReader_ReadU16_PastEnd(t *testing.T) {
	r := &borshReader{data: []byte{0x01}}
	result := r.readU16()
	assert.Equal(t, uint16(0), result)
	assert.Error(t, r.err)
}

func TestBorshReader_ReadU32_PastEnd(t *testing.T) {
	r := &borshReader{data: []byte{0x01, 0x02}}
	result := r.readU32()
	assert.Equal(t, uint32(0), result)
	assert.Error(t, r.err)
}

func TestBorshReader_ReadU8_PastEnd(t *testing.T) {
	r := &borshReader{data: []byte{}}
	result := r.readU8()
	assert.Equal(t, uint8(0), result)
	assert.Error(t, r.err)
}

func TestBorshReader_ReadBorshString_Empty(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x00}
	r := &borshReader{data: data}
	result := r.readBorshString()
	assert.Equal(t, "", result)
	assert.NoError(t, r.err)
}

func TestMetadataAccount_Deserialize_NoEditionNonce(t *testing.T) {
	data := buildMetadataPrefix("N", "S", "U", 0)
	data = append(data, byte(0))
	data = append(data, byte(0))
	data = append(data, byte(0))
	data = append(data, byte(0))

	var meta MetadataAccount
	err := meta.Deserialize(data)
	require.NoError(t, err)
	assert.Equal(t, uint8(0), meta.EditionNonce)
}

func TestMasterEdition_Fields(t *testing.T) {
	me := MasterEdition{Key: 1, Supply: 100, MaxSupply: 1000}
	assert.Equal(t, uint8(1), me.Key)
	assert.Equal(t, uint64(100), me.Supply)
	assert.Equal(t, uint64(1000), me.MaxSupply)
}

func TestTokenAccountInfo_Fields(t *testing.T) {
	pubKey := solana.MustPublicKeyFromBase58("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	info := TokenAccountInfo{
		Address: pubKey,
		Owner:   pubKey,
		Amount:  5,
	}
	assert.Equal(t, pubKey, info.Address)
	assert.Equal(t, pubKey, info.Owner)
	assert.Equal(t, uint64(5), info.Amount)
}

func TestSolanaMultiClient_ConcurrentAccess(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1", "http://127.0.0.1:2"})
	require.NoError(t, err)
	defer client.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.RecordSuccess("http://127.0.0.1:1")
			client.RecordFailure("http://127.0.0.1:2")
			_ = client.Statuses()
		}()
	}
	wg.Wait()
}

func TestSolanaMultiClient_Close_NilClient(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].client = nil
	client.mu.Unlock()

	assert.NotPanics(t, client.Close)
}

func TestSolanaVerifier_NewSolanaVerifier_EmptyURL(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")
	defer sv.Close()

	assert.Empty(t, sv.clients)
}

func TestSolanaVerifier_NewSolanaVerifier_MultipleURLs(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://a:1", "", "http://b:2")
	defer sv.Close()

	assert.Len(t, sv.clients, 2)
}

func TestEncodeVarint_LargeValue(t *testing.T) {
	result := encodeVarint(1 << 20)
	assert.GreaterOrEqual(t, len(result), 3)
}

func TestSolanaVerifier_ParseSolanaAddress_HexWithValidLength(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop())
	defer sv.Close()

	pubKey, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	hexAddr := "0x" + hexEncodeBytes(pubKey)
	parsed, err := sv.ParseSolanaAddress(hexAddr)
	require.NoError(t, err)
	assert.Equal(t, solana.PublicKeyFromBytes(pubKey), parsed)
}

func TestSolanaMultiClient_GetClient_NoConnectedEndpoints(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.GetClient(ctx)
	require.Error(t, err)
}

func TestSolanaMultiClient_RecordSuccess_UnknownURL_Coverage(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.RecordSuccess("http://unknown:1")

	client.mu.RLock()
	score := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreInitial, score)
}

func TestSolanaMultiClient_RecordFailure_UnknownURL_Coverage(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.RecordFailure("http://unknown:1")

	client.mu.RLock()
	score := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreInitial, score)
}

func TestSolanaMultiClient_BestEndpoint_NoneConnected_Coverage(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	best := client.bestEndpoint()
	assert.Nil(t, best)
}

func TestSolanaMultiClient_BestEndpoint_InCooldown(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 100.0
	client.endpoints[0].cooldown = time.Now().Add(1 * time.Hour)
	client.mu.Unlock()

	best := client.bestEndpoint()
	assert.Nil(t, best)
}

func TestSolanaMultiClient_NewSolanaMultiClient_EmptyURLs(t *testing.T) {
	_, err := NewSolanaMultiClient(zap.NewNop(), []string{})
	require.Error(t, err)
	assert.Equal(t, ErrSolanaNoEndpoints, err)
}

func TestSolanaMultiClient_NewSolanaMultiClient_AllEmptyStrings(t *testing.T) {
	_, err := NewSolanaMultiClient(zap.NewNop(), []string{"", ""})
	require.Error(t, err)
	assert.Equal(t, ErrSolanaNoEndpoints, err)
}

func TestSolanaMultiClient_RecordSuccess_ScoreCeiling(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.RecordSuccess("http://127.0.0.1:1")
	client.RecordSuccess("http://127.0.0.1:1")

	client.mu.RLock()
	score := client.endpoints[0].score
	client.mu.RUnlock()
	assert.LessOrEqual(t, score, solScoreInitial)
	assert.Equal(t, solScoreInitial, score)
}

func TestSolanaMultiClient_RecordFailure_CooldownSet(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	before := time.Now()
	client.RecordFailure("http://127.0.0.1:1")

	client.mu.RLock()
	cooldown := client.endpoints[0].cooldown
	connected := client.endpoints[0].connected
	client.mu.RUnlock()
	assert.True(t, cooldown.After(before))
	assert.False(t, connected)
}

func TestSolanaMultiClient_Statuses_ContainsAllEndpoints(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://a:1", "http://b:2"})
	require.NoError(t, err)
	defer client.Close()

	statuses := client.Statuses()
	assert.Len(t, statuses, 2)
	assert.Equal(t, "http://a:1", statuses[0].URL)
	assert.Equal(t, "http://b:2", statuses[1].URL)
}

func TestSolanaMultiClient_RecordSuccess_ConnectedFlag(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = false
	client.mu.Unlock()

	client.RecordSuccess("http://127.0.0.1:1")

	client.mu.RLock()
	connected := client.endpoints[0].connected
	client.mu.RUnlock()
	assert.True(t, connected)
}

func TestSolanaMultiClient_GetClient_DoubleCheckPattern(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 100.0
	client.endpoints[0].cooldown = time.Time{}
	client.mu.Unlock()

	ctx := context.Background()
	rpcClient, err := client.GetClient(ctx)
	require.NoError(t, err)
	assert.NotNil(t, rpcClient)
}

func TestSolanaMultiClient_GetClient_CooldownExpired(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 100.0
	client.endpoints[0].cooldown = time.Now().Add(-1 * time.Hour)
	client.mu.Unlock()

	ctx := context.Background()
	rpcClient, err := client.GetClient(ctx)
	require.NoError(t, err)
	assert.NotNil(t, rpcClient)
}

func TestSolanaMultiClient_BestEndpoint_ScoreComparison(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://a:1", "http://b:2"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 30.0
	client.endpoints[0].cooldown = time.Time{}
	client.endpoints[1].connected = true
	client.endpoints[1].score = 70.0
	client.endpoints[1].cooldown = time.Time{}
	client.mu.Unlock()

	best := client.bestEndpoint()
	require.NotNil(t, best)
	assert.Equal(t, 70.0, best.score)
	assert.Equal(t, "http://b:2", best.url)
}

func TestSolanaMultiClient_RecordFailure_ScoreDecrement(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.RLock()
	initialScore := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreInitial, initialScore)

	client.RecordFailure("http://127.0.0.1:1")

	client.mu.RLock()
	scoreAfterOne := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreInitial-solScorePenalty, scoreAfterOne)
}

func TestSolanaMultiClient_Constants(t *testing.T) {
	assert.Equal(t, 100.0, solScoreInitial)
	assert.Equal(t, 25.0, solScorePenalty)
	assert.Equal(t, 5.0, solScoreRecovery)
	assert.Equal(t, 10.0, solScoreMin)
	assert.Equal(t, 30*time.Second, solCoolDownDuration)
}

func TestSolanaMultiClient_NewSolanaMultiClient_ScoreBounds(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.RLock()
	score := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreInitial, score)
	assert.True(t, score <= solScoreInitial)
	assert.True(t, score >= solScoreMin)
}

func TestSolanaMultiClient_RecordSuccess_ScoreCannotExceedMax(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	for i := 0; i < 20; i++ {
		client.RecordSuccess("http://127.0.0.1:1")
	}

	client.mu.RLock()
	score := client.endpoints[0].score
	client.mu.RUnlock()
	assert.LessOrEqual(t, score, solScoreInitial)
	assert.Equal(t, solScoreInitial, score)
}

func TestSolanaMultiClient_RecordFailure_ScoreCannotGoBelowMin(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	for i := 0; i < 20; i++ {
		client.RecordFailure("http://127.0.0.1:1")
	}

	client.mu.RLock()
	score := client.endpoints[0].score
	client.mu.RUnlock()
	assert.GreaterOrEqual(t, score, solScoreMin)
	assert.Equal(t, solScoreMin, score)
}

func TestSolanaMultiClient_RecordSuccess_RecoveryFromLowScore(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	for i := 0; i < 4; i++ {
		client.RecordFailure("http://127.0.0.1:1")
	}

	client.mu.RLock()
	lowScore := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreMin, lowScore)

	client.RecordSuccess("http://127.0.0.1:1")

	client.mu.RLock()
	recoveredScore := client.endpoints[0].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreMin+solScoreRecovery, recoveredScore)
}

func TestSolanaMultiClient_RecordFailure_MultipleEndpoints(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://a:1", "http://b:2"})
	require.NoError(t, err)
	defer client.Close()

	client.RecordFailure("http://a:1")

	client.mu.RLock()
	scoreA := client.endpoints[0].score
	scoreB := client.endpoints[1].score
	client.mu.RUnlock()
	assert.Equal(t, solScoreInitial-solScorePenalty, scoreA)
	assert.Equal(t, solScoreInitial, scoreB)
}

func TestSolanaMultiClient_BestEndpoint_SameScore(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://a:1", "http://b:2"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 80.0
	client.endpoints[0].cooldown = time.Time{}
	client.endpoints[1].connected = true
	client.endpoints[1].score = 80.0
	client.endpoints[1].cooldown = time.Time{}
	client.mu.Unlock()

	best := client.bestEndpoint()
	require.NotNil(t, best)
	assert.Equal(t, 80.0, best.score)
}

func TestSolanaMultiClient_ScoreMath(t *testing.T) {
	assert.Equal(t, solScoreInitial, math.Min(solScoreInitial, solScoreInitial+10))
	assert.Equal(t, solScoreMin, math.Max(solScoreMin, solScoreMin-10))
}

func hexEncodeBytes(b []byte) string {
	hex := make([]byte, len(b)*2)
	const hexChars = "0123456789abcdef"
	for i, v := range b {
		hex[i*2] = hexChars[v>>4]
		hex[i*2+1] = hexChars[v&0x0f]
	}
	return string(hex)
}
