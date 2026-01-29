package security

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// KeyMetadata holds metadata about an encryption key
type KeyMetadata struct {
	ID        string    `json:"id"`
	Version   int       `json:"version"`
	Algorithm string    `json:"algorithm"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Rotated   bool      `json:"rotated"`
	Active    bool      `json:"active"`
}

// KeyEntry represents a stored key with metadata
type KeyEntry struct {
	Metadata KeyMetadata `json:"metadata"`
	Key      []byte      `json:"-"` // Never serialize
	KeyHex   string      `json:"key_hex"`
}

// KeyManager manages encryption keys
type KeyManager struct {
	keys             map[string]*KeyEntry
	activeKeyID      string
	mu               sync.RWMutex
	rotationInterval time.Duration
	keySize          int
	lastRotationTime time.Time
}

// NewKeyManager creates a new key manager
func NewKeyManager(rotationInterval time.Duration, keySize int) *KeyManager {
	if rotationInterval == 0 {
		rotationInterval = 24 * time.Hour // Default: rotate daily
	}
	if keySize == 0 {
		keySize = 32 // AES-256
	}

	return &KeyManager{
		keys:             make(map[string]*KeyEntry),
		rotationInterval: rotationInterval,
		keySize:          keySize,
		lastRotationTime: time.Now(),
	}
}

// GenerateKey generates a new encryption key
func (km *KeyManager) GenerateKey() (string, error) {
	key := make([]byte, km.keySize)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}

	keyID := hex.EncodeToString(key[:8]) // Use first 8 bytes as ID
	keyHex := hex.EncodeToString(key)

	entry := &KeyEntry{
		Metadata: KeyMetadata{
			ID:        keyID,
			Version:   1,
			Algorithm: "AES-256",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
			Active:    true,
		},
		Key:    key,
		KeyHex: keyHex,
	}

	km.mu.Lock()
	defer km.mu.Unlock()

	km.keys[keyID] = entry
	if km.activeKeyID == "" {
		km.activeKeyID = keyID
	}

	return keyID, nil
}

// GetKey retrieves a key by ID
func (km *KeyManager) GetKey(keyID string) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	entry, exists := km.keys[keyID]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", keyID)
	}

	return entry.Key, nil
}

// GetActiveKey retrieves the currently active key
func (km *KeyManager) GetActiveKey() (string, []byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.activeKeyID == "" {
		return "", nil, fmt.Errorf("no active key")
	}

	entry, exists := km.keys[km.activeKeyID]
	if !exists {
		return "", nil, fmt.Errorf("active key not found")
	}

	return km.activeKeyID, entry.Key, nil
}

// RotateKey rotates the active key
func (km *KeyManager) RotateKey() (string, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	// Mark old key as rotated
	if km.activeKeyID != "" {
		if entry, exists := km.keys[km.activeKeyID]; exists {
			entry.Metadata.Rotated = true
			entry.Metadata.Active = false
		}
	}

	// Generate new key
	key := make([]byte, km.keySize)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}

	keyID := hex.EncodeToString(key[:8])
	keyHex := hex.EncodeToString(key)

	entry := &KeyEntry{
		Metadata: KeyMetadata{
			ID:        keyID,
			Version:   1,
			Algorithm: "AES-256",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
			Active:    true,
		},
		Key:    key,
		KeyHex: keyHex,
	}

	km.keys[keyID] = entry
	km.activeKeyID = keyID
	km.lastRotationTime = time.Now()

	return keyID, nil
}

// ShouldRotate checks if key rotation is needed
func (km *KeyManager) ShouldRotate() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()

	return time.Since(km.lastRotationTime) > km.rotationInterval
}

// ListKeys lists all keys
func (km *KeyManager) ListKeys() []KeyMetadata {
	km.mu.RLock()
	defer km.mu.RUnlock()

	var keys []KeyMetadata
	for _, entry := range km.keys {
		keys = append(keys, entry.Metadata)
	}
	return keys
}

// GetKeyMetadata retrieves metadata for a key
func (km *KeyManager) GetKeyMetadata(keyID string) (*KeyMetadata, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	entry, exists := km.keys[keyID]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", keyID)
	}

	return &entry.Metadata, nil
}

// RevokeKey revokes a key
func (km *KeyManager) RevokeKey(keyID string) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	entry, exists := km.keys[keyID]
	if !exists {
		return fmt.Errorf("key not found: %s", keyID)
	}

	entry.Metadata.Active = false
	entry.Metadata.ExpiresAt = time.Now()

	return nil
}

// IsKeyActive checks if a key is active
func (km *KeyManager) IsKeyActive(keyID string) bool {
	km.mu.RLock()
	defer km.mu.RUnlock()

	entry, exists := km.keys[keyID]
	if !exists {
		return false
	}

	return entry.Metadata.Active && time.Now().Before(entry.Metadata.ExpiresAt)
}

// GetKeyCount returns the number of keys
func (km *KeyManager) GetKeyCount() int {
	km.mu.RLock()
	defer km.mu.RUnlock()

	return len(km.keys)
}

// GetActiveKeyCount returns the number of active keys
func (km *KeyManager) GetActiveKeyCount() int {
	km.mu.RLock()
	defer km.mu.RUnlock()

	count := 0
	for _, entry := range km.keys {
		if entry.Metadata.Active && time.Now().Before(entry.Metadata.ExpiresAt) {
			count++
		}
	}
	return count
}

// GetRotatedKeyCount returns the number of rotated keys
func (km *KeyManager) GetRotatedKeyCount() int {
	km.mu.RLock()
	defer km.mu.RUnlock()

	count := 0
	for _, entry := range km.keys {
		if entry.Metadata.Rotated {
			count++
		}
	}
	return count
}

// GetLastRotationTime returns the last key rotation time
func (km *KeyManager) GetLastRotationTime() time.Time {
	km.mu.RLock()
	defer km.mu.RUnlock()

	return km.lastRotationTime
}

// GetRotationInterval returns the key rotation interval
func (km *KeyManager) GetRotationInterval() time.Duration {
	km.mu.RLock()
	defer km.mu.RUnlock()

	return km.rotationInterval
}
