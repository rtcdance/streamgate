package middleware

import (
	"context"
	"math/big"
	"time"
)

type NFTAccessEntry struct {
	HasNFT      bool
	Balance     *big.Int
	BlockNumber uint64
	BlockHash   string
	Expires     time.Time
}

type NFTAccessCache interface {
	Get(ctx context.Context, key string) (NFTAccessEntry, bool)
	Set(ctx context.Context, key string, entry NFTAccessEntry)
	Delete(ctx context.Context, key string)
	DeleteByPrefix(ctx context.Context, prefix string)
}
