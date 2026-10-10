package tx

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Client interface {
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	GetGasPrice(ctx context.Context) (*big.Int, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
	GetBlockNumber(ctx context.Context) (uint64, error)
}

// EthClientAdapter adapts go-ethereum's ethclient.Client to the tx.Client
// interface. ethclient exposes gas pricing as SuggestGasPrice, which maps to
// this package's GetGasPrice (legacy gas-price oracle; EIP-1559 senders fall
// back to it when SuggestGasTipCap fails).
type EthClientAdapter struct {
	Client *ethclient.Client
}

func NewEthClientAdapter(c *ethclient.Client) *EthClientAdapter {
	return &EthClientAdapter{Client: c}
}

func (a *EthClientAdapter) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return a.Client.SendTransaction(ctx, tx)
}

func (a *EthClientAdapter) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	return a.Client.EstimateGas(ctx, msg)
}

func (a *EthClientAdapter) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return a.Client.SuggestGasTipCap(ctx)
}

func (a *EthClientAdapter) GetGasPrice(ctx context.Context) (*big.Int, error) {
	return a.Client.SuggestGasPrice(ctx)
}

func (a *EthClientAdapter) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return a.Client.HeaderByNumber(ctx, number)
}

func (a *EthClientAdapter) TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return a.Client.TransactionReceipt(ctx, hash)
}

func (a *EthClientAdapter) GetBlockNumber(ctx context.Context) (uint64, error) {
	return a.Client.BlockNumber(ctx)
}

// GetNonce adapts ethclient.NonceAt to the tx.NonceClient interface.
func (a *EthClientAdapter) GetNonce(ctx context.Context, address string) (uint64, error) {
	return a.Client.NonceAt(ctx, common.HexToAddress(address), nil)
}
