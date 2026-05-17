package web3

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// mockHeaderReader implements HeaderReader for testing.
type mockHeaderReader struct {
	headers map[int64]*types.Header // blockNumber → header; nil key = latest
	subErr  error
}

func (m *mockHeaderReader) HeaderByNumber(_ context.Context, number *big.Int) (*types.Header, error) {
	var key int64 = -1
	if number != nil {
		key = number.Int64()
	}
	h, ok := m.headers[key]
	if !ok {
		return nil, errors.New("header not found")
	}
	return h, nil
}

func (m *mockHeaderReader) SubscribeNewHead(_ context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	return nil, m.subErr
}

func makeHeader(number int64, hash, parentHash string) *types.Header {
	h := &types.Header{
		Number:     big.NewInt(number),
		ParentHash: common.HexToHash(parentHash),
	}
	// Set the hash by using the fields — we need a deterministic hash
	// For testing, we use a workaround: set extra data to encode the hash
	if hash != "" {
		h.Extra = common.Hex2Bytes(hash[2:])
	}
	return h
}

func TestReorgDetector_CheckReorg_NoReorg(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			100: makeHeader(100, "0xaaa", "0x999"),
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	// The stored header hash at block 100 matches what the mock returns
	reorged, err := rd.CheckReorg(context.Background(), 100, mock.headers[100].Hash())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reorged {
		t.Error("expected no reorg, but reorg detected")
	}
}

func TestReorgDetector_CheckReorg_ReorgDetected(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			100: makeHeader(100, "0xbbb", "0x999"),
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	// Original hash differs from current on-chain hash
	originalHash := common.HexToHash("0xaaa")
	reorged, err := rd.CheckReorg(context.Background(), 100, originalHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reorged {
		t.Error("expected reorg to be detected")
	}
}

func TestReorgDetector_CheckReorg_RPCError(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{}, // block 100 not available
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	_, err := rd.CheckReorg(context.Background(), 100, common.HexToHash("0xaaa"))
	if err == nil {
		t.Error("expected error when header not available")
	}
}

func TestReorgDetector_IsFinalized_NotEnoughConfirmations(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(105, "0xccc", "0xbbb"), // latest = 105
			100: makeHeader(100, "0xaaa", "0x999"),
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	finalized, err := rd.IsFinalized(context.Background(), 100, mock.headers[100].Hash(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finalized {
		t.Error("block 100 with 10 confirmations should not be finalized yet (latest=105)")
	}
}

func TestReorgDetector_IsFinalized_EnoughConfirmations(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(115, "0xccc", "0xbbb"), // latest = 115
			100: header100,
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	finalized, err := rd.IsFinalized(context.Background(), 100, header100.Hash(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !finalized {
		t.Error("block 100 with 10 confirmations and latest=115 should be finalized")
	}
}

func TestReorgDetector_IsFinalized_ReorgAfterConfirmations(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(115, "0xccc", "0xbbb"), // latest = 115
			100: makeHeader(100, "0xbbb", "0x998"), // different hash than original
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	originalHash := common.HexToHash("0xaaa")
	finalized, err := rd.IsFinalized(context.Background(), 100, originalHash, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finalized {
		t.Error("block 100 with different hash should not be considered finalized")
	}
}

func TestReorgDetector_RecordHeader(t *testing.T) {
	rd := NewReorgDetector(nil, zap.NewNop())

	header := BlockHeader{
		Number:     100,
		Hash:       common.HexToHash("0xaaa"),
		ParentHash: common.HexToHash("0x999"),
		Timestamp:  1234567890,
	}

	rd.RecordHeader(header)

	rd.mu.RLock()
	stored, ok := rd.headers[100]
	rd.mu.RUnlock()

	if !ok {
		t.Fatal("header not stored")
	}
	if stored.Hash != header.Hash {
		t.Errorf("stored hash mismatch: got %s, want %s", stored.Hash.Hex(), header.Hash.Hex())
	}
}

func TestReorgDetector_RecordHeader_Eviction(t *testing.T) {
	rd := NewReorgDetector(nil, zap.NewNop())
	rd.maxBlocks = 3

	for i := uint64(1); i <= 5; i++ {
		rd.RecordHeader(BlockHeader{
			Number: i,
			Hash:   common.BigToHash(big.NewInt(int64(i))),
		})
	}

	rd.mu.RLock()
	count := len(rd.headers)
	_, hasBlock1 := rd.headers[1]
	_, hasBlock5 := rd.headers[5]
	rd.mu.RUnlock()

	if count > 3 {
		t.Errorf("expected at most 3 headers, got %d", count)
	}
	if hasBlock1 {
		t.Error("block 1 should have been evicted")
	}
	if !hasBlock5 {
		t.Error("block 5 should still be stored")
	}
}

func TestReorgDetector_MarkReorgedEvents(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")

	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			100: header100,
			101: {Number: big.NewInt(101), Extra: common.Hex2Bytes("ccc")}, // different hash
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	events := []*IndexedEvent{
		{
			ID:          "event-100",
			BlockNumber: 100,
			BlockHash:   header100.Hash().Hex(),
		},
		{
			ID:          "event-101",
			BlockNumber: 101,
			BlockHash:   common.HexToHash("0xbbb").Hex(), // original hash differs from mock
		},
		{
			ID:          "event-102",
			BlockNumber: 102,
			BlockHash:   "", // no block hash
		},
	}

	reorgedIDs := rd.MarkReorgedEvents(context.Background(), events)

	// event-100 should NOT be reorged (hashes match)
	// event-101 SHOULD be reorged (hashes differ)
	// event-102 should be skipped (no block hash)
	found := false
	for _, id := range reorgedIDs {
		if id == "event-101" {
			found = true
		}
	}
	if !found {
		t.Error("event-101 should be marked as reorged")
	}

	for _, id := range reorgedIDs {
		if id == "event-100" {
			t.Error("event-100 should NOT be marked as reorged")
		}
	}
}
