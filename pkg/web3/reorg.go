package web3

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// FinalityStrategy defines chain-specific finality logic.
// Different chains have different confirmation depths and finality mechanisms:
// Ethereum L1: 12 blocks (post-merge), Polygon: ~128 blocks, L2s: rely on L1 state.
type FinalityStrategy interface {
	// RequiredConfirmations returns how many descendant blocks are needed
	// before a block is considered final. 0 means "use block tag instead."
	RequiredConfirmations() uint64

	// BlockTag returns the recommended BlockTag for read operations.
	// Empty string means no specific tag (use latest).
	BlockTag() BlockTag

	// IsFinalized checks whether a specific block is finalized.
	// The default implementation uses confirmation counting + hash check;
	// L2 strategies may delegate to L1 contract state.
	IsFinalized(ctx context.Context, blockNumber uint64, blockHash common.Hash) (bool, error)
}

// finalityBase provides the common confirmation-counting logic.
type finalityBase struct {
	confirmations uint64
	blockTag      BlockTag
}

func (f *finalityBase) RequiredConfirmations() uint64 { return f.confirmations }
func (f *finalityBase) BlockTag() BlockTag            { return f.blockTag }

// finalityDefault uses HeaderReader for confirmation counting.
type finalityDefault struct {
	finalityBase
	reader HeaderReader
	logger *zap.Logger
}

func newFinalityDefault(reader HeaderReader, confirmations uint64, blockTag BlockTag, logger *zap.Logger) *finalityDefault {
	return &finalityDefault{
		finalityBase: finalityBase{confirmations: confirmations, blockTag: blockTag},
		reader:       reader,
		logger:       logger,
	}
}

func (f *finalityDefault) IsFinalized(ctx context.Context, blockNumber uint64, blockHash common.Hash) (bool, error) {
	latest, err := f.reader.HeaderByNumber(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("finality check: cannot get latest header: %w", err)
	}
	if latest.Number.Uint64() < blockNumber+f.confirmations {
		return false, nil
	}
	current, err := f.reader.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return false, fmt.Errorf("finality check: cannot get header at %d: %w", blockNumber, err)
	}
	return current.Hash() == blockHash, nil
}

// Pre-built finality strategies for common chains.
var (
	// EthereumL1Finality: 12 confirmations, use BlockTagSafe.
	EthereumL1Finality = func(reader HeaderReader, logger *zap.Logger) FinalityStrategy {
		return newFinalityDefault(reader, 12, BlockTagSafe, logger)
	}

	// PolygonFinality: Polygon can reorg deeper than Ethereum, up to ~128 blocks.
	PolygonFinality = func(reader HeaderReader, logger *zap.Logger) FinalityStrategy {
		return newFinalityDefault(reader, 128, BlockTagSafe, logger)
	}

	// BSCFinality: BSC has fast finality (~15 blocks for safe).
	BSCFinality = func(reader HeaderReader, logger *zap.Logger) FinalityStrategy {
		return newFinalityDefault(reader, 15, BlockTagSafe, logger)
	}

	// L2Finality: L2s rely on L1 finality. Use BlockTagFinalized and the
	// L1 contract's output root for definitive finality. The default
	// confirmation-based check is a conservative approximation.
	L2Finality = func(reader HeaderReader, logger *zap.Logger) FinalityStrategy {
		return newFinalityDefault(reader, 64, BlockTagFinalized, logger)
	}

	// SolanaFinality: uses "finalized" bank state (~32 slots after block).
	SolanaFinality = func() FinalityStrategy {
		return &solanaFinality{}
	}
)

type solanaFinality struct{}

func (s *solanaFinality) RequiredConfirmations() uint64 { return 32 }
func (s *solanaFinality) BlockTag() BlockTag            { return "finalized" }
func (s *solanaFinality) IsFinalized(ctx context.Context, blockNumber uint64, blockHash common.Hash) (bool, error) {
	// Solana finality is determined by bank state, not block hash verification.
	// The SDK's GetFinalizedBlockHeight() call is the authoritative check.
	return true, nil
}

// BlockTag specifies which block the client should read state from.
// Not all RPC providers support safe/finalized tags.
type BlockTag string

const (
	BlockTagLatest    BlockTag = "latest"
	BlockTagSafe      BlockTag = "safe"      // post-merge, after 4 epochs
	BlockTagFinalized BlockTag = "finalized" // post-merge, after 2 epochs
)

// BlockHeader is a lightweight block header used for reorg detection.
type BlockHeader struct {
	Number     uint64
	Hash       common.Hash
	ParentHash common.Hash
	Timestamp  uint64
}

// ReorgDetector monitors the blockchain for chain reorganizations.
// It tracks block headers and can verify whether a previously-seen
// block hash is still part of the canonical chain.
type ReorgDetector struct {
	client    HeaderReader
	logger    *zap.Logger
	mu        sync.RWMutex
	headers   map[uint64]BlockHeader // blockNumber → header
	maxBlocks int                    // how many headers to retain
}

// HeaderReader abstracts the block header reading interface.
// *ethclient.Client satisfies this interface implicitly.
//
//go:generate mockgen -destination=mocks/mock_header_reader.go -package=mocks streamgate/pkg/web3 HeaderReader
type HeaderReader interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error)
}

// NewReorgDetector creates a new reorg detector.
func NewReorgDetector(client HeaderReader, logger *zap.Logger) *ReorgDetector {
	return &ReorgDetector{
		client:    client,
		logger:    logger,
		headers:   make(map[uint64]BlockHeader),
		maxBlocks: 256,
	}
}

// RecordHeader records a block header for future reorg checks.
func (rd *ReorgDetector) RecordHeader(header BlockHeader) {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	rd.headers[header.Number] = header

	// Evict old headers beyond retention window — bulk eviction using
	// the latest block number as the anchor point for O(n) cleanup.
	if len(rd.headers) > rd.maxBlocks {
		cutoff := header.Number - uint64(rd.maxBlocks) + 1
		for n := range rd.headers {
			if n < cutoff {
				delete(rd.headers, n)
			}
		}
	}
}

// CheckReorg verifies whether a previously-recorded block is still on the
// canonical chain. It fetches the current header at the same block number
// and compares the hash.
//
// Returns:
//   - false, nil — block is still canonical (no reorg)
//   - true, nil  — reorg detected (hash mismatch)
//   - false, error — RPC error (cannot determine; treat as inconclusive)
func (rd *ReorgDetector) CheckReorg(ctx context.Context, blockNumber uint64, originalHash common.Hash) (bool, error) {
	currentHeader, err := rd.client.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return false, fmt.Errorf("failed to fetch header for block %d: %w", blockNumber, err)
	}

	if currentHeader.Hash() != originalHash {
		rd.logger.Warn("Reorg detected",
			zap.Uint64("block_number", blockNumber),
			zap.String("original_hash", originalHash.Hex()),
			zap.String("current_hash", currentHeader.Hash().Hex()))
		return true, nil
	}

	return false, nil
}

// WaitForFinality waits until a block has enough subsequent blocks built on
// top of it to be considered final. It periodically checks that the block
// hash is still canonical.
//
// The caller should pass the block hash obtained when the event was first seen.
func (rd *ReorgDetector) WaitForFinality(ctx context.Context, blockNumber uint64, blockHash common.Hash, requiredConfirmations int) error {
	targetBlock := blockNumber + uint64(requiredConfirmations)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		// Check that our block is still canonical
		reorged, err := rd.CheckReorg(ctx, blockNumber, blockHash)
		if err != nil {
			rd.logger.Warn("Cannot verify block during finality wait, retrying",
				zap.Uint64("block_number", blockNumber),
				zap.Error(err))
		} else if reorged {
			return fmt.Errorf("block %d was reorg'd (original hash %s)", blockNumber, blockHash.Hex())
		}

		// Check if enough confirmations have passed
		latestHeader, err := rd.client.HeaderByNumber(ctx, nil)
		if err != nil {
			rd.logger.Warn("Cannot get latest header during finality wait",
				zap.Error(err))
		} else if latestHeader.Number.Uint64() >= targetBlock {
			// One final reorg check
			reorged, err := rd.CheckReorg(ctx, blockNumber, blockHash)
			if err != nil {
				return fmt.Errorf("final reorg check failed for block %d: %w", blockNumber, err)
			}
			if reorged {
				return fmt.Errorf("block %d was reorg'd after confirmations", blockNumber)
			}
			rd.logger.Debug("Block finalized",
				zap.Uint64("block_number", blockNumber),
				zap.Int("confirmations", requiredConfirmations))
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for finality of block %d: %w", blockNumber, ctx.Err())
		case <-ticker.C:
			// continue loop
		}
	}
}

// IsFinalized checks whether a block is considered finalized by verifying
// that enough subsequent blocks exist and the block hash is still canonical.
// This is a one-shot check (no polling); use WaitForFinality for active waiting.
func (rd *ReorgDetector) IsFinalized(ctx context.Context, blockNumber uint64, blockHash common.Hash, requiredConfirmations int) (bool, error) {
	latestHeader, err := rd.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to get latest header: %w", err)
	}

	if latestHeader.Number.Uint64() < blockNumber+uint64(requiredConfirmations) {
		return false, nil // not enough confirmations yet
	}

	reorged, err := rd.CheckReorg(ctx, blockNumber, blockHash)
	if err != nil {
		return false, err
	}

	return !reorged, nil
}

// MarkReorgedEvents checks all stored events against the current chain state
// and returns IDs of events whose block hash no longer matches the canonical chain.
// This is meant to be called periodically by the EventIndexer.
func (rd *ReorgDetector) MarkReorgedEvents(ctx context.Context, events []*IndexedEvent) []string {
	var reorgedIDs []string

	for _, event := range events {
		if event.BlockHash == "" {
			continue
		}

		reorged, err := rd.CheckReorg(ctx, event.BlockNumber, common.HexToHash(event.BlockHash))
		if err != nil {
			rd.logger.Debug("Cannot check reorg for event, skipping",
				zap.String("event_id", event.ID),
				zap.Error(err))
			continue
		}

		if reorged {
			reorgedIDs = append(reorgedIDs, event.ID)
			rd.logger.Warn("Event affected by reorg",
				zap.String("event_id", event.ID),
				zap.Uint64("block_number", event.BlockNumber),
				zap.String("block_hash", event.BlockHash))
		}
	}

	return reorgedIDs
}
