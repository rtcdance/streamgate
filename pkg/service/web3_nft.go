package service

import (
	"context"
	"fmt"
	"math/big"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/web3"

	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// VerifySignature verifies a message signature
func (ws *Web3Service) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	ws.logger.Debug("Verifying signature", zap.String("address", address))
	return ws.signatureVerifier.VerifySignature(ctx, address, message, signature)
}

// VerifyNFTOwnership verifies NFT ownership on the given chain.
// For EVM chains (positive chainID) it uses ERC-721/1155 ownership checks.
// For Solana chains (negative chainID) it derives the Associated Token Account
// from (owner, mint) and verifies on-chain ownership via RPC.
func (ws *Web3Service) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	ctx, span := monitoring.StartOTelSpan(ctx, "web3.verify_nft_ownership",
		attribute.Int64("chain_id", chainID),
		attribute.String("contract", contractAddress),
		attribute.String("token_id", tokenID),
	)
	defer span.End()

	ws.logger.Debug("Verifying NFT ownership",
		zap.Int64("chain_id", chainID),
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	// Solana path: negative chain IDs
	if chainID < 0 {
		solanaClient, err := ws.multiChainManager.GetSolanaClient(chainID)
		if err != nil {
			return false, fmt.Errorf("solana chain client not found for chain %d: %w", chainID, err)
		}

		// Derive the Associated Token Account from (owner, mint)
		tokenAccount, err := solanaClient.DeriveTokenAccountAddress(ownerAddress, contractAddress)
		if err != nil {
			return false, fmt.Errorf("failed to derive token account: %w", err)
		}

		// Verify on-chain token account ownership
		return solanaClient.VerifyTokenAccount(ctx, tokenAccount, ownerAddress)
	}

	// EVM path
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return false, err
	}

	ethCaller := client.GetEthClient()

	// Detect token standard and route to the appropriate verifier
	standard := web3.DetectTokenStandard(ctx, ethCaller, contractAddress, ws.logger)
	switch standard {
	case web3.TokenStandardERC1155:
		verifier := web3.NewERC1155Verifier(ethCaller, ws.logger, nil)
		return verifier.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
	default:
		// ERC-721 or unknown — use the standard NFTVerifier
		nftVerifier := web3.NewNFTVerifier(ethCaller, ws.logger).WithBlockTag(client.GetFinality().BlockTag())
		return nftVerifier.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
	}
}

// GetNFTBalance gets collection balance for an owner.
// For EVM chains (positive chainID) it uses ERC-721/1155 balanceOf.
// For Solana chains (negative chainID) it verifies the Associated Token Account
// and returns 1 if held, 0 otherwise (Solana NFTs are 1:1 mint to token-account).
func (ws *Web3Service) GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error) {
	ws.logger.Debug("Getting NFT balance",
		zap.Int64("chain_id", chainID),
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress))

	// Solana path: negative chain IDs
	if chainID < 0 {
		solanaClient, err := ws.multiChainManager.GetSolanaClient(chainID)
		if err != nil {
			return nil, fmt.Errorf("solana chain client not found for chain %d: %w", chainID, err)
		}

		// Derive the Associated Token Account from (owner, mint)
		tokenAccount, err := solanaClient.DeriveTokenAccountAddress(ownerAddress, contractAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to derive token account: %w", err)
		}

		// Verify on-chain token account ownership
		owned, err := solanaClient.VerifyTokenAccount(ctx, tokenAccount, ownerAddress)
		if err != nil {
			return nil, err
		}
		if owned {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	}

	// EVM path
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, err
	}

	ethCaller := client.GetEthClient()

	// Detect token standard and route to the appropriate verifier
	standard := web3.DetectTokenStandard(ctx, ethCaller, contractAddress, ws.logger)
	switch standard {
	case web3.TokenStandardERC1155:
		verifier := web3.NewERC1155Verifier(ethCaller, ws.logger, nil)
		// For balance checks, use tokenID "0" as default — the caller can
		// specify a specific token via VerifyNFTOwnership if needed.
		owned, err := verifier.VerifyNFTOwnership(ctx, contractAddress, "0", ownerAddress)
		if err != nil {
			return nil, err
		}
		if owned {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	default:
		nftVerifier := web3.NewNFTVerifier(ethCaller, ws.logger).WithBlockTag(client.GetFinality().BlockTag())
		balance, err := nftVerifier.GetNFTBalance(ctx, contractAddress, ownerAddress)
		if err != nil {
			return nil, err
		}
		return balance, nil
	}
}

// VerifyNFTOwnershipAutoDetect detects the token standard and routes to the
// correct verification method. For EVM chains it uses NFTVerifier.AutoDetect;
// for Solana chains it falls back to VerifyNFTOwnership.
func (ws *Web3Service) VerifyNFTOwnershipAutoDetect(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return false, err
	}
	ethCaller := client.GetEthClient()
	verifier := web3.NewNFTVerifier(ethCaller, ws.logger).WithBlockTag(client.GetFinality().BlockTag())
	return verifier.VerifyNFTOwnershipAutoDetect(ctx, contractAddress, tokenID, ownerAddress)
}

// VerifyNFTCollectionAutoDetect detects the token standard and routes to the
// correct collection-level verification.
func (ws *Web3Service) VerifyNFTCollectionAutoDetect(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (bool, error) {
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return false, err
	}
	ethCaller := client.GetEthClient()
	verifier := web3.NewNFTVerifier(ethCaller, ws.logger).WithBlockTag(client.GetFinality().BlockTag())
	return verifier.VerifyNFTCollectionAutoDetect(ctx, contractAddress, ownerAddress)
}

// GetNFT gets NFT metadata by contract and token ID on the given chain.
func (ws *Web3Service) GetNFT(ctx context.Context, chainID int64, contractAddress, tokenID string) (*web3.NFTInfo, error) {
	ws.logger.Debug("Getting NFT", zap.Int64("chain_id", chainID), zap.String("contract", contractAddress), zap.String("token_id", tokenID))

	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	nftVerifier := web3.NewNFTVerifier(client.GetEthClient(), ws.logger).WithBlockTag(client.GetFinality().BlockTag())
	return nftVerifier.GetNFTInfo(ctx, contractAddress, tokenID)
}

// DetectContractType detects whether a contract is ERC-721 or ERC-1155.
// Returns "ERC-721", "ERC-1155", or "unknown".
func (ws *Web3Service) DetectContractType(ctx context.Context, chainID int64, contractAddress string) string {
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return "unknown"
	}
	switch web3.DetectTokenStandard(ctx, client.GetEthClient(), contractAddress, ws.logger) {
	case web3.TokenStandardERC721:
		return "ERC-721"
	case web3.TokenStandardERC1155:
		return "ERC-1155"
	}
	return "unknown"
}

// ListNFTs lists NFTs
func (ws *Web3Service) ListNFTs(ctx context.Context, offset, limit int) ([]interface{}, error) {
	ws.logger.Debug("Listing NFTs", zap.Int("offset", offset), zap.Int("limit", limit))

	return []interface{}{}, nil
}

// GetNFTInfo returns NFT metadata in middleware format for the NFT gating layer.
// Implements middleware.NFTOwnershipChecker.GetNFTInfo.
func (ws *Web3Service) GetNFTInfo(ctx context.Context, chainID int64, contractAddress, tokenID string) (*middleware.NFTMetadata, error) {
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	meta, err := client.GetNFTMetadata(ctx, contractAddress, tokenID)
	if err != nil {
		return nil, err
	}

	return &middleware.NFTMetadata{
		Name:            meta.Name,
		TokenURI:        meta.Image,
		ContractAddress: meta.ContractAddress,
		TokenID:         meta.TokenID,
	}, nil
}

// HeaderByNumber returns the block header for the given block number on the default chain.
func (ws *Web3Service) HeaderByNumber(ctx context.Context, number *big.Int) (*middleware.BlockHeaderInfo, error) {
	client, err := ws.multiChainManager.GetClient(ws.config.Web3.ChainID)
	if err != nil {
		return nil, err
	}
	header, err := client.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	if header == nil {
		return nil, nil
	}
	return &middleware.BlockHeaderInfo{
		Number:     header.Number.Uint64(),
		Hash:       header.Hash().Hex(),
		ParentHash: header.ParentHash.Hex(),
	}, nil
}
