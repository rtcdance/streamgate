package web3

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// BlockchainProvider defines the interface for blockchain interactions
type BlockchainProvider interface {
	VerifyNFTOwnership(ctx context.Context, req VerifyRequest) (bool, error)
	GetNFTBalance(ctx context.Context, address, contract string) (*big.Int, error)
	GetNFTMetadata(ctx context.Context, contract, tokenID string) (*NFTMetadata, error)
	HealthCheck(ctx context.Context) error
}

// VerifyRequest represents a verification request
type VerifyRequest struct {
	WalletAddress string
	Contract      string
	TokenID       string
	MinBalance    int
	Mode          GatingMode
}

// GatingMode represents the gating mode
type GatingMode int

const (
	GatingAny GatingMode = iota
	GatingMinBalance
	GatingSpecificID
	GatingCombination
)

// BlockInfo contains block information
type BlockInfo struct {
	Number       uint64
	Hash         string
	ParentHash   string
	Timestamp    uint64
	Miner        string
	GasUsed      uint64
	GasLimit     uint64
	Difficulty   string
	Transactions uint64
}

// TransactionInfo contains transaction information
type TransactionInfo struct {
	Hash      string
	From      string
	To        string
	Value     string
	Gas       uint64
	GasPrice  string
	Nonce     uint64
	Data      string
	IsPending bool
}

// ReceiptInfo contains transaction receipt information
type ReceiptInfo struct {
	TransactionHash string        `json:"transaction_hash"`
	BlockNumber     uint64        `json:"block_number"`
	BlockHash       string        `json:"block_hash"`
	GasUsed         uint64        `json:"gas_used"`
	Status          uint64        `json:"status"`
	ContractAddress string        `json:"contract_address"`
	LogCount        uint64        `json:"log_count"`
	Events          []ParsedEvent `json:"events,omitempty"`
}

// NFTMetadata contains NFT metadata information
type NFTMetadata struct {
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Image           string         `json:"image"`
	Attributes      []NFTAttribute `json:"attributes"`
	ContractAddress string         `json:"contract_address,omitempty"`
	TokenID         string         `json:"token_id,omitempty"`
}

// NFTAttribute represents an NFT attribute
type NFTAttribute struct {
	TraitType string      `json:"trait_type"`
	Value     interface{} `json:"value"`
}

// GetBalance gets the balance of an address
func (cc *ChainClient) GetBalance(ctx context.Context, address string) (*big.Int, error) {
	cc.logger.Debug("Getting balance", zap.String("address", address))

	addr := common.HexToAddress(address)
	balance, err := withChainClient(ctx, cc, "BalanceAt", func(client *ethclient.Client) (*big.Int, error) {
		return client.BalanceAt(ctx, addr, nil)
	})
	if err != nil {
		cc.logger.Error("Failed to get balance",
			zap.String("address", address),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	cc.logger.Debug("Balance retrieved",
		zap.String("address", address),
		zap.String("balance", balance.String()))
	return balance, nil
}

// GetNonce gets the nonce of an address
func (cc *ChainClient) GetNonce(ctx context.Context, address string) (uint64, error) {
	cc.logger.Debug("Getting nonce", zap.String("address", address))

	addr := common.HexToAddress(address)
	nonce, err := withChainClient(ctx, cc, "PendingNonceAt", func(client *ethclient.Client) (uint64, error) {
		return client.PendingNonceAt(ctx, addr)
	})
	if err != nil {
		cc.logger.Error("Failed to get nonce",
			zap.String("address", address),
			zap.Error(err))
		return 0, fmt.Errorf("failed to get nonce: %w", err)
	}

	cc.logger.Debug("Nonce retrieved",
		zap.String("address", address),
		zap.Uint64("nonce", nonce))
	return nonce, nil
}

// GetGasPrice gets the current gas price
func (cc *ChainClient) GetGasPrice(ctx context.Context) (*big.Int, error) {
	cc.logger.Debug("Getting gas price")

	gasPrice, err := withChainClient(ctx, cc, "SuggestGasPrice", func(client *ethclient.Client) (*big.Int, error) {
		return client.SuggestGasPrice(ctx)
	})
	if err != nil {
		cc.logger.Error("Failed to get gas price", zap.Error(err))
		return nil, fmt.Errorf("failed to get gas price: %w", err)
	}

	cc.logger.Debug("Gas price retrieved", zap.String("gas_price", gasPrice.String()))
	return gasPrice, nil
}

// SuggestGasTipCap returns the suggested priority fee (tip) for EIP-1559 transactions.
func (cc *ChainClient) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	cc.logger.Debug("Getting gas tip cap")

	tipCap, err := withChainClient(ctx, cc, "SuggestGasTipCap", func(client *ethclient.Client) (*big.Int, error) {
		return client.SuggestGasTipCap(ctx)
	})
	if err != nil {
		cc.logger.Error("Failed to get gas tip cap", zap.Error(err))
		return nil, fmt.Errorf("failed to get gas tip cap: %w", err)
	}

	cc.logger.Debug("Gas tip cap retrieved", zap.String("tip_cap", tipCap.String()))
	return tipCap, nil
}

// HeaderByNumber returns the block header for the given block number (nil = latest).
func (cc *ChainClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	header, err := withChainClient(ctx, cc, "HeaderByNumber", func(client *ethclient.Client) (*types.Header, error) {
		return client.HeaderByNumber(ctx, number)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get header: %w", err)
	}
	return header, nil
}

// FeeHistory returns the fee history for the given block range.
// blockCount is the number of blocks to query (max 1024).
// lastBlock is the newest block in the range (nil = latest).
// rewardPercentiles is a list of percentile values (0-100) to compute suggested priority fees.
func (cc *ChainClient) FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error) {
	cc.logger.Debug("Getting fee history",
		zap.Uint64("block_count", blockCount),
		zap.Int("percentile_count", len(rewardPercentiles)))

	feeHistory, err := withChainClient(ctx, cc, "FeeHistory", func(client *ethclient.Client) (*ethereum.FeeHistory, error) {
		return client.FeeHistory(ctx, blockCount, lastBlock, rewardPercentiles)
	})
	if err != nil {
		cc.logger.Error("Failed to get fee history", zap.Error(err))
		return nil, fmt.Errorf("failed to get fee history: %w", err)
	}

	cc.logger.Debug("Fee history retrieved",
		zap.Int("base_fee_count", len(feeHistory.BaseFee)),
		zap.Int("gas_used_ratio_count", len(feeHistory.GasUsedRatio)))
	return feeHistory, nil
}

// EstimateGas estimates gas for a transaction
func (cc *ChainClient) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	cc.logger.Debug("Estimating gas")

	gas, err := withChainClient(ctx, cc, "EstimateGas", func(client *ethclient.Client) (uint64, error) {
		return client.EstimateGas(ctx, msg)
	})
	if err != nil {
		cc.logger.Error("Failed to estimate gas", zap.Error(err))
		return 0, fmt.Errorf("failed to estimate gas: %w", err)
	}

	cc.logger.Debug("Gas estimated",
		zap.Uint64("gas", gas))
	return gas, nil
}

// GetBlockNumber gets the current block number
func (cc *ChainClient) GetBlockNumber(ctx context.Context) (uint64, error) {
	cc.logger.Debug("Getting block number")

	blockNumber, err := withChainClient(ctx, cc, "BlockNumber", func(client *ethclient.Client) (uint64, error) {
		return client.BlockNumber(ctx)
	})
	if err != nil {
		cc.logger.Error("Failed to get block number", zap.Error(err))
		return 0, fmt.Errorf("failed to get block number: %w", err)
	}

	cc.logger.Debug("Block number retrieved",
		zap.Uint64("block_number", blockNumber))
	return blockNumber, nil
}

// GetBlockByNumber gets a block by number
func (cc *ChainClient) GetBlockByNumber(ctx context.Context, blockNumber *big.Int) (*BlockInfo, error) {
	cc.logger.Debug("Getting block", zap.String("block_number", blockNumber.String()))

	block, err := withChainClient(ctx, cc, "BlockByNumber", func(client *ethclient.Client) (*types.Block, error) {
		return client.BlockByNumber(ctx, blockNumber)
	})
	if err != nil {
		cc.logger.Error("Failed to get block",
			zap.String("block_number", blockNumber.String()),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	blockInfo := &BlockInfo{
		Number:       block.Number().Uint64(),
		Hash:         block.Hash().Hex(),
		ParentHash:   block.ParentHash().Hex(),
		Timestamp:    block.Time(),
		Miner:        block.Coinbase().Hex(),
		GasUsed:      block.GasUsed(),
		GasLimit:     block.GasLimit(),
		Difficulty:   block.Difficulty().String(),
		Transactions: uint64(len(block.Transactions())),
	}

	cc.logger.Debug("Block retrieved", zap.String("block_number", blockNumber.String()))
	return blockInfo, nil
}

// GetTransactionByHash gets a transaction by hash
func (cc *ChainClient) GetTransactionByHash(ctx context.Context, txHash string) (*TransactionInfo, error) {
	cc.logger.Debug("Getting transaction", zap.String("tx_hash", txHash))

	hash := common.HexToHash(txHash)
	txResult, err := withChainClient(ctx, cc, "TransactionByHash", func(client *ethclient.Client) (struct {
		tx        *types.Transaction
		isPending bool
	}, error) {
		tx, isPending, err := client.TransactionByHash(ctx, hash)
		return struct {
			tx        *types.Transaction
			isPending bool
		}{tx: tx, isPending: isPending}, err
	})
	if err != nil {
		cc.logger.Error("Failed to get transaction",
			zap.String("tx_hash", txHash),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	tx := txResult.tx
	isPending := txResult.isPending

	signer := types.LatestSignerForChainID(tx.ChainId())
	from, err := types.Sender(signer, tx)
	if err != nil {
		cc.logger.Error("Failed to get transaction sender",
			zap.String("tx_hash", txHash),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get transaction sender: %w", err)
	}

	txInfo := &TransactionInfo{
		Hash:      tx.Hash().Hex(),
		From:      from.Hex(),
		To:        tx.To().Hex(),
		Value:     tx.Value().String(),
		Gas:       tx.Gas(),
		GasPrice:  tx.GasPrice().String(),
		Nonce:     tx.Nonce(),
		Data:      fmt.Sprintf("0x%x", tx.Data()),
		IsPending: isPending,
	}

	cc.logger.Debug("Transaction retrieved", zap.String("tx_hash", txHash))
	return txInfo, nil
}

// GetTransactionReceipt gets a transaction receipt
func (cc *ChainClient) GetTransactionReceipt(ctx context.Context, txHash string) (*ReceiptInfo, error) {
	cc.logger.Debug("Getting transaction receipt", zap.String("tx_hash", txHash))

	hash := common.HexToHash(txHash)
	receipt, err := withChainClient(ctx, cc, "TransactionReceipt", func(client *ethclient.Client) (*types.Receipt, error) {
		return client.TransactionReceipt(ctx, hash)
	})
	if err != nil {
		cc.logger.Error("Failed to get transaction receipt",
			zap.String("tx_hash", txHash),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	receiptInfo := &ReceiptInfo{
		TransactionHash: receipt.TxHash.Hex(),
		BlockNumber:     receipt.BlockNumber.Uint64(),
		BlockHash:       receipt.BlockHash.Hex(),
		GasUsed:         receipt.GasUsed,
		Status:          receipt.Status,
		ContractAddress: receipt.ContractAddress.Hex(),
		LogCount:        uint64(len(receipt.Logs)),
	}

	cc.logger.Debug("Transaction receipt retrieved", zap.String("tx_hash", txHash))
	return receiptInfo, nil
}

// GetNFTMetadata retrieves NFT metadata from the blockchain
func (cc *ChainClient) GetNFTMetadata(ctx context.Context, contractAddress, tokenID string) (*NFTMetadata, error) {
	cc.logger.Debug("Getting NFT metadata",
		zap.String("contract_address", contractAddress),
		zap.String("token_id", tokenID))

	contract := common.HexToAddress(contractAddress)

	tokenURI, err := cc.getTokenURI(ctx, contract, tokenID)
	if err != nil {
		cc.logger.Error("Failed to get token URI",
			zap.String("contract_address", contractAddress),
			zap.String("token_id", tokenID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get token URI: %w", err)
	}

	metadata, err := cc.fetchMetadataFromURI(ctx, tokenURI)
	if err != nil {
		cc.logger.Error("Failed to fetch metadata from URI",
			zap.String("token_uri", tokenURI),
			zap.Error(err))
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	metadata.ContractAddress = contractAddress
	metadata.TokenID = tokenID

	cc.logger.Debug("NFT metadata retrieved",
		zap.String("contract_address", contractAddress),
		zap.String("token_id", tokenID))
	return metadata, nil
}

// getTokenURI retrieves the tokenURI from an ERC721 contract
func (cc *ChainClient) getTokenURI(ctx context.Context, contract common.Address, tokenID string) (string, error) {
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return "", fmt.Errorf("invalid token id: %s", tokenID)
	}

	data, err := erc721TokenURIABI.Pack("tokenURI", tokenIDInt)
	if err != nil {
		return "", fmt.Errorf("failed to pack tokenURI call: %w", err)
	}

	result, err := withChainClient(ctx, cc, "CallContract(tokenURI)", func(client *ethclient.Client) ([]byte, error) {
		return client.CallContract(ctx, ethereum.CallMsg{
			To:   &contract,
			Data: data,
		}, nil)
	})
	if err != nil {
		return "", fmt.Errorf("failed to call tokenURI: %w", err)
	}

	values, err := erc721TokenURIABI.Unpack("tokenURI", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack tokenURI result: %w", err)
	}
	if len(values) != 1 {
		return "", fmt.Errorf("unexpected tokenURI result length: %d", len(values))
	}

	tokenURI, ok := values[0].(string)
	if !ok {
		return "", fmt.Errorf("unexpected tokenURI result type: %T", values[0])
	}

	return tokenURI, nil
}

// fetchMetadataFromURI fetches metadata from a URI with SSRF protection.
func (cc *ChainClient) fetchMetadataFromURI(ctx context.Context, uri string) (*NFTMetadata, error) {
	var metadata NFTMetadata
	if err := safeFetchURI(ctx, uri, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

// getNFTBalance gets the balance of NFTs for a wallet (internal helper)
func (cc *ChainClient) getNFTBalance(ctx context.Context, wallet, contract common.Address) (*big.Int, error) {
	parsedABI, err := getOrParseABI(balanceOfABIJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC-721 ABI: %w", err)
	}

	data, err := parsedABI.Pack("balanceOf", wallet)
	if err != nil {
		return nil, fmt.Errorf("failed to pack balanceOf call: %w", err)
	}

	result, err := withChainClient(ctx, cc, "CallContract(balanceOf)", func(client *ethclient.Client) ([]byte, error) {
		return client.CallContract(ctx, ethereum.CallMsg{
			To:   &contract,
			Data: data,
		}, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to call balanceOf: %w", err)
	}

	values, err := parsedABI.Unpack("balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack balanceOf result: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unexpected balanceOf result length: %d", len(values))
	}

	balance, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected balanceOf result type: %T", values[0])
	}

	return balance, nil
}

// getNFTBalanceAtBlock calls balanceOf at a specific block tag (e.g. BlockTagSafe)
// to protect against reorgs. Falls back to getNFTBalance (latest) on error.
func (cc *ChainClient) getNFTBalanceAtBlock(ctx context.Context, wallet, contract common.Address, blockTag BlockTag) (*big.Int, error) {
	parsedABI, err := getOrParseABI(balanceOfABIJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC-721 ABI: %w", err)
	}

	data, err := parsedABI.Pack("balanceOf", wallet)
	if err != nil {
		return nil, fmt.Errorf("failed to pack balanceOf call: %w", err)
	}

	result, err := cc.CallContractAtBlock(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, blockTag)
	if err != nil {
		cc.logger.Error("balanceOf at block tag failed, refusing to fall back to latest for safety",
			zap.String("block_tag", string(blockTag)),
			zap.Error(err))
		return nil, fmt.Errorf("balanceOf at block tag %q failed: %w (fallback to latest disabled for reorg safety)", blockTag, err)
	}

	values, err := parsedABI.Unpack("balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack balanceOf result: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unexpected balanceOf result length: %d", len(values))
	}

	balance, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected balanceOf result type: %T", values[0])
	}

	return balance, nil
}

// getNFTOwnerAtBlock retrieves the owner of an NFT at a specific block tag.
// Using BlockTagSafe protects against reorgs that could invalidate ownership.
func (cc *ChainClient) getNFTOwnerAtBlock(ctx context.Context, contract common.Address, tokenID string, blockTag BlockTag) (common.Address, error) {
	data, err := cc.packOwnerOfCall(tokenID)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to pack ownerOf call: %w", err)
	}
	result, err := cc.CallContractAtBlock(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, blockTag)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to call ownerOf at %s: %w", blockTag, err)
	}

	values, err := erc721OwnerOfABI.Unpack("ownerOf", result)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to unpack ownerOf result: %w", err)
	}
	if len(values) != 1 {
		return common.Address{}, fmt.Errorf("unexpected ownerOf result length: %d", len(values))
	}

	owner, ok := values[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected ownerOf result type: %T", values[0])
	}
	return owner, nil
}

// packOwnerOfCall packs the ownerOf ABI call for the given token ID.
func (cc *ChainClient) packOwnerOfCall(tokenID string) ([]byte, error) {
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return nil, fmt.Errorf("invalid token id: %s", tokenID)
	}
	return erc721OwnerOfABI.Pack("ownerOf", tokenIDInt)
}
