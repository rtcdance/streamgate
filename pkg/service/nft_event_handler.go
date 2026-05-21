package service

import (
	"context"
	"fmt"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/web3/event"

	"go.uber.org/zap"
)

type NFTEventHandler struct {
	nftService      *NFTService
	middlewareCache middleware.NFTAccessCache
	defaultChainID  int64
	logger          *zap.Logger
}

func NewNFTEventHandler(nftService *NFTService, logger *zap.Logger) *NFTEventHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &NFTEventHandler{
		nftService: nftService,
		logger:     logger,
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

	h.logger.Debug("Transfer event detected, invalidating ownership cache",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("tx_hash", evt.TransactionHash))

	h.nftService.InvalidateOwnershipCache(ctx, contractAddress, tokenID)

	if h.middlewareCache != nil {
		h.invalidateMiddlewareCache(contractAddress, tokenID, evt)
	}

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

	h.logger.Debug("TransferSingle event detected, invalidating ownership cache",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("tx_hash", evt.TransactionHash))

	h.nftService.InvalidateOwnershipCache(ctx, contractAddress, tokenID)

	if h.middlewareCache != nil {
		h.invalidateMiddlewareCache(contractAddress, tokenID, evt)
	}

	return nil
}

func (h *NFTEventHandler) invalidateMiddlewareCache(contractAddress, tokenID string, evt *event.IndexedEvent) {
	from, to := h.extractAddresses(evt)
	ctx := context.Background()

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
