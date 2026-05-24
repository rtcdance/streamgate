package event

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDecodeContentRegisteredEvent_Success(t *testing.T) {
	event := &IndexedEvent{
		Topics: []string{
			"0xevent_sig",
			"0xcontent_hash_value",
			"0xowner_address",
		},
	}
	decoded, err := DecodeContentRegisteredEvent(event)
	require.NoError(t, err)
	assert.Equal(t, "0xcontent_hash_value", decoded.ContentHash)
	assert.Equal(t, "0xowner_address", decoded.Owner)
}

func TestDecodeContentRegisteredEvent_TooFewTopics(t *testing.T) {
	event := &IndexedEvent{
		Topics: []string{"0xevent_sig", "0xonly_one"},
	}
	_, err := DecodeContentRegisteredEvent(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected at least 3 topics")
}

func TestDecodeNFTMintedEvent_Success(t *testing.T) {
	event := &IndexedEvent{
		Topics: []string{
			"0xevent_sig",
			"0xfrom_address",
			"0xowner_address",
			"0xtoken_id",
		},
	}
	decoded, err := DecodeNFTMintedEvent(event)
	require.NoError(t, err)
	assert.Equal(t, "0xtoken_id", decoded.TokenID)
	assert.Equal(t, "0xowner_address", decoded.Owner)
}

func TestDecodeNFTMintedEvent_TooFewTopics(t *testing.T) {
	event := &IndexedEvent{
		Topics: []string{"0xevent_sig", "0xfrom", "0xto"},
	}
	_, err := DecodeNFTMintedEvent(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected at least 4 topics")
}

func TestEventListener_OnAndEmit(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	require.NoError(t, err)

	listener := NewEventListener(ei, zap.NewNop())

	var receivedEvent *IndexedEvent
	listener.On("ContractEvent", func(ctx context.Context, event *IndexedEvent) error {
		receivedEvent = event
		return nil
	})

	event := &IndexedEvent{
		ID:        "test-1",
		EventType: "ContractEvent",
	}
	err = listener.Emit(context.Background(), event)
	require.NoError(t, err)
	assert.NotNil(t, receivedEvent)
	assert.Equal(t, "test-1", receivedEvent.ID)
}

func TestEventListener_Emit_NoHandlers(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	require.NoError(t, err)

	listener := NewEventListener(ei, zap.NewNop())
	err = listener.Emit(context.Background(), &IndexedEvent{EventType: "Unknown"})
	assert.NoError(t, err)
}

func TestEventListener_ProcessAllEvents(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	require.NoError(t, err)

	ctx := context.Background()
	ei.processLog(ctx, makeTransferLog(5, common_HexToHash("0x01"), 0))
	ei.processLog(ctx, makeTransferLog(6, common_HexToHash("0x02"), 1))

	var count int
	listener := NewEventListener(ei, zap.NewNop())
	listener.On("ContractEvent", func(ctx context.Context, event *IndexedEvent) error {
		count++
		return nil
	})

	err = listener.ProcessAllEvents(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestEventListener_HandlerError(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	require.NoError(t, err)

	ctx := context.Background()
	ei.processLog(ctx, makeTransferLog(5, common_HexToHash("0x01"), 0))

	listener := NewEventListener(ei, zap.NewNop())
	listener.On("ContractEvent", func(ctx context.Context, event *IndexedEvent) error {
		return assert.AnError
	})

	err = listener.ProcessAllEvents(context.Background())
	assert.NoError(t, err)
}

func TestEventIndexer_GetEventsByType(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	require.NoError(t, err)

	ctx := context.Background()
	ei.processLog(ctx, makeTransferLog(5, common_HexToHash("0x01"), 0))

	events := ei.GetEventsByType("ContractEvent")
	assert.Len(t, events, 1)

	events = ei.GetEventsByType("NonExistent")
	assert.Empty(t, events)
}

func TestEventIndexer_GetReorgSafeBlock(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		reader := &mockEventReader{blockNum: 100}
		ei, err := newTestEventIndexer(reader)
		require.NoError(t, err)
		ei.confirmationBlocks = 12

		safeBlock, err := ei.GetReorgSafeBlock(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(88), safeBlock)
	})

	t.Run("block_error", func(t *testing.T) {
		reader := &mockEventReader{blockErr: assert.AnError}
		ei, err := newTestEventIndexer(reader)
		require.NoError(t, err)

		_, err = ei.GetReorgSafeBlock(context.Background())
		assert.Error(t, err)
	})

	t.Run("low_block", func(t *testing.T) {
		reader := &mockEventReader{blockNum: 5}
		ei, err := newTestEventIndexer(reader)
		require.NoError(t, err)
		ei.confirmationBlocks = 12

		safeBlock, err := ei.GetReorgSafeBlock(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(0), safeBlock)
	})
}

func TestEventIndexer_SetEventParser(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	require.NoError(t, err)

	parser := NewEventParser(zap.NewNop())
	ei.SetEventParser(parser)

	ei.mu.RLock()
	p := ei.eventParser
	ei.mu.RUnlock()
	assert.NotNil(t, p)
}

func TestEventIndexer_SetOnEvent(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	require.NoError(t, err)

	called := false
	ei.SetOnEvent(func(ctx context.Context, event *IndexedEvent) error {
		called = true
		return nil
	})

	ei.processLog(context.Background(), makeTransferLog(5, common_HexToHash("0x01"), 0))
	assert.True(t, called)
}

func common_HexToHash(s string) [32]byte {
	h := [32]byte{}
	copy(h[:], []byte(s))
	return h
}
