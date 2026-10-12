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
	// ObservedAt is when the evidence behind this entry was gathered — captured
	// at the START of the query, not when the entry was written. It is what the
	// freshness bound is measured against, so re-writing the entry into a lower
	// cache (or issuing a capability from it) cannot make stale evidence look
	// fresh again.
	ObservedAt time.Time
}

type NFTAccessCache interface {
	Get(ctx context.Context, key string) (NFTAccessEntry, bool)
	Set(ctx context.Context, key string, entry NFTAccessEntry)
	Delete(ctx context.Context, key string)
	DeleteByPrefix(ctx context.Context, prefix string)
}
