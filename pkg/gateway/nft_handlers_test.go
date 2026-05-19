package gateway

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"streamgate/pkg/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// mockNFTOwnershipChecker implements middleware.NFTOwnershipChecker
type mockNFTOwnershipChecker struct {
	balance *big.Int
	err     error
}

func (m *mockNFTOwnershipChecker) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.balance != nil && m.balance.Cmp(big.NewInt(0)) > 0, nil
}

func (m *mockNFTOwnershipChecker) GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.balance, nil
}

func (m *mockNFTOwnershipChecker) VerifyNFTOwnershipAutoDetect(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.balance != nil && m.balance.Cmp(big.NewInt(0)) > 0, nil
}

func (m *mockNFTOwnershipChecker) VerifyNFTCollectionAutoDetect(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.balance != nil && m.balance.Cmp(big.NewInt(0)) > 0, nil
}

func (m *mockNFTOwnershipChecker) GetNFTInfo(ctx context.Context, chainID int64, contractAddress, tokenID string) (*middleware.NFTMetadata, error) {
	return nil, nil
}

// mockNFTAccessCache implements middleware.NFTAccessCache
type mockNFTAccessCache struct{}

func (m *mockNFTAccessCache) Get(key string) (middleware.NFTAccessEntry, bool) {
	return middleware.NFTAccessEntry{}, false
}
func (m *mockNFTAccessCache) Set(key string, entry middleware.NFTAccessEntry) {}
func (m *mockNFTAccessCache) Delete(key string)                               {}
func (m *mockNFTAccessCache) DeleteByPrefix(prefix string)                    {}

func setupNFTRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Middleware to set wallet_address like auth middleware would
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B")
		c.Next()
	})

	checker := &mockNFTOwnershipChecker{balance: big.NewInt(1)}
	RegisterNFTRoutes(r, zap.NewNop(), checker, &mockNFTAccessCache{}, 1, 5*time.Minute)
	return r
}

func TestNFTHandlers_MissingContract(t *testing.T) {
	r := setupNFTRouter()

	t.Run("GET /nft without contract returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "MISSING_CONTRACT", resp["code"])
	})

	t.Run("GET /nft/:id without contract returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft/1", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "MISSING_CONTRACT", resp["code"])
	})
}

func TestNFTHandlers_InvalidContractAddress(t *testing.T) {
	r := setupNFTRouter()

	tests := []struct {
		name     string
		contract string
	}{
		{"not hex", "0xGGGG000000000000000000000000000000000000"},
		{"too short", "0x1234"},
		{"no 0x prefix", "Ab5801a7D398351b8bE11C439e05C5B3259aeC9B"},
		{"empty", ""},
		{"just 0x", "0x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft?contract="+tt.contract, http.NoBody)
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)

			var resp map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			assert.Equal(t, "MISSING_CONTRACT", resp["code"])
		})
	}
}

func TestNFTHandlers_ValidContractPassesValidation(t *testing.T) {
	r := setupNFTRouter()

	t.Run("valid contract address returns 200", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/nft?contract=0xdAC17F958D2ee523a2206206994597C13D831ec7", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "1", resp["balance"])
		assert.Equal(t, true, resp["has_nft"])
	})
}
