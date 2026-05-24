package gateway

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/gin-gonic/gin"
)

func TestParseChallengeTTL_Default(t *testing.T) {
	cfg := &config.Config{}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 5*time.Minute, ttl)
}

func TestParseChallengeTTL_Custom(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			NonceExpiry: "10m",
		},
	}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 10*time.Minute, ttl)
}

func TestParseChallengeTTL_Invalid(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			NonceExpiry: "invalid",
		},
	}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 5*time.Minute, ttl)
}

func TestParseChallengeTTL_Zero(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			NonceExpiry: "0s",
		},
	}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 5*time.Minute, ttl)
}

func TestParseChallengeTTL_Negative(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			NonceExpiry: "-5m",
		},
	}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 5*time.Minute, ttl)
}

func TestProvideChallengeStore_WithInjected(t *testing.T) {
	rc := &RouterConfig{}
	store := storage.NewMemoryChallengeStore()
	defer store.Close()
	rc.ChallengeStore = store

	log := zap.NewNop()
	res := &AppResources{}
	result := provideChallengeStore(rc, log, 5*time.Minute, nil, res)
	assert.Equal(t, store, result)
}

func TestProvideChallengeStore_NilRedis(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	res := &AppResources{}
	result := provideChallengeStore(rc, log, 5*time.Minute, nil, res)
	assert.NotNil(t, result)
	assert.NotNil(t, res.ChallengeStore)
}

func TestProvideTokenBlacklist_NilRedis(t *testing.T) {
	log := zap.NewNop()
	res := &AppResources{}
	result := provideTokenBlacklist(log, nil, res)
	assert.NotNil(t, result)
}

func TestProvideContentService_WithInjected(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	mockSvc := &service.ContentService{}
	rc.ContentService = mockSvc

	result := provideContentService(rc, nil, log)
	assert.Equal(t, mockSvc, result)
}

func TestProvideContentService_NilDB(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	result := provideContentService(rc, nil, log)
	assert.Nil(t, result)
}

func TestProvideUploadService_WithInjected(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	mockSvc := &service.UploadService{}
	rc.UploadService = mockSvc

	result := provideUploadService(rc, &config.Config{}, log, nil, nil, nil)
	assert.Equal(t, mockSvc, result)
}

func TestProvideUploadService_NilDB(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	result := provideUploadService(rc, &config.Config{}, log, nil, nil, nil)
	assert.Nil(t, result)
}

func TestProvideUploadService_NilObjStorage(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	result := provideUploadService(rc, &config.Config{}, log, &providerMockDB{}, nil, nil)
	assert.Nil(t, result)
}

type providerMockDB struct{}

func (m *providerMockDB) Query(ctx context.Context, query string, args ...interface{}) (storage.Rows, error) {
	return nil, nil
}
func (m *providerMockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *storage.CancelRow {
	return nil
}
func (m *providerMockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return nil, nil
}
func (m *providerMockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	return nil, nil
}
func (m *providerMockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return nil
}
func (m *providerMockDB) Ping(ctx context.Context) error { return nil }
func (m *providerMockDB) Close() error                   { return nil }

func TestParseBlockTag_Table(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected web3.BlockTag
	}{
		{"finalized", "finalized", web3.BlockTagFinalized},
		{"latest", "latest", web3.BlockTagLatest},
		{"safe_default", "safe", web3.BlockTagSafe},
		{"empty_default", "", web3.BlockTagSafe},
		{"unknown_default", "unknown", web3.BlockTagSafe},
		{"pending_default", "pending", web3.BlockTagSafe},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBlockTag(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildCircuitBreakerConfig_InvalidTimeout(t *testing.T) {
	cfg := &config.Config{
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled: true,
			Timeout: "not-a-duration",
		},
	}
	cbConfig := buildCircuitBreakerConfig(cfg)
	defaultCfg := middleware.DefaultCircuitBreakerConfig()
	assert.Equal(t, defaultCfg.Timeout, cbConfig.Timeout)
}

func TestBuildCircuitBreakerConfig_InvalidWindowTime(t *testing.T) {
	cfg := &config.Config{
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled:    true,
			WindowTime: "bad-window",
		},
	}
	cbConfig := buildCircuitBreakerConfig(cfg)
	defaultCfg := middleware.DefaultCircuitBreakerConfig()
	assert.Equal(t, defaultCfg.WindowTime, cbConfig.WindowTime)
}

func TestDefaultMaxBodySize(t *testing.T) {
	assert.Equal(t, int64(10<<20), defaultMaxBodySize)
}

func TestWithUploadService_Option(t *testing.T) {
	rc := &RouterConfig{}
	opt := WithUploadService(nil)
	opt(rc)
	assert.Nil(t, rc.UploadService)
}

func TestWithContentService_Option(t *testing.T) {
	rc := &RouterConfig{}
	opt := WithContentService(nil)
	opt(rc)
	assert.Nil(t, rc.ContentService)
}

func TestWithNFTVerifier_Option(t *testing.T) {
	rc := &RouterConfig{}
	opt := WithNFTVerifier(nil)
	opt(rc)
	assert.Nil(t, rc.NFTVerifier)
}

func TestExtractBearerToken_Valid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer mytoken123")

	token := extractBearerToken(c)
	assert.Equal(t, "mytoken123", token)
}

func TestExtractBearerToken_NoHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	token := extractBearerToken(c)
	assert.Equal(t, "", token)
}

func TestExtractBearerToken_WrongScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Basic abc123")

	token := extractBearerToken(c)
	assert.Equal(t, "", token)
}

func TestBuildCircuitBreakerConfig_CustomValues(t *testing.T) {
	cfg := &config.Config{
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled:          true,
			Timeout:          "10s",
			FailureThreshold: 7,
			SuccessThreshold: 4,
			MaxRequests:      3,
			WindowTime:       "30s",
		},
	}
	cbConfig := buildCircuitBreakerConfig(cfg)
	assert.Equal(t, 10*time.Second, cbConfig.Timeout)
	assert.Equal(t, 7, cbConfig.FailureThreshold)
	assert.Equal(t, 4, cbConfig.SuccessThreshold)
	assert.Equal(t, 3, cbConfig.MaxRequests)
	assert.Equal(t, 30*time.Second, cbConfig.WindowTime)
}

func TestErrorCodeConstants(t *testing.T) {
	assert.Equal(t, "INVALID_REQUEST", ErrInvalidRequest)
	assert.Equal(t, "UNAUTHORIZED", ErrUnauthorized)
	assert.Equal(t, "TOKEN_REVOKED", ErrTokenRevoked)
	assert.Equal(t, "TOKEN_EXPIRED", ErrTokenExpired)
	assert.Equal(t, "FORBIDDEN", ErrForbidden)
	assert.Equal(t, "NFT_REQUIRED", ErrNFTRequired)
	assert.Equal(t, "NFT_VERIFY_ERROR", ErrNFTVerifyError)
	assert.Equal(t, "MISSING_CONTRACT", ErrMissingContract)
	assert.Equal(t, "CONTENT_NOT_FOUND", ErrContentNotFound)
	assert.Equal(t, "CONTENT_FORBIDDEN", ErrContentForbidden)
	assert.Equal(t, "CONTENT_UNAVAILABLE", ErrContentUnavailable)
	assert.Equal(t, "UPLOAD_FAILED", ErrUploadFailed)
	assert.Equal(t, "NOT_FOUND", ErrNotFound)
	assert.Equal(t, "RATE_LIMITED", ErrRateLimited)
	assert.Equal(t, "PAYLOAD_TOO_LARGE", ErrPayloadTooLarge)
	assert.Equal(t, "STREAM_LIMIT_REACHED", ErrStreamLimitReached)
	assert.Equal(t, "HEALTH_CHECK_FAILED", ErrHealthCheckFailed)
	assert.Equal(t, "INTERNAL_ERROR", ErrInternalError)
}

func TestSetErrorLogger_PackageLevel(t *testing.T) {
	log := zap.NewNop()
	SetErrorLogger(log)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	result := getErrorLogger(c)
	assert.Equal(t, log, result)
}

func TestInternalErrMsg_ReturnsGenericMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	msg := internalErrMsg(c, fmt.Errorf("sensitive error"))
	assert.Equal(t, "an internal error occurred", msg)
}
