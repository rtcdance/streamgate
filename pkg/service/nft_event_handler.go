package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/web3/event"

	"go.uber.org/zap"
)

const defaultInvalidationBatchWindow = 500 * time.Millisecond

type NFTEventHandler struct {
	nftService      *NFTService
	middlewareCache middleware.NFTAccessCache
	defaultChainID  int64
	logger          *zap.Logger

	batchWindow          time.Duration
	pendingInvalidations sync.Map
	flushMu              sync.Mutex
	flushTimer           *time.Timer
	stopOnce             sync.Once
	stopped              chan struct{}
}

func NewNFTEventHandler(nftService *NFTService, logger *zap.Logger) *NFTEventHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &NFTEventHandler{
		nftService:  nftService,
		logger:      logger,
		batchWindow: defaultInvalidationBatchWindow,
		stopped:     make(chan struct{}),
	}
}

func NewNFTEventHandlerWithCache(nftService *NFTService, cache middleware.NFTAccessCache, chainID int64, logger *zap.Logger) *NFTEventHandler {
	h := NewNFTEventHandler(nftService, logger)
	h.middlewareCache = cache
	h.defaultChainID = chainID
	return h
}

func (h *NFTEventHandler) HandleTransfer(ctx context.Context, evt *event.IndexedEvent) error {
	contractAddress := evt.ContractAddress

	tokenID, err := h.extractTokenID(evt)
	if err != nil {
		h.logger.Warn("Failed to extract token ID from Transfer event",
			zap.String("tx_hash", evt.TransactionHash),
			zap.Error(err))
		return nil
	}

	h.logger.Debug("Transfer event detected, queued for batched invalidation",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("tx_hash", evt.TransactionHash))

	h.enqueueInvalidation(contractAddress, tokenID, evt)

	return nil
}

func (h *NFTEventHandler) HandleTransferSingle(ctx context.Context, evt *event.IndexedEvent) error {
	contractAddress := evt.ContractAddress

	tokenID := h.extractERC1155TokenID(evt)
	if tokenID == "" {
		h.logger.Warn("Failed to extract token ID from TransferSingle event",
			zap.String("tx_hash", evt.TransactionHash))
		return nil
	}

	h.logger.Debug("TransferSingle event detected, queued for batched invalidation",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("tx_hash", evt.TransactionHash))

	h.enqueueInvalidation(contractAddress, tokenID, evt)

	return nil
}

// HandleTransferBatch invalidates cached access evidence for EVERY token id
// carried by an ERC-1155 TransferBatch event (parallel ids[]/values[] arrays).
// Like the other transfer handlers it may only accelerate invalidation — it
// never grants access.
func (h *NFTEventHandler) HandleTransferBatch(ctx context.Context, evt *event.IndexedEvent) error {
	contractAddress := evt.ContractAddress

	tokenIDs := h.extractERC1155TokenIDs(evt)
	if len(tokenIDs) == 0 {
		h.logger.Warn("Failed to extract token IDs from TransferBatch event",
			zap.String("tx_hash", evt.TransactionHash))
		return nil
	}

	h.logger.Debug("TransferBatch event detected, queued for batched invalidation",
		zap.String("contract", contractAddress),
		zap.Int("token_count", len(tokenIDs)),
		zap.String("tx_hash", evt.TransactionHash))

	for _, tokenID := range tokenIDs {
		h.enqueueInvalidation(contractAddress, tokenID, evt)
	}

	return nil
}

func (h *NFTEventHandler) enqueueInvalidation(contractAddress, tokenID string, evt *event.IndexedEvent) {
	key := contractAddress + ":" + tokenID
	h.pendingInvalidations.Store(key, evt)
	h.scheduleFlush()
}

func (h *NFTEventHandler) scheduleFlush() {
	h.flushMu.Lock()
	defer h.flushMu.Unlock()
	if h.flushTimer != nil {
		return
	}
	h.flushTimer = time.AfterFunc(h.batchWindow, h.flushAll)
}

func (h *NFTEventHandler) flushAll() {
	h.flushMu.Lock()
	h.flushTimer = nil
	h.flushMu.Unlock()

	type pending struct {
		contractAddress string
		tokenID         string
		evt             *event.IndexedEvent
	}
	var entries []pending
	h.pendingInvalidations.Range(func(k, v any) bool {
		key := k.(string)
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			h.pendingInvalidations.Delete(k)
			return true
		}
		var evt *event.IndexedEvent
		if v != nil {
			evt, _ = v.(*event.IndexedEvent)
		}
		entries = append(entries, pending{
			contractAddress: parts[0],
			tokenID:         parts[1],
			evt:             evt,
		})
		h.pendingInvalidations.Delete(k)
		return true
	})

	if len(entries) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for _, e := range entries {
		h.nftService.InvalidateOwnershipCache(ctx, e.contractAddress, e.tokenID)
		if h.middlewareCache != nil {
			h.invalidateMiddlewareCache(ctx, e.contractAddress)
		}
	}

	h.logger.Debug("Batched cache invalidation flushed",
		zap.Int("count", len(entries)))
}

// FlushNow forces a synchronous flush of pending invalidations. Intended
// for shutdown paths where the batch window must not be waited out.
func (h *NFTEventHandler) FlushNow() {
	h.stopOnce.Do(func() {
		close(h.stopped)
	})
	h.flushAll()
}

// invalidateMiddlewareCache drops the gate's cached EVIDENCE for every subject
// of the event's contract. The cache key is
// "<chain>:<contract>:<tokenID>:<standard>:<wallet>" (middleware.nftCacheKey),
// so a delete scoped to the contract is a real literal prefix and covers every
// wallet, token and standard for it — including the collection-scoped entry,
// whose balance a transfer of one token in that collection also changes.
//
// The previous implementation built "<chain>:<wallet>:<contract>:<tokenID>"
// (missing the standard suffix) and prefixed with "<chain>::<contract>:<tokenID>",
// neither of which any writer ever produced: the event path therefore deleted
// nothing and could not accelerate invalidation at all.
func (h *NFTEventHandler) invalidateMiddlewareCache(ctx context.Context, contractAddress string) {
	prefix := fmt.Sprintf("%d:%s:", h.defaultChainID, strings.ToLower(contractAddress))
	h.middlewareCache.DeleteByPrefix(ctx, prefix)
}

func (h *NFTEventHandler) extractTokenID(evt *event.IndexedEvent) (string, error) {
	if evt.Decoded != nil {
		if tokenID, ok := evt.Decoded["tokenId"]; ok {
			return fmt.Sprintf("%v", tokenID), nil
		}
		if tokenID, ok := evt.Decoded["tokenID"]; ok {
			return fmt.Sprintf("%v", tokenID), nil
		}
	}

	if len(evt.Topics) >= 4 {
		return evt.Topics[3], nil
	}

	return "", fmt.Errorf("no token ID found in Transfer event (decoded=%v, topics=%d)",
		evt.Decoded != nil, len(evt.Topics))
}

func (h *NFTEventHandler) extractERC1155TokenID(evt *event.IndexedEvent) string {
	if evt.Decoded != nil {
		if id, ok := evt.Decoded["id"]; ok {
			return fmt.Sprintf("%v", id)
		}
	}
	return ""
}

// extractERC1155TokenIDs returns every token id from a decoded TransferBatch
// event. The parser formats uint256[] args via fmt %v, so ids arrive as a
// "[1 2 3]" string; decoded maps reloaded from the event store (or built by
// tests) may instead carry a slice.
func (h *NFTEventHandler) extractERC1155TokenIDs(evt *event.IndexedEvent) []string {
	if evt == nil || evt.Decoded == nil {
		return nil
	}
	raw, ok := evt.Decoded["ids"]
	if !ok {
		return nil
	}
	var parts []string
	switch ids := raw.(type) {
	case string:
		parts = strings.FieldsFunc(ids, func(r rune) bool {
			return r == '[' || r == ']' || r == ',' || r == ' ' || r == '\t'
		})
	case []string:
		parts = ids
	case []interface{}:
		for _, id := range ids {
			parts = append(parts, fmt.Sprintf("%v", id))
		}
	default:
		return nil
	}
	tokenIDs := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			tokenIDs = append(tokenIDs, p)
		}
	}
	return tokenIDs
}
