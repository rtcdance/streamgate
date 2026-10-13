package registry

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/rtcdance/streamgate/pkg/storage"
)

// IdempotencyStore backs the HTTP retry records of SG-13 A.2. The unique key
// is (actor, operation, scope, idempotency_key); the canonical payload digest
// is the COMPARISON column and deliberately NOT part of that key — same key +
// same payload replays the stored response, same key + different payload is a
// 409.
type IdempotencyStore struct {
	db storage.DB
}

func NewIdempotencyStore(db storage.DB) *IdempotencyStore {
	return &IdempotencyStore{db: db}
}

// AttestOperation is the operation name recorded for the attest endpoint.
const AttestOperation = "content.attest"

// AttestScope builds the chain/registry scope part of the idempotency key.
func AttestScope(chainID int64, registryAddress string) string {
	return fmt.Sprintf("chain:%d:registry:%s", chainID, strings.ToLower(registryAddress))
}

// CanonicalAttestPayload fixes the field order and normalisation of the
// comparison payload (A.2): fixed field order, normalised (lowercase)
// addresses and the 64-char lowercase digest; timestamps and any other
// non-semantic field are excluded.
func CanonicalAttestPayload(contentID, creator, contentDigest, metadataURI string) string {
	return strings.Join([]string{
		"v1",
		contentID,
		strings.ToLower(creator),
		strings.ToLower(contentDigest),
		metadataURI,
	}, "\x1f")
}

// PayloadDigest returns the hex SHA-256 of the canonical payload.
func PayloadDigest(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// Get returns the stored record for the key, or nil when none exists.
func (s *IdempotencyStore) Get(ctx context.Context, actor, scope, key string) (payloadDigest string, response []byte, found bool, err error) {
	row := s.db.QueryRow(ctx, `
		SELECT payload_digest, response FROM http_idempotency
		WHERE actor = $1 AND operation = $2 AND scope = $3 AND idempotency_key = $4`,
		strings.ToLower(actor), AttestOperation, scope, key)
	var digest string
	var resp []byte
	switch e := row.Scan(&digest, &resp); e {
	case nil:
		return digest, resp, true, nil
	case sql.ErrNoRows:
		return "", nil, false, nil
	default:
		return "", nil, false, fmt.Errorf("registry: idempotency lookup: %w", e)
	}
}

// Record inserts the response for the key. inserted=false means a concurrent
// (or earlier) record already exists; the caller must re-read it and compare
// digests (equal → replay the stored response, different → 409).
func (s *IdempotencyStore) Record(ctx context.Context, actor, scope, key, payloadDigest string, response []byte) (inserted bool, err error) {
	res, err := s.db.Exec(ctx, `
		INSERT INTO http_idempotency (actor, operation, scope, idempotency_key, payload_digest, response)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (actor, operation, scope, idempotency_key) DO NOTHING`,
		strings.ToLower(actor), AttestOperation, scope, key, payloadDigest, response)
	if err != nil {
		return false, fmt.Errorf("registry: idempotency record: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
