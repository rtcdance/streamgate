package tx

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// NonceLedgerDB is the database surface the ledger needs: Exec for the
// release statements and InTransaction for the advisory-locked reservation.
// storage.DB satisfies it structurally.
type NonceLedgerDB interface {
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error
}

// NonceLedger is THE nonce authority for a (chain, signer): the allocation
// counter lives in Postgres under a transaction-scoped advisory lock plus
// UNIQUE(chain_id, signer, nonce), so every allocator in every process
// (registry publisher, Web3Service, demo minter) draws from one sequence
// instead of racing on chain-derived values. A process mutex or an
// address-only cache cannot guarantee uniqueness across instances or chains
// (TRANSITION_PLAN SG-13 A.3).
//
// A ledger instance is bound to ONE chain. client supplies the chain's
// pending nonce, which floors EVERY allocation, and supports releasing an
// attempt-referenced nonce; it may be nil when every caller passes its own
// chainPending hint (the registry publisher does).
type NonceLedger struct {
	db      NonceLedgerDB
	chainID int64
	client  NonceClient
	logger  *zap.Logger
}

func NewNonceLedger(db NonceLedgerDB, chainID int64, client NonceClient, logger *zap.Logger) *NonceLedger {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &NonceLedger{
		db:      db,
		chainID: chainID,
		client:  client,
		logger:  logger,
	}
}

func (l *NonceLedger) ChainID() int64 { return l.chainID }

// Reserve allocates the next unused nonce for (l.chainID, signer): the chain's
// pending nonce (via the injected client) and the persisted ledger both floor
// the result, whichever is higher.
func (l *NonceLedger) Reserve(ctx context.Context, signer string) (uint64, error) {
	return l.reserve(ctx, signer, nil)
}

// ReserveWithPending is Reserve for callers that already fetched the chain's
// pending nonce; the hint replaces the client call and still floors the
// persisted ledger.
func (l *NonceLedger) ReserveWithPending(ctx context.Context, signer string, chainPending uint64) (uint64, error) {
	return l.reserve(ctx, signer, &chainPending)
}

// NextNonce implements NonceProvider so the ledger can replace an in-memory
// NonceManager inside Web3Service's SendTransaction path.
func (l *NonceLedger) NextNonce(ctx context.Context, address string) (uint64, error) {
	return l.reserve(ctx, address, nil)
}

func (l *NonceLedger) reserve(ctx context.Context, signer string, hint *uint64) (uint64, error) {
	signerKey := strings.ToLower(signer)

	// The chain's pending nonce floors EVERY allocation, not only the first.
	// A ledger row can fall behind the chain (a process crashed after
	// broadcast without recording its reservation, or a pre-ledger binary
	// wrote directly): without the floor the ledger would hand out an
	// already-consumed nonce forever and every send would fail with
	// "nonce too low" while the chain nonce keeps rising (observed in the
	// fullchain stack, SG-13 B T9).
	var chainPending uint64
	haveChain := false
	if hint != nil {
		chainPending, haveChain = *hint, true
	} else if l.client != nil {
		n, err := l.client.GetNonce(ctx, signerKey)
		if err != nil {
			return 0, fmt.Errorf("nonce ledger: chain nonce for %s: %w", signerKey, err)
		}
		chainPending, haveChain = n, true
	}

	var next uint64
	var floored bool
	err := l.db.InTransaction(ctx, func(tx *sql.Tx) error {
		// Security: nonce allocation is a cross-process critical section; the
		// advisory lock serialises the max(reserved) read and the insert so
		// two instances racing on first-time allocation can never return the
		// same nonce (A.3).
		lockKey := fmt.Sprintf("sg13-nonce:%d:%s", l.chainID, signerKey)
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return fmt.Errorf("advisory lock: %w", err)
		}
		var maxReserved sql.NullInt64
		if err := tx.QueryRowContext(ctx,
			`SELECT MAX(nonce) FROM tx_nonce_reservations WHERE chain_id = $1 AND signer = $2`,
			l.chainID, signerKey).Scan(&maxReserved); err != nil {
			return fmt.Errorf("read reservations: %w", err)
		}
		if maxReserved.Valid {
			next = uint64(maxReserved.Int64) + 1
			// A warm ledger whose next value sits below the chain's pending
			// nonce has fallen behind the chain (a write the ledger never
			// saw); the floor below corrects it, but it should be visible.
			floored = haveChain && chainPending > next
		} else if !haveChain {
			return fmt.Errorf("no chain client and no chain pending nonce for an empty ledger")
		}
		if haveChain && chainPending > next {
			next = chainPending
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tx_nonce_reservations (chain_id, signer, nonce) VALUES ($1, $2, $3)`,
			l.chainID, signerKey, int64(next)); err != nil {
			return fmt.Errorf("insert reservation: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("nonce ledger: reserve for %s: %w", signerKey, err)
	}
	if floored {
		l.logger.Warn("chain nonce ran ahead of the ledger; allocation jumped to the chain floor",
			zap.String("signer", signerKey), zap.Uint64("chain_pending", chainPending))
	}
	return next, nil
}

// Release returns a provably-unused nonce to the ledger. The reservation is
// deleted only when NO persisted signed attempt references it; an
// attempt-referenced nonce is deleted only when the chain proves it was never
// consumed (chain pending <= nonce — the publisher's adjudication rule), so a
// broadcast attempt can never be recycled underneath a transaction that may
// still land.
func (l *NonceLedger) Release(ctx context.Context, signer string, nonce uint64) error {
	signerKey := strings.ToLower(signer)
	err := l.db.InTransaction(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			DELETE FROM tx_nonce_reservations
			WHERE chain_id = $1 AND signer = $2 AND nonce = $3
			  AND NOT EXISTS (
				SELECT 1 FROM tx_signed_attempts
				WHERE chain_id = $1 AND signer = $2 AND nonce = $3)`,
			l.chainID, signerKey, int64(nonce))
		if err != nil {
			return fmt.Errorf("delete reservation: %w", err)
		}
		if affected, aerr := res.RowsAffected(); aerr == nil && affected > 0 {
			return nil
		}
		// Either nothing was reserved (idempotent release) or a signed
		// attempt references the nonce.
		var attempts int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM tx_signed_attempts WHERE chain_id = $1 AND signer = $2 AND nonce = $3`,
			l.chainID, signerKey, int64(nonce)).Scan(&attempts); err != nil {
			return fmt.Errorf("count attempts: %w", err)
		}
		if attempts == 0 {
			return nil
		}
		if l.client == nil {
			return fmt.Errorf("signed attempt references nonce %d for %s and no chain client is available to adjudicate", nonce, signerKey)
		}
		chainPending, err := l.client.GetNonce(ctx, signerKey)
		if err != nil {
			return fmt.Errorf("cannot adjudicate release of nonce %d for %s: %w", nonce, signerKey, err)
		}
		if chainPending > nonce {
			// The nonce was consumed by another transaction: the reservation
			// must stay as the durable record; releasing it would free a nonce
			// that is gone.
			return nil
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM tx_nonce_reservations WHERE chain_id = $1 AND signer = $2 AND nonce = $3`,
			l.chainID, signerKey, int64(nonce)); err != nil {
			return fmt.Errorf("delete adjudicated reservation: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("nonce ledger: release %d for %s: %w", nonce, signerKey, err)
	}
	return nil
}

// Rollback implements NonceProvider: a nonce that was reserved but never
// broadcast goes back to the ledger. The DB write cannot fail silently — a
// leaked reservation would leave a gap — so failures are logged loudly.
func (l *NonceLedger) Rollback(address string, nonce uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := l.Release(ctx, address, nonce); err != nil {
		l.logger.Warn("NonceLedger: rollback release failed",
			zap.String("address", address), zap.Uint64("nonce", nonce), zap.Error(err))
	}
}

// Reset implements NonceProvider. With the chain nonce flooring every
// allocation there is no per-signer warm-up state to drop, so this is a no-op:
// after a "nonce too low" rejection the next Reserve already re-reads the
// chain and jumps to its floor.
func (l *NonceLedger) Reset(address string) {
	l.logger.Debug("NonceLedger: reset is a no-op (chain nonce floors every allocation)", zap.String("address", address))
}
