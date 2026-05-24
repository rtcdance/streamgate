package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockWeb3StatusProvider struct {
	statuses map[int64][]web3.RPCStatus
	chains   []*web3.ChainConfig
}

func (m *mockWeb3StatusProvider) GetRPCStatuses() map[int64][]web3.RPCStatus {
	return m.statuses
}

func (m *mockWeb3StatusProvider) GetSupportedChains() []*web3.ChainConfig {
	return m.chains
}

func setupWeb3Router(provider Web3StatusProvider) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterWeb3Routes(r, zap.NewNop(), provider)
	return r
}

func TestWeb3Handlers_RPCStatus_Empty(t *testing.T) {
	provider := &mockWeb3StatusProvider{
		statuses: map[int64][]web3.RPCStatus{},
		chains:   []*web3.ChainConfig{},
	}
	r := setupWeb3Router(provider)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", http.NoBody)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	chains, ok := resp["chains"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, chains)
}

func TestWeb3Handlers_RPCStatus_WithData(t *testing.T) {
	provider := &mockWeb3StatusProvider{
		statuses: map[int64][]web3.RPCStatus{
			1: {
				{URL: "https://rpc1.example.com", IsActive: true, Failures: 0},
				{URL: "https://rpc2.example.com", IsActive: false, Failures: 3},
			},
		},
		chains: []*web3.ChainConfig{
			{ID: 1, Name: "Ethereum"},
		},
	}
	r := setupWeb3Router(provider)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", http.NoBody)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	chains, ok := resp["chains"].([]interface{})
	require.True(t, ok)
	assert.Len(t, chains, 1)

	chain := chains[0].(map[string]interface{})
	assert.Equal(t, float64(1), chain["chain_id"])
	assert.Equal(t, "Ethereum", chain["name"])

	rpcs, ok := chain["rpcs"].([]interface{})
	require.True(t, ok)
	assert.Len(t, rpcs, 2)

	rpc0 := rpcs[0].(map[string]interface{})
	assert.Equal(t, "https://rpc1.example.com", rpc0["url"])
	assert.Equal(t, true, rpc0["is_active"])
}

func TestWeb3Handlers_RPCStatus_WithFailureTimestamps(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	provider := &mockWeb3StatusProvider{
		statuses: map[int64][]web3.RPCStatus{
			1: {
				{
					URL:           "https://rpc.example.com",
					IsActive:      false,
					Failures:      5,
					LastFailureAt: ts,
					CooldownUntil: ts.Add(5 * time.Minute),
				},
			},
		},
		chains: []*web3.ChainConfig{{ID: 1, Name: "Ethereum"}},
	}
	r := setupWeb3Router(provider)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", http.NoBody)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	chains := resp["chains"].([]interface{})
	chain := chains[0].(map[string]interface{})
	rpcs := chain["rpcs"].([]interface{})
	rpc0 := rpcs[0].(map[string]interface{})
	assert.Contains(t, rpc0, "last_failure_at")
	assert.Contains(t, rpc0, "cooldown_until")
}

func TestWeb3Handlers_RPCStatus_MultipleChains(t *testing.T) {
	provider := &mockWeb3StatusProvider{
		statuses: map[int64][]web3.RPCStatus{
			1:  {{URL: "https://eth.example.com", IsActive: true}},
			137: {{URL: "https://polygon.example.com", IsActive: true}},
		},
		chains: []*web3.ChainConfig{
			{ID: 1, Name: "Ethereum"},
			{ID: 137, Name: "Polygon"},
		},
	}
	r := setupWeb3Router(provider)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", http.NoBody)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	chains := resp["chains"].([]interface{})
	assert.Len(t, chains, 2)
}

func TestWeb3Handlers_RPCStatus_ZeroFailureTimestamps(t *testing.T) {
	provider := &mockWeb3StatusProvider{
		statuses: map[int64][]web3.RPCStatus{
			1: {{URL: "https://rpc.example.com", IsActive: true, Failures: 0}},
		},
		chains: []*web3.ChainConfig{{ID: 1, Name: "Ethereum"}},
	}
	r := setupWeb3Router(provider)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", http.NoBody)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	chains := resp["chains"].([]interface{})
	chain := chains[0].(map[string]interface{})
	rpcs := chain["rpcs"].([]interface{})
	rpc0 := rpcs[0].(map[string]interface{})
	_, hasLastFailure := rpc0["last_failure_at"]
	assert.False(t, hasLastFailure)
	_, hasCooldown := rpc0["cooldown_until"]
	assert.False(t, hasCooldown)
}

func TestParseBlockTag(t *testing.T) {
	tests := []struct {
		input string
		want  web3.BlockTag
	}{
		{"finalized", web3.BlockTagFinalized},
		{"latest", web3.BlockTagLatest},
		{"safe", web3.BlockTagSafe},
		{"", web3.BlockTagSafe},
		{"unknown", web3.BlockTagSafe},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, parseBlockTag(tt.input))
		})
	}
}

func TestDetectVideoFormat_Table(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		expect string
	}{
		{"mp4 ftypisom", []byte{0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}, "mp4"},
		{"mp4 ftypmp42", []byte{0x66, 0x74, 0x79, 0x70, 0x6D, 0x70, 0x34, 0x32}, "mp4"},
		{"webm", []byte{0x1A, 0x45, 0xDF, 0xA3}, "webm"},
		{"mpeg sequence", []byte{0x00, 0x00, 0x01, 0xBA}, "mpeg"},
		{"mpeg video", []byte{0x00, 0x00, 0x01, 0xB3}, "mpeg"},
		{"avi riff", []byte{0x52, 0x49, 0x46, 0x46}, "avi"},
		{"too short", []byte{0x00, 0x01}, ""},
		{"unknown", []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00, 0x00, 0x00}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectVideoFormat(bytesReader(tt.input))
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestDetectVideoFormat_MP4Fallback(t *testing.T) {
	header := make([]byte, 16)
	copy(header[4:8], []byte("ftyp"))
	result := detectVideoFormat(bytesReader(header))
	assert.Equal(t, "mp4", result)
}

func TestSanitizeObjectKey_Table(t *testing.T) {
	tests := []struct {
		input   string
		wantKey string
		wantOk  bool
	}{
		{"valid-key", "valid-key", true},
		{"key_with_underscore", "key_with_underscore", true},
		{"key123", "key123", true},
		{"../traversal", "", false},
		{"path/slash", "", false},
		{"back\\slash", "", false},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			key, ok := sanitizeObjectKey(tt.input)
			assert.Equal(t, tt.wantOk, ok)
			if ok {
				assert.Equal(t, tt.wantKey, key)
			}
		})
	}
}

func TestIsValidUsername_Table(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"validuser", true},
		{"ValidUser123", true},
		{"user_name", true},
		{"user-name", false},
		{"user@name", false},
		{"user name", false},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidUsername(tt.input))
		})
	}
}

func TestExtractBearerToken_Table(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid bearer", "Bearer my-token-123", "my-token-123"},
		{"bearer with extra spaces", "Bearer   spaced-token  ", "spaced-token"},
		{"empty authorization", "", ""},
		{"non-bearer", "Basic dXNlcjpwYXNz", ""},
		{"bearer empty", "Bearer ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptestNewRecorder()
			c := newTestContext(w)
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}
			assert.Equal(t, tt.want, extractBearerToken(c))
		})
	}
}

func TestReadFileHeader(t *testing.T) {
	mp4Header := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	format, combined, err := readFileHeader(bytesReader(mp4Header))
	assert.NoError(t, err)
	assert.Equal(t, "mp4", format)
	assert.NotNil(t, combined)

	all, err := io.ReadAll(combined)
	assert.NoError(t, err)
	assert.Equal(t, len(mp4Header), len(all))
}

func TestValidationError_NilError(t *testing.T) {
	result := validationError(nil)
	assert.Nil(t, result)
}

func TestInternalErrMsg_WithLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptestNewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	msg := internalErrMsg(c, assert.AnError)
	assert.Equal(t, "an internal error occurred", msg)
}

func TestInternalErrMsg_NilError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptestNewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	msg := internalErrMsg(c, nil)
	assert.Equal(t, "an internal error occurred", msg)
}

func TestGetErrorLogger_FromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptestNewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	ctxLog := zap.NewNop()
	c.Set("logger", ctxLog)

	result := getErrorLogger(c)
	assert.Equal(t, ctxLog, result)
}

func TestGetErrorLogger_NilContext(t *testing.T) {
	SetErrorLogger(zap.NewNop())
	result := getErrorLogger(nil)
	assert.NotNil(t, result)
}

func TestBuildCircuitBreakerConfig_Defaults(t *testing.T) {
	cfg := &config.Config{}
	result := buildCircuitBreakerConfig(cfg)
	assert.Equal(t, 5, result.FailureThreshold)
	assert.Equal(t, 2, result.SuccessThreshold)
	assert.Equal(t, 30*time.Second, result.Timeout)
}
