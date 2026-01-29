package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// NFTService handles NFT operations
type NFTService struct {
	ethClient     *ethclient.Client
	cache         NFTCacheStorage
	rpcURL        string
	cacheEnabled  bool
	cacheDuration time.Duration
}

// NFTCacheStorage defines the interface for NFT cache storage
type NFTCacheStorage interface {
	Get(key string) (interface{}, error)
	Set(key string, value interface{}) error
	Delete(key string) error
}

// NFTMetadata represents NFT metadata
type NFTMetadata struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Image       string                 `json:"image"`
	Attributes  []NFTAttribute         `json:"attributes"`
	Properties  map[string]interface{} `json:"properties"`
}

// NFTAttribute represents an NFT attribute
type NFTAttribute struct {
	TraitType string      `json:"trait_type"`
	Value     interface{} `json:"value"`
}

// ERC721 ABI for ownerOf function
const erc721ABI = `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"tokenURI","outputs":[{"name":"","type":"string"}],"type":"function"}]`

// NewNFTService creates a new NFT service
func NewNFTService(rpcURL string, cache NFTCacheStorage) (*NFTService, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum node: %w", err)
	}

	return &NFTService{
		ethClient:     client,
		cache:         cache,
		rpcURL:        rpcURL,
		cacheEnabled:  cache != nil,
		cacheDuration: 1 * time.Hour,
	}, nil
}

// VerifyNFT verifies NFT ownership
func (s *NFTService) VerifyNFT(address, contractAddress, tokenID string) (bool, error) {
	// Validate inputs
	if !common.IsHexAddress(address) {
		return false, fmt.Errorf("invalid address: %s", address)
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
	owner, err := s.getOwnerOf(contractAddr, tokenIDInt)
	if err != nil {
		return false, fmt.Errorf("failed to get NFT owner: %w", err)
	}

	// Cache the result
	if s.cacheEnabled {
		s.cache.Set(cacheKey, owner.Hex())
	}

	// Compare addresses (case-insensitive)
	return strings.EqualFold(owner.Hex(), address), nil
}

// GetNFTMetadata gets NFT metadata
func (s *NFTService) GetNFTMetadata(contractAddress, tokenID string) (*NFTMetadata, error) {
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
	tokenURI, err := s.getTokenURI(contractAddr, tokenIDInt)
	if err != nil {
		return nil, fmt.Errorf("failed to get token URI: %w", err)
	}

	// Fetch metadata from URI
	// Note: In production, you would fetch from IPFS or HTTP
	// For now, return a basic structure
	metadata := &NFTMetadata{
		Name:        fmt.Sprintf("NFT #%s", tokenID),
		Description: "NFT from contract " + contractAddress,
		Image:       tokenURI,
		Attributes:  []NFTAttribute{},
		Properties:  make(map[string]interface{}),
	}

	// Cache the result
	if s.cacheEnabled {
		s.cache.Set(cacheKey, metadata)
	}

	return metadata, nil
}

// getOwnerOf calls the ownerOf function on an ERC-721 contract
func (s *NFTService) getOwnerOf(contractAddress common.Address, tokenID *big.Int) (common.Address, error) {
	// Parse ABI
	parsedABI, err := abi.JSON(strings.NewReader(erc721ABI))
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Pack the function call
	data, err := parsedABI.Pack("ownerOf", tokenID)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to pack function call: %w", err)
	}

	// Call the contract
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := s.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return common.Address{}, fmt.Errorf("contract call failed: %w", err)
	}

	// Unpack the result
	var owner common.Address
	err = parsedABI.UnpackIntoInterface(&owner, "ownerOf", result)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to unpack result: %w", err)
	}

	return owner, nil
}

// getTokenURI calls the tokenURI function on an ERC-721 contract
func (s *NFTService) getTokenURI(contractAddress common.Address, tokenID *big.Int) (string, error) {
	// Parse ABI
	parsedABI, err := abi.JSON(strings.NewReader(erc721ABI))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Pack the function call
	data, err := parsedABI.Pack("tokenURI", tokenID)
	if err != nil {
		return "", fmt.Errorf("failed to pack function call: %w", err)
	}

	// Call the contract
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := s.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("contract call failed: %w", err)
	}

	// Unpack the result
	var tokenURI string
	err = parsedABI.UnpackIntoInterface(&tokenURI, "tokenURI", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack result: %w", err)
	}

	return tokenURI, nil
}

// VerifyNFTBatch verifies multiple NFTs in batch
func (s *NFTService) VerifyNFTBatch(address string, nfts []struct {
	ContractAddress string
	TokenID         string
}) (map[string]bool, error) {
	results := make(map[string]bool)

	for _, nft := range nfts {
		key := fmt.Sprintf("%s:%s", nft.ContractAddress, nft.TokenID)
		verified, err := s.VerifyNFT(address, nft.ContractAddress, nft.TokenID)
		if err != nil {
			results[key] = false
		} else {
			results[key] = verified
		}
	}

	return results, nil
}

// Close closes the Ethereum client connection
func (s *NFTService) Close() {
	if s.ethClient != nil {
		s.ethClient.Close()
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
