package service

import (
	"context"
	"fmt"

	"streamgate/pkg/middleware"
	"streamgate/pkg/web3"

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

func (h *NFTEventHandler) HandleTransfer(ctx context.Context, event *web3.IndexedEvent) error {
	contractAddress := event.ContractAddress

	tokenID, err := h.extractTokenID(event)
	if err != nil {
		h.logger.Warn("Failed to extract token ID from Transfer event",
			zap.String("tx_hash", event.TransactionHash),
			zap.Error(err))
		return nil
	}

	h.logger.Debug("Transfer event detected, invalidating ownership cache",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("tx_hash", event.TransactionHash))

	h.nftService.InvalidateOwnershipCache(ctx, contractAddress, tokenID)

	if h.middlewareCache != nil {
		h.invalidateMiddlewareCache(contractAddress, tokenID, event)
	}

	return nil
}

func (h *NFTEventHandler) HandleTransferSingle(ctx context.Context, event *web3.IndexedEvent) error {
	contractAddress := event.ContractAddress

	tokenID := h.extractERC1155TokenID(event)
	if tokenID == "" {
		h.logger.Warn("Failed to extract token ID from TransferSingle event",
			zap.String("tx_hash", event.TransactionHash))
		return nil
	}

	h.logger.Debug("TransferSingle event detected, invalidating ownership cache",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("tx_hash", event.TransactionHash))

	h.nftService.InvalidateOwnershipCache(ctx, contractAddress, tokenID)

	if h.middlewareCache != nil {
		h.invalidateMiddlewareCache(contractAddress, tokenID, event)
	}

	return nil
}

func (h *NFTEventHandler) invalidateMiddlewareCache(contractAddress, tokenID string, event *web3.IndexedEvent) {
	from, to := h.extractAddresses(event)

	if from != "" {
		key := fmt.Sprintf("%d:%s:%s:%s", h.defaultChainID, from, contractAddress, tokenID)
		h.middlewareCache.Delete(key)
	}
	if to != "" {
		key := fmt.Sprintf("%d:%s:%s:%s", h.defaultChainID, to, contractAddress, tokenID)
		h.middlewareCache.Delete(key)
	}

	prefix := fmt.Sprintf("%d:", h.defaultChainID)
	h.middlewareCache.DeleteByPrefix(prefix + ":" + contractAddress + ":" + tokenID)
}

func (h *NFTEventHandler) extractAddresses(event *web3.IndexedEvent) (from, to string) {
	if event.Decoded != nil {
		if f, ok := event.Decoded["from"]; ok {
			from = fmt.Sprintf("%v", f)
		}
		if t, ok := event.Decoded["to"]; ok {
			to = fmt.Sprintf("%v", t)
		}
	}
	if from == "" && len(event.Topics) >= 2 {
		from = event.Topics[1]
	}
	if to == "" && len(event.Topics) >= 3 {
		to = event.Topics[2]
	}
	return
}

func (h *NFTEventHandler) extractTokenID(event *web3.IndexedEvent) (string, error) {
	if event.Decoded != nil {
		if tokenID, ok := event.Decoded["tokenId"]; ok {
			return fmt.Sprintf("%v", tokenID), nil
		}
		if tokenID, ok := event.Decoded["tokenID"]; ok {
			return fmt.Sprintf("%v", tokenID), nil
		}
	}

	if len(event.Topics) >= 4 {
		return event.Topics[3], nil
	}

	return "", fmt.Errorf("no token ID found in Transfer event (decoded=%v, topics=%d)",
		event.Decoded != nil, len(event.Topics))
}

func (h *NFTEventHandler) extractERC1155TokenID(event *web3.IndexedEvent) string {
	if event.Decoded != nil {
		if id, ok := event.Decoded["id"]; ok {
			return fmt.Sprintf("%v", id)
		}
	}
	return ""
}
