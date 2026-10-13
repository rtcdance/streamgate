// Package registry implements SG-13 A (登记与交易): durable off-chain
// consistency for the on-chain content registration flow.
//
// The chain write is NEVER atomic with the DB write. Business success means
// the media and the register INTENT are committed; the chain registration is a
// separate, visible status tracked by the publisher loop. Every state the
// intent can be in is persisted in Postgres, so nothing lives only in a
// process map and nothing is "recovered" by re-reading a log line.
//
// Intent lifecycle (persisted in content_registry_intents):
//
//	pending          intent recorded in the same tx as the content row
//	submitted        signed raw tx persisted, then broadcast accepted (or the
//	                 node is known to have the tx)
//	confirmed        receipt.status=1 AND head >= receipt block + N confirmations
//	broadcast_unknown broadcast outcome ambiguous (RPC timeout): the nonce is
//	                 kept, the SAME raw tx is re-sent after a receipt lookup
//	failed           definitively dead (revert, or nonce consumed by another tx)
//	disabled         registry not configured (no intent row is created)
//
// Transition guards: every UPDATE is conditioned on the current status so two
// publishers racing on a lease boundary cannot stomp each other's outcome.
package registry

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3/tx"
)

// Intent status vocabulary (SG-13 A.1). A log line is not a status.
const (
	StatusPending          = "pending"
	StatusSubmitted        = "submitted"
	StatusConfirmed        = "confirmed"
	StatusFailed           = "failed"
	StatusBroadcastUnknown = "broadcast_unknown"
	StatusDisabled         = "disabled"
)

// Intent is one durable register intent: unique on (chain_id, registry_address,
// content_id). A client HTTP retry can therefore never create a second chain
// write for the same content.
type Intent struct {
	ContentID       string
	ChainID         int64
	RegistryAddress string
	Creator         string
	ContentDigest   string
	MetadataURI     string
	Status          string
	TxHash          string
	Nonce           sql.NullInt64
	BlockNumber     sql.NullInt64
	BlockHash       string
	ConfirmedAt     sql.NullTime
	LastError       string
	Attempt         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Attempt is one signed raw transaction, persisted BEFORE broadcast.
type Attempt struct {
	ReservationKey string
	ChainID        int64
	Signer         string
	Nonce          int64
	TxHash         string
	RawTx          string
	Status         string
}

// ReservationKey links an attempt to its nonce reservation. The same key is
// reused for an explicit same-nonce replacement, which adds an attempt row but
// never a new reservation.
func ReservationKey(chainID int64, signer string, nonce uint64) string {
	return fmt.Sprintf("%d|%s|%d", chainID, strings.ToLower(signer), nonce)
}

// Store is the Postgres-backed intent/nonce/attempt repository plus the
// registry coordinates (chain id + contract address) every write is scoped to.
type Store struct {
	db              storage.DB
	chainID         int64
	registryAddress string
	// ledger is the SHARED tx.NonceLedger: one allocator implementation, one
	// reservations table, for every signer path in the process.
	ledger      *tx.NonceLedger
	nonceClient tx.NonceClient
}

// StoreOption customises Store construction.
type StoreOption func(*Store)

// WithNonceClient injects the chain client the shared nonce ledger needs to
// prime a first allocation and to adjudicate releasing an attempt-referenced
// nonce. Callers that always pass their own chainPending hint (and never
// release signed nonces) do not need it.
func WithNonceClient(client tx.NonceClient) StoreOption {
	return func(s *Store) {
		s.nonceClient = client
	}
}

func NewStore(db storage.DB, chainID int64, registryAddress string, opts ...StoreOption) *Store {
	s := &Store{
		db:              db,
		chainID:         chainID,
		registryAddress: strings.ToLower(registryAddress),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.ledger = tx.NewNonceLedger(db, chainID, s.nonceClient, nil)
	return s
}

func (s *Store) ChainID() int64          { return s.chainID }
func (s *Store) RegistryAddress() string { return s.registryAddress }
func (s *Store) DB() storage.DB          { return s.db }

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

var intentColumns = `content_id, chain_id, registry_address, creator, content_digest, metadata_uri,
	status, tx_hash, nonce, block_number, block_hash, confirmed_at, last_error, attempt, created_at, updated_at`

func scanIntent(row interface{ Scan(...interface{}) error }) (*Intent, error) {
	var in Intent
	err := row.Scan(&in.ContentID, &in.ChainID, &in.RegistryAddress, &in.Creator, &in.ContentDigest,
		&in.MetadataURI, &in.Status, &in.TxHash, &in.Nonce, &in.BlockNumber, &in.BlockHash,
		&in.ConfirmedAt, &in.LastError, &in.Attempt, &in.CreatedAt, &in.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &in, nil
}

// EnsureIntent records the register intent with an EXTERNAL executor, so the
// caller can run it inside its own contents-row transaction (SG-13 A.1: the
// intent persists in the same DB transaction as the content). ON CONFLICT DO
// NOTHING makes retries and concurrent creators converge on ONE intent row.
func (s *Store) EnsureIntentExec(ctx context.Context, ex execContext, contentID, creator, contentDigest, metadataURI string) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO content_registry_intents
			(content_id, chain_id, registry_address, creator, content_digest, metadata_uri, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending')
		ON CONFLICT (chain_id, registry_address, content_id) DO NOTHING`,
		contentID, s.chainID, s.registryAddress, strings.ToLower(creator), strings.ToLower(contentDigest), metadataURI)
	if err != nil {
		return fmt.Errorf("registry: ensure intent %s: %w", contentID, err)
	}
	return nil
}

// EnsureIntentTx is the transaction-bound variant (SG-13 A.1): content and
// upload services call it with THEIR transaction so the intent row commits in
// the same database transaction as the contents row.
func (s *Store) EnsureIntentTx(ctx context.Context, sqlTx *sql.Tx, contentID, creator, contentDigest, metadataURI string) error {
	return s.EnsureIntentExec(ctx, sqlTx, contentID, creator, contentDigest, metadataURI)
}

// EnsureIntent is the standalone (no caller transaction) variant used by the
// attest endpoint.
func (s *Store) EnsureIntent(ctx context.Context, contentID, creator, contentDigest, metadataURI string) error {
	return s.EnsureIntentExec(ctx, storeExec{s}, contentID, creator, contentDigest, metadataURI)
}

type storeExec struct{ s *Store }

func (e storeExec) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return e.s.db.Exec(ctx, query, args...)
}

func (s *Store) Get(ctx context.Context, contentID string) (*Intent, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+intentColumns+` FROM content_registry_intents
		WHERE chain_id = $1 AND registry_address = $2 AND content_id = $3`,
		s.chainID, s.registryAddress, contentID)
	in, err := scanIntent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registry: get intent %s: %w", contentID, err)
	}
	return in, nil
}

// ClaimProcessable leases pending/broadcast_unknown intents with
// FOR UPDATE SKIP LOCKED, so two app instances never work the same intent.
func (s *Store) ClaimProcessable(ctx context.Context, owner string, leaseTTL time.Duration, limit int) ([]*Intent, error) {
	rows, err := s.db.Query(ctx, `
		UPDATE content_registry_intents i
		SET lease_owner = $1, lease_expires_at = NOW() + ($2 * INTERVAL '1 second'), updated_at = NOW()
		WHERE i.chain_id = $3 AND i.registry_address = $4
		  AND i.status IN ('pending', 'broadcast_unknown')
		  AND (i.lease_expires_at IS NULL OR i.lease_expires_at < NOW())
		  AND i.content_id IN (
			SELECT content_id FROM content_registry_intents
			WHERE chain_id = $3 AND registry_address = $4
			  AND status IN ('pending', 'broadcast_unknown')
			  AND (lease_expires_at IS NULL OR lease_expires_at < NOW())
			ORDER BY created_at
			LIMIT $5
			FOR UPDATE SKIP LOCKED)
		RETURNING `+intentColumns,
		owner, leaseTTL.Seconds(), s.chainID, s.registryAddress, limit)
	if err != nil {
		return nil, fmt.Errorf("registry: claim processable: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Intent
	for rows.Next() {
		in, err := scanIntent(rows)
		if err != nil {
			return nil, fmt.Errorf("registry: scan claimed intent: %w", err)
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// ListSubmitted lists submitted intents carrying a tx hash: the receipt-poller
// input. Reads are lease-free (idempotent lookups); updates are status-guarded.
func (s *Store) ListSubmitted(ctx context.Context, limit int) ([]*Intent, error) {
	return s.listBy(ctx, `status = 'submitted' AND tx_hash <> ''`, limit)
}

// ListConfirmed lists confirmed intents with recorded block evidence: the
// reorg-checker input. Confirmed history is durable — it is never pruned from
// a process-local map (SG-13 A.5).
func (s *Store) ListConfirmed(ctx context.Context, limit int) ([]*Intent, error) {
	return s.listBy(ctx, `status = 'confirmed' AND block_number IS NOT NULL`, limit)
}

func (s *Store) listBy(ctx context.Context, cond string, limit int) ([]*Intent, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+intentColumns+` FROM content_registry_intents
		WHERE chain_id = $1 AND registry_address = $2 AND `+cond+`
		ORDER BY created_at LIMIT $3`,
		s.chainID, s.registryAddress, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Intent
	for rows.Next() {
		in, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Store) execGuarded(ctx context.Context, verbSet, contentID, fromStatus string, extraArgs ...interface{}) error {
	args := []interface{}{s.chainID, s.registryAddress, contentID, fromStatus}
	args = append(args, extraArgs...)
	query := `UPDATE content_registry_intents SET ` + verbSet + `, updated_at = NOW() WHERE chain_id = $1 AND registry_address = $2 AND content_id = $3 AND status = $4`
	_, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("registry: update intent %s: %w", contentID, err)
	}
	return nil
}

// MarkSubmitted records the persisted broadcast facts: tx hash + nonce. It
// bumps the attempt counter so every signing round is visible.
func (s *Store) MarkSubmitted(ctx context.Context, contentID, txHash string, nonce uint64, fromStatus string) error {
	return s.MarkSubmittedExec(ctx, storeExec{s}, contentID, txHash, nonce, fromStatus)
}

// MarkSubmittedExec is the transaction-bound variant: the intent flip is
// committed together with the attempt row (persist-before-broadcast, A.4).
func (s *Store) MarkSubmittedExec(ctx context.Context, ex execContext, contentID, txHash string, nonce uint64, fromStatus string) error {
	_, err := ex.ExecContext(ctx, `
		UPDATE content_registry_intents
		SET status = 'submitted', tx_hash = $5, nonce = $6, attempt = attempt + 1,
		    last_error = '', lease_owner = '', lease_expires_at = NULL, updated_at = NOW()
		WHERE chain_id = $1 AND registry_address = $2 AND content_id = $3 AND status = $4`,
		s.chainID, s.registryAddress, contentID, fromStatus, txHash, int64(nonce))
	if err != nil {
		return fmt.Errorf("registry: mark submitted %s: %w", contentID, err)
	}
	return nil
}

// MarkSubmittedExisting records that a tx previously persisted elsewhere is
// (still) the one the node knows; the hash/nonce are not rewritten.
func (s *Store) MarkSubmittedExisting(ctx context.Context, contentID, fromStatus string) error {
	return s.execGuarded(ctx,
		`status = 'submitted', last_error = '', lease_owner = '', lease_expires_at = NULL`,
		contentID, fromStatus)
}

// MarkBroadcastUnknown is the state for an AMBIGUOUS broadcast outcome (RPC
// timeout): the nonce reservation and the persisted raw tx stay untouched;
// recovery looks the known hash up and/or re-sends the SAME raw tx (A.4).
func (s *Store) MarkBroadcastUnknown(ctx context.Context, contentID, fromStatus, lastErr string) error {
	return s.execGuarded(ctx,
		`status = 'broadcast_unknown', last_error = $5, lease_owner = '', lease_expires_at = NULL`,
		contentID, fromStatus, truncateErr(lastErr))
}

// MarkReceipt records the block evidence of a mined (status=1) tx without
// flipping to confirmed: the confirmation policy is evaluated separately.
func (s *Store) MarkReceipt(ctx context.Context, contentID, fromStatus, blockHash string, blockNumber uint64) error {
	return s.execGuarded(ctx,
		`block_number = $5, block_hash = $6`,
		contentID, fromStatus, int64(blockNumber), blockHash)
}

func (s *Store) MarkConfirmed(ctx context.Context, contentID, fromStatus string) error {
	return s.execGuarded(ctx,
		`status = 'confirmed', confirmed_at = NOW(), last_error = '', lease_owner = '', lease_expires_at = NULL`,
		contentID, fromStatus)
}

func (s *Store) MarkFailed(ctx context.Context, contentID, fromStatus, reason string) error {
	return s.execGuarded(ctx,
		`status = 'failed', last_error = $5, lease_owner = '', lease_expires_at = NULL`,
		contentID, fromStatus, truncateErr(reason))
}

// ResetToPending clears the broadcast facts so the next pass signs with a NEW
// nonce. Only used when rejection is definitive AND the nonce provably went
// unused (A.4: a timeout must never roll the nonce back).
func (s *Store) ResetToPending(ctx context.Context, contentID, fromStatus, reason string) error {
	return s.execGuarded(ctx,
		`status = 'pending', tx_hash = '', nonce = NULL, last_error = $5, lease_owner = '', lease_expires_at = NULL`,
		contentID, fromStatus, truncateErr(reason))
}

// ReorgRevert reopens a confirmed intent whose block no longer matches the
// chain (A.5): receipt.status=1 was never finality.
func (s *Store) ReorgRevert(ctx context.Context, contentID, reason string) error {
	return s.execGuarded(ctx,
		`status = 'submitted', block_number = NULL, block_hash = '', confirmed_at = NULL, last_error = $5`,
		contentID, StatusConfirmed, truncateErr(reason))
}

func (s *Store) ReleaseLease(ctx context.Context, contentID, owner string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE content_registry_intents
		SET lease_owner = '', lease_expires_at = NULL, updated_at = NOW()
		WHERE chain_id = $1 AND registry_address = $2 AND content_id = $3 AND lease_owner = $4`,
		s.chainID, s.registryAddress, contentID, owner)
	return err
}

// ReserveNonce allocates the next nonce for (chainID, signer). The SQL, the
// transaction-scoped advisory lock and the UNIQUE(chain, signer, nonce)
// backstop all live in the shared tx.NonceLedger — the registry keeps its own
// signature only so publishers can pass the chain pending nonce they already
// fetched (A.3).
func (s *Store) ReserveNonce(ctx context.Context, signer string, chainPending uint64) (uint64, error) {
	next, err := s.ledger.ReserveWithPending(ctx, signer, chainPending)
	if err != nil {
		return 0, fmt.Errorf("registry: reserve nonce for %s: %w", strings.ToLower(signer), err)
	}
	return next, nil
}

// ReleaseReservation returns a provably-unused nonce to the shared ledger. It
// is only ever called when the node definitively rejected the tx AND the chain
// nonce proves the nonce was never consumed (A.4); the ledger additionally
// refuses to delete a nonce a persisted signed attempt references without that
// chain proof.
func (s *Store) ReleaseReservation(ctx context.Context, signer string, nonce uint64) error {
	if err := s.ledger.Release(ctx, signer, nonce); err != nil {
		return fmt.Errorf("registry: release reservation: %w", err)
	}
	return nil
}

func (s *Store) CountReservations(ctx context.Context, signer string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM tx_nonce_reservations WHERE chain_id = $1 AND signer = $2`,
		s.chainID, strings.ToLower(signer)).Scan(&n)
	return n, err
}

// InsertAttempt persists the signed raw tx BEFORE it is broadcast (A.4). An
// explicit same-nonce replacement inserts another attempt row under the same
// reservation key; it never creates a new reservation.
func (s *Store) InsertAttempt(ctx context.Context, a *Attempt) error {
	return s.InsertAttemptExec(ctx, storeExec{s}, a)
}

// InsertAttemptExec is the transaction-bound variant.
func (s *Store) InsertAttemptExec(ctx context.Context, ex execContext, a *Attempt) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO tx_signed_attempts (reservation_key, chain_id, signer, nonce, tx_hash, raw_tx, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tx_hash) DO NOTHING`,
		a.ReservationKey, a.ChainID, strings.ToLower(a.Signer), a.Nonce, a.TxHash, a.RawTx, a.Status)
	if err != nil {
		return fmt.Errorf("registry: insert attempt %s: %w", a.TxHash, err)
	}
	return nil
}

func (s *Store) GetAttempt(ctx context.Context, txHash string) (*Attempt, error) {
	var a Attempt
	err := s.db.QueryRow(ctx, `
		SELECT reservation_key, chain_id, signer, nonce, tx_hash, raw_tx, status
		FROM tx_signed_attempts WHERE tx_hash = $1`, txHash).
		Scan(&a.ReservationKey, &a.ChainID, &a.Signer, &a.Nonce, &a.TxHash, &a.RawTx, &a.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// DecodeDigest parses a 64-char hex SHA-256 digest into its 32-byte form.
// Anything else is not a verifiable original digest and must never be
// registered on-chain (the digest comes from contents.file_hash server-side;
// a client can never supply it).
func DecodeDigest(fileHash string) ([32]byte, error) {
	var out [32]byte
	if len(fileHash) != 64 {
		return out, fmt.Errorf("registry: digest must be 64 hex chars, got %d", len(fileHash))
	}
	raw, err := hex.DecodeString(fileHash)
	if err != nil || len(raw) != 32 {
		return out, fmt.Errorf("registry: digest is not 32-byte hex")
	}
	copy(out[:], raw)
	return out, nil
}

func truncateErr(s string) string {
	const maxLen = 2000
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}
