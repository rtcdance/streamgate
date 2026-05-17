package storage

import "time"

// WalletChallenge represents a one-time wallet login challenge.
type WalletChallenge struct {
	ID            string    `json:"id"`
	WalletAddress string    `json:"wallet_address"`
	ChainID       int64     `json:"chain_id"`
	SigningType   string    `json:"signing_type"` // "personal_sign" or "eip712"
	Nonce         string    `json:"nonce"`
	Message       string    `json:"message"`
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	UsedAt        time.Time `json:"used_at,omitempty"`
}
