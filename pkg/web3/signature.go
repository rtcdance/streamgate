package web3

import (
	"crypto/ecdsa"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// SignatureVerifier handles signature verification
type SignatureVerifier struct {
	logger *zap.Logger
}

// NewSignatureVerifier creates a new signature verifier
func NewSignatureVerifier(logger *zap.Logger) *SignatureVerifier {
	return &SignatureVerifier{
		logger: logger,
	}
}

// VerifySignature verifies a message signature
func (sv *SignatureVerifier) VerifySignature(address string, message string, signature string) (bool, error) {
	sv.logger.Debug("Verifying signature", "address", address, "message_length", len(message))

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
		sv.logger.Error("Invalid signature length", "length", len(sig))
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
		sv.logger.Error("Failed to recover public key", "error", err)
		return false, fmt.Errorf("failed to recover public key: %w", err)
	}

	// Get address from public key
	recoveredAddress := crypto.PubkeyToAddress(*pubKey)

	// Compare addresses
	expectedAddress := common.HexToAddress(address)
	if recoveredAddress != expectedAddress {
		sv.logger.Warn("Signature verification failed", "expected", expectedAddress.Hex(), "recovered", recoveredAddress.Hex())
		return false, nil
	}

	sv.logger.Debug("Signature verified successfully", "address", address)
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
	sv.logger.Debug("Signing message", "message_length", len(message))

	// Create message hash
	messageHash := sv.hashMessage(message)

	// Sign the hash
	sig, err := crypto.Sign(messageHash, privateKey)
	if err != nil {
		sv.logger.Error("Failed to sign message", "error", err)
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

// Challenge represents a signing challenge
type Challenge struct {
	ID        string
	Address   string
	Message   string
	ExpiresAt int64
}

// ChallengeGenerator generates signing challenges
type ChallengeGenerator struct {
	logger *zap.Logger
}

// NewChallengeGenerator creates a new challenge generator
func NewChallengeGenerator(logger *zap.Logger) *ChallengeGenerator {
	return &ChallengeGenerator{
		logger: logger,
	}
}

// GenerateChallenge generates a new signing challenge
func (cg *ChallengeGenerator) GenerateChallenge(address string) *Challenge {
	cg.logger.Debug("Generating challenge", "address", address)

	// Create challenge message
	message := fmt.Sprintf("Sign this message to verify your wallet ownership.\nAddress: %s\nTimestamp: %d", address, getCurrentTimestamp())

	challenge := &Challenge{
		ID:        generateChallengeID(),
		Address:   address,
		Message:   message,
		ExpiresAt: getCurrentTimestamp() + 3600, // 1 hour expiry
	}

	cg.logger.Debug("Challenge generated", "challenge_id", challenge.ID, "address", address)
	return challenge
}

// Helper functions

func getCurrentTimestamp() int64 {
	// TODO: Use proper timestamp
	return 0
}

func generateChallengeID() string {
	// TODO: Generate unique challenge ID
	return "challenge-" + fmt.Sprintf("%d", getCurrentTimestamp())
}
