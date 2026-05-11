package web3

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// EncryptPrivateKey encrypts a private key into Ethereum keystore V3 JSON format.
// This is the standard format used by geth and other Ethereum clients.
// The scryptN and scryptP parameters control the KDF strength:
//   - Standard: scryptN=1<<18 (262144), scryptP=1
//   - Light:   scryptN=1<<12 (4096), scryptP=6  (for testing)
func EncryptPrivateKey(privateKey *ecdsa.PrivateKey, password string, scryptN, scryptP int) ([]byte, error) {
	enc, err := keystore.EncryptKey(
		&keystore.Key{
			PrivateKey: privateKey,
			Address:    crypto.PubkeyToAddress(privateKey.PublicKey),
		},
		password,
		scryptN,
		scryptP,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt private key: %w", err)
	}
	return enc, nil
}

// DecryptPrivateKey decrypts a private key from Ethereum keystore V3 JSON.
func DecryptPrivateKey(keystoreJSON []byte, password string) (*ecdsa.PrivateKey, error) {
	key, err := keystore.DecryptKey(keystoreJSON, password)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt keystore: %w", err)
	}
	return key.PrivateKey, nil
}

// KeystoreManager manages encrypted keystore operations.
type KeystoreManager struct {
	logger *zap.Logger
}

// NewKeystoreManager creates a new KeystoreManager.
func NewKeystoreManager(logger *zap.Logger) *KeystoreManager {
	return &KeystoreManager{logger: logger}
}

// ImportFromKeystore imports a wallet from an encrypted keystore V3 JSON.
func (km *KeystoreManager) ImportFromKeystore(keystoreJSON []byte, password string) (*ecdsa.PrivateKey, string, error) {
	privateKey, err := DecryptPrivateKey(keystoreJSON, password)
	if err != nil {
		return nil, "", fmt.Errorf("keystore import failed: %w", err)
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	km.logger.Info("Wallet imported from keystore", zap.String("address", address))
	return privateKey, address, nil
}

// ValidateKeystoreJSON checks if the given bytes are valid keystore V3 JSON.
func ValidateKeystoreJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	// Check for required keystore fields
	version, ok := raw["version"]
	if !ok {
		return fmt.Errorf("missing 'version' field in keystore JSON")
	}
	if v, ok := version.(float64); !ok || v != 3 {
		return fmt.Errorf("unsupported keystore version: %v (expected 3)", version)
	}
	return nil
}
