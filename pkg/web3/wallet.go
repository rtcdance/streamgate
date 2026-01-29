package web3

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// Wallet represents a blockchain wallet
type Wallet struct {
	Address    string
	PrivateKey *ecdsa.PrivateKey
	PublicKey  *ecdsa.PublicKey
}

// WalletManager manages wallet operations
type WalletManager struct {
	logger *zap.Logger
}

// NewWalletManager creates a new wallet manager
func NewWalletManager(logger *zap.Logger) *WalletManager {
	return &WalletManager{
		logger: logger,
	}
}

// CreateWallet creates a new wallet
func (wm *WalletManager) CreateWallet() (*Wallet, error) {
	wm.logger.Debug("Creating new wallet")

	// Generate private key
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		wm.logger.Error("Failed to generate private key", "error", err)
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Get public key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		wm.logger.Error("Failed to cast public key to ECDSA")
		return nil, fmt.Errorf("failed to cast public key to ECDSA")
	}

	// Get address
	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	wallet := &Wallet{
		Address:    address.Hex(),
		PrivateKey: privateKey,
		PublicKey:  publicKeyECDSA,
	}

	wm.logger.Info("Wallet created", "address", wallet.Address)
	return wallet, nil
}

// ImportWallet imports a wallet from private key
func (wm *WalletManager) ImportWallet(privateKeyHex string) (*Wallet, error) {
	wm.logger.Debug("Importing wallet from private key")

	// Parse private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		wm.logger.Error("Failed to parse private key", "error", err)
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Get public key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		wm.logger.Error("Failed to cast public key to ECDSA")
		return nil, fmt.Errorf("failed to cast public key to ECDSA")
	}

	// Get address
	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	wallet := &Wallet{
		Address:    address.Hex(),
		PrivateKey: privateKey,
		PublicKey:  publicKeyECDSA,
	}

	wm.logger.Info("Wallet imported", "address", wallet.Address)
	return wallet, nil
}

// ExportPrivateKey exports the private key as hex string
func (wm *WalletManager) ExportPrivateKey(wallet *Wallet) string {
	return fmt.Sprintf("0x%x", wallet.PrivateKey.D)
}

// ValidateAddress validates an Ethereum address
func (wm *WalletManager) ValidateAddress(address string) bool {
	if len(address) != 42 {
		return false
	}

	if address[:2] != "0x" {
		return false
	}

	// Try to parse as hex
	_, err := crypto.HexToECDSA(address)
	return err == nil
}

// WalletInfo contains wallet information
type WalletInfo struct {
	Address string
	Balance string
	ChainID int64
	Network string
	IsValid bool
}

// GetWalletInfo gets wallet information
func (wm *WalletManager) GetWalletInfo(address string) *WalletInfo {
	wm.logger.Debug("Getting wallet info", "address", address)

	return &WalletInfo{
		Address: address,
		IsValid: wm.ValidateAddress(address),
	}
}
