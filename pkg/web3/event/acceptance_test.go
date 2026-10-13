package event

// SG-13 B acceptance tests. Durability and journal cases run against a real
// Postgres because the semantics under test (single-transaction batch commit,
// scoped checkpoints across restarts, journal orphaning) only exist there;
// point STREAMGATE_TEST_DB_DSN at a SCRATCH database — the tests delete only
// their own rows (rows scoped to chain ids 991341/991342).

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	testEventChainID  int64 = 991341
	testEventChainID2 int64 = 991342

	transferSig       = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	transferSingleSig = "0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62"
	transferBatchSig  = "0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb"

	testWhitelistA = "0x0000000000000000000000000000000000000abc"
	testWhitelistB = "0x0000000000000000000000000000000000000def"
)

func requireEventTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set; SG-13 B event tests need a scratch Postgres")
	}
	return dsn
}

func openEventTestDB(t *testing.T) *storage.Database {
	t.Helper()
	dsn := requireEventTestDSN(t)
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	if err != nil {
		t.Skipf("event test DB unavailable: %v", err)
	}
	if err := db.Ping(context.Background()); err != nil {
		t.Skipf("event test DB unreachable: %v", err)
	}
	if err := helpers.ApplyMigrations(t, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	cleanupEventTestRows(t, db)
	t.Cleanup(func() {
		cleanupEventTestRows(t, db)
		_ = db.Close()
	})
	return db
}

func cleanupEventTestRows(t *testing.T, db *storage.Database) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`DELETE FROM event_store_events WHERE chain_id IN ($1, $2)`,
		`DELETE FROM event_block_journal WHERE chain_id IN ($1, $2)`,
		`DELETE FROM event_checkpoints WHERE chain_id IN ($1, $2)`,
	} {
		if _, err := db.Exec(ctx, q, testEventChainID, testEventChainID2); err != nil {
			t.Fatalf("cleanup %q: %v", q, err)
		}
	}
	// Legacy address-only checkpoints are written under chain 0 by the reorg
	// rollback compatibility path.
	if _, err := db.Exec(ctx,
		`DELETE FROM event_checkpoints WHERE chain_id = 0 AND filter_key = 'addr:0x0000000000000000000000000000000000000AbC'`); err != nil {
		t.Fatalf("cleanup legacy checkpoints: %v", err)
	}
}

// ---- T3: durability --------------------------------------------------------

func TestAcceptance_EventStoreDurability(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)

	ev := &IndexedEvent{
		ID:              fmt.Sprintf("%d-%s-%s-%d", testEventChainID, "0xaaa", "0xbbb", 0),
		ChainID:         testEventChainID,
		EventType:       "ContractEvent",
		ContractAddress: testWhitelistA,
		TransactionHash: "0xbbb",
		BlockNumber:     42,
		BlockHash:       "0xaaa",
		LogIndex:        0,
	}
	require.NoError(t, ds.SaveEvent(ev))
	require.NoError(t, ds.SaveEvent(ev), "duplicate SaveEvent must be idempotent")

	var rows int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT COUNT(*) FROM event_store_events WHERE event_id = $1`, ev.ID).Scan(&rows))
	assert.Equal(t, 1, rows, "the same event must occupy exactly one row")

	key := "v1|" + testWhitelistA
	ev2 := *ev
	ev2.ID = fmt.Sprintf("%d-%s-%s-%d", testEventChainID, "0xccc", "0xddd", 0)
	ev2.BlockNumber = 43
	ev2.BlockHash = "0xccc"
	require.NoError(t, ds.SaveBatch(ctx, testEventChainID, key, []*IndexedEvent{ev, &ev2}, 43, "0xccc"))

	ckpt, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Equal(t, uint64(43), ckpt, "SaveBatch must commit events and checkpoint together")

	// Restart simulation: a brand-new store over an independent connection pool.
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	db2, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })
	ds2 := NewPostgresEventStore(db2)
	ckpt2, err := ds2.LoadScopedCheckpoint(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Equal(t, uint64(43), ckpt2, "checkpoint must survive a restart")
	exists, err := ds2.EventExists(ev.ID)
	require.NoError(t, err)
	assert.True(t, exists, "events must survive a restart")
}

// ---- T4: bounded batches ---------------------------------------------------

type recordingReader struct {
	mu       sync.Mutex
	blockNum uint64
	queries  []ethereum.FilterQuery
}

func (r *recordingReader) BlockNumber(context.Context) (uint64, error) { return r.blockNum, nil }

func (r *recordingReader) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, q)
	return nil, nil
}

func TestAcceptance_EventBatchSizeBounded(t *testing.T) {
	requireEventTestDSN(t)

	assert.Equal(t, uint64(100), indexBatchSize, "batch bound must be 100 blocks")

	reader := &recordingReader{blockNum: 300}
	cfg := EventIndexerConfig{
		ContractAddresses: []string{testWhitelistA},
		EventSignatures:   []string{transferSig},
		ChainID:           testEventChainID,
	}
	idx, err := NewEventIndexerWithConfig(reader, cfg, zap.NewNop())
	require.NoError(t, err)

	require.NoError(t, idx.indexRange(context.Background(), 1, 250))

	reader.mu.Lock()
	queries := append([]ethereum.FilterQuery(nil), reader.queries...)
	reader.mu.Unlock()

	require.Len(t, queries, 3, "250 blocks at 100/batch must take 3 FilterLogs calls")
	for _, q := range queries {
		require.NotNil(t, q.FromBlock)
		require.NotNil(t, q.ToBlock)
		width := new(big.Int).Sub(q.ToBlock, q.FromBlock).Uint64() + 1
		assert.LessOrEqual(t, width, uint64(100),
			"one FilterLogs call may never span more than %d blocks", indexBatchSize)
	}
}

type failOnCallReader struct {
	mu         sync.Mutex
	blockNum   uint64
	calls      int
	failOnCall int
	logsFn     func(call int, q ethereum.FilterQuery) []types.Log
}

func (r *failOnCallReader) BlockNumber(context.Context) (uint64, error) { return r.blockNum, nil }

func (r *failOnCallReader) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	logs := r.logsFn(call, q)
	if call == r.failOnCall {
		return logs, fmt.Errorf("scripted FilterLogs failure on batch %d", call)
	}
	return logs, nil
}

func TestAcceptance_EventBatchCursorSafety(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)

	reader := &failOnCallReader{
		blockNum:   250,
		failOnCall: 2,
		logsFn: func(call int, _ ethereum.FilterQuery) []types.Log {
			if call == 1 {
				return []types.Log{makeTransferLog(10, common.HexToHash("0xa1"), 0)}
			}
			return []types.Log{makeTransferLog(150, common.HexToHash("0xb2"), 0)}
		},
	}
	cfg := EventIndexerConfig{
		ContractAddresses:  []string{testWhitelistA},
		EventSignatures:    []string{transferSig},
		ConfirmationBlocks: 2,
		ChainID:            testEventChainID,
		FilterVersion:      "v1",
	}
	idx, err := NewEventIndexerWithConfig(reader, cfg, zap.NewNop())
	require.NoError(t, err)
	idx.SetEventStore(ds)

	var mu sync.Mutex
	var consumed []uint64
	idx.SetOnEvent(func(_ context.Context, ev *IndexedEvent) error {
		mu.Lock()
		consumed = append(consumed, ev.BlockNumber)
		mu.Unlock()
		return nil
	})

	// safe = 250-2 = 248 → batch 1 = [1,100] commits, batch 2 = [101,200] fails.
	err = idx.indexEvents(ctx)
	require.Error(t, err)

	assert.Equal(t, uint64(100), idx.GetCurrentBlock(),
		"cursor must stop at the end of the first committed batch")

	ckpt, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, idx.filterKey())
	require.NoError(t, err)
	assert.Equal(t, uint64(100), ckpt,
		"durable checkpoint must not advance past the first batch")

	mu.Lock()
	got := append([]uint64(nil), consumed...)
	mu.Unlock()
	assert.Equal(t, []uint64{10}, got,
		"only batch-1 events may be consumed; batch-2 events must never reach handlers")
}

// ---- T5: journal rollback --------------------------------------------------

// scriptedReorgChecker reports blocks above keepBelow as reorged; when
// allReorged is set, no journaled block matches any more.
type scriptedReorgChecker struct {
	keepBelow  uint64
	allReorged bool
}

func (s *scriptedReorgChecker) CheckReorg(_ context.Context, number uint64, _ common.Hash) (bool, error) {
	if s.allReorged {
		return true, nil
	}
	return number > s.keepBelow, nil
}

func (s *scriptedReorgChecker) MarkReorgedEvents(_ context.Context, _ []*IndexedEvent) []string {
	return nil
}

func journalTestBlocks(first, last uint64) []JournalBlock {
	blocks := make([]JournalBlock, 0, last-first+1)
	for n := first; n <= last; n++ {
		blocks = append(blocks, JournalBlock{Number: n, Hash: fmt.Sprintf("0x%064x", n)})
	}
	return blocks
}

func TestAcceptance_EventReorgJournalRollback(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)

	const (
		firstBlock uint64 = 1000
		lastBlock  uint64 = 1063
		ancestor   uint64 = 1050
	)

	cfg := EventIndexerConfig{
		ContractAddresses: []string{testWhitelistA},
		EventSignatures:   []string{transferSig},
		ChainID:           testEventChainID,
		FilterVersion:     "v1",
	}
	idx, err := NewEventIndexerWithConfig(&mockEventReader{blockNum: 1100}, cfg, zap.NewNop())
	require.NoError(t, err)
	idx.SetEventStore(ds)

	saveEventAt := func(n uint64) {
		ev := &IndexedEvent{
			ID:              fmt.Sprintf("%d-0x%064x-0xfeed-%d", testEventChainID, n, n),
			ChainID:         testEventChainID,
			EventType:       "ContractEvent",
			ContractAddress: testWhitelistA,
			TransactionHash: "0xfeed",
			BlockNumber:     n,
			BlockHash:       fmt.Sprintf("0x%064x", n),
		}
		require.NoError(t, ds.SaveEvent(ev))
	}

	require.NoError(t, ds.SaveBlockJournal(ctx, testEventChainID, journalTestBlocks(firstBlock, lastBlock), journalDepth))
	saveEventAt(1040) // below the ancestor: must survive
	saveEventAt(1051) // orphan side: must be flagged
	saveEventAt(1060) // orphan side: must be flagged
	key := idx.filterKey()
	require.NoError(t, ds.SaveBatch(ctx, testEventChainID, key, nil, lastBlock, fmt.Sprintf("0x%064x", lastBlock)))

	idx.SetReorgDetector(&scriptedReorgChecker{keepBelow: ancestor})
	idx.currentBlock = lastBlock

	require.NoError(t, idx.rollbackToCommonAncestor(ctx, ds),
		"a reorg inside the journal must roll back to the common ancestor")

	ckpt, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, key)
	require.NoError(t, err)
	assert.Equal(t, ancestor, ckpt, "checkpoint must rewind to the common ancestor")
	assert.Equal(t, ancestor, idx.GetCurrentBlock(), "cursor must rewind to the common ancestor")

	journalAfter, err := ds.LoadBlockJournal(ctx, testEventChainID, 0)
	require.NoError(t, err)
	require.NotEmpty(t, journalAfter)
	assert.LessOrEqual(t, journalAfter[len(journalAfter)-1].Number, ancestor,
		"blocks above the ancestor must be orphaned out of the journal")

	orphaned, err := ds.GetEventsByBlockRange(ancestor+1, lastBlock)
	require.NoError(t, err)
	assert.Empty(t, orphaned, "events above the ancestor must be flagged reorged")
	kept, err := ds.GetEventsByBlockRange(firstBlock, ancestor)
	require.NoError(t, err)
	assert.Len(t, kept, 1, "events at or below the ancestor must survive")

	// A reorg deeper than the journal must fail loudly, never pretend success.
	chain2 := testEventChainID2
	cfg2 := cfg
	cfg2.ChainID = chain2
	idx2, err := NewEventIndexerWithConfig(&mockEventReader{blockNum: 1100}, cfg2, zap.NewNop())
	require.NoError(t, err)
	idx2.SetEventStore(ds)
	require.NoError(t, ds.SaveBlockJournal(ctx, chain2, journalTestBlocks(firstBlock, lastBlock), journalDepth))
	key2 := idx2.filterKey()
	require.NoError(t, ds.SaveBatch(ctx, chain2, key2, nil, lastBlock, fmt.Sprintf("0x%064x", lastBlock)))
	idx2.SetReorgDetector(&scriptedReorgChecker{allReorged: true})
	idx2.currentBlock = lastBlock

	err = idx2.rollbackToCommonAncestor(ctx, ds)
	require.Error(t, err, "a reorg deeper than the journal must return an explicit error")
	assert.Contains(t, err.Error(), "reorg deeper than journal depth")

	deepCkpt, err := ds.LoadScopedCheckpoint(ctx, chain2, key2)
	require.NoError(t, err)
	assert.Equal(t, lastBlock, deepCkpt, "a failed rollback must leave the checkpoint untouched")
}

// ---- T6: whitelist and filter versions -------------------------------------

func TestAcceptance_EventFilterWhitelist(t *testing.T) {
	requireEventTestDSN(t)
	ctx := context.Background()

	// No whitelist → Start refuses (full-chain scanning is forbidden), but the
	// constructor itself must tolerate empty addresses.
	reader := &mockEventReader{blockNum: 100}
	empty, err := NewEventIndexerWithConfig(reader,
		EventIndexerConfig{EventSignatures: []string{transferSig}, ChainID: testEventChainID}, zap.NewNop())
	require.NoError(t, err, "constructor must accept an empty address list")
	err = empty.Start(ctx)
	require.Error(t, err, "Start without a contract whitelist must fail")
	assert.Contains(t, err.Error(), "whitelist")
	assert.False(t, empty.started.Load())

	addrs := []string{testWhitelistA, testWhitelistB}
	cfg := EventIndexerConfig{
		ContractAddresses: addrs,
		EventSignatures:   []string{transferSig, transferSingleSig, transferBatchSig},
		ChainID:           testEventChainID,
	}
	idx, err := NewEventIndexerWithConfig(reader, cfg, zap.NewNop())
	require.NoError(t, err)

	query := idx.buildFilterQuery()
	assert.ElementsMatch(t,
		[]common.Address{common.HexToAddress(addrs[0]), common.HexToAddress(addrs[1])},
		query.Addresses, "query addresses must equal the whitelist exactly")

	require.Len(t, query.Topics, 1)
	sigs := query.Topics[0]
	require.Len(t, sigs, 3)
	for _, sig := range []string{transferSig, transferSingleSig, transferBatchSig} {
		assert.Contains(t, sigs, common.HexToHash(sig), "signature %s must be in the topic set", sig)
	}
}

func TestAcceptance_EventCheckpointFilterVersion(t *testing.T) {
	db := openEventTestDB(t)
	ctx := context.Background()
	ds := NewPostgresEventStore(db)

	cfg := EventIndexerConfig{
		ContractAddresses: []string{testWhitelistA},
		EventSignatures:   []string{transferSig},
		ChainID:           testEventChainID,
		FilterVersion:     "v1",
	}
	idx, err := NewEventIndexerWithConfig(&mockEventReader{blockNum: 900}, cfg, zap.NewNop())
	require.NoError(t, err)
	keyV1 := idx.filterKey()

	ev := &IndexedEvent{
		ID:              fmt.Sprintf("%d-0xaaa-0xbbb-0", testEventChainID),
		ChainID:         testEventChainID,
		EventType:       "ContractEvent",
		ContractAddress: testWhitelistA,
		TransactionHash: "0xbbb",
		BlockNumber:     500,
		BlockHash:       "0xaaa",
	}
	require.NoError(t, ds.SaveBatch(ctx, testEventChainID, keyV1, []*IndexedEvent{ev}, 500, "0xaaa"))

	got, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, keyV1)
	require.NoError(t, err)
	assert.Equal(t, uint64(500), got)

	idx.SetFilterVersion("v2")
	keyV2 := idx.filterKey()
	require.NotEqual(t, keyV1, keyV2, "a new filter version must produce a new filter key")

	gotV2, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, keyV2)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), gotV2, "a new filter key must never reuse the old checkpoint")

	require.NoError(t, ds.SaveBatch(ctx, testEventChainID, keyV2, nil, 700, ""))

	gotV1, err := ds.LoadScopedCheckpoint(ctx, testEventChainID, keyV1)
	require.NoError(t, err)
	assert.Equal(t, uint64(500), gotV1, "the old filter key keeps its own checkpoint")
	gotV2, err = ds.LoadScopedCheckpoint(ctx, testEventChainID, keyV2)
	require.NoError(t, err)
	assert.Equal(t, uint64(700), gotV2)
}
