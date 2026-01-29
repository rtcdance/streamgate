package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

// EncryptionConfig holds encryption configuration
type EncryptionConfig struct {
	Algorithm string // "AES-256-GCM"
	KeySize   int    // 32 for AES-256
	NonceSize int    // 12 for GCM
	Iterations int   // PBKDF2 iterations
}

// Encryptor handles encryption and decryption operations
type Encryptor struct {
	config EncryptionConfig
	mu     sync.RWMutex
}

// EncryptedData represents encrypted data with metadata
type EncryptedData struct {
	Algorithm string `json:"algorithm"`
	Ciphertext string `json:"ciphertext"`
	Nonce     string `json:"nonce"`
	Salt      string `json:"salt"`
	KeyID     string `json:"key_id"`
}

// NewEncryptor creates a new encryptor instance
func NewEncryptor(config EncryptionConfig) *Encryptor {
	if config.KeySize == 0 {
		config.KeySize = 32 // AES-256
	}
	if config.NonceSize == 0 {
		config.NonceSize = 12 // GCM standard
	}
	if config.Iterations == 0 {
		config.Iterations = 100000 // PBKDF2 iterations
	}
	if config.Algorithm == "" {
		config.Algorithm = "AES-256-GCM"
	}

	return &Encryptor{
		config: config,
	}
}

// DeriveKey derives an encryption key from a password using PBKDF2
func (e *Encryptor) DeriveKey(password string, salt []byte) ([]byte, error) {
	if len(salt) == 0 {
		var err error
		salt, err = e.generateSalt()
		if err != nil {
			return nil, fmt.Errorf("failed to generate salt: %w", err)
		}
	}

	key := pbkdf2.Key([]byte(password), salt, e.config.Iterations, e.config.KeySize, sha256.New)
	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM
func (e *Encryptor) Encrypt(plaintext string, key []byte) (*EncryptedData, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(key) != e.config.KeySize {
		return nil, fmt.Errorf("invalid key size: expected %d, got %d", e.config.KeySize, len(key))
	}

	// Create cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, e.config.NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	return &EncryptedData{
		Algorithm:  e.config.Algorithm,
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:      hex.EncodeToString(nonce),
	}, nil
}

// Decrypt decrypts ciphertext using AES-256-GCM
func (e *Encryptor) Decrypt(encrypted *EncryptedData, key []byte) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(key) != e.config.KeySize {
		return "", fmt.Errorf("invalid key size: expected %d, got %d", e.config.KeySize, len(key))
	}

	if encrypted.Algorithm != e.config.Algorithm {
		return "", fmt.Errorf("algorithm mismatch: expected %s, got %s", e.config.Algorithm, encrypted.Algorithm)
	}

	// Decode ciphertext
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	// Decode nonce
	nonce, err := hex.DecodeString(encrypted.Nonce)
	if err != nil {
		return "", fmt.Errorf("failed to decode nonce: %w", err)
	}

	// Create cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// EncryptString encrypts a string with a password
func (e *Encryptor) EncryptString(plaintext, password string) (*EncryptedData, error) {
	// Generate salt
	salt, err := e.generateSalt()
	if err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key
	key, err := e.DeriveKey(password, salt)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	// Encrypt
	encrypted, err := e.Encrypt(plaintext, key)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt: %w", err)
	}

	encrypted.Salt = hex.EncodeToString(salt)
	return encrypted, nil
}

// DecryptString decrypts a string with a password
func (e *Encryptor) DecryptString(encrypted *EncryptedData, password string) (string, error) {
	if encrypted.Salt == "" {
		return "", errors.New("salt not provided")
	}

	// Decode salt
	salt, err := hex.DecodeString(encrypted.Salt)
	if err != nil {
		return "", fmt.Errorf("failed to decode salt: %w", err)
	}

	// Derive key
	key, err := e.DeriveKey(password, salt)
	if err != nil {
		return "", fmt.Errorf("failed to derive key: %w", err)
	}

	// Decrypt
	plaintext, err := e.Decrypt(encrypted, key)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

// generateSalt generates a random salt
func (e *Encryptor) generateSalt() ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	return salt, nil
}

// HashPassword hashes a password using SHA256
func (e *Encryptor) HashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// VerifyPassword verifies a password against a hash
func (e *Encryptor) VerifyPassword(password, hash string) bool {
	return e.HashPassword(password) == hash
}

// GenerateKey generates a random encryption key
func (e *Encryptor) GenerateKey() ([]byte, error) {
	key := make([]byte, e.config.KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}

// GenerateKeyHex generates a random encryption key and returns it as hex string
func (e *Encryptor) GenerateKeyHex() (string, error) {
	key, err := e.GenerateKey()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}
