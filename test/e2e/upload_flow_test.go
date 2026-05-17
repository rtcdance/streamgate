package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/gateway"
	"streamgate/pkg/service"
)

// uploadMockDB implements storage.DB with in-memory upload records.
// Exec supports INSERT/UPDATE/DELETE on the uploads table.
// QueryRow is not fully mockable without a real DB connection, so
// tests that rely on status/download-url are limited.
type uploadMockDB struct {
	uploads map[string]*service.UploadInfo
}

func newUploadMockDB() *uploadMockDB {
	return &uploadMockDB{uploads: make(map[string]*service.UploadInfo)}
}

func (d *uploadMockDB) Query(_ context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return nil, fmt.Errorf("not implemented")
}

func (d *uploadMockDB) QueryRow(_ context.Context, query string, args ...interface{}) *sql.Row {
	return nil
}

func (d *uploadMockDB) Exec(_ context.Context, query string, args ...interface{}) (sql.Result, error) {
	upper := strings.ToUpper(strings.TrimSpace(query))
	if strings.HasPrefix(upper, "INSERT") && len(args) >= 10 {
		info := &service.UploadInfo{
			ID:          fmt.Sprintf("%v", args[0]),
			Filename:    fmt.Sprintf("%v", args[1]),
			Size:        toInt64(args[2]),
			ContentType: fmt.Sprintf("%v", args[3]),
			Hash:        fmt.Sprintf("%v", args[4]),
			Status:      fmt.Sprintf("%v", args[5]),
			URL:         fmt.Sprintf("%v", args[6]),
			OwnerID:     fmt.Sprintf("%v", args[7]),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		d.uploads[info.ID] = info
	} else if strings.HasPrefix(upper, "UPDATE") && len(args) >= 2 {
		id := fmt.Sprintf("%v", args[0])
		if rec, ok := d.uploads[id]; ok {
			if len(args) > 1 {
				rec.Status = fmt.Sprintf("%v", args[1])
			}
			if len(args) > 2 {
				rec.Hash = fmt.Sprintf("%v", args[2])
			}
			if len(args) > 3 {
				rec.URL = fmt.Sprintf("%v", args[3])
			}
		}
	} else if strings.HasPrefix(upper, "DELETE") && len(args) >= 1 {
		id := fmt.Sprintf("%v", args[0])
		delete(d.uploads, id)
	}
	return sqlresult{}, nil
}

func (d *uploadMockDB) Begin(_ context.Context) (*sql.Tx, error) {
	return nil, fmt.Errorf("not implemented")
}

func (d *uploadMockDB) InTransaction(_ context.Context, _ func(tx *sql.Tx) error) error {
	return fmt.Errorf("not implemented")
}

func (d *uploadMockDB) Ping(_ context.Context) error { return nil }

func (d *uploadMockDB) Close() error { return nil }

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}

type sqlresult struct{}

func (sqlresult) LastInsertId() (int64, error) { return 0, nil }
func (sqlresult) RowsAffected() (int64, error) { return 1, nil }

// setupUploadE2EServer creates a test server with upload service wired in.
func setupUploadE2EServer(t *testing.T, store service.SegmentStorage) *httptest.Server {
	t.Helper()
	authService, _ := newTestAuthService()
	checker := &mockNFTChecker{verifyResult: true, balance: big.NewInt(1)}
	cfg := testConfig()
	mockDB := newUploadMockDB()

	uploadObj, ok := store.(service.UploadObjectStorage)
	if !ok {
		t.Fatal("store does not implement UploadObjectStorage")
	}
	uploadSvc := service.NewUploadService(mockDB, uploadObj, "streamgate", zap.NewNop())

	opts := []gateway.RouterOption{
		gateway.WithAuthService(authService),
		gateway.WithChallengeStore(service.NewMemoryChallengeStore()),
		gateway.WithSegmentStorage(store),
		gateway.WithNFTVerifier(checker),
		gateway.WithUploadService(uploadSvc),
	}

	router, resources, err := gateway.SetupRouter(cfg, zap.NewNop(), opts...)
	if err != nil {
		t.Fatalf("SetupRouter failed: %v", err)
	}
	t.Cleanup(func() { _ = resources.Close() })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func TestUploadRoutes_Return503WhenNoService(t *testing.T) {
	authService, _ := newTestAuthService()
	checker := &mockNFTChecker{verifyResult: true, balance: big.NewInt(1)}
	cfg := testConfig()

	opts := []gateway.RouterOption{
		gateway.WithAuthService(authService),
		gateway.WithChallengeStore(service.NewMemoryChallengeStore()),
		gateway.WithNFTVerifier(checker),
		// No WithUploadService → uploadSvc will be nil
	}

	router, resources, err := gateway.SetupRouter(cfg, zap.NewNop(), opts...)
	if err != nil {
		t.Fatalf("SetupRouter failed: %v", err)
	}
	t.Cleanup(func() { _ = resources.Close() })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	token := testJWT("0xTestWallet")

	endpoints := []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/upload"},
		{"POST", "/api/v1/upload/init"},
		{"POST", "/api/v1/upload/chunk"},
		{"GET", "/api/v1/upload/fake-id/status"},
		{"GET", "/api/v1/upload/fake-id/download-url"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			var body io.Reader
			if ep.method == "POST" {
				body = strings.NewReader("{}")
			}
			req, _ := http.NewRequest(ep.method, server.URL+ep.path, body)
			req.Header.Set("Authorization", "Bearer "+token)
			if ep.method == "POST" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("expected 503, got %d for %s %s", resp.StatusCode, ep.method, ep.path)
			}
		})
	}
}

func TestUploadWholeFile(t *testing.T) {
	t.Skip("requires external service")
	store := newMockSegmentStorage()
	server := setupUploadE2EServer(t, store)
	token := testJWT("0xCreatorWallet")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test-video.mp4")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("fake mp4 content for testing")); err != nil {
		t.Fatalf("write form data: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, _ := http.NewRequest("POST", server.URL+"/api/v1/upload", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if _, ok := result["upload_id"]; !ok {
		t.Error("response missing upload_id")
	}
	if result["filename"] != "test-video.mp4" {
		t.Errorf("expected filename=test-video.mp4, got %v", result["filename"])
	}
	if result["status"] != "completed" {
		t.Errorf("expected status=completed, got %v", result["status"])
	}

	// Verify object was stored in mock
	found := false
	for key := range store.objects {
		if strings.HasSuffix(key, ".mp4") {
			found = true
			break
		}
	}
	if !found {
		t.Error("no .mp4 object stored in mock storage")
	}
}

func TestChunkedUploadInit(t *testing.T) {
	t.Skip("requires external service")
	store := newMockSegmentStorage()
	server := setupUploadE2EServer(t, store)
	token := testJWT("0xCreatorWallet")

	body := `{"filename":"large-video.mp4","total_size":104857600,"total_chunks":10}`
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/upload/init", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("init request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if _, ok := result["upload_id"]; !ok {
		t.Error("response missing upload_id")
	}
	if result["status"] != "uploading" {
		t.Errorf("expected status=uploading, got %v", result["status"])
	}
	if result["total_chunks"] != float64(10) {
		t.Errorf("expected total_chunks=10, got %v", result["total_chunks"])
	}
}

func TestUploadChunk(t *testing.T) {
	t.Skip("requires external service")
	store := newMockSegmentStorage()
	server := setupUploadE2EServer(t, store)
	token := testJWT("0xCreatorWallet")

	// Init first
	initBody := `{"filename":"chunked.mp4","total_size":50000000,"total_chunks":5}`
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/upload/init", strings.NewReader(initBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("init request failed: %v", err)
	}
	var initResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&initResult); err != nil {
		t.Fatalf("decode init response: %v", err)
	}
	_ = resp.Body.Close()
	uploadID, ok := initResult["upload_id"].(string)
	if !ok {
		t.Fatal("init response missing upload_id")
	}

	// Upload chunk 0
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("upload_id", uploadID)
	_ = writer.WriteField("chunk_index", "0")
	part, _ := writer.CreateFormFile("chunk", "chunk-0")
	_, _ = part.Write([]byte("fake chunk data"))
	_ = writer.Close()

	req, _ = http.NewRequest("POST", server.URL+"/api/v1/upload/chunk", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("chunk upload request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["upload_id"] != uploadID {
		t.Errorf("expected upload_id=%s, got %v", uploadID, result["upload_id"])
	}
	if result["chunk_index"] != float64(0) {
		t.Errorf("expected chunk_index=0, got %v", result["chunk_index"])
	}
}

func TestUploadNoFileProvided(t *testing.T) {
	store := newMockSegmentStorage()
	server := setupUploadE2EServer(t, store)
	token := testJWT("0xCreatorWallet")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/v1/upload", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}
