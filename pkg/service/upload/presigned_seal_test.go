package upload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/service/serviceerrors"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestAcceptance_PresignedOriginalSealed pins the presigned contract: the bytes
// are written by the client straight to the staging key, so completion must
// read them back, hash the actual bytes, and only then move the upload to
// "completed" so the sealing path can run. Skipping the read-back would record
// an empty digest for bytes the server never saw — a digest that attests to
// nothing, exactly what this task exists to prevent.
func TestAcceptance_PresignedOriginalSealed(t *testing.T) {
	globalSealRecorder.reset()

	presigned := []byte("client-written presigned original bytes")
	digest := testSHA256Hex(presigned)
	const uploadID = "up-presigned"
	const stagingKey = "bucket/owner1/up-presigned.mp4"

	store := newMockObjStore()
	store.data[stagingKey] = presigned

	phase := "uploading" // uploading -> completed by FinalizePresignedUpload
	var finalizeHash string
	var finalizeQuery string

	db := &mockDB{
		execFn: func(_ context.Context, query string, args ...interface{}) (sql.Result, error) {
			if strings.Contains(query, "SET status = 'completed'") {
				finalizeQuery = query
				if len(args) > 1 {
					if h, ok := args[1].(string); ok {
						finalizeHash = h
					}
				}
				phase = "completed"
			}
			return &mockResult{rowsAffected: 1}, nil
		},
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			switch {
			case strings.Contains(query, "filename"):
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					uploadID, "video.mp4", int64(len(presigned)),
					"video/mp4", "", phase,
					"/bucket/owner1/up-presigned.mp4", "owner1",
					time.Now(), time.Now(),
				}})
			case strings.Contains(query, "upload_chunks"):
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{0}})
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

	// A chunked upload must not be presigned-finalisable: its bytes live in
	// chunks that were never merged, so the staged object does not exist yet.
	chunked := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "filename") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"up-chunked", "video.mp4", int64(len(presigned)),
					"video/mp4", "", "uploading",
					"/bucket/owner1/up-chunked.mp4", "owner1",
					time.Now(), time.Now(),
				}})
			}
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{3}})
		},
	}
	chunkedSvc := NewUploadService(chunked, newMockObjStore(), "bucket", zap.NewNop())
	err := chunkedSvc.FinalizePresignedUpload(context.Background(), "up-chunked", "owner1")
	require.Error(t, err, "a chunked upload must not be presigned-finalised without merging")

	// Owner mismatch is refused before any state change.
	wrongOwnerSvc := NewUploadService(db, store, "bucket", zap.NewNop())
	err = wrongOwnerSvc.FinalizePresignedUpload(context.Background(), uploadID, "owner2")
	require.Error(t, err, "finalising another wallet's upload must fail")
	require.Empty(t, finalizeHash, "no digest may be recorded for a rejected finalise")

	// The real finalise hashes the staged bytes the client PUT.
	require.NoError(t, svc.FinalizePresignedUpload(context.Background(), uploadID, "owner1"))
	require.NotEmpty(t, finalizeQuery, "finalise must update the upload row")
	require.Equal(t, digest, finalizeHash,
		"uploads.hash must be the SHA-256 of the bytes actually sitting in storage")

	// Completion now runs the normal sealing path over those bytes.
	contentID, err := svc.CompleteUploadWithTx(context.Background(), uploadID)
	require.NoError(t, err)
	require.NotEmpty(t, contentID)

	sealedKey := "originals/" + contentID + "/" + digest + ".mp4"
	require.Equal(t, presigned, store.data["bucket/"+sealedKey],
		"the sealed original must equal the client-written bytes")

	insert, ok := sealRecorderFind(t, "INSERT INTO contents")
	require.True(t, ok, "contents insert not recorded")
	require.Contains(t, insert.args, digest, "contents.file_hash must carry the verified digest")

	// Missing staged object (client never PUT): refused, nothing recorded.
	phase = "uploading"
	empty := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	err = empty.FinalizePresignedUpload(context.Background(), uploadID, "owner1")
	require.Error(t, err)
	require.True(t, errors.Is(err, serviceerrors.ErrStagedObjectMissing) || strings.Contains(err.Error(), "not found"),
		"missing staged object must fail explicitly, got %v", err)
}
