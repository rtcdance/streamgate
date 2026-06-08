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
			h.invalidateMiddlewareCache(ctx, e.contractAddress, e.tokenID, e.evt)
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

func (h *NFTEventHandler) invalidateMiddlewareCache(ctx context.Context, contractAddress, tokenID string, evt *event.IndexedEvent) {
	from, to := h.extractAddresses(evt)

	if from != "" {
		key := fmt.Sprintf("%d:%s:%s:%s", h.defaultChainID, from, contractAddress, tokenID)
		h.middlewareCache.Delete(ctx, key)
	}
	if to != "" {
		key := fmt.Sprintf("%d:%s:%s:%s", h.defaultChainID, to, contractAddress, tokenID)
		h.middlewareCache.Delete(ctx, key)
	}

	prefix := fmt.Sprintf("%d:", h.defaultChainID)
	h.middlewareCache.DeleteByPrefix(ctx, prefix+":"+contractAddress+":"+tokenID)
}

func (h *NFTEventHandler) extractAddresses(evt *event.IndexedEvent) (from, to string) {
	if evt == nil {
		return "", ""
	}
	if evt.Decoded != nil {
		if f, ok := evt.Decoded["from"]; ok {
			from = fmt.Sprintf("%v", f)
		}
		if t, ok := evt.Decoded["to"]; ok {
			to = fmt.Sprintf("%v", t)
		}
	}
	if from == "" && len(evt.Topics) >= 2 {
		from = evt.Topics[1]
	}
	if to == "" && len(evt.Topics) >= 3 {
		to = evt.Topics[2]
	}
	return
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
