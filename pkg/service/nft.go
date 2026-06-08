package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/cachetypes"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// NFTService handles NFT operations
type NFTService struct {
	ethClient     web3.EthCaller
	cache         cachetypes.CacheBackend
	rpcURL        string
	cacheEnabled  bool
	cacheDuration time.Duration
	parsedABI     abi.ABI // pre-parsed at construction
	closer        io.Closer
	logger        *zap.Logger
	eventHandler  *NFTEventHandler
}

// NFTMetadata represents NFT metadata for API responses.
// Extends web3.NFTMetadata with a Properties field for arbitrary JSON data.
type NFTMetadata = web3.NFTMetadata

// NFTAttribute is an alias for web3.NFTAttribute.
type NFTAttribute = web3.NFTAttribute

// erc721ServiceABI contains ownerOf and tokenURI methods used by NFTService.
const erc721ServiceABI = `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"tokenURI","outputs":[{"name":"","type":"string"}],"type":"function"}]`

// NewNFTService creates a new NFT service
func NewNFTService(rpcURL string, cache cachetypes.CacheBackend) (*NFTService, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum node: %w", err)
	}

	parsedABI, err := abi.JSON(bytes.NewReader([]byte(erc721ServiceABI)))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to parse ERC-721 ABI: %w", err)
	}

	nftSvc := &NFTService{
		ethClient:     client,
		cache:         cache,
		rpcURL:        rpcURL,
		cacheEnabled:  cache != nil,
		cacheDuration: 1 * time.Hour,
		parsedABI:     parsedABI,
		logger:        zap.NewNop(),
	}
	if c, ok := any(client).(io.Closer); ok {
		nftSvc.closer = c
	}
	return nftSvc, nil
}

// NewNFTServiceWithCaller creates a new NFT service with a custom EthCaller.
// This enables multi-RPC failover when used with a ChainClient wrapper.
func NewNFTServiceWithCaller(caller web3.EthCaller, rpcURL string, cache cachetypes.CacheBackend) (*NFTService, error) {
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(erc721ServiceABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC-721 ABI: %w", err)
	}

	nftSvc := &NFTService{
		ethClient:     caller,
		cache:         cache,
		rpcURL:        rpcURL,
		cacheEnabled:  cache != nil,
		cacheDuration: 1 * time.Hour,
		parsedABI:     parsedABI,
		logger:        zap.NewNop(),
	}
	if closer, ok := caller.(io.Closer); ok {
		nftSvc.closer = closer
	}
	return nftSvc, nil
}

// VerifyNFT verifies NFT ownership
func (s *NFTService) VerifyNFT(ctx context.Context, address, contractAddress, tokenID string) (bool, error) {
	ctx, span := monitoring.StartOTelSpan(ctx, "nft.verify",
		attribute.String("nft.contract", contractAddress),
		attribute.String("nft.token_id", tokenID),
	)
	defer span.End()

	if !common.IsHexAddress(address) {
		return false, fmt.Errorf("invalid address %s: %w", address, ErrInvalidAddress)
	}
	if !common.IsHexAddress(contractAddress) {
		return false, fmt.Errorf("invalid contract address: %s", contractAddress)
	}

	// Check cache first
	cacheKey := fmt.Sprintf("nft:owner:%s:%s", contractAddress, tokenID)
	if s.cacheEnabled {
		if cached, err := s.cache.Get(cacheKey); err == nil {
			if owner, ok := cached.(string); ok {
				return strings.EqualFold(owner, address), nil
			}
		}
	}

	// Parse contract address
	contractAddr := common.HexToAddress(contractAddress)

	// Parse token ID
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return false, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	// Get owner from blockchain
	owner, err := s.getOwnerOf(ctx, contractAddr, tokenIDInt)
	if err != nil {
		return false, fmt.Errorf("failed to get NFT owner: %w", err)
	}

	// Cache the result with TTL so stale ownership data doesn't persist indefinitely
	// if the EventIndexer is down and Transfer events are missed.
	if s.cacheEnabled {
		if err := s.cache.SetWithExpiration(cacheKey, owner.Hex(), s.cacheDuration); err != nil {
			s.logger.Debug("Failed to cache NFT ownership", zap.Error(err))
		}
	}

	// Compare addresses (case-insensitive)
	return strings.EqualFold(owner.Hex(), address), nil
}

// GetNFTMetadata gets NFT metadata
func (s *NFTService) GetNFTMetadata(ctx context.Context, contractAddress, tokenID string) (*NFTMetadata, error) {
	// Validate inputs
	if !common.IsHexAddress(contractAddress) {
		return nil, fmt.Errorf("invalid contract address: %s", contractAddress)
	}

	// Check cache first
	cacheKey := fmt.Sprintf("nft:metadata:%s:%s", contractAddress, tokenID)
	if s.cacheEnabled {
		if cached, err := s.cache.Get(cacheKey); err == nil {
			if metadata, ok := cached.(*NFTMetadata); ok {
				return metadata, nil
			}
		}
	}

	// Parse contract address
	contractAddr := common.HexToAddress(contractAddress)

	// Parse token ID
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return nil, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	// Get token URI from blockchain
	tokenURI, err := s.getTokenURI(ctx, contractAddr, tokenIDInt)
	if err != nil {
		return nil, fmt.Errorf("failed to get token URI: %w", err)
	}

	// Fetch metadata from URI with SSRF protection (supports https://, ipfs://, ar://)
	var metadata NFTMetadata
	if err := web3.SafeFetchURI(ctx, tokenURI, &metadata); err != nil {
		s.logger.Debug("Failed to fetch NFT metadata from URI, using fallback",
			zap.String("token_uri", tokenURI),
			zap.Error(err))
		metadata = NFTMetadata{
			Name:        fmt.Sprintf("NFT #%s", tokenID),
			Description: "NFT from contract " + contractAddress,
			Image:       tokenURI,
			Attributes:  []NFTAttribute{},
		}
	}

	// Cache the result
	if s.cacheEnabled {
		_ = s.cache.SetWithExpiration(cacheKey, &metadata, s.cacheDuration)
	}

	return &metadata, nil
}

// getOwnerOf calls the ownerOf function on an ERC-721 contract
func (s *NFTService) getOwnerOf(ctx context.Context, contractAddress common.Address, tokenID *big.Int) (common.Address, error) {
	// Pack the function call
	data, err := s.parsedABI.Pack("ownerOf", tokenID)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to pack function call: %w", err)
	}

	// Call the contract with caller's context (respects cancellation/timeout)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := s.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return common.Address{}, fmt.Errorf("contract call failed: %w", err)
	}

	if len(result) < 32 {
		return common.Address{}, fmt.Errorf("ownerOf returned insufficient data (len=%d): contract may not exist or is not a valid ERC-721 contract", len(result))
	}

	// Unpack the result
	var owner common.Address
	err = s.parsedABI.UnpackIntoInterface(&owner, "ownerOf", result)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to unpack result: %w", err)
	}

	return owner, nil
}

// getTokenURI calls the tokenURI function on an ERC-721 contract
func (s *NFTService) getTokenURI(ctx context.Context, contractAddress common.Address, tokenID *big.Int) (string, error) {
	// Pack the function call
	data, err := s.parsedABI.Pack("tokenURI", tokenID)
	if err != nil {
		return "", fmt.Errorf("failed to pack function call: %w", err)
	}

	// Call the contract with caller's context (respects cancellation/timeout)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := s.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("contract call failed: %w", err)
	}

	if len(result) < 32 {
		return "", fmt.Errorf("tokenURI returned insufficient data (len=%d): contract may not exist or is not a valid ERC-721 contract", len(result))
	}

	// Unpack the result
	var tokenURI string
	err = s.parsedABI.UnpackIntoInterface(&tokenURI, "tokenURI", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack result: %w", err)
	}

	return tokenURI, nil
}

// VerifyNFTBatch verifies multiple NFTs in batch
func (s *NFTService) VerifyNFTBatch(ctx context.Context, address string, nfts []struct {
	ContractAddress string
	TokenID         string
}) (map[string]bool, error) {
	results := make(map[string]bool)

	for _, nft := range nfts {
		key := fmt.Sprintf("%s:%s", nft.ContractAddress, nft.TokenID)
		verified, err := s.VerifyNFT(ctx, address, nft.ContractAddress, nft.TokenID)
		if err != nil {
			results[key] = false
		} else {
			results[key] = verified
		}
	}

	return results, nil
}

// InvalidateOwnershipCache removes cached NFT ownership data for a specific token.
// This is called when a Transfer event is detected on-chain, ensuring the next
// VerifyNFT call queries fresh chain state instead of returning stale cached data.
func (s *NFTService) InvalidateOwnershipCache(ctx context.Context, contractAddress, tokenID string) {
	if s.cacheEnabled && s.cache != nil {
		key := fmt.Sprintf("nft:owner:%s:%s", contractAddress, tokenID)
		if err := s.cache.Delete(key); err != nil {
			s.logger.Error("Failed to invalidate NFT ownership cache — stale data may be served",
				zap.String("contract", contractAddress),
				zap.String("token_id", tokenID),
				zap.Error(err))
			return
		}
		s.logger.Debug("Invalidated NFT ownership cache",
			zap.String("contract", contractAddress),
			zap.String("token_id", tokenID))
	}
}

// RegisterEventHandler registers a Transfer event handler on the given EventListener
// that automatically invalidates NFT ownership cache when tokens are transferred.
func (s *NFTService) RegisterEventHandler(listener *web3.EventListener) {
	handler := NewNFTEventHandler(s, s.logger)
	s.eventHandler = handler
	listener.On("Transfer", handler.HandleTransfer)
	listener.On("TransferSingle", handler.HandleTransferSingle)
}

func (s *NFTService) RegisterEventHandlerWithCache(listener *web3.EventListener, cache middleware.NFTAccessCache, chainID int64) {
	handler := NewNFTEventHandlerWithCache(s, cache, chainID, s.logger)
	s.eventHandler = handler
	listener.On("Transfer", handler.HandleTransfer)
	listener.On("TransferSingle", handler.HandleTransferSingle)
}

// SetLogger sets the logger for the NFT service.
func (s *NFTService) SetLogger(logger *zap.Logger) {
	s.logger = logger
}

// Close closes the Ethereum client connection
func (s *NFTService) Close() {
	if s.eventHandler != nil {
		s.eventHandler.FlushNow()
	}
	if s.closer != nil {
		s.closer.Close()
	}
}

// ParseMetadataJSON parses NFT metadata from JSON
func ParseMetadataJSON(data []byte) (*NFTMetadata, error) {
	var metadata NFTMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}
	return &metadata, nil
}
