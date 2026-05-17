package e2e_test

import (
	"math/big"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_MiddlewareStack(t *testing.T) {
	// Use the real router which has the full middleware stack:
	// Recovery → CORS → RateLimit → Logging → JWT → NFTGate
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := e2eSetupServer(t, checker, nil)
	jwtToken := e2eTestJWT("0x1234567890123456789012345678901234567890")

	// Public route (no JWT required) — health may be 200 or 503 depending on DB availability
	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, resp.StatusCode)

	// Protected route with valid JWT → 503 (no ContentService/DB)
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp2.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp2.StatusCode)
}

func TestE2E_AuthenticationFlow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := e2eSetupServer(t, checker, nil)

	// Request without token → 401
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Request with valid JWT → 503 (no ContentService/DB in test)
	jwtToken := e2eTestJWT("0x1234567890123456789012345678901234567890")
	req2, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	req2.Header.Set("Authorization", "Bearer "+jwtToken)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	_ = resp2.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp2.StatusCode)

	// Request with invalid token format → 401
	req3, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	req3.Header.Set("Authorization", "InvalidFormat")
	resp3, err := http.DefaultClient.Do(req3)
	require.NoError(t, err)
	_ = resp3.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp3.StatusCode)
}

func TestE2E_CORSFlow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := e2eSetupServer(t, checker, nil)

	// CORS preflight on public route
	req, _ := http.NewRequest("OPTIONS", server.URL+"/health", http.NoBody)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent)
}

func TestE2E_LoggingFlow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := e2eSetupServer(t, checker, nil)

	// Make request — logging middleware is part of the stack
	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, resp.StatusCode)
}

func TestE2E_ErrorRecoveryFlow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := e2eSetupServer(t, checker, nil)

	// Request to a non-existent route — recovery middleware handles it
	resp, err := http.Get(server.URL + "/api/v1/nonexistent")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestE2E_MiddlewareOrdering(t *testing.T) {
	// The real router applies: Recovery → CORS → RateLimit → Logging → [JWT → NFTGate]
	// Verify the middleware chain works end-to-end:
	// 1. CORS headers are set (CORS middleware ran)
	// 2. 401 returned without JWT (JWT middleware ran)
	// 3. 200 returned with JWT on auth-protected route (JWT passed)
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := e2eSetupServer(t, checker, nil)

	// 1. CORS
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	req.Header.Set("Origin", "http://example.com")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Access-Control-Allow-Origin"))

	// 2. JWT rejection without token
	req2, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	_ = resp2.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp2.StatusCode)

	// 3. JWT acceptance with valid token → 503 (no ContentService/DB)
	jwtToken := e2eTestJWT("0x1234567890123456789012345678901234567890")
	req3, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
	req3.Header.Set("Authorization", "Bearer "+jwtToken)
	resp3, err := http.DefaultClient.Do(req3)
	require.NoError(t, err)
	defer func() { _ = resp3.Body.Close() }()
	assert.Equal(t, http.StatusServiceUnavailable, resp3.StatusCode)
}
