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

	var hash [32]byte
	_, err := binding.RegisterContent(context.Background(), hash, "metadata")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "writer not configured")
}

func TestContentRegistryBinding_VerifyContent_NilReader(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	var hash [32]byte
	assert.Panics(t, func() {
		_, _ = binding.VerifyContent(context.Background(), hash)
	})
}

func TestContentRegistryBinding_GetContentInfo_NilReader(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	var hash [32]byte
	assert.Panics(t, func() {
		_, _ = binding.GetContentInfo(context.Background(), hash)
	})
}

func TestEventTopics_NonEmpty(t *testing.T) {
	assert.NotEqual(t, common.Hash{}, contentRegisteredTopic)
	assert.NotEqual(t, common.Hash{}, contentVerifiedTopic)
	assert.NotEqual(t, common.Hash{}, contentDeletedTopic)
	assert.NotEqual(t, common.Hash{}, transferTopic)
	assert.NotEqual(t, common.Hash{}, approvalTopic)
}
