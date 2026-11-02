//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rtcdance/streamgate/pkg/gateway"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedDemoMedia writes the published HLS layout for the "demo" content: a
// multivariant playlist pointing at one media playlist, plus a segment. The
// manifest handler lists streams/<id>/ and serves the stored playlist, so this
// is the minimum that produces a real, capability-rewritten response.
func seedDemoMedia(t *testing.T, store *mockSegmentStorage) {
	t.Helper()
	objects := map[string]string{
		"streams/demo/master.m3u8": "#EXTM3U\n#EXT-X-VERSION:3\n" +
			"#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=1280x720\n720p/index.m3u8\n",
		"streams/demo/720p/index.m3u8": "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n" +
			"#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:10.000000,\nsegment0.ts\n#EXT-X-ENDLIST\n",
		"streams/demo/720p/segment0.ts": string([]byte{0x47, 0x40, 0x00, 0x10}),
	}
	for key, body := range objects {
		require.NoError(t, store.Upload(context.Background(), "streamgate", key, []byte(body)))
	}
}

func TestE2ENFTVerifyWorkflow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(3)}
	svc, _, server := setupE2EServer(t, checker, nil)
	jwtToken := testJWT(svc, "0x1234567890123456789012345678901234567890")

	t.Run("VerifyByBalance", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"wallet":    "0x1234567890123456789012345678901234567890",
			"contract":  "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
			"chain_id":  11155111,
			"sign_type": "personal_sign",
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
		// uint256 balances are serialized as decimal strings so no precision is
		// lost in JSON; the handler returns balance.String().
		assert.Equal(t, "3", result["balance"])
		assert.Equal(t, false, result["cache_hit"])
	})

	t.Run("VerifyByTokenOwnership", func(t *testing.T) {
		checker2 := &mockNFTChecker{verifyResult: true}
		svc2, _, server2 := setupE2EServer(t, checker2, nil)
		jwtToken2 := testJWT(svc2, "0x1234567890123456789012345678901234567890")

		body, _ := json.Marshal(map[string]interface{}{
			"wallet":    "0x1234567890123456789012345678901234567890",
			"contract":  "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
			"token_id":  "42",
			"chain_id":  11155111,
			"sign_type": "personal_sign",
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
		assert.Equal(t, "1", result["balance"])
	})
}

func TestE2EStreamingWorkflow(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(1)}
	// The gate fails closed on content with no rules, and the manifest handler
	// answers 404 when nothing was published for the content. So this workflow
	// needs both halves of a real deployment: a server-side rule that this
	// holder satisfies, and stored media for the content being requested.
	const demoContract = "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f"
	rules := staticGatingRuleResolver{rules: []middleware.GatingRule{{
		ContractAddress: demoContract,
		ChainID:         11155111,
		Standard:        "erc721",
		MinBalance:      1,
	}}}
	store := newMockSegmentStorage()
	seedDemoMedia(t, store)
	_, verifier, server := setupE2EServer(t, checker, store, gateway.WithGatingRuleResolver(rules))

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	// Full auth flow to get a real JWT
	challengeBody, _ := json.Marshal(map[string]interface{}{
		"wallet":    wallet,
		"chain_id":  11155111,
		"sign_type": "personal_sign",
	})
	resp, err := http.Post(server.URL+"/api/v1/auth/challenge", "application/json", bytes.NewReader(challengeBody))
	require.NoError(t, err)
	challengeRaw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "challenge failed: %s", challengeRaw)
	var cr map[string]interface{}
	require.NoError(t, json.Unmarshal(challengeRaw, &cr))
	message, ok := cr["message"].(string)
	require.True(t, ok, "challenge response has no message: %s", challengeRaw)
	challengeID, ok := cr["challenge_id"].(string)
	require.True(t, ok, "challenge response has no challenge_id: %s", challengeRaw)
	challengeChainID, ok := cr["chain_id"].(float64)
	require.True(t, ok, "challenge response has no chain_id: %s", challengeRaw)

	signature, err := verifier.SignMessage(message, privateKey)
	require.NoError(t, err)

	// The challenge pins a chain id; the login request must repeat it, because
	// AuthenticateWithWallet rejects a mismatch (and a zero id) with
	// ErrChainIDMismatch.
	loginBody, _ := json.Marshal(map[string]interface{}{
		"wallet":       wallet,
		"challenge_id": challengeID,
		"signature":    signature,
		"chain_id":     int64(challengeChainID),
	})
	resp2, err := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	require.NoError(t, err)
	loginRaw, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode, "login failed: %s", loginRaw)
	var lr map[string]interface{}
	require.NoError(t, json.Unmarshal(loginRaw, &lr))
	token, ok := lr["token"].(string)
	require.True(t, ok, "login response has no token: %s", loginRaw)

	t.Run("ManifestReturnsHLS", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/streaming/demo/manifest.m3u8?contract=0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f", http.NoBody)
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
	svc, _, server := setupE2EServer(t, checker, nil)
	jwtToken := testJWT(svc, "0x1234567890123456789012345678901234567890")

	t.Run("SubmitTask", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"content_id": "content-42",
			"profile":    "720p",
			// The submit API only accepts input URLs the policy allows (SSRF
			// guard, off-by-default external): testConfig's storage endpoint is
			// localhost:9000, which transcodeInputAllowlist includes, so an
			// arbitrary public host would be refused with 400.
			"input_url": "http://localhost:9000/streamgate/input.mp4",
			"priority":  1,
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
	// The upload routes answer 503 unless the upload service is wired in, so
	// this workflow uses the full upload server rather than the bare router.
	store := newMockSegmentStorage()
	svc, server := setupUploadE2EServer(t, store)
	jwtToken := testJWT(svc, "0xCreatorWallet")

	t.Run("UploadAndPollStatus", func(t *testing.T) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormFile("file", "workflow.mp4")
		require.NoError(t, err)
		_, err = part.Write(minimalMP4())
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		req, _ := http.NewRequest("POST", server.URL+"/api/v1/upload", &buf)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		uploadRaw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode, "upload failed: %s", uploadRaw)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(uploadRaw, &result))
		uploadID, ok := result["upload_id"].(string)
		require.True(t, ok, "upload response has no upload_id: %s", uploadRaw)

		// The same wallet must be able to read the record it just created.
		req2, _ := http.NewRequest("GET", server.URL+"/api/v1/upload/"+uploadID+"/status", http.NoBody)
		req2.Header.Set("Authorization", "Bearer "+jwtToken)
		resp2, err := http.DefaultClient.Do(req2)
		require.NoError(t, err)
		statusRaw, _ := io.ReadAll(resp2.Body)
		_ = resp2.Body.Close()
		require.Equal(t, http.StatusOK, resp2.StatusCode, "status failed: %s", statusRaw)

		var status map[string]interface{}
		require.NoError(t, json.Unmarshal(statusRaw, &status))
		assert.Equal(t, uploadID, status["upload_id"])
		assert.Equal(t, "workflow.mp4", status["filename"])
		assert.Equal(t, "completed", status["status"])
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
	svc, _, server := setupE2EServer(t, checker, nil)

	t.Run("NoAuth_Returns401", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/v1/content")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("WithAuth_NoDB_Returns503", func(t *testing.T) {
		jwtToken := testJWT(svc, "0x1234567890123456789012345678901234567890")
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/content", http.NoBody)
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

func TestE2ENFTGateEnriched403(t *testing.T) {
	checker := &mockNFTChecker{balance: big.NewInt(0)} // no NFT
	const demoContract = "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f"
	// Server-side rule for the demo content (A9: the request no longer picks
	// the contract; the resolver is the only source).
	rules := staticGatingRuleResolver{rules: []middleware.GatingRule{{
		ContractAddress: demoContract,
		TokenID:         "",
		ChainID:         11155111,
		Standard:        "erc721",
		MinBalance:      1,
	}}}
	svc, _, server := setupE2EServer(t, checker, nil, gateway.WithGatingRuleResolver(rules))
	jwtToken := testJWT(svc, "0x1234567890123456789012345678901234567890")

	t.Run("StreamingWithoutNFT_Enriched403", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/streaming/demo/manifest.m3u8", http.NoBody)
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
