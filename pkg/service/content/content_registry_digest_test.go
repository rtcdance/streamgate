package content

import (
	"context"
	"database/sql"
	"encoding/hex"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type recordedRegistration struct {
	contentID   string
	creator     string
	digest      [32]byte
	metadataURI string
}

type recordingContentRegistry struct {
	mu    sync.Mutex
	calls []recordedRegistration
	err   error
}

func (r *recordingContentRegistry) RegisterContent(_ context.Context, contentID, creator string, contentDigest [32]byte, metadataURI string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, recordedRegistration{
		contentID:   contentID,
		creator:     creator,
		digest:      contentDigest,
		metadataURI: metadataURI,
	})
	r.mu.Unlock()
	if r.err != nil {
		return "", r.err
	}
	return "0xregtx", nil
}

func (r *recordingContentRegistry) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestContentService_CreateContentWithTx_RegistersVerifiedDigest(t *testing.T) {
	scanDB := contentOpenDB()
	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	reg := &recordingContentRegistry{}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
	svc.SetContentRegistry(reg)

	digestHex := strings.Repeat("ab", 32)
	id, err := svc.CreateContentWithTx(context.Background(), &Content{
		ID:       "c1",
		Title:    "Sealed Video",
		Type:     "video",
		OwnerID:  "0xabc0000000000000000000000000000000000001",
		FileHash: digestHex,
	})
	require.NoError(t, err)
	require.Equal(t, "c1", id)

	require.Len(t, reg.calls, 1, "verified digest must trigger on-chain registration")
	call := reg.calls[0]
	assert.Equal(t, "c1", call.contentID)
	assert.Equal(t, "0xabc0000000000000000000000000000000000001", call.creator,
		"creator must be the verified content owner, not a client field")

	raw, err := hex.DecodeString(digestHex)
	require.NoError(t, err)
	var want [32]byte
	copy(want[:], raw)
	assert.Equal(t, want, call.digest, "digest must be the hex-decoded 32-byte original digest")
}

func TestContentService_CreateContentWithTx_RegistryErrorKeepsInsert(t *testing.T) {
	scanDB := contentOpenDB()
	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	reg := &recordingContentRegistry{err: assert.AnError}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
	svc.SetContentRegistry(reg)

	// Chain write is not atomic with the DB insert: a registry failure must
	// never roll back or fail the content row.
	id, err := svc.CreateContentWithTx(context.Background(), &Content{
		ID:       "c1",
		Title:    "Sealed Video",
		Type:     "video",
		OwnerID:  "0xabc0000000000000000000000000000000000001",
		FileHash: strings.Repeat("cd", 32),
	})
	require.NoError(t, err)
	assert.Equal(t, "c1", id)
	assert.Equal(t, 1, reg.callCount())
}

func TestContentService_CreateContentWithTx_UnverifiedDigestDefersRegistration(t *testing.T) {
	tests := []struct {
		name     string
		fileHash string
		ownerID  string
	}{
		{name: "empty digest", fileHash: "", ownerID: "0xabc0000000000000000000000000000000000001"},
		{name: "not a 64-char hex digest", fileHash: "deadbeef", ownerID: "0xabc0000000000000000000000000000000000001"},
		{name: "non-hex digest", fileHash: strings.Repeat("zz", 32), ownerID: "0xabc0000000000000000000000000000000000001"},
		{name: "missing verified owner", fileHash: strings.Repeat("ab", 32), ownerID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanDB := contentOpenDB()
			db := &mockDB{
				beginFn: func(_ context.Context) (*sql.Tx, error) {
					return scanDB.Begin()
				},
			}
			reg := &recordingContentRegistry{}
			svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
			svc.SetContentRegistry(reg)

			id, err := svc.CreateContentWithTx(context.Background(), &Content{
				ID:       "c1",
				Title:    "Unverified",
				Type:     "video",
				OwnerID:  tt.ownerID,
				FileHash: tt.fileHash,
			})
			require.NoError(t, err)
			assert.Equal(t, "c1", id)
			assert.Equal(t, 0, reg.callCount(),
				"without a verified digest + owner the registration must stay deferred")
		})
	}
}
