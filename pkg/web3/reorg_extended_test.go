package web3

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rtcdance/streamgate/pkg/web3/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestReorgDetector_CheckReorg_TableDriven(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")
	header100Reorged := makeHeader(100, "0xbbb", "0x999")

	tests := []struct {
		name        string
		headers     map[int64]*types.Header
		blockNum    uint64
		origHash    common.Hash
		expectReorg bool
		expectErr   bool
	}{
		{
			name:        "no reorg - matching hash",
			headers:     map[int64]*types.Header{100: header100},
			blockNum:    100,
			origHash:    header100.Hash(),
			expectReorg: false,
			expectErr:   false,
		},
		{
			name:        "reorg - different hash",
			headers:     map[int64]*types.Header{100: header100Reorged},
			blockNum:    100,
			origHash:    common.HexToHash("0xaaa"),
			expectReorg: true,
			expectErr:   false,
		},
		{
			name:        "rpc error - header not found",
			headers:     map[int64]*types.Header{},
			blockNum:    100,
			origHash:    common.HexToHash("0xaaa"),
			expectReorg: false,
			expectErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockHeaderReader{headers: tc.headers}
			rd := NewReorgDetector(mock, zap.NewNop())

			reorged, err := rd.CheckReorg(context.Background(), tc.blockNum, tc.origHash)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectReorg, reorged)
			}
		})
	}
}

func TestReorgDetector_IsFinalized_TableDriven(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")
	header100Reorged := makeHeader(100, "0xbbb", "0x998")

	tests := []struct {
		name            string
		headers         map[int64]*types.Header
		blockNum        uint64
		origHash        common.Hash
		confirmations   int
		expectFinalized bool
		expectErr       bool
	}{
		{
			name:            "not enough confirmations",
			headers:         map[int64]*types.Header{-1: makeHeader(105, "0xccc", "0xbbb"), 100: header100},
			blockNum:        100,
			origHash:        header100.Hash(),
			confirmations:   10,
			expectFinalized: false,
			expectErr:       false,
		},
		{
			name:            "enough confirmations and matching hash",
			headers:         map[int64]*types.Header{-1: makeHeader(115, "0xccc", "0xbbb"), 100: header100},
			blockNum:        100,
			origHash:        header100.Hash(),
			confirmations:   10,
			expectFinalized: true,
			expectErr:       false,
		},
		{
			name:            "enough confirmations but reorged",
			headers:         map[int64]*types.Header{-1: makeHeader(115, "0xccc", "0xbbb"), 100: header100Reorged},
			blockNum:        100,
			origHash:        common.HexToHash("0xaaa"),
			confirmations:   10,
			expectFinalized: false,
			expectErr:       false,
		},
		{
			name:            "rpc error getting latest",
			headers:         map[int64]*types.Header{100: header100},
			blockNum:        100,
			origHash:        header100.Hash(),
			confirmations:   10,
			expectFinalized: false,
			expectErr:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockHeaderReader{headers: tc.headers}
			rd := NewReorgDetector(mock, zap.NewNop())

			finalized, err := rd.IsFinalized(context.Background(), tc.blockNum, tc.origHash, tc.confirmations)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectFinalized, finalized)
			}
		})
	}
}

func TestReorgDetector_WaitForFinality_AlreadyFinalized(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(115, "0xccc", "0xbbb"),
			100: header100,
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := rd.WaitForFinality(ctx, 100, header100.Hash(), 10)
	assert.NoError(t, err)
}

func TestReorgDetector_WaitForFinality_ReorgDetected(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(115, "0xccc", "0xbbb"),
			100: makeHeader(100, "0xbbb", "0x998"),
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := rd.WaitForFinality(ctx, 100, common.HexToHash("0xaaa"), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reorg")
}

func TestReorgDetector_WaitForFinality_ContextCancelled(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(105, "0xccc", "0xbbb"),
			100: header100,
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rd.WaitForFinality(ctx, 100, header100.Hash(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}

func TestReorgDetector_SetReorgCallback(t *testing.T) {
	rd := NewReorgDetector(nil, zap.NewNop())

	rd.SetReorgCallback(func(addresses []string) {})

	rd.mu.RLock()
	cb := rd.reorgCallback
	rd.mu.RUnlock()
	assert.NotNil(t, cb)
}

func TestReorgDetector_MarkReorgedEvents_WithCallback(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")

	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			100: header100,
			101: makeHeader(101, "0xbbb_reorged", "0xaaa"),
		},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	var callbackAddresses []string
	rd.SetReorgCallback(func(addresses []string) {
		callbackAddresses = addresses
	})

	events := []*event.IndexedEvent{
		{
			ID:              "event-101",
			BlockNumber:     101,
			BlockHash:       common.HexToHash("0xbbb").Hex(),
			ContractAddress: "0x1234567890123456789012345678901234567890",
		},
	}

	reorgedIDs := rd.MarkReorgedEvents(context.Background(), events)
	assert.Contains(t, reorgedIDs, "event-101")
	assert.NotEmpty(t, callbackAddresses)
	assert.Contains(t, callbackAddresses, "0x1234567890123456789012345678901234567890")
}

func TestReorgDetector_MarkReorgedEvents_RPCError(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{},
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	events := []*event.IndexedEvent{
		{
			ID:          "event-100",
			BlockNumber: 100,
			BlockHash:   "0xaaa",
		},
	}

	reorgedIDs := rd.MarkReorgedEvents(context.Background(), events)
	assert.Empty(t, reorgedIDs)
}

func TestReorgDetector_RecordHeader_Update(t *testing.T) {
	rd := NewReorgDetector(nil, zap.NewNop())

	header1 := BlockHeader{
		Number:     100,
		Hash:       common.HexToHash("0xaaa"),
		ParentHash: common.HexToHash("0x999"),
	}
	header2 := BlockHeader{
		Number:     100,
		Hash:       common.HexToHash("0xbbb"),
		ParentHash: common.HexToHash("0x999"),
	}

	rd.RecordHeader(header1)
	rd.RecordHeader(header2)

	rd.mu.RLock()
	stored := rd.headers[100]
	count := len(rd.blockOrder)
	rd.mu.RUnlock()

	assert.Equal(t, header2.Hash, stored.Hash)
	assert.Equal(t, 1, count)
}

func TestFinalityDefault_IsFinalized(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(115, "0xccc", "0xbbb"),
			100: header100,
		},
	}

	f := newFinalityDefault(mock, 10, BlockTagSafe, zap.NewNop())

	finalized, err := f.IsFinalized(context.Background(), 100, header100.Hash())
	require.NoError(t, err)
	assert.True(t, finalized)
}

func TestFinalityDefault_IsFinalized_NotEnoughConfirmations(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(105, "0xccc", "0xbbb"),
			100: makeHeader(100, "0xaaa", "0x999"),
		},
	}

	f := newFinalityDefault(mock, 10, BlockTagSafe, zap.NewNop())

	finalized, err := f.IsFinalized(context.Background(), 100, common.HexToHash("0xaaa"))
	require.NoError(t, err)
	assert.False(t, finalized)
}

func TestFinalityDefault_IsFinalized_RPCError(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{},
	}

	f := newFinalityDefault(mock, 10, BlockTagSafe, zap.NewNop())

	_, err := f.IsFinalized(context.Background(), 100, common.HexToHash("0xaaa"))
	require.Error(t, err)
}

func TestFinalityDefault_IsFinalized_HashMismatch(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(115, "0xccc", "0xbbb"),
			100: makeHeader(100, "0xbbb", "0x998"),
		},
	}

	f := newFinalityDefault(mock, 10, BlockTagSafe, zap.NewNop())

	finalized, err := f.IsFinalized(context.Background(), 100, common.HexToHash("0xaaa"))
	require.NoError(t, err)
	assert.False(t, finalized)
}

func TestSolanaFinality_IsFinalized(t *testing.T) {
	tests := []struct {
		name        string
		slot        uint64
		blockNum    uint64
		expectFinal bool
		expectErr   bool
	}{
		{"finalized", 200, 100, true, false},
		{"not finalized", 120, 100, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSolClient := &mockSolanaRPCClient{slot: tc.slot}
			f := SolanaFinality(mockSolClient)

			finalized, err := f.IsFinalized(context.Background(), tc.blockNum, common.Hash{})
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectFinal, finalized)
			}
		})
	}
}

func TestSolanaFinality_NoClient(t *testing.T) {
	f := SolanaFinality(nil)
	_, err := f.IsFinalized(context.Background(), 100, common.Hash{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no RPC client")
}

func TestSolanaFinality_Properties(t *testing.T) {
	f := SolanaFinality(nil)
	assert.Equal(t, uint64(32), f.RequiredConfirmations())
	assert.Equal(t, BlockTag("finalized"), f.BlockTag())
}

type mockSolanaRPCClient struct {
	slot uint64
	err  error
}

func (m *mockSolanaRPCClient) GetSlot(_ context.Context, _ string) (uint64, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.slot, nil
}

func TestSolanaFinality_RPCError(t *testing.T) {
	mockSolClient := &mockSolanaRPCClient{err: errors.New("rpc error")}
	f := SolanaFinality(mockSolClient)

	_, err := f.IsFinalized(context.Background(), 100, common.Hash{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get finalized slot")
}

func TestL1OutputRootFinality_IsFinalized(t *testing.T) {
	header100 := makeHeader(100, "0xaaa", "0x999")
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(200, "0xddd", "0xccc"),
			100: header100,
		},
	}

	f := NewL1OutputRootFinality(mock, nil, common.Address{}, 42161, zap.NewNop())
	assert.Equal(t, uint64(64), f.RequiredConfirmations())
	assert.Equal(t, BlockTagFinalized, f.BlockTag())
}

func TestL1OutputRootFinality_IsFinalized_NotEnough(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{
			-1:  makeHeader(105, "0xccc", "0xbbb"),
			100: makeHeader(100, "0xaaa", "0x999"),
		},
	}

	f := NewL1OutputRootFinality(mock, nil, common.Address{}, 42161, zap.NewNop())
	finalized, err := f.IsFinalized(context.Background(), 100, common.HexToHash("0xaaa"))
	require.NoError(t, err)
	assert.False(t, finalized)
}

func TestL1OutputRootFinality_IsFinalized_RPCError(t *testing.T) {
	mock := &mockHeaderReader{
		headers: map[int64]*types.Header{},
	}

	f := NewL1OutputRootFinality(mock, nil, common.Address{}, 42161, zap.NewNop())
	_, err := f.IsFinalized(context.Background(), 100, common.HexToHash("0xaaa"))
	require.Error(t, err)
}

func TestEthereumL1Finality(t *testing.T) {
	f := EthereumL1Finality(nil, zap.NewNop())
	assert.Equal(t, uint64(12), f.RequiredConfirmations())
	assert.Equal(t, BlockTagSafe, f.BlockTag())
}

func TestPolygonFinality(t *testing.T) {
	f := PolygonFinality(nil, zap.NewNop())
	assert.Equal(t, uint64(128), f.RequiredConfirmations())
	assert.Equal(t, BlockTagSafe, f.BlockTag())
}

func TestBSCFinality(t *testing.T) {
	f := BSCFinality(nil, zap.NewNop())
	assert.Equal(t, uint64(15), f.RequiredConfirmations())
	assert.Equal(t, BlockTagSafe, f.BlockTag())
}

func TestL2Finality(t *testing.T) {
	f := L2Finality(nil, zap.NewNop())
	assert.Equal(t, uint64(64), f.RequiredConfirmations())
	assert.Equal(t, BlockTagFinalized, f.BlockTag())
}

func TestReorgDetector_SubscribeNewHead(t *testing.T) {
	mock := &mockHeaderReader{
		subErr: errors.New("subscription not supported"),
	}
	rd := NewReorgDetector(mock, zap.NewNop())

	ch := make(chan *types.Header)
	_, err := rd.client.SubscribeNewHead(context.Background(), ch)
	require.Error(t, err)
}
