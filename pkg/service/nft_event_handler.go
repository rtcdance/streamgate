package service

import (
	"context"
	"fmt"

	"streamgate/pkg/web3"

	"go.uber.org/zap"
)

// NFTEventHandler handles blockchain Transfer events by invalidating
// the NFTService ownership cache, ensuring subsequent ownership checks
// query fresh chain state.
type NFTEventHandler struct {
	nftService *NFTService
	logger     *zap.Logger
}

// NewNFTEventHandler creates a new Transfer event handler for cache invalidation.
func NewNFTEventHandler(nftService *NFTService, logger *zap.Logger) *NFTEventHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &NFTEventHandler{
		nftService: nftService,
		logger:     logger,
	}
}

// HandleTransfer processes a Transfer event and invalidates the ownership cache
// for the affected token. The event's Decoded field is expected to contain:
//   - "tokenId" (string or numeric): the token ID that was transferred
//   - "from" (string): the previous owner address
//   - "to" (string): the new owner address
//
// If the Decoded field is empty or missing required fields, the handler
// falls back to extracting token ID from the event Topics.
func (h *NFTEventHandler) HandleTransfer(ctx context.Context, event *web3.IndexedEvent) error {
	contractAddress := event.ContractAddress

	tokenID, err := h.extractTokenID(event)
	if err != nil {
		h.logger.Warn("Failed to extract token ID from Transfer event",
			zap.String("tx_hash", event.TransactionHash),
			zap.Error(err))
		return nil // best-effort: don't fail the event pipeline
	}

	h.logger.Debug("Transfer event detected, invalidating ownership cache",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("tx_hash", event.TransactionHash))

	h.nftService.InvalidateOwnershipCache(contractAddress, tokenID)
	return nil
}

// extractTokenID attempts to extract the token ID from the event's Decoded
// field first, then falls back to Topics[2] (ERC-721 Transfer: topic2 = tokenId).
func (h *NFTEventHandler) extractTokenID(event *web3.IndexedEvent) (string, error) {
	// Try Decoded field first (requires EventParser to be configured)
	if event.Decoded != nil {
		if tokenID, ok := event.Decoded["tokenId"]; ok {
			return fmt.Sprintf("%v", tokenID), nil
		}
		if tokenID, ok := event.Decoded["tokenID"]; ok {
			return fmt.Sprintf("%v", tokenID), nil
		}
	}

	// Fallback: ERC-721 Transfer event has tokenId in Topics[2]
	// Transfer(address indexed from, address indexed to, uint256 indexed tokenId)
	if len(event.Topics) >= 3 {
		return event.Topics[2], nil
	}

	return "", fmt.Errorf("no token ID found in Transfer event (decoded=%v, topics=%d)",
		event.Decoded != nil, len(event.Topics))
}
