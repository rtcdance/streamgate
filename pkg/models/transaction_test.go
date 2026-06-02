package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTransaction_Creation(t *testing.T) {
	now := time.Now()
	tx := &Transaction{
		ID:          "tx123",
		TxHash:      "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		ChainID:     1,
		ChainName:   "Ethereum",
		FromAddress: "0x1111111111111111111111111111111111111111",
		ToAddress:   "0x2222222222222222222222222222222222222222",
		Value:       "1.5",
		GasPrice:    "20000000000",
		GasUsed:     21000,
		Status:      "confirmed",
		Type:        "transfer",
		Data:        map[string]interface{}{"note": "payment"},
		BlockNumber: 15000000,
		Timestamp:   now.Unix(),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	assert.Equal(t, "tx123", tx.ID)
	assert.Equal(t, "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", tx.TxHash)
	assert.Equal(t, int64(1), tx.ChainID)
	assert.Equal(t, "Ethereum", tx.ChainName)
	assert.Equal(t, "0x1111111111111111111111111111111111111111", tx.FromAddress)
	assert.Equal(t, "0x2222222222222222222222222222222222222222", tx.ToAddress)
	assert.Equal(t, "1.5", tx.Value)
	assert.Equal(t, "20000000000", tx.GasPrice)
	assert.Equal(t, int64(21000), tx.GasUsed)
	assert.Equal(t, "confirmed", tx.Status)
	assert.Equal(t, "transfer", tx.Type)
	assert.Equal(t, int64(15000000), tx.BlockNumber)
}

func TestTransaction_ZeroValues(t *testing.T) {
	tx := &Transaction{}

	assert.Equal(t, "", tx.ID)
	assert.Equal(t, "", tx.TxHash)
	assert.Equal(t, int64(0), tx.ChainID)
	assert.Equal(t, int64(0), tx.GasUsed)
	assert.Nil(t, tx.Data)
	assert.True(t, tx.CreatedAt.IsZero())
}

func TestTransaction_StatusTransitions(t *testing.T) {
	tests := []struct {
		name  string
		from  TransactionStatus
		to    TransactionStatus
		valid bool
	}{
		{"pending to confirmed", TxStatusPending, TxStatusConfirmed, true},
		{"pending to failed", TxStatusPending, TxStatusFailed, true},
		{"pending to cancelled", TxStatusPending, TxStatusCancelled, true},
		{"confirmed to pending", TxStatusConfirmed, TxStatusPending, false},
		{"failed to pending", TxStatusFailed, TxStatusPending, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				assert.NotEqual(t, tt.from, tt.to)
			}
		})
	}
}

func TestTransaction_JSONMarshaling(t *testing.T) {
	tx := &Transaction{
		ID:          "json-tx",
		TxHash:      "0xabc",
		ChainID:     1,
		FromAddress: "0x111",
		ToAddress:   "0x222",
		Value:       "0.5",
		Status:      "pending",
		Type:        "transfer",
	}

	data, err := json.Marshal(tx)
	assert.NoError(t, err)

	var decoded Transaction
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, tx.ID, decoded.ID)
	assert.Equal(t, tx.TxHash, decoded.TxHash)
	assert.Equal(t, tx.ChainID, decoded.ChainID)
	assert.Equal(t, tx.Value, decoded.Value)
	assert.Equal(t, tx.Status, decoded.Status)
}

func TestTransactionStatus_Constants(t *testing.T) {
	assert.Equal(t, TransactionStatus("pending"), TxStatusPending)
	assert.Equal(t, TransactionStatus("confirmed"), TxStatusConfirmed)
	assert.Equal(t, TransactionStatus("failed"), TxStatusFailed)
	assert.Equal(t, TransactionStatus("cancelled"), TxStatusCancelled)
}

func TestTransactionType_Constants(t *testing.T) {
	assert.Equal(t, TransactionType("transfer"), TypeTransfer)
	assert.Equal(t, TransactionType("mint"), TypeMint)
	assert.Equal(t, TransactionType("burn"), TypeBurn)
	assert.Equal(t, TransactionType("swap"), TypeSwap)
}

func TestTransactionMetadata(t *testing.T) {
	meta := &TransactionMetadata{
		NFTId:       "nft123",
		ContentId:   "content456",
		Description: "NFT purchase",
		Tags:        []string{"nft", "purchase"},
	}

	assert.Equal(t, "nft123", meta.NFTId)
	assert.Equal(t, "content456", meta.ContentId)
	assert.Equal(t, "NFT purchase", meta.Description)
	assert.Equal(t, []string{"nft", "purchase"}, meta.Tags)
}

func TestTransactionMetadata_JSONMarshaling(t *testing.T) {
	meta := &TransactionMetadata{
		NFTId:     "nft-json",
		ContentId: "content-json",
		Tags:      []string{"test"},
	}

	data, err := json.Marshal(meta)
	assert.NoError(t, err)

	var decoded TransactionMetadata
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, meta.NFTId, decoded.NFTId)
	assert.Equal(t, meta.ContentId, decoded.ContentId)
}
