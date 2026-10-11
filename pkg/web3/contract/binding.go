package contract

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/web3/tx"
	"go.uber.org/zap"
)

type ContentRegistryBinding struct {
	address common.Address
	abi     abi.ABI
	reader  *ContractInteractor
	writer  *tx.ContractWriter
	logger  *zap.Logger

	contentRegisteredTopic common.Hash
}

var (
	contentRegistryABIParseOnce sync.Once
	contentRegistryABICached    abi.ABI
)

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

func NewContentRegistryBinding(address string, reader *ContractInteractor, writer *tx.ContractWriter, logger *zap.Logger) *ContentRegistryBinding {
	parsedABI := parseContentRegistryABI()

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

// RegisterContent registers a content's existence on-chain. Signatures mirror
// contracts/src/ContentRegistry.sol: registerContent(string contentId,
// string contentHash, string metadataURI) — the tx sender must be the
// contract owner (onlyOwner).
func (b *ContentRegistryBinding) RegisterContent(ctx context.Context, contentID, contentHash, metadataURI string) (string, error) {
	if b.writer == nil {
		return "", fmt.Errorf("content_registry_binding: writer not configured")
	}

	result, err := b.writer.SendTx(ctx, tx.ContractTxOpts{
		To:        b.address.Hex(),
		Method:    "registerContent",
		ParsedABI: &b.abi,
		Args:      []interface{}{contentID, contentHash, metadataURI},
	})
	if err != nil {
		return "", fmt.Errorf("content_registry_binding: RegisterContent: %w", err)
	}

	b.logger.Info("RegisterContent tx sent",
		zap.String("content_id", contentID),
		zap.String("tx_hash", result.TxHash))

	return result.TxHash, nil
}

// ContentExists checks whether a contentId is registered (status != Nonexistent).
func (b *ContentRegistryBinding) ContentExists(ctx context.Context, contentID string) (bool, error) {
	result, err := b.reader.CallContractFunction(ctx, b.address.Hex(), ContentRegistryABI, "contentExists", "", contentID)
	if err != nil {
		return false, fmt.Errorf("content_registry_binding: contentExists call: %w", err)
	}

	data, ok := result.([]byte)
	if !ok {
		return false, fmt.Errorf("content_registry_binding: unexpected result type from contentExists")
	}

	out, err := b.abi.Unpack("contentExists", data)
	if err != nil {
		return false, fmt.Errorf("content_registry_binding: unpack contentExists: %w", err)
	}

	if len(out) > 0 {
		if exists, ok := out[0].(bool); ok {
			return exists, nil
		}
	}
	return false, nil
}

// contentRecordTuple mirrors getContent's tuple output exactly (field order
// and types); abi.ConvertType needs this shape for reflection-based mapping.
type contentRecordTuple struct {
	ContentID   string
	Creator     common.Address
	ContentHash string
	MetadataURI string
	Status      uint8
	CreatedAt   *big.Int
	UpdatedAt   *big.Int
}

// ContentRecord mirrors the ContentRecord struct returned by getContent.
type ContentRecord struct {
	ContentID   string
	Creator     string
	ContentHash string
	MetadataURI string
	Status      uint8
	CreatedAt   int64
	UpdatedAt   int64
}

// GetContent reads the on-chain record for a contentId.
func (b *ContentRegistryBinding) GetContent(ctx context.Context, contentID string) (*ContentRecord, error) {
	result, err := b.reader.CallContractFunction(ctx, b.address.Hex(), ContentRegistryABI, "getContent", "", contentID)
	if err != nil {
		return nil, fmt.Errorf("content_registry_binding: getContent call: %w", err)
	}

	data, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("content_registry_binding: unexpected result type from getContent")
	}

	raw, err := b.abi.Unpack("getContent", data)
	if err != nil {
		return nil, fmt.Errorf("content_registry_binding: unpack getContent: %w", err)
	}
	if len(raw) != 1 {
		return nil, fmt.Errorf("content_registry_binding: getContent returned %d values", len(raw))
	}
	out := abi.ConvertType(raw[0], new(contentRecordTuple)).(*contentRecordTuple)

	return &ContentRecord{
		ContentID:   out.ContentID,
		Creator:     out.Creator.Hex(),
		ContentHash: out.ContentHash,
		MetadataURI: out.MetadataURI,
		Status:      out.Status,
		CreatedAt:   out.CreatedAt.Int64(),
		UpdatedAt:   out.UpdatedAt.Int64(),
	}, nil
}

func (b *ContentRegistryBinding) ContentRegisteredTopic() common.Hash {
	return b.contentRegisteredTopic
}
