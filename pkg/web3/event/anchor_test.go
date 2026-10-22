package event

// SG-13 M7 deterministic scenarios for the cursor anchor and the atomic
// block journal: sparse logs, verification across poll/restart, the 64-block
// reorg boundary, a second reorg after a rollback, and a journal commit
// failure that must block the whole batch. They run against a real Postgres
// like the rest of the SG-13 B acceptance suite.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAcceptance_EventRejectsLegacyEmptyCursorAnchor(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)
	version := "empty-anchor-reject"
	idx, err := NewEventIndexerWithConfig(&mockEventReader{blockNum: 20}, EventIndexerConfig{
		ContractAddresses: []string{testWhitelistA}, EventSignatures: []string{transferSig}, ChainID: testEventChainID,
		FilterVersion: version, UpdateInterval: time.Hour,
	}, zap.NewNop())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO event_checkpoints(chain_id,filter_key,block_number,block_hash) VALUES($1,$2,10,'') ON CONFLICT(chain_id,filter_key) DO UPDATE SET block_number=10, block_hash=''`, testEventChainID, idx.filterKey())
	require.NoError(t, err)
	idx.SetEventStore(ds)
	idx.SetReorgDetector(&scriptedReorgChecker{keepBelow: 20})
	_, err = idx.cursorAnchorReorged(ctx)
	require.ErrorContains(t, err, "no canonical anchor hash")
	fmt.Println("M7_LEGACY_EMPTY_ANCHOR_REJECTED")
}

func TestAcceptance_EventRollbackMutationIsAtomicAcrossCrashBoundary(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)
	const filter = "atomic-crash-boundary"
	const first, cursor uint64 = 64000, 64002
	hashes := hashMapFor(first, cursor)
	anchoredBatch(t, ds, testEventChainID, filter, first, cursor, hashes)
	t.Cleanup(func() {
		_, err := db.Exec(ctx, `DROP TRIGGER IF EXISTS reject_atomic_cursor_rewind ON event_checkpoints; DROP FUNCTION IF EXISTS reject_atomic_cursor_rewind()`)
		require.NoError(t, err)
	})
	_, err := db.Exec(ctx, `CREATE OR REPLACE FUNCTION reject_atomic_cursor_rewind() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.chain_id = 991341 AND NEW.filter_key = 'atomic-crash-boundary' AND NEW.block_number = 64000 THEN RAISE EXCEPTION 'injected crash boundary'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_atomic_cursor_rewind BEFORE UPDATE ON event_checkpoints FOR EACH ROW EXECUTE FUNCTION reject_atomic_cursor_rewind()`)
	require.NoError(t, err)
	err = ds.RollbackReorgState(ctx, testEventChainID, filter, "0x0000000000000000000000000000000000000abc", first, hashes[first], []string{hashes[cursor]})
	require.ErrorContains(t, err, "rewind scoped checkpoint")
	var orphaned bool
	require.NoError(t, db.QueryRow(ctx, `SELECT orphaned FROM event_block_journal WHERE chain_id=$1 AND block_number=$2`, testEventChainID, int64(cursor)).Scan(&orphaned))
	require.False(t, orphaned, "a failure after journal mutation must roll the transaction back")
	got, gotHash, err := ds.LoadScopedCheckpointAnchor(ctx, testEventChainID, filter)
	require.NoError(t, err)
	require.Equal(t, cursor, got)
	require.Equal(t, hashes[cursor], gotHash)
	fmt.Println("M7_ROLLBACK_CRASH_RECOVERY_PASS")
}

const (
	// The indexer's default confirmation depth (EventIndexerConfig zeroes to
	// 12): polls index up to latest-12, so fixtures aim their fake chain head
	// 12 blocks above the safe height they want indexed.
	testConfirmations uint64 = 12
)

// anchoredBatch commits one batch through the real save path so the journal,
// checkpoint anchor and events land in the same transaction.
func anchoredBatch(t *testing.T, ds *PostgresEventStore, chainID int64, key string, first, end uint64, hashes map[uint64]string) {
	t.Helper()
	journal := make([]JournalBlock, 0, end-first+1)
	for n := first; n <= end; n++ {
		journal = append(journal, JournalBlock{Number: n, Hash: hashes[n]})
	}
	require.NoError(t, ds.SaveBatch(context.Background(), chainID, key, nil, end, hashes[end], journal, journalDepth))
}

func hashMapFor(first, last uint64) map[uint64]string {
	hashes := make(map[uint64]string, last-first+1)
	for n := first; n <= last; n++ {
		hashes[n] = fmt.Sprintf("0x%064x", n)
	}
	return hashes
}

// Sparse logs: a batch whose only log sits far from the end must still commit a
// journal covering the whole ancestry window and anchor the checkpoint to the
// REAL batch-end hash — previously empty blocks were never journaled and an
// end block without logs committed an empty anchor.
func TestAcceptance_EventSparseLogsJournalCoversTheWindow(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)
	const first, end uint64 = 60000, 60020
	cfg := EventIndexerConfig{
		ContractAddresses:  []string{testWhitelistA},
		EventSignatures:    []string{transferSig},
		ChainID:            testEventChainID,
		StartBlock:         first - 1,
		FilterVersion:      "sparse-logs",
		ConfirmationBlocks: 0,
		UpdateInterval:     time.Hour,
	}
	logs := []struct {
		n    uint64
		hash string
	}{{60003, "0xdead0000000000000000000000000000000000000000000000000000000beef"}}
	reader := &mockEventReader{blockNum: end + testConfirmations}
	for _, l := range logs {
		log := makeTransferLog(l.n, common.HexToHash("0xfeed"), 0)
		log.BlockHash = common.HexToHash(l.hash)
		reader.setLogs(append(reader.logs, log))
	}
	idx, err := NewEventIndexerWithConfig(reader, cfg, zap.NewNop())
	require.NoError(t, err)
	idx.SetEventStore(ds)
	idx.SetReorgDetector(&scriptedReorgChecker{keepBelow: end})
	require.NoError(t, idx.Start(ctx))
	defer idx.Stop()
	require.NoError(t, idx.indexEvents(ctx))

	journal, err := ds.LoadBlockJournal(ctx, testEventChainID, first)
	require.NoError(t, err)
	seen := make(map[uint64]string, len(journal))
	for _, b := range journal {
		seen[b.Number] = b.Hash
	}
	for n := first; n <= end; n++ {
		require.NotEmpty(t, seen[n], "empty block %d inside the batch must still be journaled", n)
	}
	assert.Equal(t, common.HexToHash("0xdead0000000000000000000000000000000000000000000000000000000beef").Hex(), seen[60003],
		"the log block keeps the hash the chain reported")
	_, anchor, err := ds.LoadScopedCheckpointAnchor(ctx, testEventChainID, idx.filterKey())
	require.NoError(t, err)
	assert.Equal(t, seen[end], anchor,
		"the checkpoint anchor must be the real batch-end hash, even though the end block has no logs")
	assert.NotEmpty(t, anchor)
}

// Cross poll/restart: a same-height reorg replaces the cursor block without
// producing new blocks, so every poll — and the first poll after a restart —
// must verify the committed anchor BEFORE the "nothing new to index" path.
func TestAcceptance_EventCursorAnchorVerifiedAcrossPollAndRestart(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)
	const first, cursor uint64 = 61000, 61063
	const ancestor uint64 = 61050
	hashes := hashMapFor(first, cursor)
	cfg := EventIndexerConfig{
		ContractAddresses: []string{testWhitelistA},
		EventSignatures:   []string{transferSig},
		ChainID:           testEventChainID,
		FilterVersion:     "anchor-poll",
		UpdateInterval:    time.Hour,
	}
	idx, err := NewEventIndexerWithConfig(&mockEventReader{blockNum: cursor - 1}, cfg, zap.NewNop())
	require.NoError(t, err)
	idx.SetEventStore(ds)
	key := idx.filterKey()
	anchoredBatch(t, ds, testEventChainID, key, first, cursor, hashes)

	// Control: an intact anchor leaves the cursor alone.
	idx.SetReorgDetector(&scriptedReorgChecker{keepBelow: cursor})
	idx.currentBlock = cursor
	require.NoError(t, idx.indexEvents(ctx))
	got, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, key)
	require.NoError(t, err)
	require.Equal(t, cursor, got)

	// The chain rewound below the cursor and produced no new safe blocks: the
	// poll takes the early-return path (safe <= current) and used to walk past
	// the reorg forever. The fake head is chosen so the safe window stops at
	// the rollback point and the poll does not re-index in the same call.
	idx.SetReorgDetector(&scriptedReorgChecker{keepBelow: ancestor})
	require.NoError(t, idx.indexEvents(ctx))
	got, err = ds.LoadScopedCheckpoint(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Equal(t, ancestor, got, "the poll must roll the cursor back to the journaled ancestor")
	assert.Equal(t, ancestor, idx.GetCurrentBlock())

	// A restart (fresh process over the same store) must verify the anchor the
	// same way before it indexes anything.
	reader := &mockEventReader{blockNum: ancestor}
	restarted, err := NewEventIndexerWithConfig(reader, cfg, zap.NewNop())
	require.NoError(t, err)
	restarted.SetEventStore(ds)
	require.NoError(t, restarted.Start(ctx))
	defer restarted.Stop()
	require.Equal(t, ancestor, restarted.GetCurrentBlock())
	restarted.SetReorgDetector(&scriptedReorgChecker{keepBelow: ancestor - 5})
	// Re-anchor at the cursor so the restart check has something to verify.
	require.NoError(t, ds.SaveBatch(ctx, testEventChainID, restarted.filterKey(), nil, ancestor, hashes[ancestor],
		[]JournalBlock{{Number: ancestor, Hash: hashes[ancestor]}}, journalDepth))
	require.NoError(t, restarted.indexEvents(ctx))
	got, err = ds.LoadScopedCheckpoint(ctx, testEventChainID, restarted.filterKey())
	require.NoError(t, err)
	assert.Equal(t, ancestor-5, got, "the first poll after a restart must verify and roll back too")
}

// The 64-block boundary: a reorg of depth journalDepth must still find its
// common ancestor inside the journaled window; one block deeper must fail
// loudly instead of pretending to recover.
func TestAcceptance_EventReorgDepthBoundary64(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)
	const cursor uint64 = 62063
	first := cursor - journalDepth
	hashes := hashMapFor(first, cursor)

	idx, err := NewEventIndexerWithConfig(&mockEventReader{blockNum: cursor}, EventIndexerConfig{
		ContractAddresses: []string{testWhitelistA}, EventSignatures: []string{transferSig},
		ChainID: testEventChainID, FilterVersion: "depth-64", UpdateInterval: time.Hour,
	}, zap.NewNop())
	require.NoError(t, err)
	idx.SetEventStore(ds)
	key := idx.filterKey()
	anchoredBatch(t, ds, testEventChainID, key, first, cursor, hashes)

	idx.SetReorgDetector(&scriptedReorgChecker{keepBelow: first})
	idx.currentBlock = cursor
	require.NoError(t, idx.rollbackToCommonAncestor(ctx, ds),
		"a reorg of exactly journalDepth blocks must recover to its ancestor")
	got, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Equal(t, first, got)

	// One block deeper than the journal can prove: refuse.
	idx2, err := NewEventIndexerWithConfig(&mockEventReader{blockNum: cursor}, EventIndexerConfig{
		ContractAddresses: []string{testWhitelistA}, EventSignatures: []string{transferSig},
		ChainID: testEventChainID2, FilterVersion: "depth-65", UpdateInterval: time.Hour,
	}, zap.NewNop())
	require.NoError(t, err)
	idx2.SetEventStore(ds)
	key2 := idx2.filterKey()
	anchoredBatch(t, ds, testEventChainID2, key2, first, cursor, hashes)
	idx2.SetReorgDetector(&scriptedReorgChecker{keepBelow: first - 1})
	idx2.currentBlock = cursor
	err = idx2.rollbackToCommonAncestor(ctx, ds)
	require.Error(t, err, "a reorg one block deeper than the journal must fail loudly")
	assert.Contains(t, err.Error(), "reorg deeper than journal depth")
}

// A second reorg after a rollback must work off the re-written journal: the
// upsert replaces the hash of a reorged height instead of keeping the dead one.
func TestAcceptance_EventSecondReorgAfterRollback(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)
	const first, cursor uint64 = 63000, 63063
	hashes := hashMapFor(first, cursor)
	idx, err := NewEventIndexerWithConfig(&mockEventReader{blockNum: cursor}, EventIndexerConfig{
		ContractAddresses: []string{testWhitelistA}, EventSignatures: []string{transferSig},
		ChainID: testEventChainID, FilterVersion: "second-reorg", UpdateInterval: time.Hour,
	}, zap.NewNop())
	require.NoError(t, err)
	idx.SetEventStore(ds)
	key := idx.filterKey()
	anchoredBatch(t, ds, testEventChainID, key, first, cursor, hashes)

	idx.SetReorgDetector(&scriptedReorgChecker{keepBelow: 63050})
	idx.currentBlock = cursor
	require.NoError(t, idx.rollbackToCommonAncestor(ctx, ds))

	// The chain re-extends with NEW blocks above the ancestor: the re-index
	// commits the same heights with different hashes.
	rewritten := hashMapFor(63051, 63070)
	for n := range rewritten {
		rewritten[n] = fmt.Sprintf("0x9%063x", n)
	}
	anchoredBatch(t, ds, testEventChainID, key, 63051, 63070, rewritten)

	idx.SetReorgDetector(&scriptedReorgChecker{keepBelow: 63055})
	idx.currentBlock = 63070
	require.NoError(t, idx.rollbackToCommonAncestor(ctx, ds),
		"a second reorg must roll back against the re-written journal")
	got, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Equal(t, uint64(63055), got)

	journal, err := ds.LoadBlockJournal(ctx, testEventChainID, 63051)
	require.NoError(t, err)
	byNumber := make(map[uint64]string, len(journal))
	for _, b := range journal {
		byNumber[b.Number] = b.Hash
	}
	_, anchor, err := ds.LoadScopedCheckpointAnchor(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Equal(t, rewritten[63055], anchor,
		"the rollback must re-anchor to the hash of the CURRENT chain block")
	assert.Equal(t, rewritten[63051], byNumber[63051],
		"the journal entry of a re-written height must carry the new hash, not the dead one")
}

// A journal write failure must roll the whole batch back: no events, no
// checkpoint advance, no anchor — the cursor can never pass unjournaled blocks.
func TestAcceptance_EventJournalFailureBlocksTheBatchCommit(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)
	key := "journal-failure"
	t.Cleanup(func() {
		_, cleanupErr := db.Exec(ctx, `DROP TRIGGER IF EXISTS test_journal_failure ON event_block_journal; DROP FUNCTION IF EXISTS test_journal_failure()`)
		require.NoError(t, cleanupErr)
	})
	_, err := db.Exec(ctx, `CREATE OR REPLACE FUNCTION test_journal_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.chain_id = 991341 THEN RAISE EXCEPTION 'journal unavailable'; END IF; RETURN NEW; END $$;
CREATE TRIGGER test_journal_failure BEFORE INSERT ON event_block_journal FOR EACH ROW EXECUTE FUNCTION test_journal_failure()`)
	require.NoError(t, err)

	ev := &IndexedEvent{
		ID:              fmt.Sprintf("%d-journal-failure-0", testEventChainID),
		ChainID:         testEventChainID,
		EventType:       "ContractEvent",
		ContractAddress: testWhitelistA,
		TransactionHash: "0xfeed",
		BlockNumber:     64010,
		BlockHash:       fmt.Sprintf("0x%064x", 64010),
	}
	hashes := hashMapFor(64000, 64010)
	err = ds.SaveBatch(ctx, testEventChainID, key, []*IndexedEvent{ev}, 64010, hashes[64010],
		[]JournalBlock{{Number: 64010, Hash: hashes[64010]}}, journalDepth)
	require.Error(t, err, "a journal failure must fail the whole batch")
	assert.Contains(t, err.Error(), "journal block")

	var events int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM event_store_events WHERE chain_id=$1 AND event_id=$2`, testEventChainID, ev.ID).Scan(&events))
	assert.Zero(t, events, "no event may survive a batch whose journal failed")
	ckpt, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Zero(t, ckpt, "the cursor must not advance past a batch whose journal failed")
	_, anchor, err := ds.LoadScopedCheckpointAnchor(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Empty(t, anchor)

	// And an unverifiable anchor is refused outright.
	err = ds.SaveBatch(ctx, testEventChainID, key, nil, 64010, "", nil, journalDepth)
	require.Error(t, err, "a batch without a real anchor hash must be refused")
	assert.Contains(t, err.Error(), "no canonical anchor hash")
}
