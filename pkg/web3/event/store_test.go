package event

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMemoryEventStore_SaveAndLoadEvent(t *testing.T) {
	store := NewMemoryEventStore()
	defer func() { _ = store.Close() }()

	event := &IndexedEvent{
		ID:              "0xabc-0",
		EventType:       "Transfer",
		ContractAddress: "0x1234",
		TransactionHash: "0xabc",
		BlockNumber:     100,
		BlockHash:       "0xdef",
		LogIndex:        0,
	}

	err := store.SaveEvent(event)
	require.NoError(t, err)
	assert.Equal(t, 1, store.EventCount())

	exists, err := store.EventExists("0xabc-0")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = store.EventExists("nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestMemoryEventStore_Dedup(t *testing.T) {
	store := NewMemoryEventStore()
	defer func() { _ = store.Close() }()

	event := &IndexedEvent{
		ID:          "0xabc-0",
		EventType:   "Transfer",
		BlockNumber: 100,
	}

	err := store.SaveEvent(event)
	require.NoError(t, err)

	err = store.SaveEvent(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Equal(t, 1, store.EventCount())
}

func TestMemoryEventStore_Checkpoint(t *testing.T) {
	store := NewMemoryEventStore()
	defer func() { _ = store.Close() }()

	block, err := store.LoadCheckpoint("0x1234")
	require.NoError(t, err)
	assert.Equal(t, uint64(0), block)

	err = store.SaveCheckpoint("0x1234", 500)
	require.NoError(t, err)

	block, err = store.LoadCheckpoint("0x1234")
	require.NoError(t, err)
	assert.Equal(t, uint64(500), block)

	err = store.SaveCheckpoint("0x1234", 600)
	require.NoError(t, err)

	block, err = store.LoadCheckpoint("0x1234")
	require.NoError(t, err)
	assert.Equal(t, uint64(600), block)
}

func TestMemoryEventStore_GetEventsByBlockRange(t *testing.T) {
	store := NewMemoryEventStore()
	defer func() { _ = store.Close() }()

	for i := uint64(100); i <= 104; i++ {
		err := store.SaveEvent(&IndexedEvent{
			ID:          string(rune(i)),
			BlockNumber: i,
		})
		require.NoError(t, err)
	}

	events, err := store.GetEventsByBlockRange(101, 103)
	require.NoError(t, err)
	assert.Len(t, events, 3)
}

func TestMemoryEventStore_MarkEventsReorged(t *testing.T) {
	store := NewMemoryEventStore()
	defer func() { _ = store.Close() }()

	_ = store.SaveEvent(&IndexedEvent{ID: "e1", BlockHash: "0xaaa", BlockNumber: 100, Decoded: make(map[string]interface{})})
	_ = store.SaveEvent(&IndexedEvent{ID: "e2", BlockHash: "0xbbb", BlockNumber: 101, Decoded: make(map[string]interface{})})
	_ = store.SaveEvent(&IndexedEvent{ID: "e3", BlockHash: "0xaaa", BlockNumber: 100, Decoded: make(map[string]interface{})})

	count, err := store.MarkEventsReorged([]string{"0xaaa"})
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	exists, _ := store.EventExists("e1")
	assert.True(t, exists)
}

func TestEventIndexer_WithEventStore(t *testing.T) {
	store := NewMemoryEventStore()
	defer func() { _ = store.Close() }()

	indexer := &EventIndexer{
		store: store,
	}
	assert.NotNil(t, indexer.store)

	event := &IndexedEvent{
		ID:          "0xabc-0",
		EventType:   "Transfer",
		BlockNumber: 100,
	}
	indexer.events = make([]*IndexedEvent, 0, 1000)
	indexer.maxEvents = 10000
	indexer.seenIDs = make(map[string]struct{})
	indexer.logger = zap.NewNop()

	indexer.addEvent(context.Background(), event)
	assert.Len(t, indexer.events, 1)

	indexer.addEvent(context.Background(), event)
	assert.Len(t, indexer.events, 1)
}

func TestEventIndexer_WithReorgDetector(t *testing.T) {
	store := NewMemoryEventStore()
	defer func() { _ = store.Close() }()

	indexer := &EventIndexer{
		store: store,
	}
	assert.NotNil(t, indexer.store)
}

func TestEventIndexer_ResumeFromCheckpoint(t *testing.T) {
	store := NewMemoryEventStore()
	defer func() { _ = store.Close() }()

	_ = store.SaveCheckpoint("0x0000000000000000000000000000000000000000", 500)

	indexer := &EventIndexer{
		store:           store,
		events:          make([]*IndexedEvent, 0, 1000),
		contractAddress: common.HexToAddress("0x0000000000000000000000000000000000000000"),
	}
	indexer.logger = zap.NewNop()

	checkpoint, err := store.LoadCheckpoint(indexer.contractAddress.Hex())
	require.NoError(t, err)
	assert.Equal(t, uint64(500), checkpoint)
}
