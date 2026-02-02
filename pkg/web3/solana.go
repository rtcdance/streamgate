package web3

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gagliardetto/solana-go"
	"go.uber.org/zap"
)

// SolanaVerifier handles Solana signature verification
type SolanaVerifier struct {
	logger *zap.Logger
}

// NewSolanaVerifier creates a new Solana verifier
func NewSolanaVerifier(logger *zap.Logger) *SolanaVerifier {
	return &SolanaVerifier{
		logger: logger,
	}
}

// VerifySignature verifies a Solana signature
func (sv *SolanaVerifier) VerifySignature(address, message, signature string) (bool, error) {
	sv.logger.Debug("Verifying Solana signature",
		zap.String("address", address),
		zap.Int("message_length", len(message)))

	// Decode address
	pubKey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return false, fmt.Errorf("invalid Solana address: %w", err)
	}

	// Decode signature
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	if len(sigBytes) != 64 {
		return false, fmt.Errorf("invalid signature length: expected 64, got %d", len(sigBytes))
	}

	// Convert to ed25519 signature
	var sig [64]byte
	copy(sig[:], sigBytes)

	// Decode message
	messageBytes, err := base64.StdEncoding.DecodeString(message)
	if err != nil {
		messageBytes = []byte(message)
	}

	// Verify signature
	verified := ed25519.Verify(pubKey[:], messageBytes, sig[:])

	if !verified {
		sv.logger.Warn("Solana signature verification failed", zap.String("address", address))
		return false, nil
	}

	sv.logger.Debug("Solana signature verified successfully", zap.String("address", address))
	return true, nil
}

// VerifyTransaction verifies a Solana transaction signature
func (sv *SolanaVerifier) VerifyTransaction(address string, transaction []byte, signature string) (bool, error) {
	sv.logger.Debug("Verifying Solana transaction",
		zap.String("address", address),
		zap.Int("tx_length", len(transaction)))

	// Decode address
	pubKey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return false, fmt.Errorf("invalid Solana address: %w", err)
	}

	// Decode signature
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	if len(sigBytes) != 64 {
		return false, fmt.Errorf("invalid signature length: expected 64, got %d", len(sigBytes))
	}

	// Convert to ed25519 signature
	var sig [64]byte
	copy(sig[:], sigBytes)

	// Verify transaction signature
	verified := ed25519.Verify(pubKey[:], transaction, sig[:])

	if !verified {
		sv.logger.Warn("Solana transaction verification failed", zap.String("address", address))
		return false, nil
	}

	sv.logger.Debug("Solana transaction verified successfully", zap.String("address", address))
	return true, nil
}

// SignMessage signs a message with Solana private key (for testing)
func (sv *SolanaVerifier) SignMessage(message string, privateKey ed25519.PrivateKey) (string, error) {
	sv.logger.Debug("Signing Solana message", zap.Int("message_length", len(message)))

	messageBytes := []byte(message)
	signature := ed25519.Sign(privateKey, messageBytes)

	// Return as base64 string
	return base64.StdEncoding.EncodeToString(signature[:]), nil
}

// GetPublicKeyFromPrivateKey gets public key from private key
func (sv *SolanaVerifier) GetPublicKeyFromPrivateKey(privateKey ed25519.PrivateKey) string {
	publicKey := make([]byte, ed25519.PublicKeySize)
	copy(publicKey, privateKey[32:])

	pubKey := solana.PublicKeyFromBytes(publicKey)
	return pubKey.String()
}

// VerifyMessage verifies a signed message
func (sv *SolanaVerifier) VerifyMessage(address, message, signature string) (bool, error) {
	return sv.VerifySignature(address, message, signature)
}

// SignTransaction signs a Solana transaction
func (sv *SolanaVerifier) SignTransaction(transaction []byte, privateKey ed25519.PrivateKey) (string, error) {
	sv.logger.Debug("Signing Solana transaction", zap.Int("tx_length", len(transaction)))

	signature := ed25519.Sign(privateKey, transaction)

	// Return as base64 string
	return base64.StdEncoding.EncodeToString(signature[:]), nil
}

// VerifyMultiSignature verifies multiple signatures (for multisig)
func (sv *SolanaVerifier) VerifyMultiSignature(addresses []string, message string, signatures []string) (bool, error) {
	if len(addresses) != len(signatures) {
		return false, fmt.Errorf("number of addresses (%d) does not match number of signatures (%d)",
			len(addresses), len(signatures))
	}

	for i, address := range addresses {
		verified, err := sv.VerifySignature(address, message, signatures[i])
		if err != nil {
			return false, fmt.Errorf("failed to verify signature %d: %w", i, err)
		}
		if !verified {
			return false, nil
		}
	}

	return true, nil
}

// VerifyOffchainMessage verifies an off-chain message
func (sv *SolanaVerifier) VerifyOffchainMessage(address, message, signature string) (bool, error) {
	// Off-chain messages are typically prefixed
	prefix := "solana offchain message:"
	prefixedMessage := prefix + message

	return sv.VerifySignature(address, prefixedMessage, signature)
}

// SignOffchainMessage signs an off-chain message
func (sv *SolanaVerifier) SignOffchainMessage(message string, privateKey ed25519.PrivateKey) (string, error) {
	prefix := "solana offchain message:"
	prefixedMessage := prefix + message

	return sv.SignMessage(prefixedMessage, privateKey)
}

// MetaplexAttribute represents an NFT attribute
type MetaplexAttribute struct {
	TraitType string `json:"trait_type"`
	Value     string `json:"value"`
}

// MetaplexProperties represents NFT properties
type MetaplexProperties struct {
	Creators []MetaplexCreator `json:"creators"`
	Files    []MetaplexFile    `json:"files"`
}

// MetaplexCreator represents a creator
type MetaplexCreator struct {
	Address  string `json:"address"`
	Share    int    `json:"share"`
	Verified bool   `json:"verified"`
}

// MetaplexFile represents a file
type MetaplexFile struct {
	URI  string `json:"uri"`
	Type string `json:"type"`
}

// VerifyMetaplexMetadata verifies Metaplex NFT metadata signature
func (sv *SolanaVerifier) VerifyMetaplexMetadata(metadataURI, creatorAddress, signature string) (bool, error) {
	sv.logger.Debug("Verifying Metaplex metadata",
		zap.String("creator", creatorAddress),
		zap.String("metadata_uri", metadataURI))

	// In production, you would:
	// 1. Fetch metadata from URI
	// 2. Serialize metadata according to Metaplex spec
	// 3. Verify signature with creator's public key

	// For now, just verify the signature format
	if len(signature) == 0 {
		return false, fmt.Errorf("empty signature")
	}

	_, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, fmt.Errorf("invalid signature format: %w", err)
	}

	// TODO: Implement full Metaplex metadata verification
	sv.logger.Debug("Metaplex metadata format verified (full verification not implemented)")
	return true, nil
}

// VerifyTokenAccount verifies token account ownership
func (sv *SolanaVerifier) VerifyTokenAccount(tokenAccount, ownerAddress string) (bool, error) {
	sv.logger.Debug("Verifying token account",
		zap.String("token_account", tokenAccount),
		zap.String("owner", ownerAddress))

	// In production, you would:
	// 1. Query the token account from Solana RPC
	// 2. Extract the owner field
	// 3. Verify it matches the provided owner address

	// TODO: Implement token account verification
	sv.logger.Debug("Token account verification not fully implemented")
	return true, nil
}

// VerifyMintAuthority verifies mint authority
func (sv *SolanaVerifier) VerifyMintAuthority(mintAddress, authorityAddress string) (bool, error) {
	sv.logger.Debug("Verifying mint authority",
		zap.String("mint", mintAddress),
		zap.String("authority", authorityAddress))

	// In production, you would:
	// 1. Query the mint account from Solana RPC
	// 2. Extract the mint authority field
	// 3. Verify it matches the provided authority address

	// TODO: Implement mint authority verification
	sv.logger.Debug("Mint authority verification not fully implemented")
	return true, nil
}

// ParseSolanaAddress parses and validates a Solana address
func (sv *SolanaVerifier) ParseSolanaAddress(address string) (solana.PublicKey, error) {
	if !strings.HasPrefix(address, "0x") {
		pubKey, err := solana.PublicKeyFromBase58(address)
		if err != nil {
			return solana.PublicKey{}, fmt.Errorf("invalid Solana address: %w", err)
		}
		return pubKey, nil
	}

	// Handle hex format (less common)
	pubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(address, "0x"))
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("invalid hex address: %w", err)
	}

	if len(pubKeyBytes) != 32 {
		return solana.PublicKey{}, fmt.Errorf("invalid address length: expected 32, got %d", len(pubKeyBytes))
	}

	return solana.PublicKeyFromBytes(pubKeyBytes), nil
}

// IsValidAddress checks if an address is a valid Solana address
func (sv *SolanaVerifier) IsValidAddress(address string) bool {
	_, err := sv.ParseSolanaAddress(address)
	return err == nil
}

// DeriveTokenAccountAddress derives a token account address
func (sv *SolanaVerifier) DeriveTokenAccountAddress(walletAddress, mintAddress string) (string, error) {
	sv.logger.Debug("Deriving token account address",
		zap.String("wallet", walletAddress),
		zap.String("mint", mintAddress))

	walletPubKey, err := sv.ParseSolanaAddress(walletAddress)
	if err != nil {
		return "", fmt.Errorf("invalid wallet address: %w", err)
	}

	mintPubKey, err := sv.ParseSolanaAddress(mintAddress)
	if err != nil {
		return "", fmt.Errorf("invalid mint address: %w", err)
	}

	// Derive token account address using Solana program derivation
	// Token program ID: TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA
	tokenProgramID := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")

	// Derive PDA (Program Derived Address)
	seed := [][]byte{
		walletPubKey[:],
		mintPubKey[:],
	}

	pda, _, err := solana.FindProgramAddress(seed, tokenProgramID)
	if err != nil {
		return "", fmt.Errorf("failed to derive token account address: %w", err)
	}

	return pda.String(), nil
}

// VerifyPDASignature verifies a PDA (Program Derived Address) signature
func (sv *SolanaVerifier) VerifyPDASignature(pdaAddress string, seeds []string, programID string) (bool, error) {
	sv.logger.Debug("Verifying PDA signature",
		zap.String("pda", pdaAddress),
		zap.Int("seeds_count", len(seeds)),
		zap.String("program", programID))

	// Convert seeds to byte slices
	seedBytes := make([][]byte, len(seeds))
	for i, seed := range seeds {
		seedBytes[i] = []byte(seed)
	}

	// Parse program ID
	programPubKey, err := solana.PublicKeyFromBase58(programID)
	if err != nil {
		return false, fmt.Errorf("invalid program ID: %w", err)
	}

	// Derive PDA
	expectedPDA, _, err := solana.FindProgramAddress(seedBytes, programPubKey)
	if err != nil {
		return false, fmt.Errorf("failed to derive PDA: %w", err)
	}

	// Compare with provided PDA
	providedPDA, err := solana.PublicKeyFromBase58(pdaAddress)
	if err != nil {
		return false, fmt.Errorf("invalid PDA address: %w", err)
	}

	if expectedPDA != providedPDA {
		sv.logger.Warn("PDA verification failed",
			zap.String("expected", expectedPDA.String()),
			zap.String("provided", providedPDA.String()))
		return false, nil
	}

	sv.logger.Debug("PDA verified successfully", zap.String("pda", pdaAddress))
	return true, nil
}
