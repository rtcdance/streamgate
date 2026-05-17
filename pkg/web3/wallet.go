package web3

import (
	"crypto/ecdsa"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// Wallet represents a blockchain wallet
type Wallet struct {
	Address    string
	PrivateKey *ecdsa.PrivateKey
	PublicKey  *ecdsa.PublicKey
}

func (w *Wallet) Destroy() {
	if w.PrivateKey != nil {
		w.PrivateKey.D.SetBytes(make([]byte, 32))
		w.PrivateKey = nil
	}
	if w.PublicKey != nil {
		w.PublicKey = nil
	}
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
		wm.logger.Error("Failed to generate private key", zap.Error(err))
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

	wm.logger.Info("Wallet created", zap.String("address", wallet.Address))
	return wallet, nil
}

// ImportWallet imports a wallet from private key
func (wm *WalletManager) ImportWallet(privateKeyHex string) (*Wallet, error) {
	wm.logger.Debug("Importing wallet from private key")

	// Normalize hex: strip optional 0x prefix and left-pad to 64 chars
	hex := strings.TrimPrefix(privateKeyHex, "0x")
	if len(hex) < 64 {
		hex = strings.Repeat("0", 64-len(hex)) + hex
	}

	privateKey, err := crypto.HexToECDSA(hex)
	if err != nil {
		wm.logger.Error("Failed to parse private key", zap.Error(err))
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

	wm.logger.Info("Wallet imported", zap.String("address", wallet.Address))
	return wallet, nil
}

// ValidateAddress validates an Ethereum address
func (wm *WalletManager) ValidateAddress(address string) bool {
	return common.IsHexAddress(address)
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
	wm.logger.Debug("Getting wallet info", zap.String("address", address))

	return &WalletInfo{
		Address: address,
		IsValid: wm.ValidateAddress(address),
	}
}

// ImportFromMnemonic creates a wallet from a BIP-39 mnemonic phrase.
// It derives the key at the standard Ethereum path m/44'/60'/0'/0/{accountIndex}.
func (wm *WalletManager) ImportFromMnemonic(mnemonic, password string, accountIndex uint32) (*Wallet, error) {
	wm.logger.Debug("Importing wallet from mnemonic")

	hdWallet, err := NewHDWalletFromMnemonic(mnemonic, password, wm.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create HD wallet from mnemonic: %w", err)
	}

	path := DefaultEthereumPath(accountIndex)
	privKeyInt, err := hdWallet.DeriveKey(path)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key at path %s: %w", path, err)
	}

	privKey, err := crypto.ToECDSA(privKeyInt.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to convert derived key: %w", err)
	}

	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("failed to cast public key to ECDSA")
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	wallet := &Wallet{
		Address:    address.Hex(),
		PrivateKey: privKey,
		PublicKey:  publicKeyECDSA,
	}

	wm.logger.Info("Wallet imported from mnemonic", zap.String("address", wallet.Address))
	return wallet, nil
}
