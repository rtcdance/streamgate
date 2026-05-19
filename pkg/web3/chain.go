package web3

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// Pre-parsed ERC-721 ABIs for contract calls.
var erc721OwnerOfABI = mustParseABI("ERC721-ownerOf", `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`)
var erc721TokenURIABI = mustParseABI("ERC721-tokenURI", `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"tokenURI","outputs":[{"name":"","type":"string"}],"type":"function"}]`)

func (cc *ChainClient) VerifyNFTOwnership(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	return withChainClient(ctx, cc, "VerifyNFTOwnership", func(client *ethclient.Client) (bool, error) {
		v := NewNFTVerifier(client, cc.logger).WithBlockTag(cc.GetFinality().BlockTag())
		return v.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
	})
}

func (cc *ChainClient) GetNFTBalance(ctx context.Context, contractAddress, ownerAddress string) (*big.Int, error) {
	return withChainClient(ctx, cc, "GetNFTBalance", func(client *ethclient.Client) (*big.Int, error) {
		v := NewNFTVerifier(client, cc.logger).WithBlockTag(cc.GetFinality().BlockTag())
		return v.GetNFTBalance(ctx, contractAddress, ownerAddress)
	})
}

func (cc *ChainClient) VerifyNFTOwnershipAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	return withChainClient(ctx, cc, "VerifyNFTOwnershipAutoDetect", func(client *ethclient.Client) (bool, error) {
		v := NewNFTVerifier(client, cc.logger).WithBlockTag(cc.GetFinality().BlockTag())
		return v.VerifyNFTOwnershipAutoDetect(ctx, contractAddress, tokenID, ownerAddress)
	})
}

func (cc *ChainClient) VerifyNFTCollectionAutoDetect(ctx context.Context, contractAddress, ownerAddress string) (bool, error) {
	return withChainClient(ctx, cc, "VerifyNFTCollectionAutoDetect", func(client *ethclient.Client) (bool, error) {
		v := NewNFTVerifier(client, cc.logger).WithBlockTag(cc.GetFinality().BlockTag())
		return v.VerifyNFTCollectionAutoDetect(ctx, contractAddress, ownerAddress)
	})
}

// CallContractAtBlock executes a contract call at the given block tag.
// BlockTagSafe and BlockTagFinalized read from post-merge finalized blocks,
// which protects against reorgs. Falls back to latest if the RPC doesn't
// support the requested tag.
func (cc *ChainClient) CallContractAtBlock(ctx context.Context, msg ethereum.CallMsg, blockTag BlockTag) ([]byte, error) {
	var blockNum *big.Int
	switch blockTag {
	case BlockTagSafe:
		blockNum = big.NewInt(-4) // go-ethereum convention for "safe"
	case BlockTagFinalized:
		blockNum = big.NewInt(-3) // go-ethereum convention for "finalized"
	default:
		blockNum = nil // latest
	}

	result, err := withChainClient(ctx, cc, "CallContractAtBlock", func(client *ethclient.Client) ([]byte, error) {
		return client.CallContract(ctx, msg, blockNum)
	})

	if err != nil && blockNum != nil {
		cc.logger.Error("Block tag call failed, refusing to fall back to latest for safety",
			zap.String("block_tag", string(blockTag)),
			zap.Error(err))
		return nil, fmt.Errorf("CallContract at block tag %q failed: %w (fallback to latest disabled for reorg safety)", blockTag, err)
	}

	return result, err
}

// VerifyNFTOwnershipByRequest verifies if a wallet owns an NFT
func (cc *ChainClient) VerifyNFTOwnershipByRequest(ctx context.Context, req VerifyRequest) (bool, error) {
	cc.logger.Debug("Verifying NFT ownership",
		zap.String("wallet_address", req.WalletAddress),
		zap.String("contract", req.Contract),
		zap.String("token_id", req.TokenID),
		zap.Int("min_balance", req.MinBalance))

	contract := common.HexToAddress(req.Contract)
	wallet := common.HexToAddress(req.WalletAddress)

	switch req.Mode {
	case GatingAny:
		balance, err := cc.getNFTBalanceAtBlock(ctx, wallet, contract, BlockTagSafe)
		if err != nil {
			return false, fmt.Errorf("failed to get NFT balance: %w", err)
		}
		return balance.Sign() > 0, nil

	case GatingMinBalance:
		balance, err := cc.getNFTBalanceAtBlock(ctx, wallet, contract, BlockTagSafe)
		if err != nil {
			return false, fmt.Errorf("failed to get NFT balance: %w", err)
		}
		return balance.Cmp(big.NewInt(int64(req.MinBalance))) >= 0, nil

	case GatingSpecificID:
		owner, err := cc.getNFTOwnerAtBlock(ctx, contract, req.TokenID, BlockTagSafe)
		if err != nil {
			return false, fmt.Errorf("failed to get NFT owner: %w", err)
		}
		return owner == wallet, nil

	case GatingCombination:
		balance, err := cc.getNFTBalanceAtBlock(ctx, wallet, contract, BlockTagSafe)
		if err != nil {
			return false, fmt.Errorf("failed to get NFT balance: %w", err)
		}
		return balance.Cmp(big.NewInt(int64(req.MinBalance))) >= 0, nil

	default:
		return false, fmt.Errorf("unsupported gating mode: %d", req.Mode)
	}
}

// GetWalletNFTBalance gets the NFT balance for a wallet
func (cc *ChainClient) GetWalletNFTBalance(ctx context.Context, address, contract string) (*big.Int, error) {
	cc.logger.Debug("Getting NFT balance",
		zap.String("address", address),
		zap.String("contract", contract))

	wallet := common.HexToAddress(address)
	contractAddr := common.HexToAddress(contract)

	balance, err := cc.getNFTBalance(ctx, wallet, contractAddr)
	if err != nil {
		cc.logger.Error("Failed to get NFT balance",
			zap.String("address", address),
			zap.String("contract", contract),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get NFT balance: %w", err)
	}

	cc.logger.Debug("NFT balance retrieved",
		zap.String("address", address),
		zap.String("contract", contract),
		zap.String("balance", balance.String()))
	return balance, nil
}
