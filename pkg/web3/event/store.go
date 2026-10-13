package event

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/lib/pq"
	"github.com/rtcdance/streamgate/pkg/storage"
)

//go:generate mockgen -destination=../mocks/mock_event_store.go -package=mocks streamgate/pkg/web3/event EventStore

type EventStore interface {
	SaveEvent(event *IndexedEvent) error
	SaveCheckpoint(contractAddress string, blockNumber uint64) error
	LoadCheckpoint(contractAddress string) (uint64, error)
	GetEventsByBlockRange(fromBlock, toBlock uint64) ([]*IndexedEvent, error)
	MarkEventsReorged(blockHashes []string) (int, error)
	EventExists(eventID string) (bool, error)
	Close() error
}

// JournalBlock is one journaled block hash used to find the common ancestor
// after a reorg (SG-13 B.3).
type JournalBlock struct {
	Number uint64
	Hash   string
}

// DurableEventStore is the Postgres-backed extension of EventStore (SG-13 B).
// When the indexer finds one, each batch's events AND its checkpoint commit in
// a SINGLE database transaction — a failed batch can never advance the cursor
// — checkpoints are scoped by (chain, filter key) instead of address-only, and
// a bounded block journal supports common-ancestor rollback on reorg.
type DurableEventStore interface {
	EventStore
	// SaveBatch atomically persists the batch events and advances the
	// checkpoint for (chainID, filterKey). Any failure leaves both untouched.
	SaveBatch(ctx context.Context, chainID int64, filterKey string, events []*IndexedEvent, endBlock uint64, endBlockHash string) error
	// LoadScopedCheckpoint returns the last committed block for the filter.
	LoadScopedCheckpoint(ctx context.Context, chainID int64, filterKey string) (uint64, error)
	// RollbackScopedCheckpoint moves the checkpoint back after a reorg.
	RollbackScopedCheckpoint(ctx context.Context, chainID int64, filterKey string, toBlock uint64) error
	// SaveBlockJournal appends journaled block hashes, keeping only the newest
	// keepDepth entries for the chain.
	SaveBlockJournal(ctx context.Context, chainID int64, blocks []JournalBlock, keepDepth int) error
	// LoadBlockJournal returns journaled blocks with number >= fromBlock, ascending.
	LoadBlockJournal(ctx context.Context, chainID int64, fromBlock uint64) ([]JournalBlock, error)
	// MarkJournalOrphaned flags journaled blocks above the common ancestor.
	MarkJournalOrphaned(ctx context.Context, chainID int64, aboveBlock uint64) error
}

// PostgresEventStore persists events, chain/filter-scoped checkpoints and the
// block journal in Postgres (migration 041).
type PostgresEventStore struct {
	db storage.DB
}

func NewPostgresEventStore(db storage.DB) *PostgresEventStore {
	return &PostgresEventStore{db: db}
}

var _ DurableEventStore = (*PostgresEventStore)(nil)

func (s *PostgresEventStore) SaveEvent(event *IndexedEvent) error {
	_, err := s.db.Exec(context.Background(), `
		INSERT INTO event_store_events
			(event_id, chain_id, contract_address, block_number, block_hash, tx_hash, log_index, event_type, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (event_id) DO NOTHING`,
		event.ID, event.ChainID, event.ContractAddress, int64(event.BlockNumber), event.BlockHash,
		event.TransactionHash, int(event.LogIndex), event.EventType, eventPayload(event))
	if err != nil {
		return fmt.Errorf("event store: save event %s: %w", event.ID, err)
	}
	return nil
}

// SaveBatch commits the whole batch's events and the new checkpoint in ONE
// transaction (SG-13 B.2): a mid-batch failure rolls everything back and the
// cursor stays where it was.
func (s *PostgresEventStore) SaveBatch(ctx context.Context, chainID int64, filterKey string, events []*IndexedEvent, endBlock uint64, endBlockHash string) error {
	return s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		for _, ev := range events {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO event_store_events
					(event_id, chain_id, contract_address, block_number, block_hash, tx_hash, log_index, event_type, payload)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (event_id) DO NOTHING`,
				ev.ID, chainID, ev.ContractAddress, int64(ev.BlockNumber), ev.BlockHash,
				ev.TransactionHash, int(ev.LogIndex), ev.EventType, eventPayload(ev)); err != nil {
				return fmt.Errorf("event store: batch event %s: %w", ev.ID, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO event_checkpoints (chain_id, filter_key, block_number, block_hash)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (chain_id, filter_key)
			DO UPDATE SET block_number = EXCLUDED.block_number, block_hash = EXCLUDED.block_hash, updated_at = NOW()`,
			chainID, filterKey, int64(endBlock), endBlockHash); err != nil {
			return fmt.Errorf("event store: batch checkpoint: %w", err)
		}
		return nil
	})
}

func (s *PostgresEventStore) LoadScopedCheckpoint(ctx context.Context, chainID int64, filterKey string) (uint64, error) {
	var n int64
	err := s.db.QueryRow(ctx,
		`SELECT block_number FROM event_checkpoints WHERE chain_id = $1 AND filter_key = $2`,
		chainID, filterKey).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("event store: load checkpoint: %w", err)
	}
	return uint64(n), nil
}

func (s *PostgresEventStore) RollbackScopedCheckpoint(ctx context.Context, chainID int64, filterKey string, toBlock uint64) error {
	_, err := s.db.Exec(ctx,
		`UPDATE event_checkpoints SET block_number = $3, updated_at = NOW() WHERE chain_id = $1 AND filter_key = $2`,
		chainID, filterKey, int64(toBlock))
	return err
}

func (s *PostgresEventStore) SaveBlockJournal(ctx context.Context, chainID int64, blocks []JournalBlock, keepDepth int) error {
	if len(blocks) == 0 {
		return nil
	}
	return s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		for _, b := range blocks {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO event_block_journal (chain_id, block_number, block_hash)
				VALUES ($1, $2, $3) ON CONFLICT (chain_id, block_number) DO NOTHING`,
				chainID, int64(b.Number), b.Hash); err != nil {
				return fmt.Errorf("event store: journal block %d: %w", b.Number, err)
			}
		}
		// Trim to the newest keepDepth entries so the journal stays bounded.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM event_block_journal j
			WHERE j.chain_id = $1 AND j.block_number < (
				SELECT MAX(block_number) - $2 FROM event_block_journal WHERE chain_id = $1)`,
			chainID, keepDepth); err != nil {
			return fmt.Errorf("event store: trim journal: %w", err)
		}
		return nil
	})
}

func (s *PostgresEventStore) LoadBlockJournal(ctx context.Context, chainID int64, fromBlock uint64) ([]JournalBlock, error) {
	rows, err := s.db.Query(ctx, `
		SELECT block_number, block_hash FROM event_block_journal
		WHERE chain_id = $1 AND block_number >= $2 AND NOT orphaned
		ORDER BY block_number ASC`, chainID, int64(fromBlock))
	if err != nil {
		return nil, fmt.Errorf("event store: load journal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []JournalBlock
	for rows.Next() {
		var b JournalBlock
		var n int64
		if err := rows.Scan(&n, &b.Hash); err != nil {
			return nil, err
		}
		b.Number = uint64(n)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *PostgresEventStore) MarkJournalOrphaned(ctx context.Context, chainID int64, aboveBlock uint64) error {
	_, err := s.db.Exec(ctx,
		`UPDATE event_block_journal SET orphaned = TRUE WHERE chain_id = $1 AND block_number > $2`,
		chainID, int64(aboveBlock))
	return err
}

// SaveCheckpoint persists the legacy address-only checkpoint (in-memory store
// compatibility). Durable deployments use SaveBatch / LoadScopedCheckpoint.
func (s *PostgresEventStore) SaveCheckpoint(contractAddress string, blockNumber uint64) error {
	_, err := s.db.Exec(context.Background(), `
		INSERT INTO event_checkpoints (chain_id, filter_key, block_number)
		VALUES (0, $1, $2)
		ON CONFLICT (chain_id, filter_key)
		DO UPDATE SET block_number = EXCLUDED.block_number, updated_at = NOW()`,
		"addr:"+contractAddress, int64(blockNumber))
	return err
}

func (s *PostgresEventStore) LoadCheckpoint(contractAddress string) (uint64, error) {
	var n int64
	err := s.db.QueryRow(context.Background(),
		`SELECT block_number FROM event_checkpoints WHERE chain_id = 0 AND filter_key = $1`,
		"addr:"+contractAddress).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return uint64(n), nil
}

func (s *PostgresEventStore) GetEventsByBlockRange(fromBlock, toBlock uint64) ([]*IndexedEvent, error) {
	rows, err := s.db.Query(context.Background(), `
		SELECT event_id, chain_id, contract_address, block_number, block_hash, tx_hash, log_index, event_type, payload
		FROM event_store_events
		WHERE block_number BETWEEN $1 AND $2 AND NOT reorged
		ORDER BY block_number ASC, log_index ASC`, int64(fromBlock), int64(toBlock))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*IndexedEvent
	for rows.Next() {
		ev, err := scanStoredEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// MarkEventsReorged flags every stored event in the given blocks. Reorged
// events stay on record (they are facts about a chain that no longer exists)
// but are excluded from reads and from handler dispatch.
func (s *PostgresEventStore) MarkEventsReorged(blockHashes []string) (int, error) {
	if len(blockHashes) == 0 {
		return 0, nil
	}
	res, err := s.db.Exec(context.Background(),
		`UPDATE event_store_events SET reorged = TRUE WHERE block_hash = ANY($1)`,
		pq.Array(blockHashes))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *PostgresEventStore) EventExists(eventID string) (bool, error) {
	var one int
	err := s.db.QueryRow(context.Background(),
		`SELECT 1 FROM event_store_events WHERE event_id = $1`, eventID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *PostgresEventStore) Close() error { return nil }

func eventPayload(ev *IndexedEvent) []byte {
	payload := map[string]interface{}{
		"topics": ev.Topics,
		"data":   ev.Data,
	}
	if len(ev.Decoded) > 0 {
		payload["decoded"] = ev.Decoded
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return []byte("{}")
	}
	return b
}

type rowScanner interface{ Scan(...interface{}) error }

func scanStoredEvent(row rowScanner) (*IndexedEvent, error) {
	var ev IndexedEvent
	var blockNumber int64
	var logIndex int
	var payload []byte
	if err := row.Scan(&ev.ID, &ev.ChainID, &ev.ContractAddress, &blockNumber, &ev.BlockHash,
		&ev.TransactionHash, &logIndex, &ev.EventType, &payload); err != nil {
		return nil, err
	}
	ev.BlockNumber = uint64(blockNumber)
	ev.LogIndex = uint(logIndex)
	var p struct {
		Topics  []string               `json:"topics"`
		Data    string                 `json:"data"`
		Decoded map[string]interface{} `json:"decoded"`
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &p)
	}
	ev.Topics = p.Topics
	ev.Data = p.Data
	ev.Decoded = p.Decoded
	return &ev, nil
}

type MemoryEventStore struct {
	mu          sync.RWMutex
	events      map[string]*IndexedEvent
	checkpoints map[string]uint64
	blockIndex  map[uint64][]*IndexedEvent
}

func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		events:      make(map[string]*IndexedEvent),
		checkpoints: make(map[string]uint64),
		blockIndex:  make(map[uint64][]*IndexedEvent),
	}
}

func (s *MemoryEventStore) SaveEvent(event *IndexedEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.events[event.ID]; exists {
		return fmt.Errorf("event already exists: %s", event.ID)
	}

	s.events[event.ID] = event
	s.blockIndex[event.BlockNumber] = append(s.blockIndex[event.BlockNumber], event)
	return nil
}

func (s *MemoryEventStore) SaveCheckpoint(contractAddress string, blockNumber uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints[contractAddress] = blockNumber
	return nil
}

func (s *MemoryEventStore) LoadCheckpoint(contractAddress string) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkpoints[contractAddress], nil
}

func (s *MemoryEventStore) GetEventsByBlockRange(fromBlock, toBlock uint64) ([]*IndexedEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*IndexedEvent
	for block := fromBlock; block <= toBlock; block++ {
		result = append(result, s.blockIndex[block]...)
	}
	return result, nil
}

func (s *MemoryEventStore) MarkEventsReorged(blockHashes []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hashSet := make(map[string]bool, len(blockHashes))
	for _, h := range blockHashes {
		hashSet[h] = true
	}

	count := 0
	for _, event := range s.events {
		if hashSet[event.BlockHash] {
			if event.Decoded == nil {
				event.Decoded = make(map[string]interface{})
			}
			event.Decoded["reorged"] = true
			count++
		}
	}
	return count, nil
}

func (s *MemoryEventStore) EventExists(eventID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.events[eventID]
	return exists, nil
}

func (s *MemoryEventStore) Close() error {
	return nil
}

func (s *MemoryEventStore) EventCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

func (s *MemoryEventStore) GetCheckpointTimestamp(contractAddress string) time.Time {
	return time.Time{}
}
