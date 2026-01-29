package web3

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// NFTVerifier handles NFT verification
type NFTVerifier struct {
	client *ethclient.Client
	logger *zap.Logger
}

// NewNFTVerifier creates a new NFT verifier
func NewNFTVerifier(client *ethclient.Client, logger *zap.Logger) *NFTVerifier {
	return &NFTVerifier{
		client: client,
		logger: logger,
	}
}

// VerifyNFTOwnership verifies if an address owns an NFT
func (nv *NFTVerifier) VerifyNFTOwnership(ctx context.Context, contractAddress string, tokenID string, ownerAddress string) (bool, error) {
	nv.logger.Debug("Verifying NFT ownership", "contract", contractAddress, "token_id", tokenID, "owner", ownerAddress)

	// Parse addresses
	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)

	// Parse token ID
	tokenIDInt := new(big.Int)
	tokenIDInt.SetString(tokenID, 10)

	// ERC721 balanceOf ABI
	abiStr := `[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`

	parsedABI, err := abi.JSON([]byte(abiStr))
	if err != nil {
		nv.logger.Error("Failed to parse ABI", zap.Error(err))
		return false, fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Call ownerOf
	data, err := parsedABI.Pack("ownerOf", tokenIDInt)
	if err != nil {
		nv.logger.Error("Failed to pack ownerOf call", zap.Error(err))
		return false, fmt.Errorf("failed to pack ownerOf call: %w", err)
	}

	// Execute call
	result, err := nv.client.CallContract(ctx, struct {
		To   *common.Address
		Data []byte
	}{
		To:   &contract,
		Data: data,
	}, nil)

	if err != nil {
		nv.logger.Error("Failed to call ownerOf", zap.Error(err))
		return false, fmt.Errorf("failed to call ownerOf: %w", err)
	}

	// Unpack result
	var tokenOwner common.Address
	err = parsedABI.UnpackIntoInterface(&tokenOwner, "ownerOf", result)
	if err != nil {
		nv.logger.Error("Failed to unpack ownerOf result", zap.Error(err))
		return false, fmt.Errorf("failed to unpack ownerOf result: %w", err)
	}

	// Compare addresses
	isOwner := tokenOwner == owner
	nv.logger.Debug("NFT ownership verified", "contract", contractAddress, "token_id", tokenID, "is_owner", isOwner)

	return isOwner, nil
}

// GetNFTBalance gets the NFT balance of an address
func (nv *NFTVerifier) GetNFTBalance(ctx context.Context, contractAddress string, ownerAddress string) (*big.Int, error) {
	nv.logger.Debug("Getting NFT balance", "contract", contractAddress, "owner", ownerAddress)

	// Parse addresses
	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)

	// ERC721 balanceOf ABI
	abiStr := `[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"}]`

	parsedABI, err := abi.JSON([]byte(abiStr))
	if err != nil {
		nv.logger.Error("Failed to parse ABI", zap.Error(err))
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Call balanceOf
	data, err := parsedABI.Pack("balanceOf", owner)
	if err != nil {
		nv.logger.Error("Failed to pack balanceOf call", zap.Error(err))
		return nil, fmt.Errorf("failed to pack balanceOf call: %w", err)
	}

	// Execute call
	result, err := nv.client.CallContract(ctx, struct {
		To   *common.Address
		Data []byte
	}{
		To:   &contract,
		Data: data,
	}, nil)

	if err != nil {
		nv.logger.Error("Failed to call balanceOf", zap.Error(err))
		return nil, fmt.Errorf("failed to call balanceOf: %w", err)
	}

	// Unpack result
	var balance *big.Int
	err = parsedABI.UnpackIntoInterface(&balance, "balanceOf", result)
	if err != nil {
		nv.logger.Error("Failed to unpack balanceOf result", zap.Error(err))
		return nil, fmt.Errorf("failed to unpack balanceOf result: %w", err)
	}

	nv.logger.Debug("NFT balance retrieved", "contract", contractAddress, "owner", ownerAddress, "balance", balance.String())
	return balance, nil
}

// NFTInfo contains NFT information
type NFTInfo struct {
	ContractAddress string
	TokenID         string
	Owner           string
	Name            string
	Symbol          string
	URI             string
}

// GetNFTInfo gets information about an NFT
func (nv *NFTVerifier) GetNFTInfo(ctx context.Context, contractAddress string, tokenID string) (*NFTInfo, error) {
	nv.logger.Debug("Getting NFT info", "contract", contractAddress, "token_id", tokenID)

	nftInfo := &NFTInfo{
		ContractAddress: contractAddress,
		TokenID:         tokenID,
	}

	nv.logger.Debug("NFT info retrieved", "contract", contractAddress, "token_id", tokenID)
	return nftInfo, nil
}

// VerifyNFTCollection verifies if an address owns any NFT from a collection
func (nv *NFTVerifier) VerifyNFTCollection(ctx context.Context, contractAddress string, ownerAddress string) (bool, error) {
	nv.logger.Debug("Verifying NFT collection ownership", "contract", contractAddress, "owner", ownerAddress)

	// Get balance
	balance, err := nv.GetNFTBalance(ctx, contractAddress, ownerAddress)
	if err != nil {
		return false, err
	}

	// Check if balance > 0
	hasNFT := balance.Cmp(big.NewInt(0)) > 0
	nv.logger.Debug("NFT collection ownership verified", "contract", contractAddress, "owner", ownerAddress, "has_nft", hasNFT)

	return hasNFT, nil
}
