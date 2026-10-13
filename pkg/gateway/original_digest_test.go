package gateway

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/service"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// digestRowScanner answers the upload/content lookups needed by the sealing
// conflict path (upload row, uploads.content_id binding, contents digest state).
type digestRowScanner struct {
	vals []interface{}
}

func (s *digestRowScanner) Scan(dest ...interface{}) error {
	if len(s.vals) < len(dest) {
		return fmt.Errorf("not enough columns: %d < %d", len(s.vals), len(dest))
	}
	for i, d := range dest {
		switch v := s.vals[i].(type) {
		case string:
			switch p := d.(type) {
			case *string:
				*p = v
			case *sql.NullString:
				p.String = v
				p.Valid = true
			}
		case bool:
			if p, ok := d.(*bool); ok {
				*p = v
			}
		case int64:
			if p, ok := d.(*int64); ok {
				*p = v
			}
		case time.Time:
			if p, ok := d.(*time.Time); ok {
				*p = v
			}
		}
	}
	return nil
}

func digestTestSHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// setupDigestConflictDB returns a DB mock whose queries match the processed
// completion path: upload already sealed against origDigest for content c1,
// while the staged object currently holds different bytes.
func setupDigestConflictDB(contentID, origDigest, owner string) *uploadMockDB {
	return &uploadMockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			switch {
			case strings.Contains(query, "filename"):
				return stg.NewTestCancelRow(&digestRowScanner{vals: []interface{}{
					"up-1", "video.mp4", int64(24),
					"video/mp4", origDigest, "processed",
					"/bucket/key", owner,
					time.Now(), time.Now(),
				}})
			case strings.Contains(query, "content_id FROM uploads"):
				return stg.NewTestCancelRow(&digestRowScanner{vals: []interface{}{contentID}})
			case strings.Contains(query, "FROM contents"):
				return stg.NewTestCancelRow(&digestRowScanner{vals: []interface{}{origDigest, true}})
			default:
				return stg.NewErrorCancelRow(fmt.Errorf("unexpected query: %s", query))
			}
		},
	}
}

// TestUploadHandlers_CompleteUpload_OriginalConflict409: re-completing a sealed
// content whose staged bytes now hash to a different digest is a 409 conflict.
func TestUploadHandlers_CompleteUpload_OriginalConflict409(t *testing.T) {
	origBytes := []byte("already sealed original bytes")
	origDigest := digestTestSHA256Hex(origBytes)
	origKey := "originals/c1/" + origDigest + ".mp4"

	store := newUploadMockObjStore()
	store.data["bucket/"+origKey] = origBytes
	store.data["bucket/key"] = []byte("tampered staged bytes")

	db := setupDigestConflictDB("c1", origDigest, "0xOwner")
	svc := service.NewUploadService(db, store, "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/up-1/complete-upload", http.NoBody)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrOriginalImmutable, resp["code"])

	// The rejected re-seal must not have replaced the sealed object.
	require.Equal(t, origBytes, store.data["bucket/"+origKey])
}

// TestUploadHandlers_CompleteUpload_SameDigestIdempotent: the same sealed digest
// completes again with 200 and the original content id, without a new content.
func TestUploadHandlers_CompleteUpload_SameDigestIdempotent(t *testing.T) {
	origBytes := []byte("already sealed original bytes")
	origDigest := digestTestSHA256Hex(origBytes)

	store := newUploadMockObjStore()
	store.data["bucket/key"] = origBytes

	db := setupDigestConflictDB("c1", origDigest, "0xOwner")
	svc := service.NewUploadService(db, store, "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/up-1/complete-upload", http.NoBody)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "c1", resp["content_id"])
}

// TestHandleGetContent_IncludesVerifiedDigestFileHash: GET /api/v1/content/:id
// exposes the verified original digest.
func TestHandleGetContent_IncludesVerifiedDigestFileHash(t *testing.T) {
	digest := digestTestSHA256Hex([]byte("sealed original"))
	cache := newContentMockCache()
	_ = cache.Set("content:c1", &service.Content{
		ID:       "c1",
		Title:    "Sealed",
		Type:     "video",
		OwnerID:  "0xOwner",
		FileHash: digest,
	})
	svc := service.NewContentService(&contentMockDB{}, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/c1", http.NoBody)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	contentMap, ok := resp["content"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, digest, contentMap["file_hash"])
}

// TestHandleCreateContent_RejectsClientDigest: the create response carries
// file_hash but a client-supplied digest is never trusted.
func TestHandleCreateContent_RejectsClientDigest(t *testing.T) {
	db := &contentMockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &contentMockResult{}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := `{"title":"test","type":"video","file_hash":"` + strings.Repeat("ab", 32) + `"}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	contentMap, ok := resp["content"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, contentMap, "file_hash", "create response must expose file_hash")
	assert.Equal(t, "", contentMap["file_hash"], "client-supplied file_hash must never be accepted")
}
