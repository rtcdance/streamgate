package nft

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/cachetypes"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/web3/internal/abiutil"
	"go.uber.org/zap"
)

type ERC1155Verifier struct {
	ethClient        EthCaller
	logger           *zap.Logger
	cache            cachetypes.CacheBackend
	cacheTTL         time.Duration
	parsedERC1155ABI abi.ABI
}

func NewERC1155Verifier(ethClient EthCaller, logger *zap.Logger, cache cachetypes.CacheBackend) *ERC1155Verifier {
	return &ERC1155Verifier{
		ethClient:        ethClient,
		logger:           logger,
		cache:            cache,
		cacheTTL:         5 * time.Minute,
		parsedERC1155ABI: abiutil.MustParseABI("ERC-1155", erc1155FullABI),
	}
}

const erc1155FullABI = `[{"constant":true,"inputs":[{"name":"account","type":"address"},{"name":"id","type":"uint256"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"name":"accounts","type":"address[]"},{"name":"ids","type":"uint256[]"}],"name":"balanceOfBatch","outputs":[{"name":"","type":"uint256[]"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"name":"account","type":"address"},{"name":"operator","type":"address"}],"name":"isApprovedForAll","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"name":"id","type":"uint256"}],"name":"uri","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"}]`

func (ev *ERC1155Verifier) VerifyNFTOwnership(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	ev.logger.Debug("Verifying ERC-1155 NFT ownership",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	if !common.IsHexAddress(contractAddress) {
		return false, fmt.Errorf("invalid contract address: %s", contractAddress)
	}
	if !common.IsHexAddress(ownerAddress) {
		return false, fmt.Errorf("invalid owner address: %s", ownerAddress)
	}

	cacheKey := fmt.Sprintf("erc1155:balance:%s:%s:%s", contractAddress, tokenID, ownerAddress)
	if ev.cache != nil {
		if cached, err := ev.cache.Get(cacheKey); err == nil {
			if balance, ok := cached.(*big.Int); ok {
				return balance.Cmp(big.NewInt(0)) > 0, nil
			}
		}
	}

	contractAddr := common.HexToAddress(contractAddress)

	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return false, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	ownerAddr := common.HexToAddress(ownerAddress)

	balance, err := ev.getBalance(ctx, contractAddr, ownerAddr, tokenIDInt)
	if err != nil {
		return false, fmt.Errorf("failed to get balance: %w", err)
	}

	if ev.cache != nil {
		_ = ev.cache.SetWithExpiration(cacheKey, balance, ev.cacheTTL)
	}

	owned := balance.Cmp(big.NewInt(0)) > 0

	ev.logger.Debug("ERC-1155 ownership verified",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress),
		zap.Bool("owned", owned),
		zap.String("balance", balance.String()))

	return owned, nil
}

func (ev *ERC1155Verifier) VerifyBatchNFTOwnership(ctx context.Context, contractAddress string, tokenIDs []string, ownerAddress string) (map[string]bool, error) {
	ev.logger.Debug("Verifying ERC-1155 batch ownership",
		zap.String("contract", contractAddress),
		zap.Int("token_count", len(tokenIDs)),
		zap.String("owner", ownerAddress))

	if !common.IsHexAddress(contractAddress) {
		return nil, fmt.Errorf("invalid contract address: %s", contractAddress)
	}
	if !common.IsHexAddress(ownerAddress) {
		return nil, fmt.Errorf("invalid owner address: %s", ownerAddress)
	}

	contractAddr := common.HexToAddress(contractAddress)

	ownerAddr := common.HexToAddress(ownerAddress)

	tokenIDInts := make([]*big.Int, len(tokenIDs))
	for i, tokenID := range tokenIDs {
		tokenIDInt := new(big.Int)
		if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
			return nil, fmt.Errorf("invalid token ID: %s", tokenID)
		}
		tokenIDInts[i] = tokenIDInt
	}

	balances, err := ev.getBalanceBatch(ctx, contractAddr, ownerAddr, tokenIDInts)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch balance: %w", err)
	}

	results := make(map[string]bool)
	for i, tokenID := range tokenIDs {
		owned := balances[i].Cmp(big.NewInt(0)) > 0
		results[tokenID] = owned
	}

	return results, nil
}

func (ev *ERC1155Verifier) VerifyTotalSupply(ctx context.Context, contractAddress, tokenID string, expectedSupply *big.Int) (bool, error) {
	ev.logger.Debug("Verifying ERC-1155 total supply",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("expected_supply", expectedSupply.String()))

	if !common.IsHexAddress(contractAddress) {
		return false, fmt.Errorf("invalid contract address: %s", contractAddress)
	}

	contractAddr := common.HexToAddress(contractAddress)

	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return false, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	supply, err := ev.getTotalSupply(ctx, contractAddr, tokenIDInt)
	if err != nil {
		return false, fmt.Errorf("failed to get total supply: %w", err)
	}

	valid := supply.Cmp(expectedSupply) == 0

	ev.logger.Debug("ERC-1155 total supply verified",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("expected_supply", expectedSupply.String()),
		zap.String("actual_supply", supply.String()),
		zap.Bool("valid", valid))

	return valid, nil
}

func (ev *ERC1155Verifier) VerifyURI(ctx context.Context, contractAddress, tokenID, expectedURI string) (bool, error) {
	ev.logger.Debug("Verifying ERC-1155 token URI",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("expected_uri", expectedURI))

	if !common.IsHexAddress(contractAddress) {
		return false, fmt.Errorf("invalid contract address: %s", contractAddress)
	}

	contractAddr := common.HexToAddress(contractAddress)

	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return false, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	uri, err := ev.getURI(ctx, contractAddr, tokenIDInt)
	if err != nil {
		return false, fmt.Errorf("failed to get token URI: %w", err)
	}

	valid := uri == expectedURI

	ev.logger.Debug("ERC-1155 token URI verified",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("expected_uri", expectedURI),
		zap.String("actual_uri", uri),
		zap.Bool("valid", valid))

	return valid, nil
}

func (ev *ERC1155Verifier) getBalance(ctx context.Context, contractAddress, account common.Address, tokenID *big.Int) (*big.Int, error) {
	data, err := ev.parsedERC1155ABI.Pack("balanceOf", account, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to pack function call: %w", err)
	}

	result, err := ev.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("contract call failed: %w", err)
	}

	if len(result) < 32 {
		return nil, fmt.Errorf("balanceOf returned insufficient data (len=%d): contract may not exist or is not a valid ERC-1155 contract", len(result))
	}

	var balance *big.Int
	err = ev.parsedERC1155ABI.UnpackIntoInterface(&balance, "balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack result: %w", err)
	}

	return balance, nil
}

func (ev *ERC1155Verifier) getBalanceBatch(ctx context.Context, contractAddress, account common.Address, tokenIDs []*big.Int) ([]*big.Int, error) {
	data, err := ev.parsedERC1155ABI.Pack("balanceOfBatch", []common.Address{account}, tokenIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to pack function call: %w", err)
	}

	result, err := ev.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("contract call failed: %w", err)
	}

	if len(result) < 32 {
		return nil, fmt.Errorf("balanceOfBatch returned insufficient data (len=%d): contract may not exist or is not a valid ERC-1155 contract", len(result))
	}

	var balances []*big.Int
	err = ev.parsedERC1155ABI.UnpackIntoInterface(&balances, "balanceOfBatch", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack result: %w", err)
	}

	return balances, nil
}

func (ev *ERC1155Verifier) getTotalSupply(ctx context.Context, contractAddress common.Address, tokenID *big.Int) (*big.Int, error) {
	return nil, fmt.Errorf("totalSupply is not part of the ERC-1155 standard")
}

func (ev *ERC1155Verifier) getURI(ctx context.Context, contractAddress common.Address, tokenID *big.Int) (string, error) {
	data, err := ev.parsedERC1155ABI.Pack("uri", tokenID)
	if err != nil {
		return "", fmt.Errorf("failed to pack function call: %w", err)
	}

	result, err := ev.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("contract call failed: %w", err)
	}

	if len(result) < 32 {
		return "", fmt.Errorf("uri returned insufficient data (len=%d): contract may not exist or is not a valid ERC-1155 contract", len(result))
	}

	var uri string
	err = ev.parsedERC1155ABI.UnpackIntoInterface(&uri, "uri", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack result: %w", err)
	}

	hexID := fmt.Sprintf("%064x", tokenID)
	uri = strings.ReplaceAll(uri, "{id}", hexID)

	return uri, nil
}

func (ev *ERC1155Verifier) IsERC1155Contract(ctx context.Context, contractAddress string) (bool, error) {
	ev.logger.Debug("Checking if contract is ERC-1155 compliant",
		zap.String("contract", contractAddress))

	if !common.IsHexAddress(contractAddress) {
		return false, fmt.Errorf("invalid contract address: %s", contractAddress)
	}

	contractAddr := common.HexToAddress(contractAddress)

	_, err := ev.getBalance(ctx, contractAddr, contractAddr, big.NewInt(0))
	if err != nil {
		return false, nil
	}

	return true, nil
}

func (ev *ERC1155Verifier) GetTokenInfo(ctx context.Context, contractAddress, tokenID string) (*TokenInfo, error) {
	ev.logger.Debug("Getting ERC-1155 token info",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID))

	if !common.IsHexAddress(contractAddress) {
		return nil, fmt.Errorf("invalid contract address: %s", contractAddress)
	}

	contractAddr := common.HexToAddress(contractAddress)

	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return nil, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	uri, err := ev.getURI(ctx, contractAddr, tokenIDInt)
	if err != nil {
		return nil, fmt.Errorf("failed to get token URI: %w", err)
	}

	info := &TokenInfo{
		ContractAddress: contractAddress,
		TokenID:         tokenID,
		TokenType:       "ERC-1155",
		URI:             uri,
	}

	return info, nil
}

type TokenInfo struct {
	ContractAddress string
	TokenID         string
	TokenType       string
	URI             string
	Metadata        map[string]interface{}
}

func (ev *ERC1155Verifier) VerifyOperatorApproval(ctx context.Context, contractAddress, ownerAddress, operatorAddress string) (bool, error) {
	ev.logger.Debug("Verifying ERC-1155 operator approval",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress),
		zap.String("operator", operatorAddress))

	if !common.IsHexAddress(contractAddress) {
		return false, fmt.Errorf("invalid contract address: %s", contractAddress)
	}
	if !common.IsHexAddress(ownerAddress) {
		return false, fmt.Errorf("invalid owner address: %s", ownerAddress)
	}
	if !common.IsHexAddress(operatorAddress) {
		return false, fmt.Errorf("invalid operator address: %s", operatorAddress)
	}

	contractAddr := common.HexToAddress(contractAddress)

	ownerAddr := common.HexToAddress(ownerAddress)

	operatorAddr := common.HexToAddress(operatorAddress)

	approved, err := ev.isApprovedForAll(ctx, contractAddr, ownerAddr, operatorAddr)
	if err != nil {
		return false, fmt.Errorf("failed to check operator approval: %w", err)
	}

	ev.logger.Debug("ERC-1155 operator approval verified",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress),
		zap.String("operator", operatorAddress),
		zap.Bool("approved", approved))

	return approved, nil
}

func (ev *ERC1155Verifier) isApprovedForAll(ctx context.Context, contractAddress, account, operator common.Address) (bool, error) {
	data, err := ev.parsedERC1155ABI.Pack("isApprovedForAll", account, operator)
	if err != nil {
		return false, fmt.Errorf("failed to pack function call: %w", err)
	}

	result, err := ev.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return false, fmt.Errorf("contract call failed: %w", err)
	}

	if len(result) < 32 {
		return false, fmt.Errorf("isApprovedForAll returned insufficient data (len=%d): contract may not exist or is not a valid ERC-1155 contract", len(result))
	}

	var approved bool
	err = ev.parsedERC1155ABI.UnpackIntoInterface(&approved, "isApprovedForAll", result)
	if err != nil {
		return false, fmt.Errorf("failed to unpack result: %w", err)
	}

	return approved, nil
}
