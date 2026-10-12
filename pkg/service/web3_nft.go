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

	// Use auto-detect verifier which handles both ERC-721 and ERC-1155.
	// For ERC-1155, GetNFTBalanceAutoDetect requires a tokenID parameter;
	// callers must use VerifyNFTOwnership with a specific tokenID instead.
	nftVerifier := web3.NewNFTVerifier(ethCaller, ws.logger).WithBlockTag(client.GetFinality().BlockTag())
	balance, err := nftVerifier.GetNFTBalanceAutoDetect(ctx, contractAddress, ownerAddress)
	if err != nil {
		return nil, err
	}
	return balance, nil
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

// GetNFTOwnershipQuantity returns the holder's quantity for the gating subject
// on the given chain: the collection balance when tokenID is empty, otherwise
// the token quantity (ERC-721 → 0/1, ERC-1155 → on-chain uint256). Unknown /
// non-NFT contracts return an error, never a permissive value.
//
// block pins the ERC-165 probe and the balance query to one exact block, so an
// authorization cannot be assembled from two different chain states. A nil
// block keeps the previous tag-based behaviour for callers that are not making
// an authorization decision.
// Implements middleware.NFTOwnershipChecker.GetNFTOwnershipQuantity.
func (ws *Web3Service) GetNFTOwnershipQuantity(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string, block *big.Int) (*big.Int, error) {
	// Solana NFTs are 1:1 mint→token-account, so ownership maps to quantity 1.
	if chainID < 0 {
		owned, err := ws.VerifyNFTOwnership(ctx, chainID, contractAddress, tokenID, ownerAddress)
		if err != nil {
			return nil, err
		}
		if owned {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	}

	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, err
	}
	// Pass the ChainClient, not its raw ethclient: only the ChainClient can pin
	// a call to a specific block, and the verifier refuses to probe at a tag or
	// latest when a block is pinned. Handing it the raw client would make every
	// pinned read fail closed as "not a recognised ERC-721/ERC-1155 contract".
	verifier := web3.NewNFTVerifier(client, ws.logger).
		WithBlockTag(client.GetFinality().BlockTag()).
		WithBlockNumber(block)
	return verifier.GetNFTQuantityAutoDetect(ctx, contractAddress, tokenID, ownerAddress)
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

// HeaderByNumber returns the block header for the given block number on the
// named chain. The chain is explicit so block evidence can never be read from
// one chain and used to justify an authorization on another; a zero chainID
// means "the configured default chain".
func (ws *Web3Service) HeaderByNumber(ctx context.Context, chainID int64, number *big.Int) (*middleware.BlockHeaderInfo, error) {
	if chainID == 0 {
		chainID = ws.config.Web3.ChainID
	}
	client, err := ws.multiChainManager.GetClient(chainID)
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

// HeaderByTag resolves a finality tag ("latest", "safe", "finalized") to a
// concrete header on the named chain. The NFT gate uses it to choose the ONE
// block an authorization reads at, instead of letting the node re-resolve a tag
// on every individual call. An unknown tag is refused rather than treated as
// latest.
func (ws *Web3Service) HeaderByTag(ctx context.Context, chainID int64, tag string) (*middleware.BlockHeaderInfo, error) {
	if chainID == 0 {
		chainID = ws.config.Web3.ChainID
	}
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, err
	}

	// go-ethereum's sentinels: -4 = safe, -3 = finalized, nil = latest. These are
	// the same values ChainClient.CallContractAtBlock uses for its tags.
	var number *big.Int
	switch tag {
	case "safe":
		number = big.NewInt(-4)
	case "finalized":
		number = big.NewInt(-3)
	case "latest", "":
		number = nil
	default:
		return nil, fmt.Errorf("unsupported block tag %q on chain %d", tag, chainID)
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
