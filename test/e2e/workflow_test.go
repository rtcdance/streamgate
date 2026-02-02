package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type E2ETestSuite struct {
	server   *httptest.Server
	baseURL  string
	logger   *zap.Logger
	testData map[string]string
	cleanup  func()
}

func SetupE2ETestSuite(t *testing.T) *E2ETestSuite {
	logger := zap.NewNop()

	suite := &E2ETestSuite{
		logger:   logger,
		testData: make(map[string]string),
	}

	suite.setupTestServer(t)
	suite.setupTestData(t)

	return suite
}

func (s *E2ETestSuite) Teardown() {
	if s.cleanup != nil {
		s.cleanup()
	}
	if s.server != nil {
		s.server.Close()
	}
}

func (s *E2ETestSuite) setupTestServer(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/upload/initiate", s.handleInitiateUpload)
	mux.HandleFunc("/api/v1/upload/", s.handleUploadRouter)
	mux.HandleFunc("/api/v1/transcode/submit", s.handleSubmitTranscode)
	mux.HandleFunc("/api/v1/transcode/", s.handleTranscodeStatus)
	mux.HandleFunc("/api/v1/streaming/", s.handleStreaming)
	mux.HandleFunc("/api/v1/metadata", s.handleMetadata)
	mux.HandleFunc("/api/v1/metadata/", s.handleMetadata)
	mux.HandleFunc("/api/v1/auth/challenge", s.handleChallenge)
	mux.HandleFunc("/api/v1/auth/verify", s.handleVerify)
	mux.HandleFunc("/api/v1/nft/verify", s.handleNFTVerify)
	mux.HandleFunc("/api/v1/health/live", s.handleHealth)
	mux.HandleFunc("/api/v1/health/ready", s.handleHealth)
	mux.HandleFunc("/api/v1/cache/warm", s.handleCacheWarm)
	mux.HandleFunc("/api/v1/cache/stats", s.handleCacheStats)

	s.server = httptest.NewServer(mux)
	s.baseURL = s.server.URL
}

func (s *E2ETestSuite) handleUploadRouter(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if len(path) > len("/api/v1/upload/") && strings.HasSuffix(path, "/complete") {
		s.handleCompleteUpload(w, r)
		return
	}

	s.handleUpload(w, r)
}

func (s *E2ETestSuite) setupTestData(t *testing.T) {
	tmpDir := t.TempDir()
	s.testData["tmpDir"] = tmpDir

	testFile := filepath.Join(tmpDir, "test-video.mp4")
	err := os.WriteFile(testFile, []byte("fake video content for testing"), 0644)
	require.NoError(t, err)
	s.testData["testFile"] = testFile
}

func TestE2EUploadTranscodeStreamWorkflow(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	defer suite.Teardown()

	t.Run("Step1_InitiateUpload", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"fileName":    "test-video.mp4",
			"fileSize":    1024000,
			"contentType": "video/mp4",
		}

		resp := suite.makeRequest(t, "POST", "/api/v1/upload/initiate", reqBody)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		uploadID, ok := result["uploadId"].(string)
		require.True(t, ok)
		suite.testData["uploadId"] = uploadID
		assert.NotEmpty(t, uploadID)
	})

	t.Run("Step2_UploadChunks", func(t *testing.T) {
		uploadID := suite.testData["uploadId"]

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("chunk", suite.testData["testFile"])
		require.NoError(t, err)

		testFile, err := os.Open(suite.testData["testFile"])
		require.NoError(t, err)
		defer testFile.Close()

		_, err = io.Copy(part, testFile)
		require.NoError(t, err)

		writer.WriteField("chunkNumber", "1")
		writer.WriteField("totalChunks", "1")
		writer.Close()

		req, _ := http.NewRequest("POST", suite.baseURL+"/api/v1/upload/"+uploadID+"/chunk", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := suite.httpClient().Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.True(t, result["success"].(bool))
	})

	t.Run("Step3_CompleteUpload", func(t *testing.T) {
		uploadID := suite.testData["uploadId"]

		resp := suite.makeRequest(t, "POST", "/api/v1/upload/"+uploadID+"/complete", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		fileID, ok := result["fileId"].(string)
		require.True(t, ok)
		suite.testData["fileId"] = fileID

		transcodeJobID, ok := result["transcodingJobId"].(string)
		require.True(t, ok)
		suite.testData["transcodeJobId"] = transcodeJobID

		assert.NotEmpty(t, fileID)
		assert.NotEmpty(t, transcodeJobID)
	})

	t.Run("Step4_WaitForTranscode", func(t *testing.T) {
		jobID := suite.testData["transcodeJobId"]

		for i := 0; i < 10; i++ {
			resp := suite.makeRequest(t, "GET", "/api/v1/transcode/"+jobID+"/status", nil)

			var result map[string]interface{}
			err := json.NewDecoder(resp.Body).Decode(&result)
			require.NoError(t, err)

			status := result["status"].(string)

			if status == "completed" {
				suite.testData["contentId"] = result["contentId"].(string)
				return
			}

			if status == "failed" {
				t.Fatalf("Transcoding failed: %v", result["error"])
			}

			time.Sleep(500 * time.Millisecond)
		}

		t.Fatal("Transcoding did not complete in time")
	})

	t.Run("Step5_StreamContent", func(t *testing.T) {
		contentID := suite.testData["contentId"]

		resp := suite.makeRequest(t, "GET", "/api/v1/streaming/"+contentID+"/hls", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Contains(t, string(body), "#EXTM3U")
		assert.Contains(t, string(body), "#EXT-X-STREAM-INF")
	})
}

func TestE2ENFTProtectedContentWorkflow(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	defer suite.Teardown()

	t.Run("Step1_GetChallenge", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"address": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
		}

		resp := suite.makeRequest(t, "POST", "/api/v1/auth/challenge", reqBody)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		challenge, ok := result["challenge"].(string)
		require.True(t, ok)
		suite.testData["challenge"] = challenge

		assert.NotEmpty(t, challenge)
	})

	t.Run("Step2_VerifySignature", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"address":   "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
			"challenge": suite.testData["challenge"],
			"signature": "0x1234567890abcdef",
		}

		resp := suite.makeRequest(t, "POST", "/api/v1/auth/verify", reqBody)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		token, ok := result["token"].(string)
		require.True(t, ok)
		suite.testData["token"] = token

		assert.NotEmpty(t, token)
	})

	t.Run("Step3_VerifyNFT", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"address":         "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
			"chain":           "ethereum",
			"contractAddress": "0x1234567890abcdef1234567890abcdef1234567890",
			"tokenId":         "1",
		}

		resp := suite.makeRequest(t, "POST", "/api/v1/nft/verify", reqBody)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		valid, ok := result["valid"].(bool)
		require.True(t, ok)
		assert.True(t, valid)
	})
}

func TestE2EMetadataWorkflow(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	defer suite.Teardown()

	t.Run("Step1_CreateMetadata", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"title":       "Test Video",
			"description": "This is a test video",
			"duration":    300,
			"format":      "mp4",
		}

		resp := suite.makeRequest(t, "POST", "/api/v1/metadata", reqBody)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		contentID, ok := result["contentId"].(string)
		require.True(t, ok)
		suite.testData["contentId"] = contentID

		assert.NotEmpty(t, contentID)
	})

	t.Run("Step2_GetMetadata", func(t *testing.T) {
		contentID := suite.testData["contentId"]

		resp := suite.makeRequest(t, "GET", "/api/v1/metadata/"+contentID, nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, "Test Video", result["title"])
		assert.Equal(t, "This is a test video", result["description"])
		assert.Equal(t, float64(300), result["duration"])
	})

	t.Run("Step3_UpdateMetadata", func(t *testing.T) {
		contentID := suite.testData["contentId"]

		reqBody := map[string]interface{}{
			"title":       "Updated Test Video",
			"description": "This is an updated test video",
		}

		resp := suite.makeRequest(t, "PUT", "/api/v1/metadata/"+contentID, reqBody)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, "Updated Test Video", result["title"])
	})

	t.Run("Step4_SearchMetadata", func(t *testing.T) {
		resp := suite.makeRequest(t, "GET", "/api/v1/metadata/search?q=Test&limit=10", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		results, ok := result["results"].([]interface{})
		require.True(t, ok)
		assert.Greater(t, len(results), 0)
	})
}

func TestE2ECacheWorkflow(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	defer suite.Teardown()

	t.Run("Step1_WarmCache", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"contentIds": []string{"content-1", "content-2", "content-3"},
		}

		resp := suite.makeRequest(t, "POST", "/api/v1/cache/warm", reqBody)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		jobID, ok := result["jobId"].(string)
		require.True(t, ok)
		suite.testData["cacheJobId"] = jobID

		assert.NotEmpty(t, jobID)
	})

	t.Run("Step2_GetCacheStats", func(t *testing.T) {
		resp := suite.makeRequest(t, "GET", "/api/v1/cache/stats", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		hitRate, ok := result["hitRate"].(float64)
		require.True(t, ok)
		assert.GreaterOrEqual(t, hitRate, 0.0)
		assert.LessOrEqual(t, hitRate, 1.0)

		hits, ok := result["hits"].(float64)
		require.True(t, ok)
		assert.GreaterOrEqual(t, hits, 0.0)
	})
}

func TestE2EHealthCheckWorkflow(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	defer suite.Teardown()

	t.Run("LivenessProbe", func(t *testing.T) {
		resp := suite.makeRequest(t, "GET", "/api/v1/health/live", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		status, ok := result["status"].(string)
		require.True(t, ok)
		assert.Equal(t, "healthy", status)
	})

	t.Run("ReadinessProbe", func(t *testing.T) {
		resp := suite.makeRequest(t, "GET", "/api/v1/health/ready", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		status, ok := result["status"].(string)
		require.True(t, ok)
		assert.Equal(t, "healthy", status)

		services, ok := result["services"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, services)
	})
}

func (s *E2ETestSuite) makeRequest(t *testing.T, method, path string, body interface{}) *http.Response {
	var reqBody io.Reader

	if body != nil {
		jsonBody, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, s.baseURL+path, reqBody)
	require.NoError(t, err)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if token, ok := s.testData["token"]; ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := s.httpClient().Do(req)
	require.NoError(t, err)

	return resp
}

func (s *E2ETestSuite) httpClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}

func (s *E2ETestSuite) handleInitiateUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uploadId":    fmt.Sprintf("upload-%d", time.Now().UnixNano()),
		"chunkSize":   5242880,
		"totalChunks": 1,
		"expiresAt":   time.Now().Add(1 * time.Hour),
	})
}

func (s *E2ETestSuite) handleUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"chunkNumber":   1,
		"uploadedBytes": 1024000,
	})
}

func (s *E2ETestSuite) handleCompleteUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"fileId":           fmt.Sprintf("file-%d", time.Now().UnixNano()),
		"transcodingJobId": fmt.Sprintf("transcode-%d", time.Now().UnixNano()),
		"status":           "completed",
	})
}

func (s *E2ETestSuite) handleSubmitTranscode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobId":  fmt.Sprintf("transcode-%d", time.Now().UnixNano()),
		"status": "pending",
	})
}

func (s *E2ETestSuite) handleTranscodeStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobId":       "transcode-123",
		"status":      "completed",
		"progress":    100,
		"contentId":   "content-123",
		"inputFile":   "input.mp4",
		"outputFile":  "output.m3u8",
		"format":      "hls",
		"createdAt":   time.Now(),
		"updatedAt":   time.Now(),
		"completedAt": time.Now(),
	})
}

func (s *E2ETestSuite) handleStreaming(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write([]byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360\n360p.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=1400000,RESOLUTION=842x480\n480p.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2800000,RESOLUTION=1280x720\n720p.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\n1080p.m3u8"))
}

func (s *E2ETestSuite) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"contentId":   fmt.Sprintf("content-%d", time.Now().UnixNano()),
			"title":       "Test Video",
			"description": "This is a test video",
			"duration":    300,
			"fileSize":    1024000,
			"format":      "mp4",
			"createdAt":   time.Now(),
			"updatedAt":   time.Now(),
		})
	} else if r.Method == "PUT" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"contentId":   "content-123",
			"title":       "Updated Test Video",
			"description": "This is an updated test video",
			"duration":    300,
			"fileSize":    1024000,
			"format":      "mp4",
			"createdAt":   time.Now(),
			"updatedAt":   time.Now(),
		})
	} else if r.URL.Query().Get("q") != "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{
					"contentId":   "content-123",
					"title":       "Test Video",
					"description": "This is a test video",
					"duration":    300,
					"fileSize":    1024000,
					"format":      "mp4",
					"createdAt":   time.Now(),
					"updatedAt":   time.Now(),
				},
			},
			"total":   1,
			"page":    1,
			"perPage": 10,
		})
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"contentId":   "content-123",
			"title":       "Test Video",
			"description": "This is a test video",
			"duration":    300,
			"fileSize":    1024000,
			"format":      "mp4",
			"createdAt":   time.Now(),
			"updatedAt":   time.Now(),
		})
	}
}

func (s *E2ETestSuite) handleChallenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"challenge": fmt.Sprintf("challenge-%d", time.Now().UnixNano()),
		"expiresAt": time.Now().Add(5 * time.Minute),
	})
}

func (s *E2ETestSuite) handleVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":        "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test",
		"refreshToken": "refresh-token-123",
		"expiresAt":    time.Now().Add(24 * time.Hour),
	})
}

func (s *E2ETestSuite) handleNFTVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":   true,
		"owner":   "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
		"balance": "1",
		"cached":  false,
	})
}

func (s *E2ETestSuite) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"services": map[string]string{
			"database": "healthy",
			"redis":    "healthy",
			"storage":  "healthy",
			"cache":    "healthy",
		},
	})
}

func (s *E2ETestSuite) handleCacheWarm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobId":       fmt.Sprintf("cache-job-%d", time.Now().UnixNano()),
		"status":      "completed",
		"warmCount":   3,
		"completedAt": time.Now(),
	})
}

func (s *E2ETestSuite) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"hitRate":   0.85,
		"hits":      850.0,
		"misses":    150.0,
		"size":      100.0,
		"maxSize":   1000.0,
		"evictions": 10,
	})
}
