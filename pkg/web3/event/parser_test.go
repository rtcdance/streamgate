package event

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

func TestEventParser_ParseERC20Transfer(t *testing.T) {
	logger := zap.NewNop()
	parser := NewEventParser(logger)

	transferSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	from := common.HexToAddress("0x0000000000000000000000000000000000000001")
	to := common.HexToAddress("0x0000000000000000000000000000000000000002")

	log := &types.Log{
		Address: common.HexToAddress("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"),
		Topics:  []common.Hash{transferSig, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
		Data:    common.HexToHash("0x0000000000000000000000000000000000000000000000000de0b6b3a7640000").Bytes(),
	}

	events := parser.ParseLogs([]*types.Log{log})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Name != "Transfer" {
		t.Errorf("expected event name Transfer, got %s", events[0].Name)
	}
	if events[0].Address != "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" {
		t.Errorf("unexpected address: %s", events[0].Address)
	}
	if _, ok := events[0].Args["value"]; !ok {
		t.Error("Transfer event should have 'value' arg")
	}
	if _, ok := events[0].Args["from"]; !ok {
		t.Error("Transfer event should have 'from' arg")
	}
	if _, ok := events[0].Args["to"]; !ok {
		t.Error("Transfer event should have 'to' arg")
	}
}

func TestEventParser_ParseERC721Transfer(t *testing.T) {
	logger := zap.NewNop()
	parser := NewEventParser(logger)

	transferSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	from := common.HexToAddress("0x0000000000000000000000000000000000000001")
	to := common.HexToAddress("0x0000000000000000000000000000000000000002")
	tokenID := big.NewInt(42)

	log := &types.Log{
		Address: common.HexToAddress("0xBC4CA029A1B828F6E3E52E1241d7DC7BeE6c4e7F"),
		Topics: []common.Hash{
			transferSig,
			common.BytesToHash(from.Bytes()),
			common.BytesToHash(to.Bytes()),
			common.BytesToHash(common.LeftPadBytes(tokenID.Bytes(), 32)),
		},
		Data: []byte{},
	}

	events := parser.ParseLogs([]*types.Log{log})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Name != "Transfer" {
		t.Errorf("expected Transfer, got %s", events[0].Name)
	}
}

func TestEventParser_ParseApproval(t *testing.T) {
	logger := zap.NewNop()
	parser := NewEventParser(logger)

	approvalSig := common.HexToHash("0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925")
	owner := common.HexToAddress("0x0000000000000000000000000000000000000001")
	spender := common.HexToAddress("0x0000000000000000000000000000000000000002")

	log := &types.Log{
		Address: common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
		Topics:  []common.Hash{approvalSig, common.BytesToHash(owner.Bytes()), common.BytesToHash(spender.Bytes())},
		Data:    common.HexToHash("0x0000000000000000000000000000000000000000000000000de0b6b3a7640000").Bytes(),
	}

	events := parser.ParseLogs([]*types.Log{log})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Name != "Approval" {
		t.Errorf("expected Approval, got %s", events[0].Name)
	}
}

func TestEventParser_UnknownEvent(t *testing.T) {
	logger := zap.NewNop()
	parser := NewEventParser(logger)

	unknownSig := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	log := &types.Log{
		Address: common.HexToAddress("0x0000000000000000000000000000000000000001"),
		Topics:  []common.Hash{unknownSig},
		Data:    []byte{0x01, 0x02},
	}

	events := parser.ParseLogs([]*types.Log{log})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Name != "Unknown" {
		t.Errorf("expected Unknown, got %s", events[0].Name)
	}
}

func TestEventParser_EmptyLogs(t *testing.T) {
	logger := zap.NewNop()
	parser := NewEventParser(logger)

	events := parser.ParseLogs(nil)
	if events != nil {
		t.Errorf("expected nil for nil logs, got %v", events)
	}

	events = parser.ParseLogs([]*types.Log{})
	if events != nil {
		t.Errorf("expected nil for empty logs, got %v", events)
	}
}

// TestEventParser_ERC1155TransferBatch pins the decode the ERC-1155 batch
// invalidation depends on: ids and values are NOT indexed, so they only ever
// reach the handler through the ABI-decoded data blob. The parser formats them
// with %v, which is the exact "[1 2]" shape the handler parses.
func TestEventParser_ERC1155TransferBatch(t *testing.T) {
	logger := zap.NewNop()
	parser := NewEventParser(logger)

	batchSig := common.HexToHash("0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb")
	log := &types.Log{
		Address: common.HexToAddress("0x0000000000000000000000000000000000000004"),
		Topics: []common.Hash{
			batchSig,
			common.BytesToHash(common.HexToAddress("0x0000000000000000000000000000000000000001").Bytes()),
			common.BytesToHash(common.HexToAddress("0x0000000000000000000000000000000000000002").Bytes()),
			common.BytesToHash(common.HexToAddress("0x0000000000000000000000000000000000000003").Bytes()),
		},
		Data: erc1155BatchData(
			[]int64{1, 2},
			[]int64{1, 3},
		),
	}

	events := parser.ParseLogs([]*types.Log{log})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Name != "TransferBatch" {
		t.Fatalf("expected TransferBatch, got %s", events[0].Name)
	}
	ids, ok := events[0].Args["ids"].(string)
	if !ok || ids != "[1 2]" {
		t.Fatalf("expected the parser to expose ids as \"[1 2]\", got %#v", events[0].Args["ids"])
	}
	values, ok := events[0].Args["values"].(string)
	if !ok || values != "[1 3]" {
		t.Fatalf("expected the parser to expose values as \"[1 3]\", got %#v", events[0].Args["values"])
	}
}

// erc1155BatchData ABI-encodes the two non-indexed uint256[] arguments of a
// TransferBatch log (head: the two array offsets, then length + elements each).
func erc1155BatchData(ids, values []int64) []byte {
	word := func(n int64) []byte {
		b := make([]byte, 32)
		big.NewInt(n).FillBytes(b)
		return b
	}
	out := word(64)
	out = append(out, word(64+32*int64(1+len(ids)))...)
	out = append(out, word(int64(len(ids)))...)
	for _, id := range ids {
		out = append(out, word(id)...)
	}
	out = append(out, word(int64(len(values)))...)
	for _, v := range values {
		out = append(out, word(v)...)
	}
	return out
}

func TestEventParser_ERC1155TransferSingle(t *testing.T) {
	logger := zap.NewNop()
	parser := NewEventParser(logger)
	//nolint:gocritic
	transferSig := common.HexToHash("0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62")
	operator := common.HexToAddress("0x0000000000000000000000000000000000000001")
	from := common.HexToAddress("0x0000000000000000000000000000000000000002")
	to := common.HexToAddress("0x0000000000000000000000000000000000000003")

	log := &types.Log{
		Address: common.HexToAddress("0x0000000000000000000000000000000000000004"),
		Topics: []common.Hash{
			transferSig,
			common.BytesToHash(operator.Bytes()),
			common.BytesToHash(from.Bytes()),
			common.BytesToHash(to.Bytes()),
		},
		Data: []byte{},
	}

	events := parser.ParseLogs([]*types.Log{log})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Name != "TransferSingle" {
		t.Errorf("expected TransferSingle, got %s", events[0].Name)
	}
}
