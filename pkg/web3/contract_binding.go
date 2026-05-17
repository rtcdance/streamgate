package web3

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ContentRegistryBinding provides type-safe Go methods for the ContentRegistry
// smart contract.  All read calls go through ContractInteractor; all write
// calls go through ContractWriter.
type ContentRegistryBinding struct {
	address common.Address
	abi     abi.ABI
	reader  *ContractInteractor
	writer  *ContractWriter
	logger  *zap.Logger

	// Pre-computed event topic hashes for efficient log filtering.
	contentRegisteredTopic common.Hash
}

var (
	contentRegistryABIParseOnce sync.Once
	contentRegistryABICached    abi.ABI
)

// parseContentRegistryABI parses the ContentRegistry ABI once and caches it.
func parseContentRegistryABI() abi.ABI {
	contentRegistryABIParseOnce.Do(func() {
		parsed, err := abi.JSON(bytes.NewReader([]byte(ContentRegistryABI)))
		if err != nil {
			panic(fmt.Sprintf("ContentRegistry ABI is invalid: %v", err))
		}
		contentRegistryABICached = parsed
	})
	return contentRegistryABICached
}

// NewContentRegistryBinding creates a type-safe binding for the ContentRegistry
// contract at the given address.  writer may be nil if only read operations
// are needed.
func NewContentRegistryBinding(address string, reader *ContractInteractor, writer *ContractWriter, logger *zap.Logger) *ContentRegistryBinding {
	parsedABI := parseContentRegistryABI()

	// Pre-compute the ContentRegistered event topic
	contentRegisteredEvent, ok := parsedABI.Events["ContentRegistered"]
	var topic common.Hash
	if ok {
		topic = contentRegisteredEvent.ID
	}

	return &ContentRegistryBinding{
		address:                common.HexToAddress(address),
		abi:                    parsedABI,
		reader:                 reader,
		writer:                 writer,
		logger:                 logger,
		contentRegisteredTopic: topic,
	}
}

// RegisterContent sends a registerContent(bytes32,string) transaction on-chain.
// It returns the tx hash and any error.  The caller should wait for receipt
// confirmation separately.
func (b *ContentRegistryBinding) RegisterContent(ctx context.Context, contentHash [32]byte, metadata string) (string, error) {
	if b.writer == nil {
		return "", fmt.Errorf("content_registry_binding: writer not configured")
	}

	result, err := b.writer.SendTx(ctx, ContractTxOpts{
		To:        b.address.Hex(),
		Method:    "registerContent",
		ParsedABI: &b.abi,
		Args:      []interface{}{contentHash, metadata},
	})
	if err != nil {
		return "", fmt.Errorf("content_registry_binding: RegisterContent: %w", err)
	}

	b.logger.Info("RegisterContent tx sent",
		zap.String("content_hash", fmt.Sprintf("%x", contentHash)),
		zap.String("tx_hash", result.TxHash))

	return result.TxHash, nil
}

// VerifyContent calls verifyContent(bytes32) as a read-only call and returns
// whether the content hash is registered.
func (b *ContentRegistryBinding) VerifyContent(ctx context.Context, contentHash [32]byte) (bool, error) {
	result, err := b.reader.CallContractFunction(ctx, b.address.Hex(), ContentRegistryABI, "verifyContent", "", contentHash)
	if err != nil {
		return false, fmt.Errorf("content_registry_binding: verifyContent call: %w", err)
	}

	data, ok := result.([]byte)
	if !ok {
		return false, fmt.Errorf("content_registry_binding: unexpected result type from verifyContent")
	}

	out, err := b.abi.Unpack("verifyContent", data)
	if err != nil {
		return false, fmt.Errorf("content_registry_binding: unpack verifyContent: %w", err)
	}

	if len(out) > 0 {
		if valid, ok := out[0].(bool); ok {
			return valid, nil
		}
	}
	return false, nil
}

// GetContentInfo calls getContentInfo(bytes32) and returns the on-chain record.
func (b *ContentRegistryBinding) GetContentInfo(ctx context.Context, contentHash [32]byte) (*ContentInfo, error) {
	result, err := b.reader.CallContractFunction(ctx, b.address.Hex(), ContentRegistryABI, "getContentInfo", "", contentHash)
	if err != nil {
		return nil, fmt.Errorf("content_registry_binding: getContentInfo call: %w", err)
	}

	data, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("content_registry_binding: unexpected result type from getContentInfo")
	}

	type getContentInfoResult struct {
		Owner     common.Address
		Timestamp *big.Int
		Metadata  string
	}
	var out getContentInfoResult
	if err := b.abi.UnpackIntoInterface(&out, "getContentInfo", data); err != nil {
		return nil, fmt.Errorf("content_registry_binding: unpack getContentInfo: %w", err)
	}

	return &ContentInfo{
		Hash:      fmt.Sprintf("%x", contentHash),
		Owner:     out.Owner.Hex(),
		Timestamp: out.Timestamp.Int64(),
		Metadata:  out.Metadata,
		IsValid:   out.Owner != common.Address{},
	}, nil
}

// ContentRegisteredTopic returns the keccak256 topic for the ContentRegistered
// event, suitable for log filtering.
func (b *ContentRegistryBinding) ContentRegisteredTopic() common.Hash {
	return b.contentRegisteredTopic
}
