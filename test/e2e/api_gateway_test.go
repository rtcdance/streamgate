//go:build e2e

package e2e_test

import (
	"encoding/json"
	"math/big"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_APIGatewayHealthRouting(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := setupE2EServer(t, checker, nil)

	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// Health may be 200 (healthy) or 503 (unhealthy if DB/storage connected but failing)
	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, resp.StatusCode)
	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, []string{"healthy", "unhealthy"}, result["status"])
}

func TestE2E_APIGatewayContentRequiresAuth(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := setupE2EServer(t, checker, nil)

	// Without auth
	resp, err := http.Get(server.URL + "/api/v1/content")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// With auth
	jwtToken := testJWT("0x1234567890123456789012345678901234567890")
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	// With auth — returns 503 because no ContentService/DB is configured
	assert.Equal(t, http.StatusServiceUnavailable, resp2.StatusCode)
}

func TestE2E_APIGatewayCORS(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := setupE2EServer(t, checker, nil)

	req, _ := http.NewRequest("GET", server.URL+"/health", http.NoBody)
	req.Header.Set("Origin", "http://example.com")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, resp.StatusCode)
	// CORS headers should be present on cross-origin requests
	assert.NotEmpty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestE2E_APIGatewayAuthentication(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := setupE2EServer(t, checker, nil)

	// Without token → 401 on protected routes
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// With valid JWT → 200
	jwtToken := testJWT("0x1234567890123456789012345678901234567890")
	req2, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	req2.Header.Set("Authorization", "Bearer "+jwtToken)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	// With valid JWT → 503 (ContentService/DB not available in test)
	assert.Equal(t, http.StatusServiceUnavailable, resp2.StatusCode)
}
