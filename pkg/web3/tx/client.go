package tx

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
