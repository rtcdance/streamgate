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

// ChainClient handles blockchain interactions
type ChainClient struct {
	client  *ethclient.Client
	rpcURL  string
	chainID int64
	logger  *zap.Logger
}

// NewChainClient creates a new chain client
func NewChainClient(rpcURL string, chainID int64, logger *zap.Logger) (*ChainClient, error) {
	logger.Info("Connecting to blockchain",
		zap.String("rpc_url", rpcURL),
		zap.Int64("chain_id", chainID))

	// Connect to RPC
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		logger.Error("Failed to connect to blockchain", zap.Error(err))
		return nil, fmt.Errorf("failed to connect to blockchain: %w", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*1000*1000*1000) // 5 seconds
	defer cancel()

	chainIDFromRPC, err := client.ChainID(ctx)
	if err != nil {
		logger.Error("Failed to get chain ID", zap.Error(err))
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	logger.Info("Connected to blockchain",
		zap.Int64("chain_id", chainIDFromRPC.Int64()))

	return &ChainClient{
		client:  client,
		rpcURL:  rpcURL,
		chainID: chainID,
		logger:  logger,
	}, nil
}

// GetEthClient returns the underlying ethclient.Client
func (cc *ChainClient) GetEthClient() *ethclient.Client {
	return cc.client
}

// GetBalance gets the balance of an address
func (cc *ChainClient) GetBalance(ctx context.Context, address string) (*big.Int, error) {
	cc.logger.Debug("Getting balance", zap.String("address", address))

	addr := common.HexToAddress(address)
	balance, err := cc.client.BalanceAt(ctx, addr, nil)
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
	nonce, err := cc.client.PendingNonceAt(ctx, addr)
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

	gasPrice, err := cc.client.SuggestGasPrice(ctx)
	if err != nil {
		cc.logger.Error("Failed to get gas price", zap.Error(err))
		return nil, fmt.Errorf("failed to get gas price: %w", err)
	}

	cc.logger.Debug("Gas price retrieved", zap.String("gas_price", gasPrice.String()))
	return gasPrice, nil
}

// EstimateGas estimates gas for a transaction
func (cc *ChainClient) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	cc.logger.Debug("Estimating gas")

	gas, err := cc.client.EstimateGas(ctx, msg)
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

	blockNumber, err := cc.client.BlockNumber(ctx)
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

	block, err := cc.client.BlockByNumber(ctx, blockNumber)
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
	tx, isPending, err := cc.client.TransactionByHash(ctx, hash)
	if err != nil {
		cc.logger.Error("Failed to get transaction",
			zap.String("tx_hash", txHash),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

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
	receipt, err := cc.client.TransactionReceipt(ctx, hash)
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
		Logs:            uint64(len(receipt.Logs)),
	}

	cc.logger.Debug("Transaction receipt retrieved", zap.String("tx_hash", txHash))
	return receiptInfo, nil
}

// Close closes the client connection
func (cc *ChainClient) Close() {
	cc.client.Close()
	cc.logger.Info("Chain client closed")
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
	TransactionHash string
	BlockNumber     uint64
	BlockHash       string
	GasUsed         uint64
	Status          uint64
	ContractAddress string
	Logs            uint64
}
