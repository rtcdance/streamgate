package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type nftRouteMockChecker struct {
	owns    bool
	ownsErr error
	balance *big.Int
	balErr  error
}

func (m *nftRouteMockChecker) VerifyNFTOwnership(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}
func (m *nftRouteMockChecker) GetNFTBalance(_ context.Context, _ int64, _, _ string) (*big.Int, error) {
	return m.balance, m.balErr
}
func (m *nftRouteMockChecker) VerifyNFTOwnershipAutoDetect(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}
func (m *nftRouteMockChecker) VerifyNFTCollectionAutoDetect(_ context.Context, _ int64, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}
func (m *nftRouteMockChecker) GetNFTInfo(_ context.Context, _ int64, _, _ string) (*middleware.NFTMetadata, error) {
	return nil, nil
}

type nftRouteMockCache struct {
	entries map[string]middleware.NFTAccessEntry
	hit     bool
}

func newNftRouteMockCache() *nftRouteMockCache {
	return &nftRouteMockCache{entries: make(map[string]middleware.NFTAccessEntry)}
}
func (m *nftRouteMockCache) Get(_ context.Context, key string) (middleware.NFTAccessEntry, bool) {
	e, ok := m.entries[key]
	if ok {
		return e, true
	}
	return middleware.NFTAccessEntry{}, m.hit
}
func (m *nftRouteMockCache) Set(_ context.Context, key string, entry middleware.NFTAccessEntry) {
	m.entries[key] = entry
}
func (m *nftRouteMockCache) Delete(_ context.Context, _ string)            {}
func (m *nftRouteMockCache) DeleteByPrefix(_ context.Context, _ string)    {}

func setupNFTRouter(checker middleware.NFTOwnershipChecker, cache middleware.NFTAccessCache, wallet string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if wallet != "" {
			c.Set("wallet_address", wallet)
		}
		c.Next()
	})
	RegisterNFTRoutes(r, zap.NewNop(), checker, cache, 1, 5*time.Minute)
	return r
}

func TestNFTRoutes_GetBalance_MissingContract(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(1)}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTRoutes_GetBalance_InvalidContract(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(1)}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft?contract=invalid", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTRoutes_GetBalance_Success(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(3)}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "3", resp["balance"])
	assert.Equal(t, true, resp["has_nft"])
}

func TestNFTRoutes_GetBalance_Error(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balErr: errors.New("rpc fail")}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNFTRoutes_GetBalance_CustomChainID(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(1)}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft?contract=0x1234567890abcdef1234567890abcdef12345678&chain_id=137", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(137), resp["chain_id"])
}

func TestNFTRoutes_GetNFT_MissingContract(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{owns: true}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft/1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTRoutes_GetNFT_Success(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{owns: true}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft/42?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["has_nft"])
	assert.Equal(t, "42", resp["token_id"])
}

func TestNFTRoutes_GetNFT_Error(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{ownsErr: errors.New("rpc fail")}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft/1?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNFTRoutes_Verify_InvalidJSON(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{owns: true}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{invalid}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTRoutes_Verify_MissingContract(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{owns: true}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTRoutes_Verify_InvalidContract(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{owns: true}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"invalid","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTRoutes_Verify_InvalidTokenID(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{owns: true}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"not-a-number"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTRoutes_Verify_WithTokenID(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{owns: true}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["has_nft"])
}

func TestNFTRoutes_Verify_WithoutTokenID(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(2)}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["has_nft"])
	assert.Equal(t, "2", resp["balance"])
}

func TestNFTRoutes_Verify_WalletFallback(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(1)}, nil, "")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","address":"0x1234567890abcdef1234567890abcdef12345678"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTRoutes_Verify_OwnerAddressFallback(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(1)}, nil, "")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","owner_address":"0x1234567890abcdef1234567890abcdef12345678"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTRoutes_Verify_ContractAddressFallback(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(1)}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract_address":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTRoutes_Verify_VerificationError(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{ownsErr: errors.New("rpc fail"), balErr: errors.New("rpc fail")}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNFTRoutes_Verify_CacheHit(t *testing.T) {
	cache := newNftRouteMockCache()
	cache.entries["1:0x1234567890abcdef1234567890abcdef12345678:0x1234567890abcdef1234567890abcdef12345678:1"] = middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(1),
		Expires: time.Now().Add(time.Hour),
	}
	r := setupNFTRouter(&nftRouteMockChecker{owns: false, balance: big.NewInt(0)}, cache, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["has_nft"])
	assert.Equal(t, true, resp["cache_hit"])
}

func TestNFTRoutes_Verify_CachePopulated(t *testing.T) {
	cache := newNftRouteMockCache()
	r := setupNFTRouter(&nftRouteMockChecker{owns: true, balance: big.NewInt(1)}, cache, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, len(cache.entries))
}

func TestNFTRoutes_Verify_DefaultChainID(t *testing.T) {
	r := setupNFTRouter(&nftRouteMockChecker{balance: big.NewInt(1)}, nil, "0x1234567890abcdef1234567890abcdef12345678")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewBufferString(`{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(1), resp["chain_id"])
}
