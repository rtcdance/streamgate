package signature

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"runtime"
	"sync"

	"github.com/ethereum/go-ethereum/crypto"
)

type SecurePrivateKey struct {
	encKey []byte
	xorPad []byte
	mu     sync.Mutex
}

func NewSecurePrivateKey(key *ecdsa.PrivateKey) (*SecurePrivateKey, error) {
	if key == nil {
		return nil, fmt.Errorf("private key is nil")
	}

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

	encKey := make([]byte, len(keyBytes))
	subtle.XORBytes(encKey, keyBytes, xorPad)

	Zeroize(keyBytes)

	return &SecurePrivateKey{
		encKey: encKey,
		xorPad: xorPad,
	}, nil
}

func NewSecurePrivateKeyFromHex(hexKey string) (*SecurePrivateKey, error) {
	key, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key hex: %w", err)
	}
	defer ZeroizeKey(key)
	return NewSecurePrivateKey(key)
}

func (sp *SecurePrivateKey) UseKey(fn func(*ecdsa.PrivateKey) error) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	keyBytes := make([]byte, len(sp.encKey))
	subtle.XORBytes(keyBytes, sp.encKey, sp.xorPad)

	privKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		Zeroize(keyBytes)
		return fmt.Errorf("failed to reconstruct private key: %w", err)
	}

	defer Zeroize(keyBytes)

	return fn(privKey)
}

func Zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(&b)
}

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
