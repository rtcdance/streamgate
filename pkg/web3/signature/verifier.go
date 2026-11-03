package signature

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/subtle"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// SignatureVerifier handles Ethereum ECDSA signature verification.
//
// ## Signature Standards (EIP-191 / personal_sign)
//
// MetaMask and most wallets use EIP-191 to sign messages:
//
//	keccak256("\x19Ethereum Signed Message:\n" + len(message) + message)
//
// The resulting 65-byte signature [r || s || v] uses v = 27 or 28 (MetaMask
// convention). go-ethereum's crypto.SigToPub expects v = 0 or 1, so the
// conversion at lines 58-60 adjusts accordingly.
//
// ## Security Notes
//
//   - ConstantTimeCompare (line 77) prevents timing side-channel attacks.
//   - Configured contract verification classifies account code before EOA rules.
//   - This does NOT support EIP-712 typed data signing (use VerifyTypedData).
type SignatureVerifier struct {
	logger  *zap.Logger
	eip1271 *EIP1271Checker
}

// NewSignatureVerifier creates a new signature verifier
func NewSignatureVerifier(logger *zap.Logger) *SignatureVerifier {
	return &SignatureVerifier{
		logger: logger,
	}
}

// SetEIP1271Checker sets the EIP-1271 checker for smart contract wallet verification.
// Deprecated: use a server-selected AccountVerifier for challenge-bound chain routing.
func (sv *SignatureVerifier) SetEIP1271Checker(checker *EIP1271Checker) {
	sv.eip1271 = checker
}

// VerifySignature verifies a message signature
func (sv *SignatureVerifier) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	if sv.eip1271 != nil {
		chain, ok := sv.eip1271.caller.(AccountChain)
		if !ok {
			return false, ErrWalletDependency
		}
		ctx, cancel := context.WithTimeout(ctx, WalletVerificationTimeout)
		defer cancel()
		id, err := chain.ChainID(ctx)
		if err != nil || id == nil || !id.IsInt64() {
			return false, ErrWalletDependency
		}
		_, err = NewAccountVerifier(chain, sv.logger).Verify(ctx, id.Int64(), "personal_sign", address, message, signature)
		return err == nil, err
	}
	sv.logger.Debug("Verifying signature",
		zap.String("address", address),
		zap.Int("message_length", len(message)))

	if !strings.HasPrefix(address, "0x") {
		address = "0x" + address
	}

	if !strings.HasPrefix(signature, "0x") {
		signature = "0x" + signature
	}

	sig := common.FromHex(signature)
	if len(sig) != 65 {
		sv.logger.Error("Invalid signature length", zap.Int("length", len(sig)))
		return false, fmt.Errorf("invalid signature length: expected 65, got %d", len(sig))
	}

	// Reject non-canonical signatures: s value must be <= N/2 to prevent
	// malleability attacks where s can be replaced with N-s producing the
	// same address but a different signature encoding.
	sBytes := sig[32:64]
	curveN := crypto.S256().Params().N
	halfN := new(big.Int).Rsh(curveN, 1)
	halfNBytes := halfN.Bytes()
	if len(sBytes) < 32 {
		sBytes = append(make([]byte, 32-len(sBytes)), sBytes...)
	}
	paddedHalfN := make([]byte, 32)
	copy(paddedHalfN[32-len(halfNBytes):], halfNBytes)
	if bytes.Compare(sBytes, paddedHalfN) > 0 {
		sv.logger.Warn("Non-canonical signature: s value exceeds N/2")
		return false, fmt.Errorf("non-canonical signature: s value exceeds N/2 (possible malleability attempt)")
	}

	// Adjust recovery ID (v) from MetaMask convention (27/28) to
	// go-ethereum convention (0/1). go-ethereum's crypto.SigToPub
	// expects v ∈ {0,1} but wallets return v ∈ {27,28} per EIP-155.
	if sig[64] >= 27 {
		sig[64] -= 27
	}

	messageHash := sv.hashMessage(message)

	pubKey, err := crypto.SigToPub(messageHash, sig)
	if err != nil {
		sv.logger.Error("Failed to recover public key", zap.Error(err))
		return false, fmt.Errorf("failed to recover public key: %w", err)
	}

	recoveredAddress := crypto.PubkeyToAddress(*pubKey)

	expectedAddress := common.HexToAddress(address)
	if subtle.ConstantTimeCompare(recoveredAddress.Bytes(), expectedAddress.Bytes()) != 1 {

		sv.logger.Warn("Signature verification failed",
			zap.String("expected", expectedAddress.Hex()),
			zap.String("recovered", recoveredAddress.Hex()))
		return false, nil
	}

	sv.logger.Debug("Signature verified successfully", zap.String("address", address))
	return true, nil
}

// hashMessage creates the EIP-191 personal_sign message hash.
//
// Format: keccak256("\x19Ethereum Signed Message:\n" + len(message) + message)
//
// This prefix prevents users from accidentally signing transactions or other
// structured data when they intend to sign a plain text message. It ensures
// that signatures produced by eth_sign / personal_sign cannot be replayed as
// raw transaction signatures.
func (sv *SignatureVerifier) hashMessage(message string) []byte {
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	prefixedMessage := prefix + message

	hash := crypto.Keccak256([]byte(prefixedMessage))
	return hash
}

// SignMessage signs a message (for testing)
func (sv *SignatureVerifier) SignMessage(message string, privateKey *ecdsa.PrivateKey) (string, error) {
	sv.logger.Debug("Signing message", zap.Int("message_length", len(message)))

	messageHash := sv.hashMessage(message)

	sig, err := crypto.Sign(messageHash, privateKey)
	if err != nil {
		sv.logger.Error("Failed to sign message", zap.Error(err))
		return "", fmt.Errorf("failed to sign message: %w", err)
	}

	if sig[64] < 27 {
		sig[64] += 27
	}

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
