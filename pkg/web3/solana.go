package web3

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"go.uber.org/zap"
)

// SolanaVerifier handles Solana signature verification
type SolanaVerifier struct {
	logger    *zap.Logger
	rpcURL    string
	rpcClient *rpc.Client
}

// NewSolanaVerifier creates a new Solana verifier
func NewSolanaVerifier(logger *zap.Logger, rpcEndpoint ...string) *SolanaVerifier {
	var rpcURL string
	if len(rpcEndpoint) > 0 {
		rpcURL = rpcEndpoint[0]
	}
	var client *rpc.Client
	if rpcURL != "" {
		client = rpc.New(rpcURL)
	}
	return &SolanaVerifier{
		logger:    logger,
		rpcURL:    rpcURL,
		rpcClient: client,
	}
}

// Close releases resources held by the verifier.
func (sv *SolanaVerifier) Close() {}

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

func (sv *SolanaVerifier) VerifyOffchainMessage(address, message, signature string) (bool, error) {
	encoded := base64.StdEncoding.EncodeToString(encodeSIP004Message(message))
	return sv.VerifySignature(address, encoded, signature)
}

func (sv *SolanaVerifier) SignOffchainMessage(message string, privateKey ed25519.PrivateKey) (string, error) {
	encoded := base64.StdEncoding.EncodeToString(encodeSIP004Message(message))
	return sv.SignMessage(encoded, privateKey)
}

func encodeSIP004Message(message string) []byte {
	msgBytes := []byte(message)
	var buf []byte
	buf = append(buf, 0xff)
	buf = append(buf, []byte("solana offchain")...)
	buf = append(buf, 0x00)
	buf = append(buf, 0x00) // version 0
	buf = append(buf, 0x00) // format 0 (free-form)
	buf = append(buf, encodeVarint(uint64(len(msgBytes)))...)
	buf = append(buf, msgBytes...)
	return buf
}

func encodeVarint(v uint64) []byte {
	var buf []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if v == 0 {
			break
		}
	}
	return buf
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
func (sv *SolanaVerifier) VerifyMetaplexNFTOwnership(ctx context.Context, mintAddress, ownerAddress string) (bool, error) {
	sv.logger.Debug("Verifying Metaplex NFT ownership",
		zap.String("mint", mintAddress),
		zap.String("owner", ownerAddress))

	if sv.rpcClient == nil {
		return false, fmt.Errorf("solana RPC client not initialized")
	}

	verifier := NewMetaplexVerifier(sv.rpcClient, sv.logger, nil)
	return verifier.VerifyNFTOwnership(ctx, mintAddress, ownerAddress)
}

func (sv *SolanaVerifier) FetchMetaplexMetadata(ctx context.Context, mintAddress string) (*MetaplexMetadata, error) {
	sv.logger.Debug("Fetching Metaplex metadata", zap.String("mint", mintAddress))

	if sv.rpcClient == nil {
		return nil, fmt.Errorf("Solana RPC client not initialized")
	}

	verifier := NewMetaplexVerifier(sv.rpcClient, sv.logger, nil)
	return verifier.GetMetadata(ctx, mintAddress)
}

// VerifyTokenAccount verifies token account ownership by querying the Solana RPC.
func (sv *SolanaVerifier) VerifyTokenAccount(ctx context.Context, tokenAccount, ownerAddress string) (bool, error) {
	sv.logger.Debug("Verifying token account",
		zap.String("token_account", tokenAccount),
		zap.String("owner", ownerAddress))

	if sv.rpcURL == "" {
		return false, fmt.Errorf("Solana RPC client not configured")
	}

	if sv.rpcClient == nil {
		return false, fmt.Errorf("Solana RPC client not initialized")
	}

	accountInfo, err := sv.rpcClient.GetAccountInfo(ctx, solana.MustPublicKeyFromBase58(tokenAccount))
	if err != nil {
		return false, fmt.Errorf("failed to get token account info: %w", err)
	}
	if accountInfo == nil || accountInfo.Value == nil {
		return false, nil
	}

	data := accountInfo.Value.Data.GetBinary()
	if len(data) < 64 {
		return false, fmt.Errorf("token account data too short")
	}

	actualOwner := solana.PublicKeyFromBytes(data[32:64])
	expectedOwner, err := solana.PublicKeyFromBase58(ownerAddress)
	if err != nil {
		return false, fmt.Errorf("invalid owner address: %w", err)
	}

	return actualOwner.Equals(expectedOwner), nil
}

// VerifyMintAuthority verifies mint authority by querying the Solana RPC.
func (sv *SolanaVerifier) VerifyMintAuthority(ctx context.Context, mintAddress, authorityAddress string) (bool, error) {
	sv.logger.Debug("Verifying mint authority",
		zap.String("mint", mintAddress),
		zap.String("authority", authorityAddress))

	if sv.rpcURL == "" {
		return false, fmt.Errorf("Solana RPC client not configured")
	}

	if sv.rpcClient == nil {
		return false, fmt.Errorf("Solana RPC client not initialized")
	}

	accountInfo, err := sv.rpcClient.GetAccountInfo(ctx, solana.MustPublicKeyFromBase58(mintAddress))
	if err != nil {
		return false, fmt.Errorf("failed to get mint account info: %w", err)
	}
	if accountInfo == nil || accountInfo.Value == nil {
		return false, nil
	}

	data := accountInfo.Value.Data.GetBinary()
	if len(data) < 36 {
		return false, fmt.Errorf("mint account data too short")
	}

	mintAuthorityOpt := data[0:36]
	if mintAuthorityOpt[0] == 0 {
		return false, nil
	}

	mintAuthority := solana.PublicKeyFromBytes(mintAuthorityOpt[4:36])
	expectedAuthority, err := solana.PublicKeyFromBase58(authorityAddress)
	if err != nil {
		return false, fmt.Errorf("invalid authority address: %w", err)
	}

	return mintAuthority.Equals(expectedAuthority), nil
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
