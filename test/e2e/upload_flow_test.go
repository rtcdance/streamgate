//go:build e2e

package e2e_test

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

	"github.com/rtcdance/streamgate/pkg/gateway"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"go.uber.org/zap"
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

func (d *uploadMockDB) Query(_ context.Context, query string, args ...interface{}) (storage.Rows, error) {
	return nil, fmt.Errorf("not implemented")
}

// uploadQuotaRow scans a single int64 aggregate, standing in for the
// SELECT COALESCE(SUM(size), 0) the service issues in CheckStorageQuota.
type uploadQuotaRow struct{ value int64 }

func (r uploadQuotaRow) Scan(dest ...interface{}) error {
	if len(dest) != 1 {
		return fmt.Errorf("unexpected scan destinations: %d", len(dest))
	}
	target, ok := dest[0].(*int64)
	if !ok {
		return fmt.Errorf("unexpected scan destination type %T", dest[0])
	}
	*target = r.value
	return nil
}

// uploadInfoRow scans the ten columns UploadService.GetUploadStatus selects,
// in the same order, so the chunk/status paths can read a real record instead
// of failing with "not implemented".
type uploadInfoRow struct{ info *service.UploadInfo }

func (r uploadInfoRow) Scan(dest ...interface{}) error {
	if len(dest) != 10 {
		return fmt.Errorf("unexpected scan destinations: %d", len(dest))
	}
	setString := func(target interface{}, value string) error {
		switch t := target.(type) {
		case *string:
			*t = value
		case *sql.NullString:
			*t = sql.NullString{String: value, Valid: true}
		default:
			return fmt.Errorf("unexpected destination type %T", target)
		}
		return nil
	}
	if err := setString(dest[0], r.info.ID); err != nil {
		return err
	}
	if err := setString(dest[1], r.info.Filename); err != nil {
		return err
	}
	size, ok := dest[2].(*int64)
	if !ok {
		return fmt.Errorf("unexpected size destination type %T", dest[2])
	}
	*size = r.info.Size
	if err := setString(dest[3], r.info.ContentType); err != nil {
		return err
	}
	if err := setString(dest[4], r.info.Hash); err != nil {
		return err
	}
	if err := setString(dest[5], r.info.Status); err != nil {
		return err
	}
	if err := setString(dest[6], r.info.URL); err != nil {
		return err
	}
	if err := setString(dest[7], r.info.OwnerID); err != nil {
		return err
	}
	createdAt, ok := dest[8].(*time.Time)
	if !ok {
		return fmt.Errorf("unexpected created_at destination type %T", dest[8])
	}
	*createdAt = r.info.CreatedAt
	updatedAt, ok := dest[9].(*time.Time)
	if !ok {
		return fmt.Errorf("unexpected updated_at destination type %T", dest[9])
	}
	*updatedAt = r.info.UpdatedAt
	return nil
}

func (d *uploadMockDB) QueryRow(_ context.Context, query string, args ...interface{}) *storage.CancelRow {
	upper := strings.ToUpper(strings.TrimSpace(query))
	if strings.Contains(upper, "FROM UPLOADS") && strings.Contains(upper, "WHERE ID = $1") {
		uploadID := ""
		if len(args) > 0 {
			uploadID = fmt.Sprintf("%v", args[0])
		}
		record, ok := d.uploads[uploadID]
		if !ok {
			return storage.NewErrorCancelRow(sql.ErrNoRows)
		}
		return storage.NewTestCancelRow(uploadInfoRow{info: record})
	}
	if strings.Contains(upper, "COALESCE(SUM(SIZE), 0)") && strings.Contains(upper, "FROM UPLOADS") {
		owner := ""
		if len(args) > 0 {
			owner = fmt.Sprintf("%v", args[0])
		}
		var used int64
		for _, record := range d.uploads {
			if record.OwnerID == owner {
				used += record.Size
			}
		}
		return storage.NewTestCancelRow(uploadQuotaRow{value: used})
	}
	return storage.NewErrorCancelRow(fmt.Errorf("not implemented"))
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
func setupUploadE2EServer(t *testing.T, store service.SegmentStorage) (*service.AuthService, *httptest.Server) {
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
		gateway.WithChallengeStore(storage.NewMemoryChallengeStore()),
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
	return authService, server
}

func TestUploadRoutes_Return503WhenNoService(t *testing.T) {
	authService, _ := newTestAuthService()
	checker := &mockNFTChecker{verifyResult: true, balance: big.NewInt(1)}
	cfg := testConfig()

	opts := []gateway.RouterOption{
		gateway.WithAuthService(authService),
		gateway.WithChallengeStore(storage.NewMemoryChallengeStore()),
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

	token := testJWT(authService, "0xTestWallet")

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

// minimalMP4 is a valid ISO-BMFF header: the upload handler sniffs the ftyp
// box at offset 4 and rejects payloads that are not a real container format,
// so a text fixture would (correctly) be refused with 400.
func minimalMP4() []byte {
	header := []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00,
		'i', 's', 'o', 'm', 'i', 's', 'o', '2',
	}
	return append(header, bytes.Repeat([]byte{0x00}, 32)...)
}

func TestUploadWholeFile(t *testing.T) {
	store := newMockSegmentStorage()
	svc, server := setupUploadE2EServer(t, store)
	token := testJWT(svc, "0xCreatorWallet")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test-video.mp4")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(minimalMP4()); err != nil {
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
	store := newMockSegmentStorage()
	svc, server := setupUploadE2EServer(t, store)
	token := testJWT(svc, "0xCreatorWallet")

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
	store := newMockSegmentStorage()
	svc, server := setupUploadE2EServer(t, store)
	token := testJWT(svc, "0xCreatorWallet")

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
	_, _ = part.Write(minimalMP4())
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
	svc, server := setupUploadE2EServer(t, store)
	token := testJWT(svc, "0xCreatorWallet")

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
