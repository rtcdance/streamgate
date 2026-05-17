package web3

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// SignatureVerifier handles signature verification
type SignatureVerifier struct {
	logger  *zap.Logger
	eip1271 *EIP1271Checker // optional: for smart contract wallet verification
}

// NewSignatureVerifier creates a new signature verifier
func NewSignatureVerifier(logger *zap.Logger) *SignatureVerifier {
	return &SignatureVerifier{
		logger: logger,
	}
}

// SetEIP1271Checker sets the EIP-1271 checker for smart contract wallet verification.
// When set, VerifySignature will fall back to EIP-1271 if EOA recovery fails.
func (sv *SignatureVerifier) SetEIP1271Checker(checker *EIP1271Checker) {
	sv.eip1271 = checker
}

// VerifySignature verifies a message signature
func (sv *SignatureVerifier) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	sv.logger.Debug("Verifying signature",
		zap.String("address", address),
		zap.Int("message_length", len(message)))

	// Normalize address
	if !strings.HasPrefix(address, "0x") {
		address = "0x" + address
	}

	// Normalize signature
	if !strings.HasPrefix(signature, "0x") {
		signature = "0x" + signature
	}

	// Decode signature
	sig := common.FromHex(signature)
	if len(sig) != 65 {
		sv.logger.Error("Invalid signature length", zap.Int("length", len(sig)))
		return false, fmt.Errorf("invalid signature length: expected 65, got %d", len(sig))
	}

	// Adjust recovery ID (v) from 27/28 to 0/1
	if sig[64] >= 27 {
		sig[64] -= 27
	}

	// Create message hash
	messageHash := sv.hashMessage(message)

	// Recover public key
	pubKey, err := crypto.SigToPub(messageHash, sig)
	if err != nil {
		sv.logger.Error("Failed to recover public key", zap.Error(err))
		return false, fmt.Errorf("failed to recover public key: %w", err)
	}

	// Get address from public key
	recoveredAddress := crypto.PubkeyToAddress(*pubKey)

	// Compare addresses
	expectedAddress := common.HexToAddress(address)
	if recoveredAddress != expectedAddress {
		// EOA recovery failed — try EIP-1271 smart contract wallet verification
		if sv.eip1271 != nil {
			sv.logger.Debug("EOA recovery mismatch, trying EIP-1271",
				zap.String("expected", expectedAddress.Hex()),
				zap.String("recovered", recoveredAddress.Hex()))

			// Compute the EIP-191 personal_sign hash for EIP-1271 verification
			messageHash := sv.hashMessage(message)
			var hash [32]byte
			copy(hash[:], messageHash)

			sigCopy := make([]byte, len(sig))
			copy(sigCopy, sig)
			if sigCopy[64] < 27 {
				sigCopy[64] += 27
			}
			// EIP-155: if v is neither 27 nor 28, restore by adding 27 to the
			// adjusted value (which was reduced by 27 at line 61).
			if sigCopy[64] != 27 && sigCopy[64] != 28 {
				sigCopy[64] = sig[64] + 27
			}

			valid, err := sv.eip1271.IsValidSignature(ctx, address, hash, sigCopy)
			if err == nil && valid {
				sv.logger.Debug("EIP-1271 signature verified", zap.String("address", address))
				return true, nil
			}
			sv.logger.Debug("EIP-1271 verification failed",
				zap.String("address", address),
				zap.Error(err))
		}

		sv.logger.Warn("Signature verification failed",
			zap.String("expected", expectedAddress.Hex()),
			zap.String("recovered", recoveredAddress.Hex()))
		return false, nil
	}

	sv.logger.Debug("Signature verified successfully", zap.String("address", address))
	return true, nil
}

// hashMessage creates a message hash compatible with eth_sign
func (sv *SignatureVerifier) hashMessage(message string) []byte {
	// Prefix message with Ethereum prefix
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	prefixedMessage := prefix + message

	// Hash the prefixed message
	hash := crypto.Keccak256([]byte(prefixedMessage))
	return hash
}

// SignMessage signs a message (for testing)
func (sv *SignatureVerifier) SignMessage(message string, privateKey *ecdsa.PrivateKey) (string, error) {
	sv.logger.Debug("Signing message", zap.Int("message_length", len(message)))

	// Create message hash
	messageHash := sv.hashMessage(message)

	// Sign the hash
	sig, err := crypto.Sign(messageHash, privateKey)
	if err != nil {
		sv.logger.Error("Failed to sign message", zap.Error(err))
		return "", fmt.Errorf("failed to sign message: %w", err)
	}

	// Adjust recovery ID (v) from 0/1 to 27/28
	if sig[64] < 27 {
		sig[64] += 27
	}

	// Return as hex string
	return "0x" + common.Bytes2Hex(sig), nil
}

// GetAddressFromPrivateKey gets the address from a private key
func (sv *SignatureVerifier) GetAddressFromPrivateKey(privateKey *ecdsa.PrivateKey) string {
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		sv.logger.Error("Failed to cast public key to ECDSA")
		return ""
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)
	return address.Hex()
}
