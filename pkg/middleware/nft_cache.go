package middleware

import (
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
	Get(key string) (NFTAccessEntry, bool)
	Set(key string, entry NFTAccessEntry)
}
