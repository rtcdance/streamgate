package tx

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
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
// pending nonce for a first allocation and for releasing an
// attempt-referenced nonce; it may be nil when every caller passes its own
// chainPending hint (the registry publisher does).
type NonceLedger struct {
	db      NonceLedgerDB
	chainID int64
	client  NonceClient
	logger  *zap.Logger

	mu sync.Mutex
	// primed records the signers whose first allocation already consulted the
	// chain, so subsequent allocations are the persisted ledger alone.
	primed map[string]bool
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
		primed:  make(map[string]bool),
	}
}

func (l *NonceLedger) ChainID() int64 { return l.chainID }

// Reserve allocates the next unused nonce for (l.chainID, signer), querying
// the chain via the injected client on the ledger's first allocation for that
// signer; afterwards the persisted ledger alone decides.
func (l *NonceLedger) Reserve(ctx context.Context, signer string) (uint64, error) {
	return l.reserve(ctx, signer, nil)
}

// ReserveWithPending is Reserve for callers that already fetched the chain's
// pending nonce. The hint is honoured only when the ledger has no history for
// the signer; it never overrides the persisted ledger afterwards.
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

	l.mu.Lock()
	primed := l.primed[signerKey]
	l.mu.Unlock()

	var chainPending uint64
	haveChain := false
	if hint != nil {
		chainPending, haveChain = *hint, true
	} else if !primed {
		// First allocation for this signer: the chain's pending nonce is the
		// floor, so a nonce consumed outside the ledger can never be handed
		// out again.
		if l.client == nil {
			return 0, fmt.Errorf("nonce ledger: no chain client to prime first allocation for %s", signerKey)
		}
		n, err := l.client.GetNonce(ctx, signerKey)
		if err != nil {
			return 0, fmt.Errorf("nonce ledger: chain nonce for %s: %w", signerKey, err)
		}
		chainPending, haveChain = n, true
	}

	var next uint64
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
			// First use also honours the chain: a pending nonce above the
			// persisted ledger means writes happen outside it.
			if !primed && haveChain && chainPending > next {
				next = chainPending
			}
		} else {
			// Empty ledger for this signer: the chain pending nonce is the
			// only floor there is.
			if !haveChain {
				if l.client == nil {
					return fmt.Errorf("no chain client and no chain pending nonce for an empty ledger")
				}
				n, err := l.client.GetNonce(ctx, signerKey)
				if err != nil {
					return fmt.Errorf("chain nonce for %s: %w", signerKey, err)
				}
				chainPending = n
			}
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

	l.mu.Lock()
	l.primed[signerKey] = true
	l.mu.Unlock()
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

// Reset implements NonceProvider: drop the priming so the next allocation
// re-reads the chain floor (used after a "nonce too low" rejection, when the
// chain moved ahead of the persisted ledger).
func (l *NonceLedger) Reset(address string) {
	l.mu.Lock()
	delete(l.primed, strings.ToLower(address))
	l.mu.Unlock()
	l.logger.Debug("NonceLedger: reset chain priming", zap.String("address", address))
}
