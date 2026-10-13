package registry

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"github.com/rtcdance/streamgate/pkg/web3/tx"
	"go.uber.org/zap"
)

// ChainClient is the chain subset the publisher needs. The client is selected
// by the INTENT's chain id — never a hard-coded network (A.5).
type ChainClient interface {
	GetNonce(ctx context.Context, address string) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
	GetBlockNumber(ctx context.Context) (uint64, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
}

// TxPipeline signs and broadcasts register transactions. PrepareRegisterContent
// must NOT broadcast (SG-13 A.4: persist first); Broadcast may be called
// repeatedly with identical raw bytes.
type TxPipeline interface {
	PrepareRegisterContent(ctx context.Context, contentID, creator string, digest [32]byte, metadataURI string, nonce uint64) (*tx.SignedTx, error)
	Broadcast(ctx context.Context, signed *tx.SignedTx) error
}

// PublisherConfig are the tunables of the intent publisher.
type PublisherConfig struct {
	Signer        string
	ChainID       int64
	PollInterval  time.Duration // loop cadence, default 2s
	LeaseTTL      time.Duration // claim lease, default 30s
	Confirmations uint64        // policy confirmation point, default 2
	BatchSize     int           // default 25
	MaxAttempts   int           // signing attempts before permanent failure, default 5
}

func (c PublisherConfig) withDefaults() PublisherConfig {
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 30 * time.Second
	}
	if c.Confirmations == 0 {
		c.Confirmations = 2
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 25
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	return c
}

// Publisher drives the intent state machine. It runs inside app processes
// (like the SG-12 outbox pump) and only when the registry is enabled.
type Publisher struct {
	store    *Store
	client   ChainClient
	pipeline TxPipeline
	cfg      PublisherConfig
	owner    string
	log      *zap.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewPublisher(store *Store, client ChainClient, pipeline TxPipeline, cfg PublisherConfig, log *zap.Logger) *Publisher {
	if log == nil {
		log = zap.NewNop()
	}
	host, _ := os.Hostname()
	return &Publisher{
		store:    store,
		client:   client,
		pipeline: pipeline,
		cfg:      cfg.withDefaults(),
		owner:    fmt.Sprintf("%s/%s/%s", host, os.Getenv("SERVICE_NAME"), uuid.NewString()[:8]),
		log:      log.Named("registry-publisher"),
	}
}

func (p *Publisher) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.wg.Add(1)
	go p.loop(ctx)
}

func (p *Publisher) Stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.wg.Wait()
}

func (p *Publisher) loop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				p.log.Debug("registry publisher pass failed", zap.Error(err))
			}
		}
	}
}

// RunOnce performs one full pass: drive pending/unknown intents, poll
// receipts, verify confirmed block hashes. A single pass never spans a DB
// transaction across an RPC call (A.7): every RPC happens between two short,
// committed DB writes.
func (p *Publisher) RunOnce(ctx context.Context) error {
	var errs []error
	if err := p.processClaimed(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := p.pollReceipts(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := p.checkReorgs(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (p *Publisher) processClaimed(ctx context.Context) error {
	claimed, err := p.store.ClaimProcessable(ctx, p.owner, p.cfg.LeaseTTL, p.cfg.BatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, in := range claimed {
		switch in.Status {
		case StatusPending:
			errs = append(errs, p.drivePending(ctx, in))
		case StatusBroadcastUnknown:
			errs = append(errs, p.recoverUnknown(ctx, in))
		}
		if err := p.store.ReleaseLease(ctx, in.ContentID, p.owner); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// drivePending: pending → (persist signed attempt + intent.tx_hash) → submitted.
func (p *Publisher) drivePending(ctx context.Context, in *Intent) error {
	digest, err := DecodeDigest(in.ContentDigest)
	if err != nil {
		return p.store.MarkFailed(ctx, in.ContentID, StatusPending, "invalid persisted digest: "+err.Error())
	}

	chainPending, err := p.client.GetNonce(ctx, p.cfg.Signer)
	if err != nil {
		// Transient: leave pending for the next pass. The intent is durable.
		p.log.Warn("registry: chain nonce unavailable", zap.String("content_id", in.ContentID), zap.Error(err))
		return nil
	}

	nonce, err := p.store.ReserveNonce(ctx, p.cfg.Signer, chainPending)
	if err != nil {
		return err
	}

	signed, err := p.pipeline.PrepareRegisterContent(ctx, in.ContentID, in.Creator, digest, in.MetadataURI, nonce)
	if err != nil {
		// Nothing was broadcast, so the reserved nonce is provably unused and
		// can go back to the ledger (A.4's recycling rule).
		if rerr := p.store.ReleaseReservation(ctx, p.cfg.Signer, nonce); rerr != nil {
			p.log.Warn("registry: release reservation after prepare failure", zap.Error(rerr))
		}
		if in.Attempt+1 >= p.cfg.MaxAttempts {
			return p.store.MarkFailed(ctx, in.ContentID, StatusPending, "sign/register tx: "+err.Error())
		}
		return p.store.execGuarded(ctx, `attempt = attempt + 1, last_error = $5`, in.ContentID, StatusPending, truncateErr("sign/register tx: "+err.Error()))
	}

	// A.4: persist the signed raw tx, nonce, hash and intent BEFORE broadcast.
	// If this write fails, the tx is NOT broadcast — an unwritable database
	// must never produce an untracked chain write. After this transaction the
	// intent row is already 'submitted', so the broadcast outcome transitions
	// are guarded on 'submitted', not on the pre-persist status.
	if err := p.persistBroadcastFacts(ctx, in, signed); err != nil {
		return err
	}

	return p.broadcast(ctx, in, signed, StatusSubmitted)
}

// persistBroadcastFacts writes attempt + submitted intent in ONE transaction.
func (p *Publisher) persistBroadcastFacts(ctx context.Context, in *Intent, signed *tx.SignedTx) error {
	attempt := &Attempt{
		ReservationKey: ReservationKey(p.cfg.ChainID, p.cfg.Signer, signed.Nonce),
		ChainID:        p.cfg.ChainID,
		Signer:         p.cfg.Signer,
		Nonce:          int64(signed.Nonce),
		TxHash:         signed.Hash,
		RawTx:          hex.EncodeToString(signed.Raw),
		Status:         "signed",
	}
	err := p.store.DB().InTransaction(ctx, func(sqlTx *sql.Tx) error {
		if err := p.store.EnsureIntentExec(ctx, sqlTx, in.ContentID, in.Creator, in.ContentDigest, in.MetadataURI); err != nil {
			return err
		}
		if err := p.store.InsertAttemptExec(ctx, sqlTx, attempt); err != nil {
			return err
		}
		return p.store.MarkSubmittedExec(ctx, sqlTx, in.ContentID, signed.Hash, signed.Nonce, in.Status)
	})
	if err != nil {
		return fmt.Errorf("registry: persist broadcast facts for %s: %w (not broadcasting)", in.ContentID, err)
	}
	in.Status = StatusSubmitted
	in.TxHash = signed.Hash
	return nil
}

// broadcast sends the persisted raw tx. Ambiguous failures (timeouts, conn
// resets) are broadcast_unknown: the nonce is NEVER rolled back and no new tx
// is signed (A.4). Definitive rejections are adjudicated against the chain
// nonce.
func (p *Publisher) broadcast(ctx context.Context, in *Intent, signed *tx.SignedTx, fromStatus string) error {
	err := p.pipeline.Broadcast(ctx, signed)
	if err == nil || isAlreadyKnown(err) {
		if fromStatus == StatusBroadcastUnknown {
			// A recovery re-send of the SAME raw tx succeeded (or the node
			// already had it): back to submitted.
			return p.store.MarkSubmittedExisting(ctx, in.ContentID, fromStatus)
		}
		return nil
	}
	if isAmbiguous(err) {
		p.log.Warn("registry: broadcast outcome unknown; keeping nonce, will re-send same raw tx",
			zap.String("content_id", in.ContentID), zap.String("tx_hash", signed.Hash), zap.Error(err))
		return p.store.MarkBroadcastUnknown(ctx, in.ContentID, fromStatus, "broadcast outcome unknown: "+err.Error())
	}
	return p.adjudicateRejection(ctx, in, signed, fromStatus, err)
}

// recoverUnknown: broadcast_unknown → receipt lookup by the KNOWN hash first,
// then re-send the SAME raw tx (A.4). It never signs a new nonce.
func (p *Publisher) recoverUnknown(ctx context.Context, in *Intent) error {
	if in.TxHash == "" {
		return p.store.MarkFailed(ctx, in.ContentID, StatusBroadcastUnknown, "broadcast_unknown without a persisted tx hash")
	}

	receipt, err := p.client.TransactionReceipt(ctx, common.HexToHash(in.TxHash))
	if err == nil && receipt != nil {
		return p.handleReceipt(ctx, in, receipt)
	}

	attempt, aerr := p.store.GetAttempt(ctx, in.TxHash)
	if aerr != nil {
		return aerr
	}
	if attempt == nil {
		// Defensive: the attempt row is written before broadcast, so this only
		// happens if someone deleted history.
		return p.store.MarkFailed(ctx, in.ContentID, StatusBroadcastUnknown, "no persisted raw tx for "+in.TxHash)
	}
	raw, derr := hex.DecodeString(attempt.RawTx)
	if derr != nil {
		return p.store.MarkFailed(ctx, in.ContentID, StatusBroadcastUnknown, "persisted raw tx is not hex: "+derr.Error())
	}
	signed := &tx.SignedTx{Raw: raw, Hash: attempt.TxHash, Nonce: uint64(attempt.Nonce)}
	return p.broadcast(ctx, in, signed, StatusBroadcastUnknown)
}

// adjudicateRejection handles a DEFINITIVE node rejection: the node answered
// and did not accept the tx. If the chain nonce proves the nonce was never
// consumed, the nonce is recycled and the intent returns to pending for a NEW
// nonce; if the nonce was consumed, the known hash is the last hope and
// otherwise the intent is failed.
func (p *Publisher) adjudicateRejection(ctx context.Context, in *Intent, signed *tx.SignedTx, fromStatus string, sendErr error) error {
	chainNonce, nerr := p.client.GetNonce(ctx, p.cfg.Signer)
	if nerr != nil {
		// Cannot adjudicate without chain state: treat as unknown, keep the nonce.
		p.log.Warn("registry: cannot adjudicate rejection, keeping nonce",
			zap.String("content_id", in.ContentID), zap.Error(nerr), zap.Error(sendErr))
		return p.store.MarkBroadcastUnknown(ctx, in.ContentID, fromStatus, "rejection unadjudicated: "+sendErr.Error())
	}
	if chainNonce > signed.Nonce {
		// The nonce is gone. Ours may still have landed (race); check the hash.
		if receipt, rerr := p.client.TransactionReceipt(ctx, common.HexToHash(signed.Hash)); rerr == nil && receipt != nil {
			return p.handleReceipt(ctx, in, receipt)
		}
		return p.store.MarkFailed(ctx, in.ContentID, fromStatus,
			fmt.Sprintf("rejected (%v) and nonce %d consumed by another tx", sendErr, signed.Nonce))
	}
	// Provably unused: recycle the reservation and re-sign with a new nonce.
	if rerr := p.store.ReleaseReservation(ctx, p.cfg.Signer, signed.Nonce); rerr != nil {
		p.log.Warn("registry: release recycled reservation", zap.Error(rerr))
	}
	if in.Attempt+1 >= p.cfg.MaxAttempts {
		return p.store.MarkFailed(ctx, in.ContentID, fromStatus, "definitive rejection: "+sendErr.Error())
	}
	return p.store.ResetToPending(ctx, in.ContentID, fromStatus, "definitive rejection, nonce recycled: "+sendErr.Error())
}

func (p *Publisher) handleReceipt(ctx context.Context, in *Intent, receipt *types.Receipt) error {
	if receipt.Status != types.ReceiptStatusSuccessful {
		return p.store.MarkFailed(ctx, in.ContentID, in.Status, "transaction reverted on-chain")
	}
	blockHash := receipt.BlockHash.Hex()
	if err := p.store.MarkReceipt(ctx, in.ContentID, in.Status, blockHash, receipt.BlockNumber.Uint64()); err != nil {
		return err
	}
	if in.Status != StatusSubmitted {
		if err := p.store.MarkSubmittedExisting(ctx, in.ContentID, in.Status); err != nil {
			return err
		}
	}
	return p.maybeConfirm(ctx, in.ContentID, receipt.BlockNumber.Uint64())
}

// maybeConfirm applies the confirmation policy: head >= receipt block + N.
// receipt.status=1 alone is NOT finality (A.5).
func (p *Publisher) maybeConfirm(ctx context.Context, contentID string, blockNumber uint64) error {
	head, err := p.client.GetBlockNumber(ctx)
	if err != nil {
		return nil // stays submitted; the next pass re-evaluates
	}
	if head < blockNumber+p.cfg.Confirmations {
		return nil
	}
	return p.store.MarkConfirmed(ctx, contentID, StatusSubmitted)
}

// pollReceipts: submitted intents move to confirmed (with recorded block
// number/hash) once the policy confirmation point is reached, or to failed on
// a reverted receipt.
func (p *Publisher) pollReceipts(ctx context.Context) error {
	submitted, err := p.store.ListSubmitted(ctx, p.cfg.BatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, in := range submitted {
		receipt, rerr := p.client.TransactionReceipt(ctx, common.HexToHash(in.TxHash))
		if rerr != nil || receipt == nil {
			// Not mined yet (or the node cannot see it); recovery of a silently
			// dropped tx is driven by broadcast_unknown, not by guessing here.
			p.log.Debug("registry: no receipt yet",
				zap.String("content_id", in.ContentID), zap.String("tx_hash", in.TxHash))
			continue
		}
		errs = append(errs, p.handleReceipt(ctx, in, receipt))
	}
	return errors.Join(errs...)
}

// checkReorgs verifies the persisted block hash of every confirmed intent
// against the chain (A.5). A mismatch (or a vanished block) reverts the intent
// to submitted for re-verification — confirmed is not a one-way door.
func (p *Publisher) checkReorgs(ctx context.Context) error {
	confirmed, err := p.store.ListConfirmed(ctx, p.cfg.BatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, in := range confirmed {
		if !in.BlockNumber.Valid {
			continue
		}
		header, herr := p.client.HeaderByNumber(ctx, big.NewInt(in.BlockNumber.Int64))
		if herr != nil || header == nil || !strings.EqualFold(header.Hash().Hex(), in.BlockHash) {
			p.log.Warn("registry: reorg detected, reverting confirmed intent for re-verification",
				zap.String("content_id", in.ContentID),
				zap.Int64("block_number", in.BlockNumber.Int64),
				zap.String("stored_block_hash", in.BlockHash))
			errs = append(errs, p.store.ReorgRevert(ctx, in.ContentID, fmt.Sprintf("reorg: block %d hash mismatch", in.BlockNumber.Int64)))
		}
	}
	return errors.Join(errs...)
}

// isAmbiguous reports whether a broadcast error leaves the outcome unknown:
// the node MAY have accepted the tx. Only infrastructure failures qualify; a
// protocol-level rejection answered by the node is definitive.
func isAmbiguous(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// isAlreadyKnown reports whether the node already has the tx — an idempotent
// success for re-sends of the SAME raw bytes.
func isAlreadyKnown(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already known") ||
		strings.Contains(msg, "known transaction") ||
		strings.Contains(msg, "alreadyexists")
}
