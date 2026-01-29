package models

import "time"

// Transaction represents a blockchain transaction
type Transaction struct {
	ID          string                 `json:"id"`
	TxHash      string                 `json:"tx_hash"`
	ChainID     int64                  `json:"chain_id"`
	ChainName   string                 `json:"chain_name"`
	FromAddress string                 `json:"from_address"`
	ToAddress   string                 `json:"to_address"`
	Value       string                 `json:"value"`
	GasPrice    string                 `json:"gas_price"`
	GasUsed     int64                  `json:"gas_used"`
	Status      string                 `json:"status"`
	Type        string                 `json:"type"`
	Data        map[string]interface{} `json:"data"`
	BlockNumber int64                  `json:"block_number"`
	Timestamp   int64                  `json:"timestamp"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// TransactionStatus defines transaction status
type TransactionStatus string

const (
	TxStatusPending   TransactionStatus = "pending"
	TxStatusConfirmed TransactionStatus = "confirmed"
	TxStatusFailed    TransactionStatus = "failed"
	TxStatusCancelled TransactionStatus = "cancelled"
)

// TransactionType defines transaction types
type TransactionType string

const (
	TypeTransfer TransactionType = "transfer"
	TypeMint     TransactionType = "mint"
	TypeBurn     TransactionType = "burn"
	TypeSwap     TransactionType = "swap"
)

// TransactionMetadata represents transaction metadata
type TransactionMetadata struct {
	NFTId       string   `json:"nft_id"`
	ContentId   string   `json:"content_id"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}
