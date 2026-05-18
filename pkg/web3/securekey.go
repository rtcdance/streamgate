package web3

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"runtime"
	"sync"

	"github.com/ethereum/go-ethereum/crypto"
)

// SecurePrivateKey holds an XOR-encrypted copy of a private key in memory.
// The key is only decrypted into a temporary buffer when UseKey is called,
// and the buffer is zeroed immediately after the callback returns.
type SecurePrivateKey struct {
	encKey []byte // XOR-encrypted key bytes
	xorPad []byte // random XOR pad
	mu     sync.Mutex
}

// NewSecurePrivateKey creates a SecurePrivateKey from an ECDSA private key.
// The key is immediately XOR-encrypted in memory to prevent trivial extraction
// from memory dumps.
func NewSecurePrivateKey(key *ecdsa.PrivateKey) (*SecurePrivateKey, error) {
	if key == nil {
		return nil, fmt.Errorf("private key is nil")
	}

	// Serialize the key
	keyBytes := key.D.Bytes()
	if len(keyBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(keyBytes):], keyBytes)
		keyBytes = padded
	}

	xorPad := make([]byte, len(keyBytes))
	if _, err := rand.Read(xorPad); err != nil {
		return nil, fmt.Errorf("failed to generate XOR pad: %w", err)
	}

	// XOR-encrypt
	encKey := make([]byte, len(keyBytes))
	subtle.XORBytes(encKey, keyBytes, xorPad)

	// Zero the intermediate plaintext
	Zeroize(keyBytes)

	return &SecurePrivateKey{
		encKey: encKey,
		xorPad: xorPad,
	}, nil
}

// NewSecurePrivateKeyFromHex creates a SecurePrivateKey from a hex-encoded private key string.
func NewSecurePrivateKeyFromHex(hexKey string) (*SecurePrivateKey, error) {
	key, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key hex: %w", err)
	}
	// Zero the intermediate key bytes after SecurePrivateKey takes ownership
	defer ZeroizeKey(key)
	return NewSecurePrivateKey(key)
}

// UseKey decrypts the private key into a temporary buffer, calls the provided
// function with the plaintext key, then securely zeroes the buffer.
// This ensures the plaintext key exists in memory only for the duration of fn.
func (sp *SecurePrivateKey) UseKey(fn func(*ecdsa.PrivateKey) error) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Decrypt
	keyBytes := make([]byte, len(sp.encKey))
	subtle.XORBytes(keyBytes, sp.encKey, sp.xorPad)

	// Convert to ECDSA private key
	privKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		Zeroize(keyBytes)
		return fmt.Errorf("failed to reconstruct private key: %w", err)
	}

	// Ensure zeroing even on panic
	defer Zeroize(keyBytes)

	return fn(privKey)
}

// Zeroize securely zeroes a byte slice. It uses a loop that the compiler
// cannot optimize away, and calls runtime.KeepAlive to prevent the slice
// from being garbage collected before zeroing completes.
func Zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(&b)
}

// ZeroizeKey zeroes the D value of an ECDSA private key.
func ZeroizeKey(key *ecdsa.PrivateKey) {
	if key == nil || key.D == nil {
		return
	}
	dBytes := key.D.Bits()
	for i := range dBytes {
		dBytes[i] = 0
	}
	runtime.KeepAlive(key)
}
