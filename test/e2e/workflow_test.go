package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Auth + NFT + Streaming workflow (real router) ---

func TestE2EAuthNFTStreamingWorkflow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(2)}
	storage := newMockSegmentStorage()
	_, verifier, server := setupE2EServer(t, checker, storage)

	t.Run("Step1_Challenge", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"wallet":   "0x1234567890123456789012345678901234567890",
			"chain_id": 11155111,
		})
		resp, err := http.Post(server.URL+"/api/v1/auth/challenge", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.NotEmpty(t, result["challenge_id"])
		assert.NotEmpty(t, result["message"])
	})

	t.Run("Step2_LoginWithRealSignature", func(t *testing.T) {
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)
		wallet := verifier.GetAddressFromPrivateKey(privateKey)

		// Get challenge
		challengeBody, _ := json.Marshal(map[string]interface{}{
			"wallet":   wallet,
			"chain_id": 11155111,
		})
		resp, err := http.Post(server.URL+"/api/v1/auth/challenge", "application/json", bytes.NewReader(challengeBody))
		require.NoError(t, err)
		var cr map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&cr))
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		message, _ := cr["message"].(string)
		challengeID, _ := cr["challenge_id"].(string)

		// Sign with real crypto
		signature, err := verifier.SignMessage(message, privateKey)
		require.NoError(t, err)

		// Login
		loginBody, _ := json.Marshal(map[string]string{
			"wallet":       wallet,
			"challenge_id": challengeID,
			"signature":    signature,
		})
		resp3, err := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
		require.NoError(t, err)
		defer func() { _ = resp3.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp3.StatusCode)

		var loginResult map[string]interface{}
		require.NoError(t, json.NewDecoder(resp3.Body).Decode(&loginResult))
		assert.NotEmpty(t, loginResult["token"])
		assert.Equal(t, wallet, loginResult["wallet_address"])
	})
}

func TestE2ENFTVerifyWorkflow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(3)}
	_, _, server := setupE2EServer(t, checker, nil)
	jwtToken := testJWT("0x1234567890123456789012345678901234567890")

	t.Run("VerifyByBalance", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"wallet":   "0x1234567890123456789012345678901234567890",
			"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
			"chain_id": 11155111,
		})
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/nft/verify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, true, result["has_nft"])
		assert.Equal(t, float64(3), result["balance"])
		assert.Equal(t, false, result["cache_hit"])
	})

	t.Run("VerifyByTokenOwnership", func(t *testing.T) {
		checker2 := &mockNFTChecker{verifyResult: true}
		_, _, server2 := setupE2EServer(t, checker2, nil)
		jwtToken2 := testJWT("0x1234567890123456789012345678901234567890")

		body, _ := json.Marshal(map[string]interface{}{
			"wallet":   "0x1234567890123456789012345678901234567890",
			"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
			"token_id": "42",
			"chain_id": 11155111,
		})
		req, _ := http.NewRequest("POST", server2.URL+"/api/v1/nft/verify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+jwtToken2)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, true, result["has_nft"])
		assert.Equal(t, float64(1), result["balance"])
	})
}

func TestE2EStreamingWorkflow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, verifier, server := setupE2EServer(t, checker, nil)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	// Full auth flow to get a real JWT
	challengeBody, _ := json.Marshal(map[string]interface{}{
		"wallet":   wallet,
		"chain_id": 11155111,
	})
	resp, err := http.Post(server.URL+"/api/v1/auth/challenge", "application/json", bytes.NewReader(challengeBody))
	require.NoError(t, err)
	var cr map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cr))
	_ = resp.Body.Close()

	signature, err := verifier.SignMessage(cr["message"].(string), privateKey)
	require.NoError(t, err)

	loginBody, _ := json.Marshal(map[string]string{
		"wallet":       wallet,
		"challenge_id": cr["challenge_id"].(string),
		"signature":    signature,
	})
	resp2, err := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	require.NoError(t, err)
	var lr map[string]interface{}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&lr))
	_ = resp2.Body.Close()
	token := lr["token"].(string)

	t.Run("ManifestReturnsHLS", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/streaming/demo/manifest.m3u8?contract=0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(body), "#EXTM3U")
		assert.Contains(t, string(body), "playback_token=")
	})
}

func TestE2ETranscodingWorkflow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := setupE2EServer(t, checker, nil)
	jwtToken := testJWT("0x1234567890123456789012345678901234567890")

	t.Run("SubmitTask", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"content_id": "content-42",
			"profile":    "720p",
			"input_url":  "https://example.com/input.mp4",
			"priority":   1,
		})
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/transcode/submit", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)

		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.NotEmpty(t, result["task_id"])
	})
}

func TestE2EUploadWorkflow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	_, _, server := setupE2EServer(t, checker, newMockSegmentStorage())
	jwtToken := testJWT("0x1234567890123456789012345678901234567890")

	t.Run("UploadFile", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/upload", bytes.NewReader([]byte("fake video")))
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		// Without multipart form, should get 400 (no file provided)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestE2EHealthEndpoints(t *testing.T) {
	checker := &mockNFTChecker{}
	_, _, server := setupE2EServer(t, checker, nil)

	t.Run("Health", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/health")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Contains(t, []string{"healthy", "unhealthy"}, result["status"])
	})

	t.Run("Ready", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/ready")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		// ReadinessResponse has "ready" bool, not "status" string
		_, hasReady := result["ready"]
		assert.True(t, hasReady, "response should have 'ready' field")
	})
}

func TestE2EWeb3RPCStatus(t *testing.T) {
	checker := &mockNFTChecker{}
	_, _, server := setupE2EServer(t, checker, nil)

	resp, err := http.Get(server.URL + "/api/v1/web3/rpc-status")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	chains, ok := result["chains"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, chains)
}

func TestE2EContentRoutesRequireAuth(t *testing.T) {
	checker := &mockNFTChecker{}
	_, _, server := setupE2EServer(t, checker, nil)

	t.Run("NoAuth_Returns401", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/v1/content")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("WithAuth_NoDB_Returns503", func(t *testing.T) {
		jwtToken := testJWT("0x1234567890123456789012345678901234567890")
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/content", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, "CONTENT_UNAVAILABLE", result["code"])
	})
}

func TestE2EAuthLogoutVerifyWorkflow(t *testing.T) {
	checker := &mockNFTChecker{}
	authService, verifier, server := setupE2EServer(t, checker, nil)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	// Full auth flow to get a real JWT (with JTI)
	challengeBody, _ := json.Marshal(map[string]interface{}{
		"wallet":   wallet,
		"chain_id": 11155111,
	})
	resp, err := http.Post(server.URL+"/api/v1/auth/challenge", "application/json", bytes.NewReader(challengeBody))
	require.NoError(t, err)
	var cr map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cr))
	_ = resp.Body.Close()

	signature, err := verifier.SignMessage(cr["message"].(string), privateKey)
	require.NoError(t, err)

	loginBody, _ := json.Marshal(map[string]string{
		"wallet":       wallet,
		"challenge_id": cr["challenge_id"].(string),
		"signature":    signature,
	})
	resp2, err := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	require.NoError(t, err)
	var lr map[string]interface{}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&lr))
	_ = resp2.Body.Close()
	jwtToken := lr["token"].(string)

	t.Run("VerifyToken_Valid", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/auth/verify", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, true, result["valid"])
		assert.Equal(t, wallet, result["wallet_address"])
	})

	t.Run("Logout", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, "logged out", result["message"])
	})

	t.Run("VerifyToken_AfterLogout_Invalid", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/auth/verify", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Contains(t, []string{"invalid token", "token revoked"}, result["error"])
	})

	t.Run("AccessProtectedRoute_AfterLogout_401", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/content", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	_ = authService // used implicitly via server
}

func TestE2ENFTGateEnriched403(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(0)} // no NFT
	_, _, server := setupE2EServer(t, checker, nil)
	jwtToken := testJWT("0x1234567890123456789012345678901234567890")

	t.Run("StreamingWithoutNFT_Enriched403", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/streaming/demo/manifest.m3u8?contract=0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, "NFT_REQUIRED", result["code"])
		require.NotNil(t, result["required_nft"])
		nftInfo, ok := result["required_nft"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f", nftInfo["contract"])
		assert.NotNil(t, nftInfo["chain_name"])
		assert.NotNil(t, nftInfo["marketplace_url"])
	})
}

func TestE2EStructuredErrorResponses(t *testing.T) {
	checker := &mockNFTChecker{}
	_, _, server := setupE2EServer(t, checker, nil)

	t.Run("AuthChallenge_InvalidRequest_HasCode", func(t *testing.T) {
		resp, err := http.Post(server.URL+"/api/v1/auth/challenge", "application/json", bytes.NewReader([]byte("invalid")))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, "INVALID_REQUEST", result["code"])
		assert.NotNil(t, result["request_id"])
	})

	t.Run("ProtectedRoute_NoAuth_HasCode", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/v1/content")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, "UNAUTHORIZED", result["code"])
	})
}
