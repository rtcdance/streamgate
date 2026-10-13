package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/service/serviceerrors"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// sealRecorder records every statement executed inside a transaction so tests
// can assert on the exact contents/ uploads writes produced by sealing.
type sealRecorder struct {
	mu    sync.Mutex
	stmts []sealRecordedStmt
}

type sealRecordedStmt struct {
	query string
	args  []driver.Value
}

var globalSealRecorder = &sealRecorder{}

func init() {
	sql.Register("sealrecord", globalSealRecorder)
}

func (d *sealRecorder) Open(_ string) (driver.Conn, error) { return &sealConn{d: d}, nil }

func (d *sealRecorder) reset() {
	d.mu.Lock()
	d.stmts = nil
	d.mu.Unlock()
}

func (d *sealRecorder) find(substr string) (sealRecordedStmt, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.stmts {
		if strings.Contains(s.query, substr) {
			return s, true
		}
	}
	return sealRecordedStmt{}, false
}

func (d *sealRecorder) count(substr string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, s := range d.stmts {
		if strings.Contains(s.query, substr) {
			n++
		}
	}
	return n
}

type sealConn struct{ d *sealRecorder }

func (c *sealConn) Prepare(query string) (driver.Stmt, error) {
	return &sealStmt{d: c.d, query: query}, nil
}
func (c *sealConn) Close() error              { return nil }
func (c *sealConn) Begin() (driver.Tx, error) { return sealTx{}, nil }

type sealTx struct{}

func (sealTx) Commit() error   { return nil }
func (sealTx) Rollback() error { return nil }

type sealStmt struct {
	d     *sealRecorder
	query string
}

func (s *sealStmt) Close() error  { return nil }
func (s *sealStmt) NumInput() int { return -1 }
func (s *sealStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.d.mu.Lock()
	s.d.stmts = append(s.d.stmts, sealRecordedStmt{query: s.query, args: args})
	s.d.mu.Unlock()
	return sealExecResult{}, nil
}
func (s *sealStmt) Query(_ []driver.Value) (driver.Rows, error) {
	return nil, errors.New("not implemented")
}

type sealExecResult struct{}

func (sealExecResult) LastInsertId() (int64, error) { return 0, nil }
func (sealExecResult) RowsAffected() (int64, error) { return 1, nil }

func sealTxDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sealrecord", "")
	require.NoError(t, err)
	return db
}

func testSHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestAcceptance_OriginalMediaDigest verifies the end-to-end sealing contract:
// the digest stored on the content is the SHA-256 of the actual original bytes,
// those bytes are copied to a content-bound immutable key, and the content row
// records digest + key + verified flag with the server-verified owner.
func TestAcceptance_OriginalMediaDigest(t *testing.T) {
	globalSealRecorder.reset()

	media := []byte("real original media bytes for digest acceptance")
	uploadID := ""
	var savedHash string
	var savedHashQuery string

	db := &mockDB{
		execFn: func(_ context.Context, query string, args ...interface{}) (sql.Result, error) {
			if strings.Contains(query, "INSERT INTO uploads") {
				// INSERT INTO uploads (id, filename, size, content_type, hash, ...)
				if len(args) >= 5 {
					if h, ok := args[4].(string); ok {
						savedHash = h
						savedHashQuery = query
					}
				}
			}
			return &mockResult{rowsAffected: 1}, nil
		},
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "filename") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					uploadID, "video.mp4", int64(len(media)),
					"video/mp4", savedHash, "completed",
					"/bucket/owner1/" + uploadID + ".mp4", "owner1",
					time.Now(), time.Now(),
				}})
			}
			return stg.NewErrorCancelRow(fmt.Errorf("unexpected query: %s", query))
		},
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			txDB := sealTxDB(t)
			defer func() { _ = txDB.Close() }()
			tx, err := txDB.Begin()
			if err != nil {
				return err
			}
			if err := fn(tx); err != nil {
				_ = tx.Rollback()
				return err
			}
			return tx.Commit()
		},
	}
	store := newMockObjStore()
	svc := NewUploadService(db, store, "bucket", zap.NewNop())

	// The upload path computes SHA-256 while writing the staged object; no
	// client-supplied hash is involved anywhere in this flow.
	id, err := svc.UploadStream(context.Background(), "video.mp4", bytes.NewReader(media), int64(len(media)), "owner1")
	require.NoError(t, err)
	uploadID = id

	require.NotEmpty(t, savedHashQuery, "upload row insert not recorded")
	require.Equal(t, testSHA256Hex(media), savedHash,
		"uploads.hash must be the server-computed SHA-256 of the streamed bytes")

	contentID, err := svc.CompleteUploadWithTx(context.Background(), uploadID)
	require.NoError(t, err)
	require.NotEmpty(t, contentID)

	// The sealed original lives at a content-bound, digest-named key and holds
	// exactly the original bytes.
	sealedKey := "originals/" + contentID + "/" + savedHash + ".mp4"
	sealed, ok := store.data["bucket/"+sealedKey]
	require.True(t, ok, "sealed original missing at %s", sealedKey)
	require.Equal(t, media, sealed, "sealed bytes must equal the uploaded original")

	// The recorded digest is reproducible from the sealed bytes alone.
	require.Equal(t, savedHash, testSHA256Hex(sealed))

	// contents row: file_hash, original_key, original_verified=TRUE, owner from upload.
	insert, ok := sealRecorderFind(t, "INSERT INTO contents")
	require.True(t, ok, "contents insert not recorded")
	require.Contains(t, insert.args, savedHash, "contents.file_hash must carry the verified digest")
	require.Contains(t, insert.args, sealedKey, "contents.original_key must carry the sealed key")
	require.Contains(t, insert.args, true, "contents.original_verified must be TRUE")
	require.Contains(t, insert.args, "owner1", "contents.owner_id must be the verified upload owner")

	// The upload row is bound to the content it produced.
	bind, ok := sealRecorderFind(t, "UPDATE uploads")
	require.True(t, ok, "uploads update not recorded")
	require.Contains(t, bind.args, contentID, "uploads.content_id must be bound to the created content")
}

// TestAcceptance_ImmutableOriginalConflict verifies that once a content's
// original digest is sealed, re-completing the same content against different
// staged bytes fails with ErrOriginalImmutable (HTTP 409), while the same
// digest stays idempotent and the sealed object is never replaced.
func TestAcceptance_ImmutableOriginalConflict(t *testing.T) {
	globalSealRecorder.reset()

	original := []byte("first sealed original bytes")
	tampered := []byte("second, different original bytes")
	origDigest := testSHA256Hex(original)

	const uploadID = "up-1"
	const stagingKey = "bucket/owner1/up-1.mp4"

	store := newMockObjStore()
	store.data[stagingKey] = original

	phase := "fresh" // fresh | processed
	var contentID string

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			switch {
			case strings.Contains(query, "filename"):
				status := "completed"
				if phase == "processed" {
					status = "processed"
				}
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					uploadID, "video.mp4", int64(len(original)),
					"video/mp4", origDigest, status,
					"/bucket/owner1/up-1.mp4", "owner1",
					time.Now(), time.Now(),
				}})
			case strings.Contains(query, "content_id FROM uploads"):
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{contentID}})
			case strings.Contains(query, "FROM contents"):
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{origDigest, true}})
			default:
				return stg.NewErrorCancelRow(fmt.Errorf("unexpected query: %s", query))
			}
		},
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			txDB := sealTxDB(t)
			defer func() { _ = txDB.Close() }()
			tx, err := txDB.Begin()
			if err != nil {
				return err
			}
			if err := fn(tx); err != nil {
				_ = tx.Rollback()
				return err
			}
			return tx.Commit()
		},
	}
	svc := NewUploadService(db, store, "bucket", zap.NewNop())

	// First completion seals the original.
	cid, err := svc.CompleteUploadWithTx(context.Background(), uploadID)
	require.NoError(t, err)
	require.NotEmpty(t, cid)
	contentID = cid
	sealedKey := "originals/" + cid + "/" + origDigest + ".mp4"
	require.Equal(t, original, store.data["bucket/"+sealedKey])

	// Same digest again: idempotent, returns the same content, no change.
	phase = "processed"
	cid2, err := svc.CompleteUploadWithTx(context.Background(), uploadID)
	require.NoError(t, err)
	require.Equal(t, cid, cid2)
	require.Equal(t, 1, globalSealRecorder.count("INSERT INTO contents"),
		"idempotent completion must not create a second content")

	// Staged bytes replaced (e.g. presigned re-PUT): re-sealing a different
	// digest for the same content must be rejected, not silently accepted.
	store.data[stagingKey] = tampered
	_, err = svc.CompleteUploadWithTx(context.Background(), uploadID)
	require.Error(t, err)
	require.True(t, errors.Is(err, serviceerrors.ErrOriginalImmutable),
		"expected ErrOriginalImmutable, got %v", err)

	// The rejected re-seal left the sealed original untouched.
	require.Equal(t, original, store.data["bucket/"+sealedKey])
	require.Equal(t, 1, globalSealRecorder.count("INSERT INTO contents"))
}

func sealRecorderFind(t *testing.T, substr string) (sealRecordedStmt, bool) {
	t.Helper()
	s, ok := globalSealRecorder.find(substr)
	return s, ok
}
