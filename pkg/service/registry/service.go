package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Attest-time sentinel errors mapped to HTTP statuses by the gateway.
var (
	ErrContentNotFound       = errors.New("registry: content not found")
	ErrNotOwner              = errors.New("registry: caller does not own the content")
	ErrOriginalNotVerified   = errors.New("registry: original digest not verified")
	ErrRegistryMisconfigured = errors.New("registry: registry not configured")
)

// AttestResponse is the attest endpoint payload: the durable intent status,
// never a "success" claim about the chain (A.7 — the chain registration is a
// separate visible status).
type AttestResponse struct {
	ContentID       string `json:"content_id"`
	Status          string `json:"status"`
	TxHash          string `json:"tx_hash,omitempty"`
	Nonce           *int64 `json:"nonce,omitempty"`
	BlockNumber     *int64 `json:"block_number,omitempty"`
	BlockHash       string `json:"block_hash,omitempty"`
	ChainID         int64  `json:"chain_id"`
	RegistryAddress string `json:"registry_address"`
	Creator         string `json:"creator"`
	ContentDigest   string `json:"content_digest"`
	MetadataURI     string `json:"metadata_uri,omitempty"`
	Attempt         int    `json:"attempt"`
	LastError       string `json:"last_error,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// Service is the attest orchestration: it reads the verified digest from the
// contents row server-side (a client can never submit an arbitrary bytes32),
// creates-or-reuses the unique (chain, registry, content) intent, and wraps
// the whole exchange in the HTTP idempotency record when the client sent an
// Idempotency-Key.
type Service struct {
	store *Store
	idem  *IdempotencyStore
}

func NewService(store *Store, idem *IdempotencyStore) *Service {
	return &Service{store: store, idem: idem}
}

// Store exposes the intent store for wiring into content/upload services.
func (s *Service) Store() *Store { return s.store }

type contentRow struct {
	ownerID     string
	fileHash    string
	verified    bool
	metadataURI string
}

func (s *Service) loadContent(ctx context.Context, contentID string) (*contentRow, error) {
	var row contentRow
	err := s.store.db.QueryRow(ctx, `
		SELECT owner_id, COALESCE(file_hash, ''), COALESCE(original_verified, FALSE), COALESCE(url, '')
		FROM contents WHERE id = $1`, contentID).
		Scan(&row.ownerID, &row.fileHash, &row.verified, &row.metadataURI)
	if err == sql.ErrNoRows {
		return nil, ErrContentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("registry: load content %s: %w", contentID, err)
	}
	return &row, nil
}

// Attest creates or reuses the register intent for a content the caller owns.
// Returns (response, conflict, error):
//
//   - new or existing intent → 200 with the CURRENT durable intent status
//   - replay with same Idempotency-Key + same canonical payload → the stored
//     response, no new row, no new chain write
//   - replay with same key + different payload (e.g. the verified digest
//     changed since) → conflict=true, the handler answers 409
func (s *Service) Attest(ctx context.Context, actor, contentID, idemKey string) (*AttestResponse, bool, error) {
	if s.store == nil {
		return nil, false, ErrRegistryMisconfigured
	}
	row, err := s.loadContent(ctx, contentID)
	if err != nil {
		return nil, false, err
	}
	// Security: creator is the server-verified owner and the digest is read
	// from contents.file_hash — never from the request body.
	if !strings.EqualFold(row.ownerID, actor) {
		return nil, false, ErrNotOwner
	}
	if !row.verified {
		return nil, false, fmt.Errorf("%w: content %s has no verified original", ErrOriginalNotVerified, contentID)
	}
	digest, err := DecodeDigest(row.fileHash)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrOriginalNotVerified, err)
	}
	_ = digest // the bytes32 form exists only server-side; the intent row carries the hex digest

	scope := AttestScope(s.store.chainID, s.store.registryAddress)
	payload := PayloadDigest(CanonicalAttestPayload(contentID, row.ownerID, row.fileHash, row.metadataURI))

	if idemKey != "" && s.idem != nil {
		digestKey, stored, found, gerr := s.idem.Get(ctx, actor, scope, idemKey)
		if gerr != nil {
			return nil, false, gerr
		}
		if found {
			if digestKey != payload {
				return nil, true, nil
			}
			var replayed AttestResponse
			if uerr := json.Unmarshal(stored, &replayed); uerr != nil {
				// A corrupted stored body must not masquerade as a replay.
				return nil, false, fmt.Errorf("registry: stored idempotent response unreadable: %w", uerr)
			}
			return &replayed, false, nil
		}
	}

	if err := s.store.EnsureIntent(ctx, contentID, row.ownerID, row.fileHash, row.metadataURI); err != nil {
		return nil, false, err
	}
	in, err := s.store.Get(ctx, contentID)
	if err != nil {
		return nil, false, err
	}
	if in == nil {
		return nil, false, fmt.Errorf("registry: intent for %s vanished after ensure", contentID)
	}
	resp := intentResponse(in)

	if idemKey != "" && s.idem != nil {
		body, _ := json.Marshal(resp)
		inserted, rerr := s.idem.Record(ctx, actor, scope, idemKey, payload, body)
		if rerr != nil {
			return nil, false, rerr
		}
		if !inserted {
			// A concurrent request with the same key won the record; its stored
			// response is authoritative for this key.
			digestKey, stored, found, gerr := s.idem.Get(ctx, actor, scope, idemKey)
			if gerr != nil {
				return nil, false, gerr
			}
			if found {
				if digestKey != payload {
					return nil, true, nil
				}
				var replayed AttestResponse
				if uerr := json.Unmarshal(stored, &replayed); uerr == nil {
					return &replayed, false, nil
				}
			}
		}
	}
	return resp, false, nil
}

func intentResponse(in *Intent) *AttestResponse {
	resp := &AttestResponse{
		ContentID:       in.ContentID,
		Status:          in.Status,
		TxHash:          in.TxHash,
		ChainID:         in.ChainID,
		RegistryAddress: in.RegistryAddress,
		Creator:         in.Creator,
		ContentDigest:   in.ContentDigest,
		MetadataURI:     in.MetadataURI,
		Attempt:         in.Attempt,
		LastError:       in.LastError,
		CreatedAt:       in.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       in.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if in.Nonce.Valid {
		n := in.Nonce.Int64
		resp.Nonce = &n
	}
	if in.BlockNumber.Valid {
		b := in.BlockNumber.Int64
		resp.BlockNumber = &b
	}
	resp.BlockHash = in.BlockHash
	return resp
}
