package web3

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"streamgate/pkg/monitoring"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// Pre-parsed ERC-721 ABIs for chain.go contract calls.
var erc721OwnerOfABI = mustParseABI("ERC721-ownerOf", `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`)
var erc721TokenURIABI = mustParseABI("ERC721-tokenURI", `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"tokenURI","outputs":[{"name":"","type":"string"}],"type":"function"}]`)

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

// ChainClient handles blockchain interactions
type ChainClient struct {
	mu          sync.RWMutex
	client      *ethclient.Client
	rpcURL      string
	rpcURLs     []string
	rpcStates   []rpcEndpointState
	activeRPC   int
	chainID     int64
	logger      *zap.Logger
	rateLimiter *RPCRateLimiter
}

type rpcEndpointState struct {
	Failures      int
	LastFailureAt time.Time
	CooldownUntil time.Time
	Score         float64
	LastLatency   time.Duration
}

// RPCStatus describes the current runtime status of an RPC endpoint.
type RPCStatus struct {
	URL           string    `json:"url"`
	IsActive      bool      `json:"is_active"`
	Failures      int       `json:"failures"`
	LastFailureAt time.Time `json:"last_failure_at,omitempty"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
	Score         float64   `json:"score"`
	LastLatencyMs int64     `json:"last_latency_ms,omitempty"`
}

const (
	rpcFailureCooldown  = 30 * time.Second
	rpcScoreInitial     = 1.0
	rpcScoreDecay       = 0.9
	rpcLatencyThreshold = 5.0
)

// NewChainClient creates a new chain client
func NewChainClient(rpcURL string, chainID int64, logger *zap.Logger) (*ChainClient, error) {
	return NewChainClientWithFallback([]string{rpcURL}, chainID, logger)
}

// NewChainClientWithFallback creates a chain client with multiple RPC candidates.
func NewChainClientWithFallback(rpcURLs []string, chainID int64, logger *zap.Logger) (*ChainClient, error) {
	normalizedRPCs := make([]string, 0, len(rpcURLs))
	for _, rpcURL := range rpcURLs {
		rpcURL = strings.TrimSpace(rpcURL)
		if rpcURL != "" {
			normalizedRPCs = append(normalizedRPCs, rpcURL)
		}
	}
	if len(normalizedRPCs) == 0 {
		return nil, fmt.Errorf("no rpc urls configured for chain %d", chainID)
	}

	states := make([]rpcEndpointState, len(normalizedRPCs))
	for i := range states {
		states[i].Score = rpcScoreInitial
	}
	cc := &ChainClient{
		rpcURL:    normalizedRPCs[0],
		rpcURLs:   normalizedRPCs,
		rpcStates: states,
		activeRPC: 0,
		chainID:   chainID,
		logger:    logger,
	}

	if err := cc.connectAny(); err != nil {
		return nil, err
	}

	return cc, nil
}

// GetEthClient returns the underlying ethclient.Client
func (cc *ChainClient) GetEthClient() *ethclient.Client {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.client
}

func (cc *ChainClient) VerifyNFTOwnership(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	return withChainClient(ctx, cc, "VerifyNFTOwnership", func(client *ethclient.Client) (bool, error) {
		v := NewNFTVerifier(client, cc.logger)
		return v.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
	})
}

// GetNFTBalance returns the NFT balance for an owner on this chain.
func (cc *ChainClient) GetNFTBalance(ctx context.Context, contractAddress, ownerAddress string) (*big.Int, error) {
	return withChainClient(ctx, cc, "GetNFTBalance", func(client *ethclient.Client) (*big.Int, error) {
		v := NewNFTVerifier(client, cc.logger)
		return v.GetNFTBalance(ctx, contractAddress, ownerAddress)
	})
}

// VerifyNFTOwnershipAutoDetect detects the token standard and verifies ownership.
func (cc *ChainClient) VerifyNFTOwnershipAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	return withChainClient(ctx, cc, "VerifyNFTOwnershipAutoDetect", func(client *ethclient.Client) (bool, error) {
		v := NewNFTVerifier(client, cc.logger)
		return v.VerifyNFTOwnershipAutoDetect(ctx, contractAddress, tokenID, ownerAddress)
	})
}

// VerifyNFTCollectionAutoDetect detects the token standard and verifies collection ownership.
func (cc *ChainClient) VerifyNFTCollectionAutoDetect(ctx context.Context, contractAddress, ownerAddress string) (bool, error) {
	return withChainClient(ctx, cc, "VerifyNFTCollectionAutoDetect", func(client *ethclient.Client) (bool, error) {
		v := NewNFTVerifier(client, cc.logger)
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
		// Fallback to latest if the RPC doesn't support safe/finalized
		cc.logger.Warn("Block tag not supported, falling back to latest",
			zap.String("block_tag", string(blockTag)),
			zap.Error(err))
		return withChainClient(ctx, cc, "CallContractAtBlock_fallback", func(client *ethclient.Client) ([]byte, error) {
			return client.CallContract(ctx, msg, nil)
		})
	}

	return result, err
}

// SetRateLimiter sets the RPC rate limiter. Pass nil to disable rate limiting.
func (cc *ChainClient) SetRateLimiter(rl *RPCRateLimiter) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.rateLimiter = rl
	if rl != nil {
		cc.logger.Info("RPC rate limiter configured",
			zap.Float64("rate", rl.rate),
			zap.Float64("burst", rl.maxTokens))
	}
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

	// Get sender address from transaction
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

// Close closes the client connection
func (cc *ChainClient) Close() {
	cc.mu.Lock()
	if cc.client != nil {
		cc.client.Close()
		cc.client = nil
	}
	cc.mu.Unlock()
	cc.logger.Info("Chain client closed")
}

// GetRPCStatuses returns the runtime status of all configured RPC endpoints.
func (cc *ChainClient) GetRPCStatuses() []RPCStatus {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	statuses := make([]RPCStatus, 0, len(cc.rpcURLs))
	for idx, rpcURL := range cc.rpcURLs {
		state := cc.rpcStates[idx]
		statuses = append(statuses, RPCStatus{
			URL:           rpcURL,
			IsActive:      idx == cc.activeRPC,
			Failures:      state.Failures,
			LastFailureAt: state.LastFailureAt,
			CooldownUntil: state.CooldownUntil,
			Score:         state.Score,
			LastLatencyMs: state.LastLatency.Milliseconds(),
		})
	}
	return statuses
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
		// Use BlockTagSafe for specific token ID checks to protect
		// against reorgs that could invalidate ownership.
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

// GetNFTBalance gets the NFT balance for a wallet
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

// getNFTBalance gets the balance of NFTs for a wallet (internal helper)
func (cc *ChainClient) getNFTBalance(ctx context.Context, wallet, contract common.Address) (*big.Int, error) {
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(`[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"}]`)))
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
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(`[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"}]`)))
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
		// Fallback to latest block
		cc.logger.Warn("balanceOf at block tag failed, falling back to latest",
			zap.String("block_tag", string(blockTag)),
			zap.Error(err))
		return cc.getNFTBalance(ctx, wallet, contract)
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

// getNFTOwner gets the owner of a specific NFT (internal helper)
//
//nolint:unused
func (cc *ChainClient) getNFTOwner(ctx context.Context, contract common.Address, tokenID string) (common.Address, error) {
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(`[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`)))
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to parse ERC-721 ABI: %w", err)
	}

	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return common.Address{}, fmt.Errorf("invalid token id: %s", tokenID)
	}

	data, err := parsedABI.Pack("ownerOf", tokenIDInt)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to pack ownerOf call: %w", err)
	}

	result, err := withChainClient(ctx, cc, "CallContract(ownerOf)", func(client *ethclient.Client) ([]byte, error) {
		return client.CallContract(ctx, ethereum.CallMsg{
			To:   &contract,
			Data: data,
		}, nil)
	})
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to call ownerOf: %w", err)
	}

	values, err := parsedABI.Unpack("ownerOf", result)
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

// updateRPCScores updates the weighted score for an RPC endpoint.
// Uses exponential moving average: recent results weigh more.
// Success latency: measured against the threshold (5s).
// Failure: halves the current score.
func (cc *ChainClient) updateRPCScores(idx int, latency time.Duration, success bool) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if idx < 0 || idx >= len(cc.rpcStates) {
		return
	}
	state := cc.rpcStates[idx]
	state.LastLatency = latency
	if success {
		latencyScore := 1.0 - (latency.Seconds() / rpcLatencyThreshold)
		if latencyScore < 0 {
			latencyScore = 0
		}
		state.Score = state.Score*rpcScoreDecay + latencyScore*(1.0-rpcScoreDecay)
		if state.Failures > 0 {
			state.Failures--
		}
	} else {
		state.Score *= 0.5
	}
	if state.Score < 0 {
		state.Score = 0
	}
	cc.rpcStates[idx] = state
}

// sortedRPCScores returns endpoint indices sorted by score descending.
func (cc *ChainClient) sortedRPCScores() []int {
	indices := make([]int, len(cc.rpcURLs))
	for i := range indices {
		indices[i] = i
	}
	cc.mu.RLock()
	scores := make([]float64, len(cc.rpcURLs))
	for i, st := range cc.rpcStates {
		scores[i] = st.Score
	}
	cc.mu.RUnlock()

	// Insertion sort by score descending
	for i := 1; i < len(indices); i++ {
		key := indices[i]
		j := i - 1
		for j >= 0 && scores[indices[j]] < scores[key] {
			indices[j+1] = indices[j]
			j--
		}
		indices[j+1] = key
	}
	return indices
}

// GetRPCScores returns a map of RPC URL to score for monitoring.
func (cc *ChainClient) GetRPCScores() map[string]float64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	scores := make(map[string]float64, len(cc.rpcURLs))
	for i, url := range cc.rpcURLs {
		scores[url] = cc.rpcStates[i].Score
	}
	return scores
}

// HealthCheck performs a health check on the blockchain connection
func (cc *ChainClient) HealthCheck(ctx context.Context) error {
	cc.logger.Debug("Performing health check")

	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Try to get the latest block number
	blockNumber, err := withChainClient(healthCtx, cc, "HealthCheck.BlockNumber", func(client *ethclient.Client) (uint64, error) {
		return client.BlockNumber(healthCtx)
	})
	if err != nil {
		cc.logger.Error("Health check failed: cannot get block number", zap.Error(err))
		return fmt.Errorf("health check failed: %w", err)
	}

	// Try to get chain ID to verify connection
	chainID, err := withChainClient(healthCtx, cc, "HealthCheck.ChainID", func(client *ethclient.Client) (*big.Int, error) {
		return client.ChainID(healthCtx)
	})
	if err != nil {
		cc.logger.Error("Health check failed: cannot get chain ID", zap.Error(err))
		return fmt.Errorf("health check failed: %w", err)
	}

	// Verify chain ID matches configured value to detect misconfigured RPC
	if cc.chainID != 0 && chainID.Int64() != cc.chainID {
		cc.logger.Error("Health check failed: chain ID mismatch",
			zap.Int64("configured_chain_id", cc.chainID),
			zap.Int64("rpc_chain_id", chainID.Int64()))
		return fmt.Errorf("chain ID mismatch: configured=%d, rpc=%d", cc.chainID, chainID.Int64())
	}

	cc.logger.Info("Health check passed",
		zap.Uint64("block_number", blockNumber),
		zap.Int64("chain_id", chainID.Int64()),
		zap.String("rpc_url", cc.rpcURL))

	return nil
}

func (cc *ChainClient) connectAny() error {
	var lastErr error
	// Try endpoints in score-descending order, skipping those in cooldown
	for _, idx := range cc.sortedRPCScores() {
		if !cc.endpointReady(idx, false) {
			continue
		}
		client, chainIDFromRPC, err := cc.connectAt(idx)
		if err != nil {
			cc.recordEndpointFailure(idx)
			lastErr = err
			continue
		}
		cc.setActiveClient(idx, client, true)
		cc.logger.Info("Connected to blockchain",
			zap.Int64("configured_chain_id", cc.chainID),
			zap.Int64("rpc_chain_id", chainIDFromRPC.Int64()),
			zap.String("rpc_url", cc.rpcURL))
		return nil
	}
	// Second pass: try all endpoints (bypassing cooldown)
	for _, idx := range cc.sortedRPCScores() {
		client, chainIDFromRPC, err := cc.connectAt(idx)
		if err != nil {
			cc.recordEndpointFailure(idx)
			lastErr = err
			continue
		}
		cc.setActiveClient(idx, client, true)
		cc.logger.Info("Connected to blockchain after cooldown bypass",
			zap.Int64("configured_chain_id", cc.chainID),
			zap.Int64("rpc_chain_id", chainIDFromRPC.Int64()),
			zap.String("rpc_url", cc.rpcURL))
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no rpc urls available")
	}
	cc.logger.Error("Failed to connect to blockchain", zap.Error(lastErr))
	return fmt.Errorf("failed to connect to blockchain: %w", lastErr)
}

func (cc *ChainClient) connectAt(idx int) (*ethclient.Client, *big.Int, error) {
	rpcURL := cc.rpcURLs[idx]
	cc.logger.Info("Connecting to blockchain",
		zap.String("rpc_url", rpcURL),
		zap.Int64("chain_id", cc.chainID))

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chainIDFromRPC, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	if cc.chainID != 0 && chainIDFromRPC.Int64() != cc.chainID {
		client.Close()
		return nil, nil, fmt.Errorf("unexpected chain id from %s: got %d want %d", rpcURL, chainIDFromRPC.Int64(), cc.chainID)
	}

	return client, chainIDFromRPC, nil
}

func (cc *ChainClient) setActiveClient(idx int, client *ethclient.Client, resetFailures bool) {
	cc.mu.Lock()
	oldClient := cc.client
	cc.client = client
	cc.activeRPC = idx
	cc.rpcURL = cc.rpcURLs[idx]
	if resetFailures {
		cc.rpcStates[idx] = rpcEndpointState{Score: rpcScoreInitial}
	}
	cc.mu.Unlock()

	if oldClient != nil {
		go func() {
			time.Sleep(30 * time.Second)
			oldClient.Close()
		}()
	}
}

func (cc *ChainClient) failover() error {
	var lastErr error
	// Try endpoints in score-descending order, skipping the current active endpoint
	active := cc.getActiveRPCIndex()
	for _, idx := range cc.sortedRPCScores() {
		if idx == active {
			continue
		}
		if !cc.endpointReady(idx, false) {
			continue
		}
		client, chainIDFromRPC, err := cc.connectAt(idx)
		if err != nil {
			cc.recordEndpointFailure(idx)
			lastErr = err
			continue
		}
		cc.setActiveClient(idx, client, true)
		cc.logger.Warn("Switched blockchain RPC endpoint (scored)",
			zap.String("rpc_url", cc.rpcURL),
			zap.Int64("rpc_chain_id", chainIDFromRPC.Int64()))
		return nil
	}
	// Second pass: try all non-active endpoints bypassing cooldown
	for _, idx := range cc.sortedRPCScores() {
		if idx == active {
			continue
		}
		client, chainIDFromRPC, err := cc.connectAt(idx)
		if err != nil {
			cc.recordEndpointFailure(idx)
			lastErr = err
			continue
		}
		cc.setActiveClient(idx, client, true)
		cc.logger.Warn("Switched blockchain RPC endpoint after cooldown bypass (scored)",
			zap.String("rpc_url", cc.rpcURL),
			zap.Int64("rpc_chain_id", chainIDFromRPC.Int64()))
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no failover rpc available")
	}
	return lastErr
}

func withChainClient[T any](ctx context.Context, cc *ChainClient, op string, fn func(*ethclient.Client) (T, error)) (T, error) {
	var zero T

	// Apply rate limiting before any RPC call
	cc.mu.RLock()
	limiter := cc.rateLimiter
	cc.mu.RUnlock()
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return zero, fmt.Errorf("rpc rate limited: %w", err)
		}
	}

	cc.mu.RLock()
	client := cc.client
	total := len(cc.rpcURLs)
	fromRPC := cc.rpcURL
	cc.mu.RUnlock()

	if client == nil {
		if err := cc.connectAny(); err != nil {
			return zero, err
		}
		cc.mu.RLock()
		client = cc.client
		fromRPC = cc.rpcURL
		cc.mu.RUnlock()
	}

	// Measure latency of the initial attempt
	start := time.Now()
	result, err := fn(client)
	latency := time.Since(start)
	monitoring.RPCLatencySeconds.WithLabelValues(op, fromRPC).Observe(latency.Seconds())
	cc.updateRPCScores(cc.getActiveRPCIndex(), latency, err == nil)

	if err == nil || total <= 1 {
		return result, err
	}

	cc.logger.Warn("RPC operation failed, attempting failover",
		zap.String("operation", op),
		zap.String("rpc_url", cc.rpcURL),
		zap.Error(err))
	cc.recordEndpointFailure(cc.getActiveRPCIndex())

	lastErr := err
	for attempts := 1; attempts < total; attempts++ {
		if failoverErr := cc.failover(); failoverErr != nil {
			lastErr = failoverErr
			continue
		}
		cc.mu.RLock()
		client = cc.client
		toRPC := cc.rpcURL
		cc.mu.RUnlock()

		// Record failover event
		monitoring.RPCFailoverTotal.WithLabelValues(op, fromRPC, toRPC).Inc()

		start = time.Now()
		result, err = fn(client)
		latency = time.Since(start)
		monitoring.RPCLatencySeconds.WithLabelValues(op, toRPC).Observe(latency.Seconds())
		cc.updateRPCScores(cc.getActiveRPCIndex(), latency, err == nil)

		if err == nil {
			return result, nil
		}
		lastErr = err
		cc.logger.Warn("RPC operation failed on fallback endpoint",
			zap.String("operation", op),
			zap.String("rpc_url", cc.rpcURL),
			zap.Error(err))
	}

	if isPermanentRPCError(lastErr) {
		return zero, NewPermanentError(fmt.Sprintf("%s failed after rpc failover attempts", op), lastErr)
	}
	return zero, NewRetryableError(fmt.Sprintf("%s failed after rpc failover attempts", op), lastErr)
}

// isPermanentRPCError inspects an RPC error to determine if it represents a
// permanent failure that will not succeed on retry (e.g. contract revert,
// invalid opcode, out of gas). When uncertain, returns false so the caller
// defaults to retryable — this is the safer choice for transient RPC issues.
func isPermanentRPCError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	permanentPatterns := []string{
		"execution reverted",
		"revert",
		"invalid opcode",
		"out of gas",
		"invalid jump destination",
		"stack limit reached",
		"contract creation code storage out of gas",
		"nonce too low",
		"insufficient funds",
		"already known",
	}
	for _, pattern := range permanentPatterns {
		if strings.Contains(strings.ToLower(msg), pattern) {
			return true
		}
	}
	return false
}

func (cc *ChainClient) getActiveRPCIndex() int {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.activeRPC
}

func (cc *ChainClient) recordEndpointFailure(idx int) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if idx < 0 || idx >= len(cc.rpcStates) {
		return
	}
	state := cc.rpcStates[idx]
	state.Failures++
	state.LastFailureAt = time.Now()
	state.CooldownUntil = state.LastFailureAt.Add(rpcFailureCooldown)
	cc.rpcStates[idx] = state
}

func (cc *ChainClient) endpointReady(idx int, allowCooling bool) bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if idx < 0 || idx >= len(cc.rpcStates) {
		return false
	}
	if allowCooling {
		return true
	}
	state := cc.rpcStates[idx]
	return state.CooldownUntil.IsZero() || time.Now().After(state.CooldownUntil)
}

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

// SendTransaction sends a signed transaction through the current RPC endpoint.
// Unlike read operations, it does NOT failover — sending the same signed tx to
// multiple RPCs risks duplicate submission. The caller should handle retries by
// building a new tx with a fresh nonce.
func (cc *ChainClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	cc.mu.RLock()
	client := cc.client
	cc.mu.RUnlock()

	if client == nil {
		if err := cc.connectAny(); err != nil {
			return fmt.Errorf("sendtx: no rpc available: %w", err)
		}
		cc.mu.RLock()
		client = cc.client
		cc.mu.RUnlock()
	}

	if err := client.SendTransaction(ctx, tx); err != nil {
		cc.logger.Warn("SendTransaction failed on RPC",
			zap.String("rpc_url", cc.rpcURL),
			zap.Error(err))
		return fmt.Errorf("sendtx failed on %s: %w", cc.rpcURL, err)
	}

	cc.logger.Info("Transaction sent",
		zap.String("tx_hash", tx.Hash().Hex()),
		zap.String("rpc_url", cc.rpcURL))
	return nil
}

// NFTAttribute represents an NFT attribute
type NFTAttribute struct {
	TraitType string      `json:"trait_type"`
	Value     interface{} `json:"value"`
}

// ParseReceiptEvents populates the Events field of a ReceiptInfo by decoding
// the raw receipt logs using the provided EventParser. It fetches the raw
// receipt from the chain to access the full log data.
func (cc *ChainClient) ParseReceiptEvents(ctx context.Context, receipt *ReceiptInfo, parser *EventParser) error {
	hash := common.HexToHash(receipt.TransactionHash)
	rawReceipt, err := withChainClient(ctx, cc, "TransactionReceipt", func(client *ethclient.Client) (*types.Receipt, error) {
		return client.TransactionReceipt(ctx, hash)
	})
	if err != nil {
		return fmt.Errorf("failed to fetch receipt for event parsing: %w", err)
	}
	receipt.Events = parser.ParseLogs(rawReceipt.Logs)
	return nil
}
