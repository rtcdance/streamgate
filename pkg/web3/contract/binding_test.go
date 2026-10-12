package contract

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewContentRegistryBinding(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil,
		nil,
		zap.NewNop(),
	)
	assert.NotNil(t, binding)
	assert.Equal(t, common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"), binding.address)
}

func TestContentRegistryBinding_ContentRegisteredTopic(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	topic := binding.ContentRegisteredTopic()
	assert.NotEqual(t, common.Hash{}, topic)
}

func TestContentRegistryBinding_RegisterContent_NilWriter(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	_, err := binding.RegisterContent(context.Background(), "content-1", "0x70997970C51812dc3A010C7d01b50e0d17dc79C8", [32]byte{}, "ipfs://meta/1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "writer not configured")
}

func TestContentRegistryBinding_VerifyContent_NilReader(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	assert.Panics(t, func() {
		_, _ = binding.ContentExists(context.Background(), "content-1")
	})
}

func TestContentRegistryBinding_GetContentInfo_NilReader(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	assert.Panics(t, func() {
		_, _ = binding.GetContent(context.Background(), "content-1")
	})
}

func TestEventTopics_NonEmpty(t *testing.T) {
	assert.NotEqual(t, common.Hash{}, contentRegisteredTopic)
	assert.NotEqual(t, common.Hash{}, contentStatusTopic)
	assert.NotEqual(t, common.Hash{}, transferTopic)
	assert.NotEqual(t, common.Hash{}, approvalTopic)
}
