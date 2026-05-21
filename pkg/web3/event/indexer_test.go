package event

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

type mockEventReader struct {
	mu        sync.Mutex
	blockNum  uint64
	logs      []types.Log
	blockErr  error
	filterErr error
}

func (m *mockEventReader) BlockNumber(ctx context.Context) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blockNum, m.blockErr
}

func (m *mockEventReader) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.logs, m.filterErr
}

func (m *mockEventReader) setLogs(logs []types.Log) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = logs
}

func makeTransferLog(blockNum uint64, txHash common.Hash, logIndex uint) types.Log {
	return types.Log{
		Address:     common.HexToAddress("0xabc"),
		Topics:      []common.Hash{common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")},
		Data:        common.Hex2Bytes("0000000000000000000000000000000000000000000000000de0b6b3a7640000"),
		BlockNumber: blockNum,
		TxHash:      txHash,
		TxIndex:     0,
		BlockHash:   common.BigToHash(big.NewInt(int64(blockNum))),
		Index:       logIndex,
		Removed:     false,
	}
}

func newTestEventIndexer(reader *mockEventReader) (*EventIndexer, error) {
	cfg := EventIndexerConfig{
		ContractAddresses:  []string{"0x0000000000000000000000000000000000000abc"},
		EventSignatures:    []string{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"},
		ConfirmationBlocks: 2,
		UpdateInterval:     50 * time.Millisecond,
	}
	return NewEventIndexerWithConfig(reader, cfg, zap.NewNop())
}

func TestEventIndexer_StartStop(t *testing.T) {
	reader := &mockEventReader{blockNum: 100}
	ei, err := newTestEventIndexer(reader)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ei.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	ei.Stop()

	if cb := ei.GetCurrentBlock(); cb != 100 {
		t.Errorf("expected current block 100, got %d", cb)
	}
}

func TestEventIndexer_Dedup(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	txHash := common.HexToHash("0x01")
	log := makeTransferLog(5, txHash, 0)

	ctx := context.Background()
	ei.processLog(ctx, log)
	ei.processLog(ctx, log)

	events := ei.GetEvents()
	if len(events) != 1 {
		t.Errorf("expected 1 event after dedup, got %d", len(events))
	}
}

func TestEventIndexer_MaxEvents(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}
	ei.maxEvents = 5

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		txHash := common.BigToHash(big.NewInt(int64(i)))
		log := makeTransferLog(uint64(i), txHash, 0)
		ei.processLog(ctx, log)
	}

	events := ei.GetEvents()
	if len(events) > 5 {
		t.Errorf("expected at most 5 events (maxEvents), got %d", len(events))
	}
}

func TestEventIndexer_ConfirmationBlocks(t *testing.T) {
	reader := &mockEventReader{blockNum: 20}
	ei, err := newTestEventIndexer(reader)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	logs := []types.Log{
		makeTransferLog(16, common.HexToHash("0x01"), 0),
		makeTransferLog(17, common.HexToHash("0x02"), 1),
		makeTransferLog(18, common.HexToHash("0x03"), 2),
		makeTransferLog(19, common.HexToHash("0x04"), 3),
	}
	reader.setLogs(logs)

	ctx := context.Background()
	ei.indexEvents(ctx)

	events := ei.GetEvents()
	if len(events) == 0 {
		t.Error("expected some events to be indexed")
	}
}

func TestEventIndexer_GetEventsByBlockRange(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	ctx := context.Background()
	ei.processLog(ctx, makeTransferLog(5, common.HexToHash("0x01"), 0))
	ei.processLog(ctx, makeTransferLog(6, common.HexToHash("0x02"), 1))
	ei.processLog(ctx, makeTransferLog(7, common.HexToHash("0x03"), 2))

	events := ei.GetEventsByBlockRange(6, 7)
	if len(events) != 2 {
		t.Errorf("expected 2 events in range [6,7], got %d", len(events))
	}
}

func TestEventIndexer_WSDisconnectSwitchToPolling(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	logsCh := make(chan types.Log)
	close(logsCh)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ei.wg.Add(1)
	go ei.websocketLoop(ctx, logsCh)

	time.Sleep(200 * time.Millisecond)

	ei.mu.RLock()
	mode := ei.mode
	ei.mu.RUnlock()

	select {
	case newMode := <-ei.modeCh:
		if newMode != "polling" {
			t.Errorf("expected mode signal 'polling', got %q", newMode)
		}
	default:
		t.Error("expected mode signal on modeCh after WS disconnect")
	}

	_ = mode
}

func TestEventIndexer_Replay(t *testing.T) {
	reader := &mockEventReader{blockNum: 20}
	logs := []types.Log{
		makeTransferLog(10, common.HexToHash("0x01"), 0),
		makeTransferLog(11, common.HexToHash("0x02"), 1),
	}
	reader.setLogs(logs)

	ei, err := newTestEventIndexer(reader)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	ctx := context.Background()
	if err := ei.Replay(ctx, 10, 11); err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	events := ei.GetEvents()
	if len(events) != 2 {
		t.Errorf("expected 2 events after replay, got %d", len(events))
	}
}

func TestEventIndexer_GetEventCount(t *testing.T) {
	reader := &mockEventReader{blockNum: 10}
	ei, err := newTestEventIndexer(reader)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	if ei.GetEventCount() != 0 {
		t.Error("expected 0 events initially")
	}

	ctx := context.Background()
	ei.processLog(ctx, makeTransferLog(5, common.HexToHash("0x01"), 0))
	if ei.GetEventCount() != 1 {
		t.Errorf("expected 1 event, got %d", ei.GetEventCount())
	}
}
